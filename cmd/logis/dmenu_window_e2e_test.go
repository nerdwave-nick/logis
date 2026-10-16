package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// `logis --dmenu` end to end (Rust tests/dmenu.rs): a real daemon in the
// test compositor, real `logis --dmenu` clients with piped stdin and
// stdout, and keys from the harness's virtual keyboard. Runs only with
// LOGIS_E2E=niri|sway. The daemon runs with the test trace on, so the tests
// wait for what the window did (show, items, query, select, accept, typed,
// dismiss, and the window's focus, commits and settled) instead of
// sleeping.

var dmenuItems = []byte("alpha\nbeta\ngamma\n")

func chose(t *testing.T, cl *e2e.Client, want string) {
	t.Helper()
	if got := cl.Ended(); got.Code != 0 || string(got.Stdout) != want {
		t.Fatalf("exit %d, stdout %q, want 0 and %q", got.Code, got.Stdout, want)
	}
}

func TestE2ETypingFiltersAndReturnPrintsTheChosenLine(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	client := c.DmenuAll(dmenuItems)
	focused := c.Shown(3)
	c.Type(focused, "gam")
	if got := c.Next("select"); got != "2" {
		t.Fatalf("select %s", got)
	}
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("accept"); got != "2" {
		t.Fatalf("accept %s", got)
	}
	chose(t, client, "gamma\n")
}

func TestE2EEscapeDismissesWithEmptyStdoutAndExit1(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	client := c.DmenuAll(dmenuItems)
	focused := c.Shown(3)
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	if got := client.Ended(); got.Code != 1 || len(got.Stdout) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestE2EIndexAndColumnsPrintWhatWasAsked(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	input := []byte("id-1\tFirst\nid-2\tSecond\n")
	client := c.DmenuAll(input, "--with-nth=2", "--accept-nth=1")
	focused := c.Shown(2)
	c.Type(focused, "sec")
	c.Tap(focused, e2e.KeyEnter)
	chose(t, client, "id-2\n")

	client = c.DmenuAll(input, "--index")
	focused = c.Shown(2)
	c.Tap(focused, e2e.KeyDown)
	c.Next("select")
	c.Tap(focused, e2e.KeyEnter)
	chose(t, client, "1\n")
}

func TestE2EReturnWithoutAMatchPrintsTheTypedText(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	client := c.DmenuAll(dmenuItems)
	focused := c.Shown(3)
	c.Type(focused, "Free Rite")
	if got := c.Next("select"); got != "none" {
		t.Fatalf("select %s", got)
	}
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("typed"); got != `"Free Rite"` {
		t.Fatalf("typed %s", got)
	}
	chose(t, client, "Free Rite\n")
}

func TestE2EOnlyMatchRefusesFreeTextUntilAMatchIsChosen(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	client := c.DmenuAll(dmenuItems, "--only-match")
	focused := c.Shown(3)
	c.Type(focused, "zz")
	c.Tap(focused, e2e.KeyEnter)
	// Return did nothing; clearing the query (Ctrl+U) brings every item back.
	c.Keyboard.Hold(focused, e2e.Control)
	c.Tap(focused, code(t, 'u'))
	c.Keyboard.Release(focused, e2e.Control)
	if got := c.Next("query"); got != `""` {
		t.Fatalf("query %s", got)
	}
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("accept"); got != "0" {
		t.Fatalf("accept %s", got)
	}
	if slices.ContainsFunc(c.Daemon.Events(), func(e trace.Event) bool { return strings.HasPrefix(e.What, "typed") }) {
		t.Fatal("free text was accepted")
	}
	chose(t, client, "alpha\n")
}

func TestE2EQueuedCallersEachGetTheirOwnAnswerInTurn(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	first := c.DmenuAll([]byte("one\ntwo\n"))
	focused := c.Shown(2)
	second := c.DmenuAll([]byte("three\nfour\nfive\n"))
	// The second caller waits while the first is answered.
	c.Tap(focused, e2e.KeyDown)
	c.Next("select")
	c.Tap(focused, e2e.KeyEnter)
	chose(t, first, "two\n")
	focused = c.Shown(3)
	c.Type(focused, "five")
	c.Tap(focused, e2e.KeyEnter)
	chose(t, second, "five\n")
}

func TestE2ETheWindowOpensBeforeTheInputEndsAndTakesStreamedLines(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	client := c.Dmenu([]byte("first\n"))
	focused := c.Shown(1)
	// Still open: more lines arrive while the window is shown.
	if _, err := client.Stdin.Write([]byte("second\nthird\n")); err != nil {
		t.Fatal(err)
	}
	c.Daemon.WaitFor("items 3", e2e.Is("items 3"))
	c.Tap(focused, e2e.KeyDown)
	c.Tap(focused, e2e.KeyDown)
	if a, b := c.Next("select"), c.Next("select"); a != "1" || b != "2" {
		t.Fatalf("select %s, %s", a, b)
	}
	c.Tap(focused, e2e.KeyEnter)
	chose(t, client, "third\n")
}

func TestE2ETheListIsDrawnWithItsMatchesHighlighted(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	input := "rite of oil\tunguent\nrite of ignition\tspark\ncafé noir\tdrink\nlitany\n"
	client := c.DmenuAll([]byte(input), "--counter", "--lines=4", "--width=40")
	focused := c.Shown(4)
	c.Type(focused, "rite")
	c.Tap(focused, e2e.KeyDown)
	c.Next("select")
	c.Daemon.WaitFor("the band at rest", e2e.Is("settled"))
	o := list.DefaultOptions()
	lines, width := uint32(4), uint32(40)
	o.Lines, o.Width = &lines, &width
	th := theme.Default()
	size, _ := scene.Fit(o, &th)
	checkWindow(t, c, "dmenu-filtered", int(size.W), int(size.H))
	c.Tap(focused, e2e.KeyEsc)
	if got := client.Ended(); got.Code != 1 {
		t.Fatalf("%+v", got)
	}
}

// checkWindow waits until the shown window has opened and has committed a
// frame drawn after its latest change (a next stage, its items), captures
// until two consecutive captures agree, then compares the window cropped
// from the output with golden e2e/<name>. Motions after the open (a
// selection's glide) are the caller's to wait for.
func checkWindow(t *testing.T, c *e2e.Cogitator, name string, w, ht int) {
	t.Helper()
	c.OpenSettled()
	c.Daemon.WaitForDrawn()
	capture := c.Harness.SettledCapture()
	g := rendertest.CheckedIn(t)
	reference, _ := g.Load("e2e", name)
	window := e2e.CropCentred(capture, w, ht, reference)
	g.Assert(t, "e2e", name, window, rendertest.Capture)
}

func TestE2ELosingTheCompositorEndsTheDaemonWithExit2(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	// The window owns the daemon's only compositor connection; its loss,
	// seen by the window's goroutine, must end the daemon.
	c.Keyboard.Close()
	c.Compositor.Kill()
	if code := c.Daemon.WaitExit(); code != 2 {
		t.Fatalf("exit %d; stderr %s", code, c.Daemon.Stderr())
	}
	if !strings.Contains(c.Daemon.Stderr(), "noosphere fell silent") {
		t.Fatalf("%s", c.Daemon.Stderr())
	}
}
