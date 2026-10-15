package theme

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/jsonc"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/text"
)

// bundled is the bundled default theme's file, read as the theme engine
// (feat/logis-go G18) will read it: a check that Default transcribes it.
type bundled struct {
	t       *testing.T
	root    *jsonc.Value
	palette map[string][]render.Color
}

func readBundled(t *testing.T) *bundled {
	t.Helper()
	raw, err := os.ReadFile("../../themes/forge-crimson-brass.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonc.Parse(raw, "the theme")
	if err != nil {
		t.Fatal(err)
	}
	b := &bundled{t: t, root: doc.Root, palette: map[string][]render.Color{}}
	for _, m := range b.at("palette").Members {
		if m.Value.Kind == jsonc.Array {
			for _, e := range m.Value.Elems {
				b.palette[m.Key] = append(b.palette[m.Key], hex(t, e.String))
			}
		} else {
			b.palette[m.Key] = []render.Color{hex(t, m.Value.String)}
		}
	}
	return b
}

func hex(t *testing.T, s string) render.Color {
	t.Helper()
	v, err := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if err != nil || len(s) != 7 && len(s) != 9 {
		t.Fatalf("colour %q", s)
	}
	if len(s) == 9 {
		return render.Hex(uint32(v >> 8)).WithAlpha(float32(v&0xff) / 255)
	}
	return render.Hex(uint32(v))
}

// at is the value at a dotted path.
func (b *bundled) at(path string) *jsonc.Value {
	v := b.root
	for _, key := range strings.Split(path, ".") {
		var next *jsonc.Value
		for _, m := range v.Members {
			if m.Key == key {
				next = m.Value
			}
		}
		if next == nil {
			b.t.Fatalf("no %s in the bundled theme", path)
		}
		v = next
	}
	return v
}

// colour resolves a colour reference: #hex, transparent, or a palette
// name (name.i for one of its colours), with an optional /alpha.
func (b *bundled) colour(ref string) render.Color {
	base, alpha, cut := strings.Cut(ref, "/")
	a := float32(1)
	if cut {
		f, err := strconv.ParseFloat(alpha, 32)
		if err != nil {
			b.t.Fatal(err)
		}
		a = float32(f)
	}
	var c render.Color
	switch {
	case base == "transparent":
	case strings.HasPrefix(base, "#"):
		c = hex(b.t, base)
	default:
		name, i := base, 0
		if n, idx, ok := strings.Cut(base, "."); ok {
			name, i = n, int(idx[0]-'0')
		}
		c = b.palette[name][i]
	}
	return c.WithAlpha(c.A * a)
}

func (b *bundled) colours(path string) []render.Color {
	v := b.at(path)
	if v.Kind != jsonc.Array {
		return []render.Color{b.colour(v.String)}
	}
	var out []render.Color
	for _, e := range v.Elems {
		out = append(out, b.colour(e.String))
	}
	return out
}

func (b *bundled) number(path string) float32 {
	f, err := b.at(path).Number.Float64()
	if err != nil {
		b.t.Fatal(err)
	}
	return float32(f)
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-6 }

func sameColours(a, b []render.Color) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !near(a[i].R, b[i].R) || !near(a[i].G, b[i].G) || !near(a[i].B, b[i].B) || !near(a[i].A, b[i].A) {
			return false
		}
	}
	return true
}

func TestTheDefaultIsTheBundledForgeTheme(t *testing.T) {
	b := readBundled(t)
	d := Default()
	colours := map[string][]render.Color{
		"glow.color": {d.Glow.Color}, "border.paint": d.Border.Paint,
		"border.highlight.color": {d.Border.Highlight.Color}, "panel.fill": d.Panel.Fill,
		"decorations.rivets.top": d.Rivets.Top, "decorations.rivets.bottom": d.Rivets.Bottom,
		"prompt.color": {d.Prompt.Color}, "prompt.sigil.color": {d.Prompt.Sigil.Color},
		"query.color": {d.Query.Color}, "query.placeholder": {d.Query.Placeholder},
		"query.caret.color": {d.Query.Caret.Color}, "query.caret.glow": {d.Query.Caret.Glow},
		"counter.color": {d.Counter.Color}, "rule.paint": d.Rule.Paint,
		"rule.ornament.fill": {d.Rule.Ornament.Fill}, "rule.ornament.edge": {d.Rule.Ornament.Edge},
		"row.columns": d.Row.Columns, "row.glyph": {d.Row.Glyph},
		"selection.band": d.Selection.Band, "selection.outline": {d.Selection.Outline},
		"selection.edge": {d.Selection.Edge}, "selection.glow": {d.Selection.Glow},
		"selection.columns": d.Selection.Columns, "match.color": {d.Match.Color},
		"match.selected": {d.Match.Selected}, "badge.ink": {d.Badge.Ink}, "badge.edge": {d.Badge.Edge},
		"badge.fill": {d.Badge.Fill}, "pending.color": {d.Pending.Color}, "palette.error": {d.Error},
	}
	for path, got := range colours {
		if want := b.colours(path); !sameColours(got, want) {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
	}
	lengths := map[string]float32{
		"window.width": d.Window.Width, "window.rows": float32(d.Window.Rows),
		"window.margin": d.Window.Margin, "glow.radius": d.Glow.Radius, "glow.pulse": d.Glow.Pulse,
		"border.width": d.Border.Width, "border.highlight.length": d.Border.Highlight.Length,
		"panel.radius": d.Panel.Radius, "panel.padding": d.Panel.Padding,
		"decorations.rivets.size": d.Rivets.Size, "decorations.rivets.inset": d.Rivets.Inset,
		"prompt.height": d.Prompt.Height, "prompt.gap": d.Prompt.Gap, "prompt.sigil.size": d.Prompt.Sigil.Size,
		"query.caret.width": d.Query.Caret.Width, "rule.thickness": d.Rule.Thickness, "rule.inset": d.Rule.Inset,
		"rule.ornament.size": d.Rule.Ornament.Size, "row.height": d.Row.Height, "row.gap": d.Row.Gap,
		"row.padding": d.Row.Padding, "row.column-gap": d.Row.ColumnGap, "row.column-max": d.Row.ColumnMax,
		"selection.inset": d.Selection.Inset, "pending.opacity": d.Pending.Opacity, "pending.shimmer": d.Pending.Shimmer,
		"effects.scanlines": d.Effects.Scanlines, "effects.grain": d.Effects.Grain,
		"effects.flicker": d.Effects.Flicker, "effects.vignette": d.Effects.Vignette,
		"fonts.body.size": d.Row.Font.Size, "fonts.query.size": d.Query.Font.Size,
		"fonts.label.size": d.Prompt.Font.Size, "fonts.label.tracking": d.Prompt.Font.Tracking,
		"fonts.small.size": d.Counter.Font.Size, "fonts.small.tracking": d.Counter.Font.Tracking,
		"fonts.badge.size": d.Badge.Font.Size, "fonts.badge.tracking": d.Badge.Font.Tracking,
	}
	for path, got := range lengths {
		if want := b.number(path); !near(got, want) {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
	}
	durations := map[string]time.Duration{
		"glow.period-ms": d.Glow.Period, "border.highlight.period-ms": d.Border.Highlight.Period,
		"pending.period-ms": d.Pending.Period, "motion.open-ms": d.Motion.Open,
		"motion.close-ms": d.Motion.Close, "motion.select-ms": d.Motion.Select,
	}
	for path, got := range durations {
		if want := time.Duration(b.number(path)) * time.Millisecond; got != want {
			t.Errorf("%s: %v, want %v", path, got, want)
		}
	}
	texts := map[string]string{
		"prompt.label": d.Prompt.Label, "prompt.sigil.image": d.Prompt.Sigil.Image,
		"counter.format": d.Counter.Format, "pending.mark": d.Pending.Mark,
	}
	for path, got := range texts {
		if want := b.at(path).String; got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	if d.Name != "forge-crimson-brass" || d.Match.Underline || !d.Badge.Uppercase || d.Badge.Style != BadgeOutline ||
		d.Query.Caret.Shape != CaretBar || d.Rule.Dashed || !d.Panel.Blur {
		t.Errorf("%+v", d)
	}
}

// Faces are interned as Rust's resolver does: the body's first, the row's
// at the match weight, then the roles in the order the parts name them.
func TestTheDefaultFacesAreTheBodysTheMatchsAndTheLabels(t *testing.T) {
	d := Default()
	plex := []string{"IBM Plex Mono", "monospace"}
	want := []text.Face{{Families: plex, Weight: 400}, {Families: plex, Weight: 600}, {Families: []string{"Cinzel", "serif"}, Weight: 600}}
	if fmt.Sprint(d.Faces) != fmt.Sprint(want) {
		t.Fatalf("%v", d.Faces)
	}
	for name, f := range map[string]Font{"row": d.Row.Font, "query": d.Query.Font, "counter": d.Counter.Font, "badge": d.Badge.Font} {
		if f.Face != 0 {
			t.Errorf("%s face %d", name, f.Face)
		}
	}
	if d.Match.Face != 1 || d.Prompt.Font.Face != 2 {
		t.Errorf("match %d, label %d", d.Match.Face, d.Prompt.Font.Face)
	}
}

// Ported from Rust src/theme/model.rs's is_continuous/shimmers/stilled.
func TestTheForgeMovesUntilStilled(t *testing.T) {
	d := Default()
	if !d.IsContinuous() || !d.Shimmers() || !d.Effects.Any() {
		t.Fatal("the forge stands still")
	}
	s := d.Stilled()
	if s.IsContinuous() || s.Shimmers() || s.Glow.Pulse != 0 || s.Border.Highlight.Period != 0 || s.Effects.Flicker != 0 {
		t.Fatalf("%+v", s)
	}
	if !s.Border.HasHighlight || !s.Effects.Any() {
		t.Fatal("stilling removed the highlight or the effects")
	}
	if !Default().IsContinuous() {
		t.Fatal("Stilled changed the default")
	}
}
