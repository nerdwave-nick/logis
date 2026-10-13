package cogitator

import (
	"errors"
	"slices"
	"testing"

	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/window"
)

// The theme the window wears (Rust src/daemon/window/wear.rs, ui.rs
// Command::Theme): worn at once, the visible panel included (re-fitted to
// the theme's size and drawn again), its faces resolved only when they
// change; the frame's time drives its continuous effects, and a window
// keeps drawing only while the theme moves on its own.

// resizable is a fake surface that can also be resized.
type resizable struct {
	*fakeSurface
	resized []scale.Size
}

func (s *resizable) Resize(size scale.Size) {
	s.resized = append(s.resized, size)
	s.size = size
}

type themedRig struct {
	*rig
	surf   *resizable
	faces  [][]text.Face
	facing error
	logged []string
	drawn  []*render.Scene
	clock  uint64
}

func newThemedRig() *themedRig {
	r := &themedRig{rig: newRig()}
	r.surf = &resizable{fakeSurface: r.rig.surf}
	r.ui.surf = r.surf
	r.ui.setFaces = func(f []text.Face) error {
		r.faces = append(r.faces, f)
		return r.facing
	}
	r.ui.logf = func(m string) { r.logged = append(r.logged, m) }
	r.ui.clock = func() uint64 { return r.clock }
	r.ui.draw = func(sc *render.Scene, _ window.Geometry) { r.drawn = append(r.drawn, sc) }
	return r
}

func (r *themedRig) paint() (*render.Scene, bool) {
	animating := r.surf.paint(window.Geometry{Logical: r.surf.size, Scale: scale.One, Physical: r.surf.size})
	return r.drawn[len(r.drawn)-1], animating
}

func loaded(t *testing.T, name string) *theme.Theme {
	t.Helper()
	th, err := theme.Themes{}.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return &th
}

func hasText(sc *render.Scene, words string) bool {
	return slices.ContainsFunc(sc.Texts, func(x render.Text) bool { return x.Text == words })
}

func TestAWornThemeReachesTheVisibleWindowAtOnce(t *testing.T) {
	r := newThemedRig()
	o := list.DefaultOptions()
	r.show(1, o, "alpha", "beta")
	redraws := r.surf.redraws
	phosphor := loaded(t, "cogitator-phosphor")
	r.ui.retheme(phosphor)
	r.saw(t, "theme cogitator-phosphor")
	size, rows := scene.Fit(o, phosphor)
	if len(r.surf.resized) != 1 || r.surf.resized[0] != size || r.ui.shown.view.Look().Lines != rows || r.ui.shown.rows != rows {
		t.Fatalf("resized %v (want %v), %d rows (want %d)", r.surf.resized, size, r.ui.shown.view.Look().Lines, rows)
	}
	if r.surf.redraws <= redraws {
		t.Fatal("no frame asked for")
	}
	if sc, _ := r.paint(); !hasText(sc, "SUMMON>") {
		t.Fatal("the frame is not drawn in the new theme")
	}
}

// A session that asked for its size keeps it in the new theme's terms.
func TestARethemedSessionKeepsTheSizeItAskedFor(t *testing.T) {
	r := newThemedRig()
	o := list.DefaultOptions()
	lines, width := uint32(3), uint32(30)
	o.Lines, o.Width = &lines, &width
	r.show(1, o, "alpha")
	phosphor := loaded(t, "cogitator-phosphor")
	r.ui.retheme(phosphor)
	if want, _ := scene.Fit(o, phosphor); len(r.surf.resized) != 1 || r.surf.resized[0] != want {
		t.Fatalf("resized %v, want %v", r.surf.resized, want)
	}
}

func TestAThemeWornWithoutAWindowSizesTheNextSession(t *testing.T) {
	r := newThemedRig()
	narrow := loaded(t, "cogitator-phosphor")
	narrow.Window.Width = 600
	r.ui.retheme(narrow)
	if len(r.surf.resized) != 0 {
		t.Fatal("resized without a window")
	}
	r.show(1, list.DefaultOptions(), "alpha")
	if want, _ := scene.Fit(list.DefaultOptions(), narrow); r.surf.size != want {
		t.Fatalf("%v, want %v", r.surf.size, want)
	}
}

func TestTheFacesAreResolvedOnlyWhenTheyChange(t *testing.T) {
	r := newThemedRig()
	still := theme.Default().Stilled()
	r.ui.retheme(&still)
	if len(r.faces) != 0 {
		t.Fatalf("the same faces resolved again: %v", r.faces)
	}
	phosphor := loaded(t, "cogitator-phosphor")
	r.ui.retheme(phosphor)
	if len(r.faces) != 1 || !slices.EqualFunc(r.faces[0], phosphor.Faces, func(a, b text.Face) bool {
		return a.Weight == b.Weight && slices.Equal(a.Families, b.Families)
	}) {
		t.Fatalf("%v", r.faces)
	}
	r.facing = errors.New("no fonts at all")
	r.ui.retheme(loaded(t, "forge-crimson-brass"))
	if len(r.logged) != 1 || r.logged[0] != "the theme's fonts fall back: no fonts at all" {
		t.Fatalf("%q", r.logged)
	}
}

func TestARejectedThemeIsToastedInTheThemesWords(t *testing.T) {
	r := newThemedRig()
	r.show(1, list.DefaultOptions(), "a")
	r.ui.toast(daemon.Toast{Kind: daemon.ToastThemeRejected, Error: "/t/mine.jsonc:2:5: why"})
	r.saw(t, `toast "the theme is heretical: /t/mine.jsonc:2:5: why"`)
}

func TestAMovingThemeKeepsTheWindowDrawingAndAStilledOneSettles(t *testing.T) {
	r := newThemedRig()
	r.show(1, list.DefaultOptions(), "a")
	first, animating := r.paint()
	if !animating {
		t.Fatal("the forge's pulse and highlight do not keep the window drawing")
	}
	r.clock = 1_500_000_000
	later, _ := r.paint()
	if first.Shapes[0].Glow.Color.A == later.Shapes[0].Glow.Color.A || later.Post.Time != 1.5 {
		t.Fatalf("the frame's time does not move the effects: glow %v → %v, time %v",
			first.Shapes[0].Glow.Color.A, later.Shapes[0].Glow.Color.A, later.Post.Time)
	}
	still := theme.Default().Stilled()
	r.ui.retheme(&still)
	if _, animating := r.paint(); animating {
		t.Fatal("a stilled theme keeps the window drawing")
	}
}
