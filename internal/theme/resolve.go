package theme

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/nerdwave-nick/logis/internal/jsonc"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/text"
)

// From a merged theme document to a Theme (Rust src/theme/resolve.rs):
// colours looked up in the palette, font roles turned into faces, ranges
// checked. Every error names the key it is about ("border.paint[1]: no
// colour named 'embr' …"). The resolver keeps the first error and reads
// in Rust's order, so the error is the one Rust reported.

// regular is the weight a face has when its role names none.
const regular = 400

type resolver struct {
	palette palette
	roles   map[string]node
	faces   []text.Face
	err     error
}

// fail keeps the first error.
func (r *resolver) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf(format, args...)
	}
}

// resolve resolves d (a theme over its bases, the bundled default last).
func resolve(name, dir string, d node) (Theme, error) {
	pal, _ := d["palette"].(palette)
	roles, _ := d["fonts"].(map[string]node)
	r := &resolver{palette: pal, roles: roles}
	// The body's face comes first: it is the text's default.
	r.font("fonts", "body")
	row := need[node](r, d, "row", "row")
	rowFont := r.font("row.font", need[string](r, row, "font", "row.font"))
	matched := need[node](r, d, "match", "match")
	matchFace := r.faceAt(rowFont.Face, need[uint32](r, matched, "weight", "match.weight"))
	t := Theme{Name: name, Dir: dir}
	t.Window = r.window(need[node](r, d, "window", "window"))
	t.Glow = r.glow(need[node](r, d, "glow", "glow"))
	t.Border = r.border(need[node](r, d, "border", "border"))
	t.Panel = r.panel(need[node](r, d, "panel", "panel"))
	t.HasRivets, t.Rivets = r.rivets(need[node](r, d, "decorations", "decorations"))
	t.Prompt = r.prompt(need[node](r, d, "prompt", "prompt"))
	t.Query = r.query(need[node](r, d, "query", "query"))
	t.Counter = r.counter(need[node](r, d, "counter", "counter"))
	t.Rule = r.rule(need[node](r, d, "rule", "rule"))
	t.Row = r.row(row, rowFont)
	t.Selection = r.selection(need[node](r, d, "selection", "selection"))
	t.Match = Match{
		Color:     r.colour(matched, "color", "match.color"),
		Selected:  r.colour(matched, "selected", "match.selected"),
		Face:      matchFace,
		Underline: need[bool](r, matched, "underline", "match.underline"),
	}
	t.Badge = r.badge(need[node](r, d, "badge", "badge"))
	t.Pending = r.pending(need[node](r, d, "pending", "pending"))
	effects := need[node](r, d, "effects", "effects")
	t.Effects = r.effects(effects)
	t.Glitch = r.glitch(effects)
	t.Motion = r.motion(need[node](r, d, "motion", "motion"))
	errorRef, _ := parseColour("error")
	t.Error = r.resolveColour(errorRef, "palette.error")
	t.Faces = r.faces
	return t, r.err
}

// need is n's value at k, which every theme must have by now.
func need[T any](r *resolver, n node, k, key string) T {
	v, ok := n[k].(T)
	if !ok {
		r.fail("%s: missing, and no base theme gives it", key)
	}
	return v
}

// show is a number as Rust's Display shows an f32.
func show(v float32) string { return jsonc.RustFloat(float64(v), 32) }

func (r *resolver) millis(n node, k, key string) time.Duration {
	return time.Duration(need[uint32](r, n, k, key)) * time.Millisecond
}

// length is a length: finite and not negative.
func (r *resolver) length(n node, k, key string) float32 {
	return r.checkLength(need[float32](r, n, k, key), key)
}

func (r *resolver) checkLength(v float32, key string) float32 {
	if math.IsInf(float64(v), 0) || v < 0 {
		r.fail("%s: %s is no length (0 or more)", key, show(v))
	}
	return v
}

// positive is a length greater than 0.
func (r *resolver) positive(n node, k, key string) float32 {
	return r.checkPositive(need[float32](r, n, k, key), key)
}

func (r *resolver) checkPositive(v float32, key string) float32 {
	before := r.err
	v = r.checkLength(v, key)
	if r.err == before && v <= 0 {
		r.fail("%s: must be more than 0", key)
	}
	return v
}

// unit is a strength or opacity: 0 to 1.
func (r *resolver) unit(n node, k, key string) float32 {
	v := need[float32](r, n, k, key)
	if !(v >= 0 && v <= 1) {
		r.fail("%s: %s is out of range (0 to 1)", key, show(v))
	}
	return v
}

func (r *resolver) resolveColour(c colourRef, key string) render.Color {
	colour, err := c.resolve(r.palette)
	if err != nil {
		r.fail("%s: %v", key, err)
	}
	return colour
}

func (r *resolver) colour(n node, k, key string) render.Color {
	if c, ok := n[k].(colourRef); ok {
		return r.resolveColour(c, key)
	}
	r.fail("%s: missing, and no base theme gives it", key)
	return render.Color{}
}

func (r *resolver) paint(n node, k, key string) []render.Color {
	stops, err := need[paint](r, n, k, key).resolve(r.palette)
	if err != nil {
		r.fail("%s%v", key, err)
	}
	return stops
}

func (r *resolver) colours(n node, k, key string) []render.Color {
	refs := need[[]colourRef](r, n, k, key)
	if refs != nil && len(refs) == 0 {
		r.fail("%s: give at least one colour", key)
	}
	out := make([]render.Color, len(refs))
	for i, c := range refs {
		out[i] = r.resolveColour(c, fmt.Sprintf("%s[%d]", key, i))
	}
	return out
}

// intern is the index of the face of families at weight, added if new.
func (r *resolver) intern(families []string, weight uint32) int {
	face := text.Face{Families: families, Weight: int(weight)}
	if i := slices.IndexFunc(r.faces, func(f text.Face) bool {
		return f.Weight == face.Weight && slices.Equal(f.Families, face.Families)
	}); i >= 0 {
		return i
	}
	r.faces = append(r.faces, face)
	return len(r.faces) - 1
}

// font is the font role named at key.
func (r *resolver) font(key, role string) Font {
	if r.err != nil {
		return Font{}
	}
	at := "fonts." + role
	d, ok := r.roles[role]
	if !ok {
		r.fail("%s: no font role '%s' in fonts", key, role)
		return Font{}
	}
	families := need[[]string](r, d, "family", at+".family")
	weight := uint32(regular)
	if w, ok := d["weight"].(uint32); ok {
		weight = w
	}
	r.checkWeight(weight, at+".weight")
	tracking, _ := d["tracking"].(float32)
	if !(tracking >= -0.5 && tracking <= 1) {
		r.fail("%s.tracking: %s is out of range (-0.5 to 1)", at, show(tracking))
	}
	if r.err != nil {
		return Font{}
	}
	face := r.intern(families, weight)
	return Font{Face: face, Size: r.positive(d, "size", at+".size"), Tracking: tracking}
}

// faceAt is the face with face's families at another weight.
func (r *resolver) faceAt(face int, weight uint32) int {
	r.checkWeight(weight, "match.weight")
	if r.err != nil {
		return 0
	}
	return r.intern(r.faces[face].Families, weight)
}

func (r *resolver) role(n node, k, key string) Font {
	role := need[string](r, n, k, key)
	return r.font(key, role)
}

func (r *resolver) checkWeight(weight uint32, key string) {
	if weight < 100 || weight > 900 {
		r.fail("%s: %d is no weight (100 to 900)", key, weight)
	}
}
