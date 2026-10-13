package icons

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// The Icon Theme Specification's lookup (Rust src/icons/lookup.rs): the
// configured theme and its parents (depth first, each once), then hicolor,
// then the pixmaps directories. Only PNG and SVG are looked for, because
// only those are rasterised; XPM files are never returned.

// Hicolor is the fallback theme every chain ends with.
const Hicolor = "hicolor"

// extensions in the spec's order of preference, minus XPM.
var extensions = [...]string{"png", "svg"}

// Themes is a theme chain loaded once: each theme's directories that exist
// on disk, in lookup order. Lookups only test which files exist.
type Themes struct {
	chain      []theme
	pixmapDirs []string
}

type theme struct {
	name string
	dirs []themeCopies
}

// themeCopies is one listed directory with its existing copies, one per
// base dir.
type themeCopies struct {
	dir    themeDir
	copies []string
}

// LoadThemes loads name (the configured theme), its Inherits chain and
// hicolor from paths. Missing or invalid theme names are skipped, so a
// misconfigured theme still finds hicolor and pixmaps icons.
func LoadThemes(paths SearchPaths, name string) *Themes {
	t := &Themes{pixmapDirs: paths.PixmapDirs}
	seen := map[string]bool{}
	t.load(paths, name, seen)
	t.load(paths, Hicolor, seen)
	return t
}

// Names are the loaded themes in lookup order.
func (t *Themes) Names() []string {
	names := make([]string, len(t.chain))
	for i, th := range t.chain {
		names[i] = th.name
	}
	return names
}

// Find is the file for icon name at size logical pixels on an output of s.
// Theme directories carry integer scales, so a fractional scale looks for
// the next integer one (1.3 → 2) and the caller rasterises the file at the
// exact pixel size. An absolute path names the file itself, as desktop
// entries may; a trailing .png/.svg/.xpm on a name is ignored.
func (t *Themes) Find(name string, size uint32, s scale.Scale) (string, bool) {
	if filepath.IsAbs(name) {
		if supported(name) && isFile(name) {
			return name, true
		}
		return "", false
	}
	name = stripExtension(name)
	if name == "" || strings.HasPrefix(name, ".") || strings.Contains(name, "/") {
		return "", false
	}
	n := max((s.As120ths()+119)/120, 1)
	for _, th := range t.chain {
		if path, ok := th.find(name, size, n); ok {
			return path, true
		}
	}
	return firstFile(t.pixmapDirs, name)
}

// find is the spec's LookupIcon: an exactly matching directory first, then
// the directory whose sizes are closest (the first of equals).
func (th *theme) find(name string, size, n uint32) (string, bool) {
	for _, d := range th.dirs {
		if d.dir.matches(size, n) {
			if path, ok := firstFile(d.copies, name); ok {
				return path, true
			}
		}
	}
	best, found := "", false
	var bestDistance uint32
	for _, d := range th.dirs {
		path, ok := firstFile(d.copies, name)
		if !ok {
			continue
		}
		if distance := d.dir.distance(size, n); !found || distance < bestDistance {
			best, bestDistance, found = path, distance, true
		}
	}
	return best, found
}

func (t *Themes) load(paths SearchPaths, name string, seen map[string]bool) {
	if !validThemeName(name) || seen[name] {
		return
	}
	seen[name] = true
	var text []byte
	for _, base := range paths.IconDirs {
		if b, err := os.ReadFile(filepath.Join(base, name, "index.theme")); err == nil {
			text = b
			break
		}
	}
	if text == nil {
		return
	}
	index := parseIndex(string(text))
	th := theme{name: name}
	for _, d := range index.Dirs {
		var copies []string
		for _, base := range paths.IconDirs {
			p := filepath.Join(base, name, d.Path)
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				copies = append(copies, p)
			}
		}
		if len(copies) > 0 {
			th.dirs = append(th.dirs, themeCopies{d, copies})
		}
	}
	t.chain = append(t.chain, th)
	for _, parent := range index.Inherits {
		t.load(paths, parent, seen)
	}
}

// validThemeName: a theme name is one directory name, never a path.
func validThemeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

// firstFile is the first <dir>/<name>.<ext> that is a file, extensions in
// order.
func firstFile(dirs []string, name string) (string, bool) {
	for _, dir := range dirs {
		for _, ext := range extensions {
			if p := filepath.Join(dir, name+"."+ext); isFile(p) {
				return p, true
			}
		}
	}
	return "", false
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

func supported(path string) bool {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	for _, e := range extensions {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

func stripExtension(name string) string {
	for _, ext := range [...]string{".png", ".svg", ".xpm"} {
		if s, ok := strings.CutSuffix(name, ext); ok {
			return s
		}
	}
	return name
}
