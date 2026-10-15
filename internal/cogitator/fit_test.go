package cogitator

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/window"
)

// The panel fits its rows into the output it shows on (scene.FitIn): the
// window says how tall that output is before a panel opens (the one the
// last panel showed on), and again once the compositor puts the panel on
// an output, which re-fits a visible panel. While a panel is open its size
// stays, whatever the query leaves of the list.

// roomy is a resizable fake surface that knows its output's height.
type roomy struct {
	*resizable
	room uint32
}

func (s *roomy) Room() uint32 { return s.room }

func newRoomyRig(room uint32) (*themedRig, *roomy) {
	r := newThemedRig()
	s := &roomy{resizable: r.surf, room: room}
	r.ui.surf = s
	return r, s
}

func TestAPanelOpensFittedToItsOutput(t *testing.T) {
	r, _ := newRoomyRig(400)
	r.show(1, list.DefaultOptions(), "alpha", "beta")
	th := theme.Default()
	size, rows := scene.FitIn(list.DefaultOptions(), &th, 400)
	if rows != 6 || r.surf.size != size || r.ui.shown.rows != rows || r.ui.shown.view.Look().Lines != rows {
		t.Fatalf("%v with %d rows (lines %d), want %v with %d", r.surf.size, r.ui.shown.rows, r.ui.shown.view.Look().Lines, size, rows)
	}
}

func TestAPanelLandingOnAnotherOutputIsRefitted(t *testing.T) {
	r, _ := newRoomyRig(0)
	o := list.DefaultOptions()
	r.show(1, o, "alpha", "beta")
	th := theme.Default()
	free, freeRows := scene.Fit(o, &th)
	if r.surf.size != free {
		t.Fatalf("%v, want %v", r.surf.size, free)
	}
	r.ui.reroom(400)
	short, rows := scene.FitIn(o, &th, 400)
	if len(r.surf.resized) != 1 || r.surf.resized[0] != short || r.ui.shown.rows != rows || r.ui.shown.view.Look().Lines != rows {
		t.Fatalf("resized %v (want %v), %d rows (want %d)", r.surf.resized, short, r.ui.shown.rows, rows)
	}
	r.ui.reroom(400)
	if len(r.surf.resized) != 1 {
		t.Fatalf("the same output resized again: %v", r.surf.resized)
	}
	r.ui.reroom(1080)
	if len(r.surf.resized) != 2 || r.surf.resized[1] != free || r.ui.shown.rows != freeRows {
		t.Fatalf("resized %v, want %v back with %d rows", r.surf.resized, free, freeRows)
	}
}

func TestAnOutputLearntWithoutAPanelFitsTheNextOne(t *testing.T) {
	r, _ := newRoomyRig(0)
	r.ui.reroom(400)
	if len(r.surf.resized) != 0 {
		t.Fatal("resized without a panel")
	}
	r.show(1, list.DefaultOptions(), "alpha")
	if r.ui.shown.rows != 6 {
		t.Fatalf("%d rows", r.ui.shown.rows)
	}
}

func TestAProvidersNextStageKeepsTheRowsOfItsOutput(t *testing.T) {
	r, _ := newRoomyRig(400)
	r.showProvider(1, list.DefaultOptions(), list.Entry{Text: "rite"})
	r.ui.step(1, provider.Step{Kind: provider.StepStage, Options: list.DefaultOptions(), Entries: entries("on", "off")})
	if r.ui.shown.rows != 6 || r.ui.shown.view.Look().Lines != 6 {
		t.Fatalf("the next stage has %d rows, want the output's 6", r.ui.shown.rows)
	}
}

func TestFilteringLeavesThePanelsSizeAlone(t *testing.T) {
	r, _ := newRoomyRig(1080)
	r.show(1, list.DefaultOptions(), "alpha", "beta", "gamma", "delta", "epsilon", "zeta",
		"eta", "theta", "iota", "kappa", "lambda", "mu")
	size, rows := r.surf.size, r.ui.shown.rows
	r.act(input.Insert("eta"))
	r.saw(t, `query "eta"`)
	if len(r.surf.resized) != 0 || r.surf.size != size || r.ui.shown.rows != rows || r.ui.shown.view.Look().Lines != rows {
		t.Fatalf("resized %v, %d rows, want %v with %d", r.surf.resized, r.ui.shown.rows, size, rows)
	}
}

// The window is a surface that knows its output's height.
var _ roomer = (*window.Window)(nil)
