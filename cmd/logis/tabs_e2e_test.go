package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/provider"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// Plain logis's tabbed launcher end to end (feat/logis-go 8cba2fc9): a real
// daemon in the test compositor over the app fixtures, plain `logis` with
// the fake servitor (census window.jsonl) and the fixture apps on its PATH,
// keys and the pointer from the harness. Runs only with
// LOGIS_E2E=niri|sway.

// e2eApps and e2eRites are how many apps and rites the fixtures give.
const e2eApps, e2eRites = 2, 3

// tabbedEnv is plain logis's environment: the fixture apps and the fake
// servitor on its PATH.
func tabbedEnv(t *testing.T, dir string, w *world) []string {
	env := appsClientEnv(t, dir)
	for _, kv := range w.env("window.jsonl", "0") {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	return append(env, "PATH="+filepath.Join(w.dir, "bin")+":"+filepath.Join(appFixtures(t), "bin")+":/usr/bin:/bin")
}

// tabbedSize is the tabbed launcher's surface in the default theme.
func tabbedSize() scale.Size {
	th := theme.Default()
	size, _ := scene.FitTabbed(provider.AppsOptions(), &th, 0)
	return size
}

// openTabbed starts plain logis and waits until both tabs are filled; it
// says the tab it opened on.
func openTabbed(t *testing.T, c *e2e.Cogitator, env []string) (*e2e.Client, e2e.Focused, string) {
	t.Helper()
	client := c.ClientWith(env)
	focused := c.Shown(e2eApps)
	tab := shownTab(t, c)
	sinceShow(t, c, "rites items 3")
	return client, focused, tab
}

// shownTab is the tab the panel Shown last waited for opened on. The tab
// is told with the show, before the focus Shown ends at.
func shownTab(t *testing.T, c *e2e.Cogitator) string {
	t.Helper()
	return strings.TrimPrefix(sinceShow(t, c, "tab ").What, "tab ")
}

// sinceShow waits for the first report starting with what since the
// last show, without moving the sequence cursor.
func sinceShow(t *testing.T, c *e2e.Cogitator, what string) trace.Event {
	t.Helper()
	var since uint64
	for _, e := range c.Daemon.Events() {
		if strings.HasPrefix(e.What, "show ") {
			since = e.NS
		}
	}
	return c.Daemon.WaitSeen(what, func(e trace.Event) bool { return e.NS >= since && strings.HasPrefix(e.What, what) })
}

func TestE2ETabSwitchesTabsTheQueryStaysAndTheTabIsRemembered(t *testing.T) {
	dir := t.TempDir()
	c := startApps(t, dir)
	w := newWorld(t)
	env := tabbedEnv(t, dir, w)
	client, focused, tab := openTabbed(t, c, env)
	if tab != "applications" {
		t.Fatalf("opened on %s", tab)
	}
	size := tabbedSize()
	checkWindow(t, c, "tabs-applications", int(size.W), int(size.H))

	// The query stays across the tabs: typed on the applications, it
	// goes on on the rites.
	c.Type(focused, "e")
	c.Tap(focused, e2e.KeyTab)
	next(t, c, "tab", "rites")
	c.Keyboard.Type(focused, "m")
	next(t, c, "query", `"em"`)
	chord(c, focused, e2e.Shift, e2e.KeyTab)
	next(t, c, "tab", "applications")
	c.Tap(focused, e2e.KeyTab)
	next(t, c, "tab", "rites")
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	if ended := client.Ended(); ended.Code != 1 {
		t.Fatalf("%+v", ended)
	}
	remembered := filepath.Join(dir, "state/logis/tab")
	waitUntil(t, "the tab to be kept", func() bool {
		b, err := os.ReadFile(remembered)
		return err == nil && string(b) == "rites\n"
	})

	// The next summons opens on the rites, and a rite chosen there leads
	// through its stages to servitor's invocation.
	client, focused, tab = openTabbed(t, c, env)
	if tab != "rites" {
		t.Fatalf("reopened on %s", tab)
	}
	checkWindow(t, c, "tabs-rites", int(size.W), int(size.H))
	w.openGate() // the censuses conclude
	c.Type(focused, "theme")
	c.Tap(focused, e2e.KeyEnter)
	next(t, c, "accept", "2")
	c.Daemon.WaitFor("the aspects", e2e.Word("rites stage"))
	c.Tap(focused, e2e.KeyEnter)
	next(t, c, "accept", "0")
	c.Daemon.WaitFor("the inscription", e2e.Word("rites stage"))
	c.Tap(focused, e2e.KeyEnter)
	next(t, c, "typed", `""`)
	if ended := client.Ended(); ended.Code != 0 || len(ended.Stdout) != 0 {
		t.Fatalf("%+v", ended)
	}
	waitUntil(t, "the invocation to be recorded", w.invoked)
	if argv := lines(w.record("argv")); !reflect.DeepEqual(argv[:2], []string{"theme", "default"}) {
		t.Fatalf("%q", argv)
	}
	w.labouredToItsEnd()
}

func TestE2EAClickOnAPlateSelectsItAndRowsLieBelowTheStrip(t *testing.T) {
	dir := t.TempDir()
	c := startApps(t, dir)
	vp := c.Harness.VirtualPointer()
	w := newWorld(t)
	client, focused, _ := openTabbed(t, c, tabbedEnv(t, dir, w))
	panel := aim(t, c, vp)
	th := theme.Default()
	size := tabbedSize()
	plates := scene.Plates(size, &th)
	rites := plates[1].Rect
	vp.MoveTo(panel.At(float64(rites[0]+rites[2]/2), float64(rites[1]+rites[3]/2)))
	vp.Click(e2e.BtnLeft)
	next(t, c, "tab", "rites")
	g := scene.TabbedGrid(size, th.Window.Rows, &th)
	for _, row := range []int{2, 1} {
		vp.MoveTo(panel.At(float64(g.Left+g.Right)/2, float64(g.Top(row)+g.Pitch/2)))
		next(t, c, "select", []string{"", "1", "2"}[row])
	}
	apps := plates[0].Rect
	vp.MoveTo(panel.At(float64(apps[0]+apps[2]/2), float64(apps[1]+apps[3]/2)))
	vp.Click(e2e.BtnLeft)
	next(t, c, "tab", "applications")
	vp.MoveTo(panel.At(float64(g.Left+g.Right)/2, float64(g.Top(1)+g.Pitch/2)))
	next(t, c, "select", "1")
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	if ended := client.Ended(); ended.Code != 1 {
		t.Fatalf("%+v", ended)
	}
	w.openGate() // the census concludes
}

// --apps is the launcher alone even with servitor on the PATH: no strip,
// and Tab selects the next item.
func TestE2EAppsIsTheLauncherAloneAndTabSelects(t *testing.T) {
	dir := t.TempDir()
	c := startApps(t, dir)
	client := c.ClientWith(tabbedEnv(t, dir, newWorld(t)), "--apps")
	focused := c.Shown(e2eApps)
	th := theme.Default()
	size, _ := scene.Fit(list.DefaultOptions(), &th)
	checkWindow(t, c, "apps", int(size.W), int(size.H))
	c.Tap(focused, e2e.KeyTab)
	next(t, c, "select", "1")
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	if ended := client.Ended(); ended.Code != 1 {
		t.Fatalf("%+v", ended)
	}
	if _, err := os.Stat(filepath.Join(dir, "state/logis/tab")); err == nil {
		t.Error("--apps kept a tab")
	}
	noTabs(t, c)
}

// noTabs: no panel showed tabs.
func noTabs(t *testing.T, c *e2e.Cogitator) {
	t.Helper()
	for _, e := range c.Daemon.Events() {
		if e.What == "tab applications" || e.What == "tab rites" || strings.HasPrefix(e.What, "rites ") {
			t.Fatalf("tabs: %q", e.What)
		}
	}
}

// Silent rites (2aaad4b9): plain logis with no servitor on its PATH shows
// the strip; Tab shows the rites' tab saying why, Return there does
// nothing, Escape exits 1, and the next summons opens on the rites.
func TestE2EWithoutServitorTheRitesSayWhyAndReturnDoesNothing(t *testing.T) {
	dir := t.TempDir()
	c := startApps(t, dir)
	env := appsClientEnv(t, dir) // no servitor on its PATH
	client := c.ClientWith(env)
	focused := c.Shown(e2eApps)
	if tab := shownTab(t, c); tab != "applications" {
		t.Fatalf("opened on %s", tab)
	}
	sinceShow(t, c, "rites silent")
	c.Tap(focused, e2e.KeyTab)
	next(t, c, "tab", "rites")
	size := tabbedSize()
	checkWindow(t, c, "tabs-rites-silent", int(size.W), int(size.H))
	// Return, then a key: the key is heard (Type waits for its query),
	// and nothing was answered.
	c.Tap(focused, e2e.KeyEnter)
	c.Type(focused, "k")
	c.Tap(focused, e2e.KeyEnter)
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	for _, e := range c.Daemon.Events() {
		if w := strings.Fields(e.What); len(w) > 0 && (w[0] == "accept" || w[0] == "typed" || w[0] == "descend") {
			t.Fatalf("silent rites answered: %q", e.What)
		}
	}
	if ended := client.Ended(); ended.Code != 1 || len(ended.Stdout) != 0 {
		t.Fatalf("%+v", ended)
	}
	remembered := filepath.Join(dir, "state/logis/tab")
	waitUntil(t, "the tab to be kept", func() bool {
		b, err := os.ReadFile(remembered)
		return err == nil && string(b) == "rites\n"
	})
	client = c.ClientWith(env)
	focused = c.Shown(e2eApps)
	if tab := shownTab(t, c); tab != "rites" {
		t.Fatalf("reopened on %s", tab)
	}
	sinceShow(t, c, "rites silent")
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	if ended := client.Ended(); ended.Code != 1 {
		t.Fatalf("%+v", ended)
	}
	if _, err := os.Stat(filepath.Join(dir, "record")); err == nil {
		t.Error("something was launched")
	}
}
