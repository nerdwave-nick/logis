package icons

import (
	"strconv"
	"strings"
)

// One icon theme's index.theme (freedesktop Icon Theme Specification): its
// parents and its directories with their size, scale and type, and the
// spec's two rules for choosing among them (Rust src/icons/theme.rs).

// dirKind is how a theme directory's icons may be sized.
type dirKind uint8

const (
	// kindThreshold: Size give or take Threshold (the spec's default).
	kindThreshold dirKind = iota
	// kindFixed: only exactly Size.
	kindFixed
	// kindScalable: anything from MinSize to MaxSize (vector icons).
	kindScalable
)

// themeDir is one subdirectory listed in Directories or ScaledDirectories.
type themeDir struct {
	// Path is relative to the theme directory.
	Path string
	// Size is the nominal size in icon units (logical pixels); Scale the
	// integer scale its pixels are made for.
	Size, Scale uint32
	Kind        dirKind
	// Min and Max bound a scalable directory; Threshold widens a
	// threshold directory.
	Min, Max, Threshold uint32
}

// themeIndex is what index.theme says about a theme.
type themeIndex struct {
	// Inherits are parent theme names, in lookup order.
	Inherits []string
	// Dirs are in the order the theme lists them.
	Dirs []themeDir
}

type keyValue struct{ key, value string }

type group struct {
	name string
	keys []keyValue
}

func (g *group) lookup(key string) (string, bool) {
	for _, kv := range g.keys {
		if kv.key == key {
			return kv.value, true
		}
	}
	return "", false
}

// parseIndex parses an index.theme. Unknown keys, localised keys and
// comments are ignored; a directory without a group or a valid Size is
// skipped, because the spec makes Size mandatory.
func parseIndex(text string) themeIndex {
	groups := parseGroups(text)
	find := func(name string) *group {
		for i := range groups {
			if groups[i].name == name {
				return &groups[i]
			}
		}
		return nil
	}
	header := find("Icon Theme")
	list := func(key string) []string {
		if header == nil {
			return nil
		}
		v, _ := header.lookup(key)
		var out []string
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var index themeIndex
	index.Inherits = list("Inherits")
	for _, name := range append(list("Directories"), list("ScaledDirectories")...) {
		if g := find(name); g != nil {
			if d, ok := parseDir(name, g); ok {
				index.Dirs = append(index.Dirs, d)
			}
		}
	}
	return index
}

// parseGroups is the groups in file order with their key=value pairs.
func parseGroups(text string) []group {
	var out []group
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			out = append(out, group{name: line[1 : len(line)-1]})
		} else if k, v, ok := strings.Cut(line, "="); ok && len(out) > 0 {
			g := &out[len(out)-1]
			g.keys = append(g.keys, keyValue{strings.TrimSpace(k), strings.TrimSpace(v)})
		}
	}
	return out
}

func parseDir(path string, g *group) (themeDir, bool) {
	ok := true
	// or is key's number, def when absent; a bad number skips the dir.
	or := func(key string, def uint32) uint32 {
		v, present := g.lookup(key)
		if !present {
			return def
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			ok = false
		}
		return uint32(n)
	}
	if _, present := g.lookup("Size"); !present {
		return themeDir{}, false
	}
	d := themeDir{Path: path, Size: or("Size", 0)}
	d.Scale = max(or("Scale", 1), 1)
	switch kind, _ := g.lookup("Type"); kind {
	case "Fixed":
		d.Kind = kindFixed
	case "Scalable":
		d.Kind = kindScalable
		d.Min, d.Max = or("MinSize", d.Size), or("MaxSize", d.Size)
	default:
		d.Kind = kindThreshold
		d.Threshold = or("Threshold", 2)
	}
	return d, ok
}

// matches is the spec's DirectoryMatchesSize: an exact fit for size at
// scale.
func (d themeDir) matches(size, scale uint32) bool {
	lo, hi := d.sizes()
	return d.Scale == scale && lo <= size && size <= hi
}

// distance is the spec's DirectorySizeDistance, in pixels: how far the
// directory's pixel sizes are from size × scale. Zero inside its range.
func (d themeDir) distance(size, scale uint32) uint32 {
	wanted := satMul(size, scale)
	lo, hi := d.sizes()
	lo, hi = satMul(lo, d.Scale), satMul(hi, d.Scale)
	switch {
	case wanted < lo:
		return lo - wanted
	case wanted > hi:
		return wanted - hi
	}
	return 0
}

// sizes are the icon sizes this directory serves, inclusive.
func (d themeDir) sizes() (lo, hi uint32) {
	switch d.Kind {
	case kindFixed:
		return d.Size, d.Size
	case kindScalable:
		return d.Min, d.Max
	}
	lo = d.Size - min(d.Size, d.Threshold)
	hi = d.Size + d.Threshold
	if hi < d.Size {
		hi = ^uint32(0)
	}
	return lo, hi
}

func satMul(a, b uint32) uint32 {
	p := uint64(a) * uint64(b)
	return uint32(min(p, uint64(^uint32(0))))
}
