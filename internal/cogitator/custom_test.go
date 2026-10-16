package cogitator

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/custom"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The custom pass as the window wears it (a5e5f876): its frames carry the
// shader, the seconds since the panel opened and the frames drawn since,
// and the panel keeps drawing while it is shown; reduce-motion freezes
// its time and lets the panel rest.

func customRig(frozen bool) *movingRig {
	r := newMovingRig()
	th := theme.Default().Stilled() // nothing else moves; the glitch is off
	th.Custom = custom.Shader{Path: "/test/glow.frag", Source: "void mainImage(out vec4 c, in vec2 p) { c = vec4(1.0); }\n", Frozen: frozen}
	r.ui.theme = &th
	return r
}

func TestACustomPassKeepsThePanelDrawingAndCountsItsFrames(t *testing.T) {
	r := customRig(false)
	r.show(1, list.DefaultOptions(), "alpha")
	for i, at := range []uint64{1000, 1250, 1500} {
		sc, d := r.frame(at, motion.Rest)
		if sc.Post.Custom.Path != "/test/glow.frag" || !d.Moving {
			t.Fatalf("frame %d: %+v moving %v", i, sc.Post.Custom, d.Moving)
		}
		if want := float32(at-1000) / 1000; sc.Post.Moment.Time != want || sc.Post.Frame != i {
			t.Errorf("frame %d: time %v frame %d, want %v and %d", i, sc.Post.Moment.Time, sc.Post.Frame, want, i)
		}
	}
	// The next panel counts from its own opening.
	r.show(2, list.DefaultOptions(), "beta")
	r.frame(5000, motion.Rest)
	if sc, _ := r.frame(5500, motion.Rest); sc.Post.Moment.Time != 0.5 || sc.Post.Frame != 1 {
		t.Errorf("the next panel: time %v frame %d", sc.Post.Moment.Time, sc.Post.Frame)
	}
}

func TestReduceMotionLetsAPanelWithACustomPassRest(t *testing.T) {
	r := customRig(true)
	r.show(1, list.DefaultOptions(), "alpha")
	r.frame(1000, motion.Rest)
	if sc, d := r.frame(1250, motion.Rest); !sc.Post.Custom.Frozen || d.Moving {
		t.Errorf("%+v moving %v", sc.Post.Custom, d.Moving)
	}
}
