package render_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The glitch pass is the clean room's glitch.frag over logis's frames:
// over real panels of both bundled themes at 1.0, 1.3 and 2.0, with and
// without two plates above the body, at rest, mid-episode, at the ambient
// peak and in a blink of an episode, logis's pass (the shader reading
// GL's bottom-up frames) draws byte for byte what the delivered shader
// draws over the same frame given top-down, as the clean room rendered
// its stills. The only changes are the flip and the hashed grids on whole
// device pixels (ad8913d4): the reference is the delivered shader with
// that edit alone (wholePixelGrids).

var glitchMoments = map[string]effects.Moment{
	"rest": {Time: 1}, "episode": {Time: 1, Event: 1}, "ambient-peak": {Time: 8.55}, "blink": {Time: 5 + 0.5/30, Event: 1},
}

// plates are two plates in the margin above the body, clear of it.
var plates = []render.Region{{Rect: [4]float32{32, 2, 236, 26}, Radius: 9}, {Rect: [4]float32{276, 2, 236, 26}, Radius: 9}}

func TestTheGlitchPassIsTheCleanRoomsOverLogissFrames(t *testing.T) {
	h := rendertest.Headless(t)
	soft := strings.Contains(h.Info().Renderer, "llvmpipe")
	compared := 0
	for _, name := range theme.Bundled() {
		for _, s := range []scale.Scale{scale.One, scale.From120ths(156), scale.FromInteger(2)} {
			sc, logical := panelFrame(t, h, name, s)
			post := sc.Post
			sc.Post = render.Post{}
			frame, err := h.Offscreen(sc, logical, s)
			if err != nil {
				t.Fatal(err)
			}
			for _, strength := range []float32{1, 0.45} {
				for label, m := range glitchMoments {
					for _, regions := range [][]render.Region{nil, plates} {
						g := effects.GlitchDefaults()
						g.Strength = strength
						sc.Post = render.Post{Rect: post.Rect, Radius: post.Radius, Inset: post.Inset,
							Regions: regions, Glitch: g, Moment: m}
						got, err := h.Offscreen(sc, logical, s)
						if err != nil {
							t.Fatal(err)
						}
						want, err := h.CleanRoomGlitchOver(frame, s, sc.Post)
						if err != nil {
							t.Fatal(err)
						}
						compared++
						where := fmt.Sprintf("%s@%s %s strength %v regions %d", name, rendertest.ScaleSuffix(s), label, strength, len(regions))
						if bytes.Equal(got.Pixels, frame.Pixels) {
							t.Errorf("%s: the pass changed nothing", where)
						}
						if bytes.Equal(got.Pixels, want.Pixels) {
							continue
						}
						c, err := rendertest.Compare(got, want, 0)
						if err != nil {
							t.Fatal(err)
						}
						if soft && c.Worst <= 1 {
							t.Logf("%s: %v (llvmpipe)", where, c)
							continue
						}
						t.Errorf("%s: %v", where, c)
					}
				}
			}
		}
	}
	t.Logf("%d frames compared on %s", compared, h.Info().Renderer)
}

// A strength of 0 is no pass at all: the frame is byte for byte the one
// drawn without it, alone or before the CRT pass.
func TestAGlitchOfStrength0IsNoPass(t *testing.T) {
	h := rendertest.Headless(t)
	sc, logical := panelFrame(t, h, "forge-crimson-brass", scale.One)
	crt := sc.Post
	off := effects.GlitchDefaults()
	off.Strength = 0
	for name, base := range map[string]render.Post{"alone": {Rect: crt.Rect, Radius: crt.Radius, Inset: crt.Inset}, "with the CRT": crt} {
		base.Glitch = effects.Glitch{}
		sc.Post = base
		want, err := h.Offscreen(sc, logical, scale.One)
		if err != nil {
			t.Fatal(err)
		}
		with := base
		with.Glitch, with.Moment = off, effects.Moment{Time: 5, Event: 1}
		if name == "alone" && with.Any() {
			t.Error("a glitch of strength 0 asks for a pass")
		}
		sc.Post = with
		got, err := h.Offscreen(sc, logical, scale.One)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Pixels, want.Pixels) {
			t.Errorf("%s: a glitch of strength 0 changed the frame", name)
		}
	}
}
