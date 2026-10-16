package render_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The CRT pass keeps the look of the post pass it replaced (the clean
// room's compatibility method, taken from its harness): with that pass's
// seven strengths and every other control at its default, it draws byte for
// byte what the legacy post shader drew over the same frame. The frames are
// real panels of both bundled themes at 1.0, 1.3 and 2.0, with the two
// themes' legacy strengths, all seven at 1 and each alone at 0.8, at three
// frozen times. Curvature is 0 throughout: above 0 the whole panel bends, a
// deliberate change.

// legacyStrengths are scanlines, grain, flicker, vignette, bloom, curvature
// and aberration.
type legacyStrengths [7]float32

func (v legacyStrengths) crt() effects.CRT {
	c := effects.Defaults()
	c.Scanlines.Strength, c.Grain.Strength, c.Flicker.Strength = v[0], v[1], v[2]
	c.Roundness.Vignette, c.Glow.Strength, c.Roundness.Curvature, c.Aberration.Amount = v[3], v[4], v[5], v[6]
	return c
}

// legacyThemes are the bundled themes' legacy strengths (phosphor's
// curvature 0.12 left out, see above).
var legacyThemes = map[string]legacyStrengths{
	"forge-crimson-brass": {0.16, 0.08, 0.015, 0.42, 0, 0, 0},
	"cogitator-phosphor":  {0.34, 0.1, 0.03, 0.5, 0.45, 0, 0.5},
}

func compatCases(theme string) map[string]legacyStrengths {
	cases := map[string]legacyStrengths{"legacy-" + theme: legacyThemes[theme], "all-at-1": {1, 1, 1, 1, 1, 0, 1}}
	for i, name := range []string{"scanlines", "grain", "flicker", "vignette", "bloom", "", "aberration"} {
		if name != "" {
			var one legacyStrengths
			one[i] = 0.8
			cases["only-"+name] = one
		}
	}
	return cases
}

// panelFrame is a bundled theme's panel over a list, drawn without the
// pass, and the scene that drew it.
func panelFrame(t *testing.T, h *render.Headless, name string, s scale.Scale) (*render.Scene, scale.Size) {
	t.Helper()
	th, err := theme.Themes{}.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	o := list.DefaultOptions()
	lines, width := uint32(5), uint32(48)
	o.Lines, o.Width, o.Counter = &lines, &width, true
	v := list.NewView(o)
	v.Push([]list.Entry{{Text: "rite of oil\tunguent"}, {Text: "rite of ignition\tspark"}, {Text: "café noir\tdrink"},
		{Text: "機械の精霊\tspirit"}, {Text: "litany of the long watch"}, {Text: "the vox of the forge"}})
	v.Apply(input.Insert("ri"))
	v.Apply(input.ActNext)
	logical, rows := scene.Fit(o, &th)
	v.SetLines(rows)
	h.SetFonts(rendertest.PinnedFonts(t))
	if err := h.SetFaces(th.Faces); err != nil {
		t.Fatal(err)
	}
	l := v.Look()
	(&scene.Columns{}).Fit(&l, &th, scene.MeasureFunc(func(tx render.Text) (float32, bool) { return h.MeasureText(tx, s) }))
	return scene.Draw(&l, logical, scene.Style{Theme: &th}), logical
}

func TestTheCRTPassDrawsTheLegacyLookWithTheLegacyStrengths(t *testing.T) {
	h := rendertest.Headless(t)
	// llvmpipe fuses the grain's arithmetic where the legacy shader did not
	// (the clean room measured 1 pixel 1 level apart in 3 of 216 frames);
	// every other driver seen matched to the byte.
	soft := strings.Contains(h.Info().Renderer, "llvmpipe")
	compared := 0
	for _, name := range theme.Bundled() {
		for _, s := range []scale.Scale{scale.One, scale.From120ths(156), scale.FromInteger(2)} {
			sc, logical := panelFrame(t, h, name, s)
			post := sc.Post
			post.Glitch = effects.Glitch{} // the legacy pass had none
			sc.Post = render.Post{}
			frame, err := h.Offscreen(sc, logical, s)
			if err != nil {
				t.Fatal(err)
			}
			for label, v := range compatCases(name) {
				for _, at := range []float32{0, 1.7, 12.345} {
					sc.Post = post
					sc.Post.CRT, sc.Post.Time = v.crt(), at
					got, err := h.Offscreen(sc, logical, s)
					if err != nil {
						t.Fatal(err)
					}
					want, err := h.LegacyPostOver(frame, s, sc.Post)
					if err != nil {
						t.Fatal(err)
					}
					c, err := rendertest.Compare(got, want, 0)
					if err != nil {
						t.Fatal(err)
					}
					compared++
					where := fmt.Sprintf("%s@%s %s t=%v", name, rendertest.ScaleSuffix(s), label, at)
					switch {
					case c.Outliers == 0:
					case soft && c.Worst <= 1 && c.Outliers <= 2:
						t.Logf("%s: %v (llvmpipe's fused grain)", where, c)
					default:
						t.Errorf("%s: %v", where, c)
					}
				}
			}
		}
	}
	t.Logf("%d frames compared on %s", compared, h.Info().Renderer)
}
