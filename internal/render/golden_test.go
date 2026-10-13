package render_test

import (
	"fmt"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Rust tests/golden_harness.rs's real use: a scene touching every shape
// primitive (a gradient body with a gradient border and a glow, a solid
// inset, a translucent bar), rendered at 120×80 logical at scales 1.0, 1.3
// and 2.0 and compared with the checked-in Rust goldens
// tests/goldens/harness/reference@<scale>.png within the offscreen
// tolerance (2 levels per channel, no outliers).
func referenceScene() *render.Scene {
	brass := render.RGBA(0.83, 0.63, 0.09, 1)
	crimson := render.RGBA(0.70, 0.06, 0.05, 1)
	body := render.RoundedRect{
		Rect:   [4]float32{12, 12, 96, 56},
		Radius: 10,
		Fill: render.Linear([2]float32{0, 12}, [2]float32{0, 68},
			render.RGBA(0.10, 0.06, 0.05, 1), render.RGBA(0.04, 0.03, 0.02, 1)),
		Border: render.Border{Width: 2, Paint: render.Linear([2]float32{12, 0}, [2]float32{108, 0}, brass, crimson)},
		Glow:   render.Glow{Radius: 8, Color: crimson.WithAlpha(0.6)},
	}
	inset := render.RoundedRect{Rect: [4]float32{24, 26, 20, 20}, Radius: 4, Fill: render.Solid(brass)}
	bar := render.RoundedRect{Rect: [4]float32{52, 32, 44, 8}, Radius: 4, Fill: render.Solid(render.RGBA(0.54, 0.48, 0.40, 0.5))}
	return &render.Scene{Clear: render.RGBA(0, 0, 0, 1), Shapes: []render.RoundedRect{body, inset, bar}}
}

func TestTheReferenceSceneMatchesTheRustGoldensAtEveryScale(t *testing.T) {
	h := rendertest.Headless(t)
	t.Logf("GL: %s", h.Info().Renderer)
	for _, tc := range []struct {
		n    uint32
		name string
	}{{120, "1.0"}, {156, "1.3"}, {240, "2.0"}} {
		img, err := h.Offscreen(referenceScene(), scale.Size{W: 120, H: 80}, scale.From120ths(tc.n))
		if err != nil {
			t.Fatal(err)
		}
		rendertest.Check(t, img, rendertest.Golden(t, "harness", "reference@"+tc.name), rendertest.Offscreen)
	}
}

func TestOffscreenReturnsTheScaledImage(t *testing.T) {
	img, err := rendertest.Headless(t).Offscreen(referenceScene(), scale.Size{W: 120, H: 80}, scale.From120ths(156))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%dx%d", img.Width, img.Height); got != "156x104" {
		t.Errorf("image %s, want 156x104", got)
	}
}
