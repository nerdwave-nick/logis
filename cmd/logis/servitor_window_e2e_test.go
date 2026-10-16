package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// servitor mode in the window: a real daemon in the test compositor, `logis
// --servitor` with the fake servitor on its PATH
// (tests/fixtures/servitor/servitor, census window.jsonl), keys from the
// harness's virtual keyboard. Runs only with LOGIS_E2E=niri|sway.

func TestE2ERitesWaitPendingSettleLiveAndLeadToADetachedInvocation(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	w := newWorld(t)
	client := c.ServitorWith(w.env("window.jsonl", "0"))

	// Every rite at once; dnd's auspex is still awaited: its row is
	// pending, and Return on it does nothing.
	focused := c.Shown(3)
	// servitor mode opens in the theme's own window.
	th := theme.Default()
	size, _ := scene.Fit(list.DefaultOptions(), &th)
	checkWindow(t, c, "servitor-pending", int(size.W), int(size.H))
	c.Tap(focused, e2e.KeyEnter)
	c.Tap(focused, e2e.KeyDown)
	if got := c.Next("select"); got != "1" {
		t.Fatalf("select %s", got)
	}
	c.Tap(focused, e2e.KeyUp)
	if got := c.Next("select"); got != "0" {
		t.Fatalf("select %s", got)
	}
	happened := func(word string) bool {
		return slices.ContainsFunc(c.Daemon.Events(), func(e trace.Event) bool { return strings.HasPrefix(e.What, word) })
	}
	if happened("accept") {
		t.Fatal("a pending rite was accepted")
	}

	// The auspex settles: the row is replaced in place, settled in "on".
	w.openGate()
	if got := c.Next("update"); got != "0" {
		t.Fatalf("update %s", got)
	}
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("accept"); got != "0" {
		t.Fatalf("accept %s", got)
	}

	// Its aspects, the current one marked and selected.
	c.Next("stage")
	if got := c.Next("items"); got != "2" {
		t.Fatalf("items %s", got)
	}
	checkWindow(t, c, "servitor-aspect", int(size.W), int(size.H))
	c.Tap(focused, e2e.KeyDown)
	if got := c.Next("select"); got != "1" {
		t.Fatalf("select %s", got)
	}
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("accept"); got != "1" {
		t.Fatalf("accept %s", got)
	}

	// The mandatory inscription takes a word; the next one comes with the
	// decree of the chosen aspect.
	c.Next("stage")
	c.Type(focused, "focus")
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("typed"); got != `"focus"` {
		t.Fatalf("typed %s", got)
	}
	c.Next("stage")
	checkWindow(t, c, "servitor-inscription", int(size.W), int(size.H))
	c.Tap(focused, e2e.KeyEnter)
	if got := c.Next("typed"); got != `"the cogitator"` {
		t.Fatalf("typed %s", got)
	}

	// servitor is invoked; the caller is answered and the window goes,
	// with no toast.
	if ended := client.Ended(); ended.Code != 0 || len(ended.Stdout) != 0 {
		t.Fatalf("%+v", ended)
	}
	c.Next("hide")
	if happened("toast") {
		t.Fatal("logis showed an outcome")
	}
	waitUntil(t, "the invocation to be recorded", w.invoked)
	if argv := lines(w.record("argv")); !reflect.DeepEqual(argv, []string{"dnd", "off", "--reason", "focus", "--origin", "the cogitator"}) {
		t.Fatalf("%q", argv)
	}
	w.labouredToItsEnd()
}
