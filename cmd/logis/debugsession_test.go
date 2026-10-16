package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ports of Rust src/window/debug/session.rs's and mod.rs's tests: the
// debug panel as a session of the input model's actions.

func TestNextAndPreviousMoveTheSelectionWithinTheRows(t *testing.T) {
	s := &debugSession{}
	if got := s.apply(input.ActPrevious); got != unchanged {
		t.Fatalf("Previous at the top: %v", got)
	}
	if got := s.apply(input.ActNext); got != selected || s.row != 1 {
		t.Fatalf("Next: %v, row %d", got, s.row)
	}
	for range debugSelectable {
		s.apply(input.ActNext)
	}
	if s.row != debugSelectable-1 {
		t.Fatalf("Next past the end: row %d", s.row)
	}
	if got := s.apply(input.ActNext); got != unchanged {
		t.Errorf("Next at the end: %v", got)
	}
	if got := s.apply(input.ActPrevious); got != selected {
		t.Errorf("Previous: %v", got)
	}
}

func TestEditsChangeTheQueryAndReportOnlyRealChanges(t *testing.T) {
	s := &debugSession{}
	if got := s.apply(input.Insert("ö")); got != edited || s.query.Text() != "ö" {
		t.Fatalf("insert: %v %q", got, s.query.Text())
	}
	clear := input.EditAction(input.Edit{Kind: input.Clear})
	if got := s.apply(clear); got != edited {
		t.Errorf("clear: %v", got)
	}
	if got := s.apply(clear); got != unchanged {
		t.Errorf("clear again: %v", got)
	}
}

func TestDismissAndAcceptEndTheSession(t *testing.T) {
	for a, want := range map[input.Action]outcome{
		input.ActDismiss: dismissed,
		input.ActAccept:  accepted,
		input.ActDescend: accepted,
	} {
		if got := (&debugSession{}).apply(a); got != want {
			t.Errorf("%+v: %v, want %v", a, got, want)
		}
	}
}

// What the panel does with each outcome: redraw on a change, print the
// accepted row, end on accept or dismiss.
func TestTheSessionRedrawsPrintsAndEnds(t *testing.T) {
	var out strings.Builder
	redraws := 0
	s := &debugSession{stdout: &out, redraw: func() { redraws++ }}
	s.handle(input.ActNext)
	s.handle(input.ActNext)
	s.handle(input.Insert("x"))
	s.handle(input.ActPrevious)
	if redraws != 4 || s.done {
		t.Fatalf("%d redraws, done %v", redraws, s.done)
	}
	s.handle(input.ActAccept)
	if !s.done || out.String() != debugRows[1]+"\n" {
		t.Errorf("accept: done %v, printed %q", s.done, out.String())
	}
	d := &debugSession{stdout: &out, redraw: func() {}}
	d.handle(input.ActDismiss)
	if !d.done || out.String() != debugRows[1]+"\n" {
		t.Errorf("dismiss: done %v, printed %q", d.done, out.String())
	}
}

// The panel's keyboard carries the config's focus-loss policy: losing the
// keyboard dismisses the panel, unless the config keeps it.
func TestThePanelsKeyboardFollowsTheFocusLossPolicy(t *testing.T) {
	for policy, done := range map[config.FocusLoss]bool{config.FocusDismiss: true, config.FocusKeep: false} {
		s := &debugSession{stdout: io.Discard, redraw: func() {}}
		kb := debugKeyboard(config.Config{FocusLoss: policy}, s, io.Discard)
		kb.Enter(1, nil)
		kb.Leave(2)
		if s.done != done {
			t.Errorf("policy %v: done %v after focus loss", policy, s.done)
		}
	}
}

// The config is read before any window: a heretical one ends the panel
// with exit 2 and says why, in the words Rust's debug panel used (the
// config's own error, without the daemon's "the config is heretical").
func TestAHereticalConfigEndsThePanelBeforeAnyWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	if err := os.WriteFile(path, []byte(`{"focus-loss": "linger"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WAYLAND_DISPLAY", "")
	src := config.NewSource(path)
	var stdout, stderr strings.Builder
	if code := debugPanel(&src, &stdout, &stderr); code != 2 {
		t.Errorf("exit %d", code)
	}
	if !strings.HasPrefix(stderr.String(), "logis: "+path+":1:") || stdout.Len() != 0 {
		t.Errorf("stderr %q, stdout %q", stderr.String(), stdout.String())
	}
}

// The query line shows the query with its cursor below every row.
func TestTheQueryLineShowsTheQueryWithItsCursor(t *testing.T) {
	var q input.Query
	q.Apply(input.Edit{Kind: input.InsertText, Text: "öλ"})
	q.Apply(input.Edit{Kind: input.Left})
	sc := debugSceneWithQuery(debugSize, 0, &q)
	line := sc.Texts[len(sc.Texts)-1]
	if line.Text != "> ö|λ" {
		t.Errorf("query line %q", line.Text)
	}
	if rows := len(sc.Texts) - 2; rows != debugSelectable {
		t.Errorf("%d rows fit, want every one", rows)
	}
	if empty := debugSceneWithQuery(debugSize, 0, &input.Query{}); empty.Texts[len(empty.Texts)-1].Text != "> |" {
		t.Errorf("empty query line %q", empty.Texts[len(empty.Texts)-1].Text)
	}
}

func TestTheSelectionAndItsMarkerMoveByOneRowPitchPerRow(t *testing.T) {
	top, third := debugSceneAt(debugSize, 0), debugSceneAt(debugSize, 2)
	plain := debugScene(debugSize)
	for i := range plain.Shapes {
		if top.Shapes[i] != plain.Shapes[i] {
			t.Errorf("row 0 changed shape %d", i)
		}
		moved := float32(0)
		if i == debugSelected || i == debugMarker {
			moved = 2 * debugRowPitch
		}
		if d := third.Shapes[i].Rect[1] - top.Shapes[i].Rect[1]; d != moved {
			t.Errorf("shape %d moved %v, want %v", i, d, moved)
		}
	}
}

func TestTheSelectionNeverLeavesASmallerBody(t *testing.T) {
	sc := debugSceneAt(scale.Size{W: 466, H: 300}, debugSelectable-1)
	body, band := sc.Shapes[0].Rect, sc.Shapes[debugSelected].Rect
	if band[1]+band[3] > body[1]+body[3]-queryBlock {
		t.Errorf("band %v reaches into the query block of %v", band, body)
	}
}
