package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/apps"
	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/dbus"
	"github.com/nerdwave-nick/logis/internal/dbus/dbustest"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// App sessions in the core, launching the fixture program
// tests/fixtures/apps/bin/anoint, which records how it was started. No
// session bus unless a test starts the fake one.

// Activation makes the recorder an Activator.
func (r *recorder) Activation(id SessionID, appID string) { r.note("activation %d %s", id, appID) }

func appFixtures(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "fixtures", "apps")
}

func appIndex(t *testing.T) *apps.Index {
	t.Helper()
	f := appFixtures(t)
	cx := apps.Context{Desktops: []string{"niri"}, Path: []string{filepath.Join(f, "bin")}}
	return apps.Scan(apps.Dirs{filepath.Join(f, "system/applications")}, cx)
}

// openApps opens app session id with the client environment env.
func openApps(t *testing.T, c *core, id SessionID, env ...string) *wire {
	t.Helper()
	w := greet(t, c, id)
	open := protocol.Open(protocol.ModeApps, nil)
	for _, kv := range env {
		open.Env = append(open.Env, []byte(kv))
	}
	c.receive(id, open, nil)
	return w
}

func anointRow(t *testing.T, c *core) int {
	t.Helper()
	i := slices.IndexFunc(c.apps.index.Apps(), func(a apps.App) bool { return a.ID == "forge-anoint.desktop" })
	if i < 0 {
		t.Fatal("no forge-anoint.desktop")
	}
	return i
}

func anoint(t *testing.T, c *core) Answer {
	return Answer{Kind: AnswerItem, Index: anointRow(t, c)}
}

// launchEnv is a client environment in which anoint is found and records
// itself in dir/record.
func launchEnv(t *testing.T, dir string) []string {
	return []string{"PATH=" + filepath.Join(appFixtures(t), "bin") + ":/usr/bin:/bin",
		"ANOINT_RECORD=" + filepath.Join(dir, "record"), "HOME=/"}
}

func recorded(t *testing.T, dir string) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if b, err := os.ReadFile(filepath.Join(dir, "record")); err == nil {
			return string(b)
		}
	}
	t.Fatal("the app never ran")
	return ""
}

func hasPrefix(log []string, i int, prefix string) bool {
	return i < len(log) && strings.HasPrefix(log[i], prefix)
}

func TestASessionBeforeTheIndexShowsAPlaceholderThenTheApps(t *testing.T) {
	c, r := newTestCore()
	openApps(t, c, 1)
	wantLog(t, r, "show 1 apps []")
	if got := r.shown[0].Options.Placeholder; got != "the census of applications is taken…" {
		t.Errorf("placeholder %q", got)
	}
	c.indexed(appIndex(t))
	if log := r.take(); len(log) != 1 || !hasPrefix(log, 0, "stage 1 [Anoint,Broken in home") {
		t.Errorf("%q", log)
	}
	openApps(t, c, 2)
	if log := r.take(); !hasPrefix(log, len(log)-1, "show 2 apps [Anoint,") {
		t.Errorf("%q", log)
	}
	if got := r.shown[len(r.shown)-1].Options.Placeholder; got != "summon an application" {
		t.Errorf("placeholder %q", got)
	}
}

// A new index reaches only the visible session still waiting for one.
func TestANewIndexIsNotPushedIntoASessionThatHasOne(t *testing.T) {
	c, r := newTestCore()
	c.indexed(appIndex(t))
	openApps(t, c, 1)
	r.take()
	c.indexed(appIndex(t))
	wantLog(t, r)
}

func TestReturnAsksForATokenAndWaitsForIt(t *testing.T) {
	c, r := newTestCore()
	c.indexed(appIndex(t))
	openApps(t, c, 1)
	r.take()
	c.answered(1, anoint(t, c))
	wantLog(t, r, "activation 1 forge-anoint")
	c.answered(1, anoint(t, c))
	wantLog(t, r) // one launch at a time
	if _, ok := c.deadline(); !ok {
		t.Error("it waits for ever")
	}
}

func TestALaunchThatFailsIsToastedAndTheStageTakesReturnAgain(t *testing.T) {
	c, r := newTestCore()
	c.indexed(appIndex(t))
	w := openApps(t, c, 1, "HOME=/") // no PATH: the program is nowhere
	r.take()
	c.answered(1, anoint(t, c))
	r.take()
	c.activated(1, "tok")
	log := r.take()
	if !hasPrefix(log, 0, "toast 1 Anoint cannot be summoned: no program 'anoint'") || !hasPrefix(log, 1, "ready 1") {
		t.Errorf("%q", log)
	}
	wantReplies(t, w, protocol.Visible())
	c.answered(1, anoint(t, c))
	wantLog(t, r, "activation 1 forge-anoint") // Return is taken again
}

func TestWithoutATokenInTimeTheLaunchGoesAhead(t *testing.T) {
	c, r := newTestCore()
	c.indexed(appIndex(t))
	openApps(t, c, 1, "HOME=/")
	c.answered(1, anoint(t, c))
	r.take()
	at, _ := c.deadline()
	c.tick(at.Add(-time.Millisecond))
	wantLog(t, r)
	c.tick(at)
	if log := r.take(); !hasPrefix(log, 0, "toast 1 ") {
		t.Errorf("it launched (and failed): %q", log)
	}
	if _, ok := c.deadline(); ok {
		t.Error("still waiting")
	}
}

func TestALaunchClosesTheSessionAndCountsForFrecency(t *testing.T) {
	dir := t.TempDir()
	c, r := newTestCore()
	c.apps.frecencyPath = filepath.Join(dir, "state/frecency.json")
	c.indexed(appIndex(t))
	w := openApps(t, c, 1, launchEnv(t, dir)...)
	c.answered(1, anoint(t, c))
	c.activated(1, "tok")
	if log := r.take(); !slices.Contains(log, "hide 1") {
		t.Errorf("%q", log)
	}
	wantReplies(t, w, protocol.Visible(), result(protocol.Closed))
	if c.apps.frecency.Boost("forge-anoint.desktop", apps.Now()) == 0 {
		t.Error("the launch did not count")
	}
	if heard := recorded(t, dir); !strings.Contains(heard, "token tok\n") || !strings.Contains(heard, "arg --vessel\n") {
		t.Errorf("%q", heard)
	}
	saved, err := apps.LoadFrecency(c.apps.frecencyPath)
	if err != nil || saved.Boost("forge-anoint.desktop", apps.Now()) == 0 {
		t.Errorf("frecency was not kept: %v", err)
	}
}

// With a user manager on the bus, the app gets a scope of its own.
func TestWithASessionBusTheAppGetsAScope(t *testing.T) {
	dir := t.TempDir()
	env := launchEnv(t, dir)
	bus := dbustest.Start(t, func(call dbus.Message) dbustest.Answer {
		switch call.Member {
		case "Get":
			items := make([]dbus.Value, len(env))
			for i, kv := range env {
				items[i] = dbus.Str(kv)
			}
			return dbustest.Return(dbus.Variant{Value: dbus.Array{Elem: "s", Items: items}})
		case "StartTransientUnit":
			return dbustest.Return(dbus.ObjectPath("/org/freedesktop/systemd1/job/7"))
		}
		return dbustest.Fail("org.freedesktop.DBus.Error.UnknownMethod", "no")
	})
	c, _ := newTestCore()
	c.apps.bus = bus.Address
	c.indexed(appIndex(t))
	w := openApps(t, c, 1, "HOME=/")
	c.answered(1, anoint(t, c))
	c.activated(1, "tok")
	wantReplies(t, w, protocol.Visible(), result(protocol.Closed))
	unit := bus.WaitFor(t, "StartTransientUnit")
	if name, _ := unit.Body[0].(dbus.Str); !strings.HasPrefix(string(name), "app-logis-forge\\x2danoint-") {
		t.Errorf("unit %q", name)
	}
	if heard := recorded(t, dir); !strings.Contains(heard, "token tok\n") {
		t.Errorf("%q", heard)
	}
}

// A resolver that hands out no tokens: the launch goes ahead at once.
type tokenless struct{ r *recorder }

func (n tokenless) Accepts(m protocol.Mode) error { return n.r.Accepts(m) }
func (n tokenless) Show(s Session)                { n.r.Show(s) }
func (n tokenless) Step(id SessionID, s provider.Step) {
	n.r.Step(id, s)
}
func (n tokenless) End(id SessionID)  { n.r.End(id) }
func (n tokenless) Hide(id SessionID) { n.r.Hide(id) }

func TestWithoutAnActivatorTheAppIsLaunchedAtOnceWithoutAToken(t *testing.T) {
	dir := t.TempDir()
	r := &recorder{}
	c := newCore(tokenless{r}, pid)
	c.indexed(appIndex(t))
	w := openApps(t, c, 1, launchEnv(t, dir)...)
	c.answered(1, anoint(t, c))
	wantReplies(t, w, protocol.Visible(), result(protocol.Closed))
	if heard := recorded(t, dir); !strings.Contains(heard, "token \n") {
		t.Errorf("%q", heard)
	}
}

// terminalRow answers the fixture terminal app, Cogitator shell.
func terminalRow(c *core) Answer {
	i := slices.IndexFunc(c.apps.index.Apps(), func(a apps.App) bool { return a.ID == "forge-terminal.desktop" })
	return Answer{Kind: AnswerItem, Index: i}
}

func TestATerminalAppRunsBehindTheConfiguredTerminalOrTERMINAL(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured []string
		env        []string
		toast      string
	}{
		{"$TERMINAL", nil, []string{"TERMINAL=forge-term"}, "toast 1 Cogitator shell cannot be summoned: no program 'forge-term'"},
		{"configured", []string{"forge-glass", "-e"}, []string{"TERMINAL=forge-term"},
			"toast 1 Cogitator shell cannot be summoned: no program 'forge-glass'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, r := newTestCore()
			c.configure(config.Config{Terminal: tc.configured})
			c.indexed(appIndex(t))
			openApps(t, c, 1, append([]string{"HOME=/"}, tc.env...)...)
			r.take()
			c.answered(1, terminalRow(c))
			c.activated(1, "")
			if log := r.take(); !slices.ContainsFunc(log, func(l string) bool { return strings.HasPrefix(l, tc.toast) }) {
				t.Errorf("%q", log)
			}
		})
	}
}

// Without a configured terminal or $TERMINAL, a terminal app runs in a
// known terminal found on the client's PATH (a stub foot here, which takes
// the command directly).
func TestATerminalAppRunsInATerminalFoundOnTheClientsPath(t *testing.T) {
	dir, bin := t.TempDir(), t.TempDir()
	if err := os.Symlink(filepath.Join(appFixtures(t), "bin", "anoint"), filepath.Join(bin, "foot")); err != nil {
		t.Fatal(err)
	}
	c := newCore(tokenless{&recorder{}}, pid)
	c.indexed(appIndex(t))
	w := openApps(t, c, 1, "PATH="+bin+":/usr/bin:/bin", "ANOINT_RECORD="+filepath.Join(dir, "record"), "HOME=/")
	c.answered(1, terminalRow(c))
	wantReplies(t, w, protocol.Visible(), result(protocol.Closed))
	if heard := recorded(t, dir); !strings.Contains(heard, "arg anoint\narg --terminal\ntoken") {
		t.Errorf("%q", heard)
	}
}

// With no terminal at all, the toast and the log say how to name one in
// the config file the daemon reads.
func TestWithoutAnyTerminalTheToastAndTheLogSayHowToNameOne(t *testing.T) {
	c, r := newTestCore()
	var logged []string
	c.logf = func(m string) { logged = append(logged, m) }
	c.apps.config = "/home/adept/.config/logis/logis.jsonc"
	c.indexed(appIndex(t))
	openApps(t, c, 1, "HOME=/", "PATH="+t.TempDir())
	r.take()
	c.answered(1, terminalRow(c))
	const want = `Cogitator shell cannot be summoned: it must run in a terminal, and no terminal was found. ` +
		`Set "terminal" in /home/adept/.config/logis/logis.jsonc, e.g. "terminal": ["foot", "-e"]`
	if log := r.take(); !slices.Contains(log, "toast 1 "+want) {
		t.Errorf("%q", log)
	}
	if !slices.Contains(logged, want) {
		t.Errorf("logged %q", logged)
	}
}

// A token for a session that is gone launches nothing.
func TestASessionGoneBeforeItsTokenLaunchesNothing(t *testing.T) {
	dir := t.TempDir()
	c, r := newTestCore()
	c.indexed(appIndex(t))
	openApps(t, c, 1, launchEnv(t, dir)...)
	c.answered(1, anoint(t, c))
	c.disconnect(1)
	r.take()
	c.activated(1, "tok")
	wantLog(t, r)
	if _, ok := c.deadline(); ok {
		t.Error("a gone session's launch still waits")
	}
	if _, err := os.Stat(filepath.Join(dir, "record")); err == nil {
		t.Error("the app ran")
	}
}

// Shift+Return on an app with actions: its actions stage, in the same
// session; Return there launches the action.
func TestShiftReturnOpensTheActionsStage(t *testing.T) {
	c, r := newTestCore()
	c.indexed(appIndex(t))
	openApps(t, c, 1)
	r.take()
	c.answered(1, Answer{Kind: AnswerDescend, Index: anointRow(t, c)})
	wantLog(t, r, "stage 1 [Anoint,Purge,Bless]")
	c.answered(1, Answer{Kind: AnswerItem, Index: 1})
	wantLog(t, r, "activation 1 forge-anoint")
}
