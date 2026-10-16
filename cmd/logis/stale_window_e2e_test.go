package main

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// A daemon left running across an upgrade, in its window (feat/logis-go
// 750bed66): once the binary it runs from is replaced, plain logis's next
// panel shows the reminder as a notice over the list, and the session
// still works. Runs only with LOGIS_E2E=niri|sway.

// upgrade replaces the file at path with a copy of itself, as an upgrade
// does: a new file renamed over the old (the running daemon keeps its
// image). The bytes stay the same, so later tests of this process run
// what they ran before.
func upgrade(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".new", b, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
}

// toastsSince are the toasts the daemon reported from the trace time ns on.
func toastsSince(c *e2e.Cogitator, ns uint64) []string {
	var toasts []string
	for _, e := range c.Daemon.Events() {
		if e.NS >= ns && strings.HasPrefix(e.What, "toast ") {
			toasts = append(toasts, e.What)
		}
	}
	return toasts
}

// lastShow is the trace time of the last session shown.
func lastShow(c *e2e.Cogitator) uint64 {
	var ns uint64
	for _, e := range c.Daemon.Events() {
		if strings.HasPrefix(e.What, "show ") {
			ns = e.NS
		}
	}
	return ns
}

func TestE2EADaemonWhoseBinaryWasReplacedShowsTheReminderAndStillServes(t *testing.T) {
	dir := t.TempDir()
	c := startApps(t, dir)
	w := newWorld(t)
	env := tabbedEnv(t, dir, w)

	// Before the upgrade the panel has nothing to say.
	client, focused, _ := openTabbed(t, c, env)
	before := lastShow(c)
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	if ended := client.Ended(); ended.Code != 1 {
		t.Fatalf("%+v", ended)
	}
	if toasts := toastsSince(c, before); len(toasts) != 0 {
		t.Fatalf("toasted before the upgrade: %q", toasts)
	}

	upgrade(t, e2e.Binary(t))
	reminder := "a newer logis is installed — restart the daemon (kill " + strconv.Itoa(c.Daemon.PID()) + ")"
	want := "toast " + trace.Quote(reminder)
	for range 2 { // every panel, not only the first
		var tab string
		client, focused, tab = openTabbed(t, c, env)
		since := lastShow(c)
		c.Daemon.WaitSeen(want, func(e trace.Event) bool { return e.NS >= since && e.What == want })
		// It still serves: the query filters, the tab switches, Escape
		// dismisses.
		c.Type(focused, "a")
		other := map[string]string{"applications": "rites", "rites": "applications"}[tab]
		c.Tap(focused, e2e.KeyTab)
		next(t, c, "tab", other)
		c.Tap(focused, e2e.KeyEsc)
		c.Next("dismiss")
		if ended := client.Ended(); ended.Code != 1 {
			t.Fatalf("%+v", ended)
		}
	}
	if n := strings.Count(c.Daemon.Stderr(), "logis: "+reminder+"\n"); n != 1 {
		t.Fatalf("the log has the reminder %d times:\n%s", n, c.Daemon.Stderr())
	}
}
