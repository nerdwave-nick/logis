package cogitator

import (
	"slices"
	"testing"

	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
)

// The pointer and paste in the session logic (Rust src/daemon/window/ui.rs
// react): a point means the row drawn under it on the panel's current
// size; a paste is the window's to read.

// rowCentre is the middle of row in view on the shown panel.
func (r *rig) rowCentre(t *testing.T, row int) (float64, float64) {
	t.Helper()
	g := scene.GridOf(r.surf.size, r.ui.shown.view.Look().Lines, r.ui.theme)
	x, y := float64(g.Left+g.Right)/2, float64(g.Top(row)+g.Pitch/2)
	if got, ok := g.RowAt(x, y); !ok || got != row {
		t.Fatalf("row %d is not under its centre", row)
	}
	return x, y
}

func pointAt(kind input.PointKind, x, y float64) input.Action {
	return input.Action{Kind: input.PointAt, Point: input.Point{Kind: kind, X: x, Y: y}}
}

func TestHoveringSelectsTheRowUnderThePointerAndAClickAcceptsIt(t *testing.T) {
	r := newRig()
	r.show(1, list.Options{Lines: lines(5)}, "alpha", "beta", "gamma")
	r.events = nil
	x, y := r.rowCentre(t, 2)
	redraws := r.surf.redraws
	r.ui.act(pointAt(input.PointMoved, x, y))
	if want := []string{`query ""`, "select 2"}; !slices.Equal(r.events, want) {
		t.Errorf("trace %q, want %q", r.events, want)
	}
	if r.surf.redraws == redraws {
		t.Error("no redraw")
	}
	r.events = nil
	x, y = r.rowCentre(t, 1)
	r.ui.act(pointAt(input.PointPressed, x, y))
	r.ui.act(pointAt(input.PointReleased, x, y))
	if want := []string{`query ""`, "select 1", "accept 1"}; !slices.Equal(r.events, want) {
		t.Errorf("trace %q, want %q", r.events, want)
	}
	want := []answered{{1, daemon.Answer{Kind: daemon.AnswerItem, Index: 1}}}
	if !slices.Equal(r.host.answers, want) || r.surf.shown {
		t.Errorf("answers %+v, shown %v", r.host.answers, r.surf.shown)
	}
}

func TestAPointBeforeThePanelIsSizedDoesNothing(t *testing.T) {
	r := newRig()
	r.show(1, list.Options{Lines: lines(5)}, "alpha", "beta", "gamma")
	x, y := r.rowCentre(t, 2)
	r.surf.unconfigured = true
	r.events = nil
	r.ui.act(pointAt(input.PointMoved, x, y))
	if len(r.events) != 0 {
		t.Errorf("%q", r.events)
	}
}

func TestAPasteIsTheWindowsToReadWhileASessionShows(t *testing.T) {
	r := newRig()
	r.ui.act(input.Action{Kind: input.Paste, Selection: input.Clipboard})
	if len(r.surf.pastes) != 0 {
		t.Error("a paste without a session")
	}
	r.show(1, list.Options{}, "alpha", "beta")
	r.events = nil
	r.ui.act(input.Action{Kind: input.Paste, Selection: input.Primary})
	if !slices.Equal(r.surf.pastes, []input.Selection{input.Primary}) || len(r.events) != 0 {
		t.Errorf("pastes %v, trace %q", r.surf.pastes, r.events)
	}
	// The text comes back as an edit at the cursor.
	r.ui.act(input.Insert("bet"))
	if want := []string{`query "bet"`, "select 1"}; !slices.Equal(r.events, want) {
		t.Errorf("trace %q, want %q", r.events, want)
	}
}

func lines(n uint32) *uint32 { return &n }
