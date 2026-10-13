package theme

import (
	"maps"
	"slices"
	"strconv"

	"github.com/nerdwave-nick/logis/internal/jsonc"
	"github.com/nerdwave-nick/logis/internal/render"
)

// A theme's palette and font roles as written, and how a theme lies over
// the theme it extends and over the bundled default (Rust Merge): a section
// key by key, a part that is on over one that is on key by key, a palette
// name by name, font roles role by role; anything else whole.

// over is n where it says something, base elsewhere.
func (n node) over(base node) node {
	out := maps.Clone(base)
	if out == nil {
		out = node{}
	}
	for k, mine := range n {
		if b, ok := base[k]; ok {
			out[k] = overValue(mine, b)
		} else {
			out[k] = mine
		}
	}
	return out
}

func overValue(mine, base any) any {
	switch m := mine.(type) {
	case node:
		return m.over(base.(node))
	case toggle:
		if b := base.(toggle); !m.off && !b.off && m.on != nil && b.on != nil {
			return toggle{on: m.on.over(b.on)}
		}
	case palette:
		return m.over(base.(palette))
	case map[string]node:
		out := maps.Clone(base.(map[string]node))
		for role, font := range m {
			if b, ok := out[role]; ok {
				font = font.over(b)
			}
			out[role] = font
		}
		return out
	}
	return mine
}

func (r reader) palette(v *jsonc.Value) (palette, error) {
	if v.Kind != jsonc.Object {
		return nil, r.wrongType(v, "a map")
	}
	p := palette{}
	for _, m := range v.Members {
		swatch, err := r.swatch(m.Value)
		if err != nil {
			return nil, err
		}
		p[m.Key] = swatch
	}
	// Rust checked the names once the map was read, in sorted order.
	for _, name := range slices.Sorted(maps.Keys(p)) {
		if !isName(name) {
			return nil, r.doc.Errorf(r.doc.Settled(v, '}'), "invalid value: string %s, expected %s", strconv.Quote(name), nameWants)
		}
	}
	return p, nil
}

func (r reader) roles(v *jsonc.Value, font *section) (map[string]node, error) {
	if v.Kind != jsonc.Object {
		return nil, r.wrongType(v, "a map")
	}
	roles := map[string]node{}
	for _, m := range v.Members {
		role, err := r.section(m.Value, font)
		if err != nil {
			return nil, err
		}
		roles[m.Key] = role
	}
	return roles, nil
}

// swatch reads a palette entry: one colour or a non-empty array of them,
// each "#rrggbb" or "#rrggbbaa".
func (r reader) swatch(v *jsonc.Value) ([]render.Color, error) {
	hex := func(s string) (render.Color, bool) {
		if len(s) == 0 || s[0] != '#' {
			return render.Color{}, false
		}
		return parseHex(s[1:])
	}
	switch v.Kind {
	case jsonc.String:
		c, ok := hex(v.String)
		if !ok {
			return nil, r.wrongValue(v, swatchWants)
		}
		return []render.Color{c}, nil
	case jsonc.Array:
		out := make([]render.Color, 0, len(v.Elems))
		for i, e := range v.Elems {
			if e.Kind != jsonc.String {
				return nil, r.wrongType(e, "a string")
			}
			c, ok := hex(e.String)
			if !ok {
				return nil, r.inSeq(v, i, "invalid value: %s, expected %s", e.Unexpected(), swatchWants)
			}
			out = append(out, c)
		}
		if len(out) == 0 {
			return nil, r.doc.Errorf(v.End, "invalid length 0, expected %s", swatchWants)
		}
		return out, nil
	}
	return nil, r.wrongAny(v, swatchWants)
}
