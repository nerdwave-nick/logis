package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// The pointer and paste end to end (Rust tests/pointer.rs): a real daemon
// in the test compositor, `logis --dmenu` clients, the harness's virtual
// pointer (zwlr_virtual_pointer_v1) and a test-only selection owner
// (wlr-data-control) in place of wl-copy, so the live session's pointer
// and clipboard are never touched. Runs only with LOGIS_E2E=niri|sway.
//
// The daemon's trace says what the pointer did (pointer-enter, cursor,
// button, scroll) and what the list made of it (select, accept, query,
// paste, pasted), so the tests wait for those, never sleep. The isolation
// guard both injectors connect through is tested in internal/e2e.

const pointerLines = 5

// pointerPanel is the panel of a list with pointerLines rows in the
// default theme, and where its rows lie.
func pointerPanel() (scene.Grid, float64) {
	th := theme.Default()
	n := uint32(pointerLines)
	size, rows := scene.Fit(list.Options{Lines: &n}, &th)
	return scene.GridOf(size, rows, &th), float64(size.W)
}

// rowCentre is the middle of row in view, in surface coordinates.
func rowCentre(t *testing.T, row int) (float64, float64) {
	t.Helper()
	g, width := pointerPanel()
	x, y := width/2, float64(g.Top(row)+g.Pitch/2)
	if got, ok := g.RowAt(x, y); !ok || got != row {
		t.Fatalf("row %d is not under %v,%v", row, x, y)
	}
	return x, y
}

func numbered(n int) []byte {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "item %d\n", i)
	}
	return []byte(b.String())
}

// aim learns how output points map onto the shown panel: the pointer
// enters it twice from a corner, at the output's centre (on the panel,
// whatever the output's size) and a little beside it, and the panel's own
// `pointer-enter` says where in it the pointer was each time. Entering
// never selects, so the list is as it was. It checks the cursor was set.
func aim(t *testing.T, c *e2e.Cogitator, vp *e2e.VirtualPointer) e2e.Panel {
	t.Helper()
	w, h := c.Harness.OutputSize()
	centre := [2]float64{float64(w / 2), float64(h / 2)}
	sent := [2][2]float64{centre, {centre[0] + 8, centre[1] + 8}}
	var seen [2][2]float64
	for i, p := range sent {
		vp.MoveTo(1, 1)
		since := trace.MonotonicNS()
		vp.MoveTo(p[0], p[1])
		seen[i] = surfacePoint(t, c, since, "pointer-enter")
		c.Daemon.WaitFor("the cursor set", e2e.Is("cursor default"))
	}
	return e2e.Calibrate(sent, seen)
}

// surfacePoint is the surface point of the next `word <x> <y>` event
// since since.
func surfacePoint(t *testing.T, c *e2e.Cogitator, since uint64, word string) [2]float64 {
	t.Helper()
	e := c.Daemon.WaitFor(word, func(e trace.Event) bool { return e.NS >= since && strings.HasPrefix(e.What, word+" ") })
	f := strings.Fields(strings.TrimPrefix(e.What, word+" "))
	x, errX := strconv.ParseFloat(f[0], 64)
	y, errY := strconv.ParseFloat(f[1], 64)
	if len(f) != 2 || errX != nil || errY != nil {
		t.Fatalf("%q", e.What)
	}
	return [2]float64{x, y}
}

// moveOver moves the pointer over row in view.
func moveOver(t *testing.T, vp *e2e.VirtualPointer, panel e2e.Panel, row int) {
	t.Helper()
	vp.MoveTo(panel.At(rowCentre(t, row)))
}

func next(t *testing.T, c *e2e.Cogitator, word, want string) {
	t.Helper()
	if got := c.Next(word); got != want {
		t.Fatalf("%s %s, want %s", word, got, want)
	}
}

func TestE2EHoveringSelectsARowAndAClickOnItAcceptsIt(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	vp := c.Harness.VirtualPointer()
	client := c.DmenuAll([]byte("alpha\nbeta\ngamma\ndelta\nepsilon\n"), fmt.Sprintf("--lines=%d", pointerLines))
	c.Shown(5)
	panel := aim(t, c, vp)
	moveOver(t, vp, panel, 3)
	next(t, c, "select", "3") // hover highlights the row
	// A press dragged off its row chooses nothing; the hover follows.
	vp.Button(e2e.BtnLeft, true)
	moveOver(t, vp, panel, 1)
	next(t, c, "select", "1")
	vp.Button(e2e.BtnLeft, false)
	vp.Click(e2e.BtnLeft)
	next(t, c, "accept", "1") // only the click on one row accepts
	chose(t, client, "beta\n")
	c.Daemon.WaitSeen("button traces", e2e.Is(fmt.Sprintf("button %d released", e2e.BtnLeft)))
}

func TestE2ETheWheelScrollsTheListThreeRowsANotch(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	vp := c.Harness.VirtualPointer()
	client := c.DmenuAll(numbered(30), fmt.Sprintf("--lines=%d", pointerLines))
	c.Shown(30)
	panel := aim(t, c, vp)
	moveOver(t, vp, panel, 0)
	vp.Wheel(2)
	next(t, c, "scroll", "wheel 240")
	next(t, c, "select", "6") // row 0 now shows item 6
	vp.Wheel(-1)
	next(t, c, "scroll", "wheel -120")
	next(t, c, "select", "3")
	moveOver(t, vp, panel, 1)
	next(t, c, "select", "4")
	vp.Click(e2e.BtnLeft)
	chose(t, client, "item 4\n")
}

func TestE2EAFingerScrollsARowPerRowHeightAndCarriesTheRest(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	vp := c.Harness.VirtualPointer()
	client := c.DmenuAll(numbered(30), fmt.Sprintf("--lines=%d", pointerLines))
	c.Shown(30)
	panel := aim(t, c, vp)
	moveOver(t, vp, panel, 0)
	g, _ := pointerPanel()
	pitch := float64(g.Pitch)
	px := func(v float64) string { return "pixels " + strconv.FormatFloat(v, 'f', -1, 64) }
	for item := 1; item <= 3; item++ {
		vp.Finger(pitch)
		next(t, c, "scroll", px(pitch))
		next(t, c, "select", strconv.Itoa(item))
	}
	// Half a row moves nothing; the next half completes the row. (Lifting
	// the finger drops a half row: unit-tested only, since nested niri's
	// virtual pointer does not forward axis_stop.)
	vp.Finger(pitch / 2)
	next(t, c, "scroll", px(pitch/2))
	vp.Finger(pitch / 2)
	c.Next("scroll")
	next(t, c, "select", "4")
	vp.Click(e2e.BtnLeft)
	next(t, c, "accept", "4")
	chose(t, client, "item 4\n")
}

// chord taps key while m is held.
func chord(c *e2e.Cogitator, f e2e.Focused, m e2e.Modifier, key uint32) {
	c.Keyboard.Hold(f, m)
	c.Tap(f, key)
	c.Keyboard.Release(f, m)
}

func usKey(t *testing.T, r rune) uint32 {
	t.Helper()
	code, _, ok := e2e.USKey(r)
	if !ok {
		t.Fatalf("no US key for %q", r)
	}
	return code
}

func TestE2ECtrlVAndShiftInsertPasteTheClipboardAtTheCursor(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	owner := e2e.OwnSelection(t, c.Compositor, "gam\nnot this line", "")
	client := c.DmenuAll(dmenuItems)
	focused := c.Shown(3)
	c.Type(focused, "ma")
	c.Tap(focused, e2e.KeyHome)
	chord(c, focused, e2e.Control, usKey(t, 'v'))
	next(t, c, "paste", "clipboard text/plain;charset=utf-8")
	next(t, c, "pasted", "clipboard 3")
	next(t, c, "query", `"gamma"`) // inserted at the cursor
	next(t, c, "select", "2")
	if got := owner.Requests(); !slices.Equal(got, []string{"clipboard text/plain;charset=utf-8"}) {
		t.Errorf("the owner was asked for %q", got)
	}

	chord(c, focused, e2e.Control, usKey(t, 'u'))
	next(t, c, "query", `""`)
	chord(c, focused, e2e.Shift, e2e.KeyInsert)
	next(t, c, "pasted", "clipboard 3")
	next(t, c, "query", `"gam"`)
	c.Tap(focused, e2e.KeyEnter)
	chose(t, client, "gamma\n")
}

func TestE2ETheMiddleButtonPastesThePrimarySelection(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	vp := c.Harness.VirtualPointer()
	owner := e2e.OwnSelection(t, c.Compositor, "not the clipboard", "bet")
	client := c.DmenuAll(dmenuItems)
	focused := c.Shown(3)
	aim(t, c, vp)
	vp.Click(e2e.BtnMiddle)
	next(t, c, "paste", "primary text/plain;charset=utf-8")
	next(t, c, "query", `"bet"`)
	next(t, c, "select", "1")
	if got := owner.Requests(); !slices.Equal(got, []string{"primary text/plain;charset=utf-8"}) {
		t.Errorf("the owner was asked for %q", got)
	}
	c.Tap(focused, e2e.KeyEnter)
	chose(t, client, "beta\n")
}
