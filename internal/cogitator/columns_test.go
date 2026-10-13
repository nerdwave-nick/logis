package cogitator

import (
	"testing"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/window"
)

// The window measures the columns of the rows it draws (Rust
// daemon/window/wear.rs drawing → ListView::look_measured): as wide as the
// widest text shown since the list opened, never narrower while typing;
// a provider's next stage starts afresh.

// tenPerChar measures ten pixels a character, at the frame's scale it was
// asked for.
func tenPerChar(scales *[]scale.Scale) func(g window.Geometry) scene.Measure {
	return func(g window.Geometry) scene.Measure {
		*scales = append(*scales, g.Scale)
		return scene.MeasureFunc(func(t render.Text) (float32, bool) {
			return 10 * float32(utf8.RuneCountInString(t.Text)), true
		})
	}
}

// columnStart is where the drawn text words starts, less where first
// starts: how far a second column stands from the first.
func columnStart(t *testing.T, sc *render.Scene, first, words string) float32 {
	t.Helper()
	at := map[string]float32{}
	for _, x := range sc.Texts {
		at[x.Text] = x.Origin[0]
	}
	a, ok1 := at[first]
	b, ok2 := at[words]
	if !ok1 || !ok2 {
		t.Fatalf("%q or %q not drawn", first, words)
	}
	return b - a
}

func (r *rig) paintOnce(t *testing.T) *render.Scene {
	t.Helper()
	var drawn *render.Scene
	r.ui.draw = func(sc *render.Scene, _ window.Geometry) { drawn = sc }
	r.surf.paint(window.Geometry{Logical: r.surf.size, Physical: r.surf.size, Scale: scale.From120ths(156)})
	if drawn == nil {
		t.Fatal("nothing drawn")
	}
	return drawn
}

func TestTheWindowMeasuresTheColumnsAtTheFramesScale(t *testing.T) {
	r := newRig()
	var scales []scale.Scale
	r.ui.measure = tenPerChar(&scales)
	r.show(1, list.DefaultOptions(), "abcdefghij\tx", "ab\ty")
	sc := r.paintOnce(t)
	gap := r.ui.theme.Row.ColumnGap
	if got := columnStart(t, sc, "abcdefghij", "x"); got != 100+gap {
		t.Errorf("second column at %v, want %v", got, 100+gap)
	}
	if len(scales) != 1 || scales[0] != scale.From120ths(156) {
		t.Errorf("measured at %v, want the frame's 1.3", scales)
	}
}

func TestColumnsKeepTheirWidthsWhileTheQueryNarrowsTheList(t *testing.T) {
	r := newRig()
	var scales []scale.Scale
	r.ui.measure = tenPerChar(&scales)
	r.show(1, list.DefaultOptions(), "abcdefghij\tx", "ab\ty")
	r.paintOnce(t)
	r.act(input.Insert("y"))
	sc := r.paintOnce(t)
	if got, want := columnStart(t, sc, "ab", "y"), 100+r.ui.theme.Row.ColumnGap; got != want {
		t.Errorf("second column at %v after typing, want %v", got, want)
	}
}

func TestAProvidersNextStageMeasuresItsColumnsAfresh(t *testing.T) {
	r := newRig()
	var scales []scale.Scale
	r.ui.measure = tenPerChar(&scales)
	r.showProvider(1, list.DefaultOptions(), entries("abcdefghij\tx")...)
	r.paintOnce(t)
	r.ui.step(1, provider.Step{Kind: provider.StepStage, Options: list.DefaultOptions(), Entries: entries("abc\ty")})
	sc := r.paintOnce(t)
	if got, want := columnStart(t, sc, "abc", "y"), 30+r.ui.theme.Row.ColumnGap; got != want {
		t.Errorf("second column at %v in the next stage, want %v", got, want)
	}
}
