package cogitator

import (
	"reflect"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/window"
)

// Motion and blur as the window wears them: the theme's timings unless
// LOGIS_MOTION outranks them, blur where both the config and the theme
// want it, and each frame drawn in its pose with the selection band
// gliding to the selected row and the body blurred behind.

// movingSurface is a fake surface that draws frames and takes motion and
// blur.
type movingSurface struct {
	*fakeSurface
	frames  window.FramePainter
	motions []motion.Motion
	blurs   []bool
}

func (s *movingSurface) ShowFrames(size scale.Size, paint window.FramePainter) error {
	s.frames = paint
	return s.Show(size, func(g window.Geometry) bool { return paint(window.Frame{Geometry: g, Pose: motion.Rest}).Moving })
}
func (s *movingSurface) SetMotion(m motion.Motion) { s.motions = append(s.motions, m) }
func (s *movingSurface) SetBlur(on bool)           { s.blurs = append(s.blurs, on) }

type movingRig struct {
	*rig
	surf  *movingSurface
	drawn []*render.Scene
}

// newMovingRig wears a stilled default theme: nothing moves but motion.
func newMovingRig() *movingRig {
	r := &movingRig{rig: newRig()}
	r.surf = &movingSurface{fakeSurface: r.rig.surf}
	r.ui.surf = r.surf
	still := theme.Default().Stilled()
	r.ui.theme = &still
	r.ui.draw = func(sc *render.Scene, _ window.Geometry) { r.drawn = append(r.drawn, sc) }
	return r
}

const msNS = uint64(time.Millisecond)

// frame draws a frame at at (ms) with a 10 ms interval in pose.
func (r *movingRig) frame(at uint64, pose motion.Pose) (*render.Scene, window.Drawing) {
	f := window.Frame{
		Geometry:  window.Geometry{Logical: r.surf.size, Physical: r.surf.size, Scale: scale.One},
		FrameTime: window.FrameTime{At: at * msNS, Interval: 10 * msNS},
		Pose:      pose,
	}
	d := r.surf.frames(f)
	return r.drawn[len(r.drawn)-1], d
}

// still is the scene of the shown list with the band at band, in pose.
func (r *movingRig) still(at uint64, band float32, pose motion.Pose) *render.Scene {
	look := r.ui.look()
	sc := scene.DrawAt(&look, r.surf.size, band, true, scene.Style{Theme: r.ui.theme, At: at * msNS}, nil)
	pose.Apply(sc)
	return sc
}

func (r *movingRig) bandAt(row int) float32 {
	return scene.GridOf(r.surf.size, r.ui.shown.rows, r.ui.theme).Top(row)
}

func TestTheWindowMovesInTheThemesTimingsUnlessTheEnvironmentSaysOtherwise(t *testing.T) {
	r := newMovingRig()
	r.ui.wearMotion()
	slow := theme.Default()
	slow.Motion = theme.Motion{Open: 200 * time.Millisecond, Close: 0, Select: 60 * time.Millisecond}
	r.ui.retheme(&slow)
	want := []motion.Motion{motion.Timed(150*time.Millisecond, 110*time.Millisecond, 90*time.Millisecond),
		motion.Timed(200*time.Millisecond, 0, 60*time.Millisecond)}
	if !reflect.DeepEqual(r.surf.motions, want) {
		t.Fatalf("%+v", r.surf.motions)
	}
	off := motion.Off
	r.ui.envMotion = &off
	r.ui.retheme(&slow)
	if got := r.surf.motions[len(r.surf.motions)-1]; got != motion.Off {
		t.Errorf("LOGIS_MOTION=off did not outrank the theme: %+v", got)
	}
}

func TestBlurIsWhereTheConfigAndTheThemeBothWantIt(t *testing.T) {
	r := newMovingRig()
	on, off := config.Default(), config.Default()
	off.Blur = false
	r.ui.configure(on)
	r.ui.configure(off)
	clear := theme.Default()
	clear.Panel.Blur = false
	r.ui.configure(on)
	r.ui.retheme(&clear)
	blurred := theme.Default()
	r.ui.retheme(&blurred)
	if want := []bool{true, false, true, false, true}; !reflect.DeepEqual(r.surf.blurs, want) {
		t.Errorf("%v, want %v", r.surf.blurs, want)
	}
}

func TestTheSelectionBandGlidesToANewRowOverFramesThenRests(t *testing.T) {
	r := newMovingRig()
	r.ui.wearMotion()
	r.show(1, list.DefaultOptions(), "alpha", "beta", "gamma")
	sc, d := r.frame(1000, motion.Rest)
	if !reflect.DeepEqual(sc, r.still(1000, r.bandAt(0), motion.Rest)) || d.Moving {
		t.Fatalf("the first band is not taken at once on the first row (moving %v)", d.Moving)
	}
	r.act(input.ActNext)
	sc, d = r.frame(1040, motion.Rest)
	if !d.Moving || reflect.DeepEqual(sc, r.still(1040, r.bandAt(0), motion.Rest)) ||
		reflect.DeepEqual(sc, r.still(1040, r.bandAt(1), motion.Rest)) {
		t.Fatalf("the band jumped (moving %v)", d.Moving)
	}
	sc, d = r.frame(1200, motion.Rest)
	if d.Moving || !reflect.DeepEqual(sc, r.still(1200, r.bandAt(1), motion.Rest)) {
		t.Fatalf("the band did not come to rest on the second row (moving %v)", d.Moving)
	}
}

func TestANewStageTakesItsBandAtOnce(t *testing.T) {
	r := newMovingRig()
	r.ui.wearMotion()
	r.showProvider(1, list.DefaultOptions(), list.Entry{Text: "a"}, list.Entry{Text: "b"})
	r.frame(1000, motion.Rest)
	r.act(input.ActNext)
	r.frame(1010, motion.Rest)
	r.ui.step(1, provider.Step{Kind: provider.StepStage, Options: list.DefaultOptions(), Entries: entries("x", "y")})
	sc, d := r.frame(1020, motion.Rest)
	if d.Moving || !reflect.DeepEqual(sc, r.still(1020, r.bandAt(0), motion.Rest)) {
		t.Fatalf("the new list's band glided (moving %v)", d.Moving)
	}
}

func TestAFrameIsDrawnInItsPoseWithTheBodyBlurredBehind(t *testing.T) {
	r := newMovingRig()
	r.show(1, list.DefaultOptions(), "alpha")
	pose := motion.Pose{Opacity: 0.4, Offset: [2]float32{0, 10}}
	sc, d := r.frame(1000, pose)
	if !reflect.DeepEqual(sc, r.still(1000, r.bandAt(0), pose)) {
		t.Error("the frame is not the list in its pose")
	}
	body, radius := scene.Body(r.surf.size, r.ui.theme)
	if d.Backdrop == nil || *d.Backdrop != (window.Backdrop{Rect: body, Radius: radius}) {
		t.Errorf("backdrop %+v, want the body at rest %v r%v", d.Backdrop, body, radius)
	}
}

func TestAMovingThemeKeepsFramesComingWithoutAGlide(t *testing.T) {
	r := newMovingRig()
	r.ui.theme = loadedTheme(t, "forge-crimson-brass")
	r.show(1, list.DefaultOptions(), "alpha")
	if _, d := r.frame(1000, motion.Rest); !d.Moving {
		t.Error("the forge's pulse does not keep the window drawing")
	}
}

func loadedTheme(t *testing.T, name string) *theme.Theme {
	t.Helper()
	th, err := theme.Themes{}.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return &th
}
