package theme

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/nerdwave-nick/logis/internal/render"
)

// Colours as a theme writes them (Rust src/theme/colour.rs): "#rrggbb" or
// "#rrggbbaa", "transparent", or a palette name ("brass", or "soot.1" for
// the second of a palette entry's colours), each optionally followed by
// "/alpha" (0 to 1), which multiplies the colour's own alpha:
// "crimson/0.55".

// colourWants is what a colour reference is, in serde's "expected …".
const colourWants = `a colour: "#rrggbb", "#rrggbbaa", "transparent" or a palette name such as ` +
	`"brass" or "soot.1", optionally with "/alpha" (0 to 1) such as "crimson/0.55"`

// paintWants is what a paint is.
var paintWants = fmt.Sprintf("a paint: one colour, or an array of 1 to %d colours (gradient stops); "+
	"a colour is %s", render.MaxStops, colourWants)

// swatchWants is what a palette entry is.
const swatchWants = `a palette colour: "#rrggbb" or "#rrggbbaa", or a non-empty array of them`

// nameWants is what a palette name is.
const nameWants = "a palette name: a lowercase letter, then lowercase letters, digits, '-' or '_'"

// colourRef is a colour reference, checked for its form when read and
// resolved against the palette once the theme is whole.
type colourRef struct {
	literal render.Color
	// name is the palette entry ("" for a literal), index its colour.
	name  string
	index int
	alpha float32
}

// parseColour reads text; false if it is no colour reference.
func parseColour(text string) (colourRef, bool) {
	c := colourRef{alpha: 1}
	base := text
	if b, a, ok := strings.Cut(text, "/"); ok {
		alpha, ok := parseAlpha(a)
		if !ok {
			return c, false
		}
		base, c.alpha = b, alpha
	}
	switch {
	case base == "transparent":
		c.literal = render.Transparent
	case strings.HasPrefix(base, "#"):
		lit, ok := parseHex(base[1:])
		if !ok {
			return c, false
		}
		c.literal = lit
	default:
		name, index, dotted := strings.Cut(base, ".")
		if dotted {
			i, ok := parseIndex(index)
			if !ok {
				return c, false
			}
			c.index = i
		}
		if !isName(name) {
			return c, false
		}
		c.name = name
	}
	return c, true
}

// resolve is the colour, its palette name looked up in p.
func (c colourRef) resolve(p palette) (render.Color, error) {
	colour := c.literal
	if c.name != "" {
		entry, ok := p[c.name]
		if !ok {
			return colour, fmt.Errorf("no colour named '%s' in the palette", c.name)
		}
		if c.index >= len(entry) {
			return colour, fmt.Errorf("'%s' has %d colour(s); '%s.%d' names none", c.name, len(entry), c.name, c.index)
		}
		colour = entry[c.index]
	}
	return colour.WithAlpha(colour.A * c.alpha), nil
}

// isName says name is a palette name: a lowercase letter, then lowercase
// letters, digits, '-' or '_'. The schema's pattern says the same.
func isName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, b := range []byte(name) {
		if (b < 'a' || b > 'z') && (b < '0' || b > '9') && b != '-' && b != '_' {
			return false
		}
	}
	return true
}

// parseIndex reads a palette index as Rust's usize::from_str does: decimal
// digits after an optional '+'.
func parseIndex(text string) (int, bool) {
	digits := strings.TrimPrefix(text, "+")
	if digits == "" || strings.ContainsAny(digits, "+-") {
		return 0, false
	}
	i, err := strconv.ParseUint(digits, 10, 63)
	return int(i), err == nil
}

func parseHex(hex string) (render.Color, bool) {
	if len(hex) != 6 && len(hex) != 8 {
		return render.Color{}, false
	}
	var bytes [4]float32
	bytes[3] = 255
	for i := 0; i < len(hex); i += 2 {
		b, err := strconv.ParseUint(hex[i:i+2], 16, 8)
		if err != nil || strings.ContainsAny(hex[i:i+2], "+-") {
			return render.Color{}, false
		}
		bytes[i/2] = float32(b)
	}
	return render.RGBA(bytes[0]/255, bytes[1]/255, bytes[2]/255, bytes[3]/255), true
}

// parseAlpha reads "/alpha": plain decimal digits, optionally with a
// fraction, 0 to 1.
func parseAlpha(text string) (float32, bool) {
	digits := func(s string) bool {
		return s != "" && strings.Trim(s, "0123456789") == ""
	}
	whole, frac, dotted := strings.Cut(text, ".")
	if !digits(whole) || dotted && !digits(frac) {
		return 0, false
	}
	a, err := strconv.ParseFloat(text, 32)
	if err != nil || a < 0 || a > 1 {
		return 0, false
	}
	return float32(a), true
}

// paint is a paint as a theme writes it: one colour, or 1 to MaxStops
// colours as evenly spaced gradient stops.
type paint []colourRef

// resolve is every stop resolved against p; an error names its stop
// ("[1]: …").
func (pt paint) resolve(p palette) ([]render.Color, error) {
	out := make([]render.Color, len(pt))
	for i, c := range pt {
		colour, err := c.resolve(p)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		out[i] = colour
	}
	return out, nil
}

// palette is a theme's named colours: each entry one colour or several
// (soot.0, soot.1).
type palette map[string][]render.Color

// over is p over base: p's names win, base's others remain.
func (p palette) over(base palette) palette {
	out := maps.Clone(base)
	if out == nil {
		out = palette{}
	}
	maps.Copy(out, p)
	return out
}
