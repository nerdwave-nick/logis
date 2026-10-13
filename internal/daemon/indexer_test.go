package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// A running daemon's apps (Rust src/daemon/mod.rs apps_state, index_apps):
// the index read on its own goroutine from the daemon's environment and
// read again when the applications directories change, frecency kept in
// $XDG_STATE_HOME/logis/. The Scripted resolver chooses the app whose name
// starts with the mark and hands out a stand-in token.

// appsEnv is a daemon environment: data dirs, state home, no session bus.
func appsEnv(dataHome, dataDirs, state string) func(string) string {
	return vars("XDG_DATA_HOME", dataHome, "XDG_DATA_DIRS", dataDirs, "XDG_STATE_HOME", state,
		"PATH", "/usr/bin:/bin")
}

func vars(pairs ...string) func(string) string {
	return func(name string) string {
		for i := 0; i+1 < len(pairs); i += 2 {
			if pairs[i] == name {
				return pairs[i+1]
			}
		}
		return ""
	}
}

// openAppsOver opens an app session over the socket with a client
// environment in which anoint records itself in dir.
func (r *rig) openAppsOver(dir string) *raw {
	r.t.Helper()
	c := r.connect()
	c.greet()
	open := protocol.Open(protocol.ModeApps, nil)
	for _, kv := range launchEnv(r.t, dir) {
		open.Env = append(open.Env, []byte(kv))
	}
	c.send(open)
	if got := c.recvType(); got != protocol.TypeVisible {
		r.t.Fatalf("the session is %s", got)
	}
	return c
}

func TestARunningDaemonLaunchesTheChosenAppAndKeepsItsFrecency(t *testing.T) {
	r := newRig(t)
	scratch := t.TempDir()
	state := filepath.Join(scratch, "state")
	d := r.run(Options{Getenv: appsEnv(filepath.Join(scratch, "none"),
		filepath.Join(appFixtures(t), "scripted"), state)})
	waitUntil(t, "the daemon to listen", func() bool { return strings.Contains(d.stdout.String(), "listens at") })
	c := r.openAppsOver(scratch)
	if got := c.outcome(); got.Kind != protocol.Closed {
		t.Fatalf("outcome %+v", got)
	}
	heard := recorded(t, scratch)
	if !strings.Contains(heard, "arg --rite\narg --name=>Rite of Anointing\n") || !strings.Contains(heard, "token scripted-") {
		t.Errorf("%q", heard)
	}
	d.cancel()
	d.exit(t) // waits for frecency to be kept
	if b, err := os.ReadFile(filepath.Join(state, "logis/frecency.json")); err != nil || !strings.Contains(string(b), "forge-rite.desktop") {
		t.Errorf("frecency %q, %v", b, err)
	}
}

func TestAnAppInstalledWhileTheDaemonRunsIsFound(t *testing.T) {
	r := newRig(t)
	scratch := t.TempDir()
	dataHome, nothing := filepath.Join(scratch, "data"), filepath.Join(scratch, "nothing")
	if err := os.Mkdir(nothing, 0o755); err != nil {
		t.Fatal(err)
	}
	d := r.run(Options{Getenv: appsEnv(dataHome, nothing, filepath.Join(scratch, "state"))})
	waitUntil(t, "the daemon to listen", func() bool { return strings.Contains(d.stdout.String(), "listens at") })
	// The first census (of nothing) is taken: a session shows an empty
	// list and waits.
	c := r.openAppsOver(scratch)
	c.send(protocol.Cancel())
	if got := c.outcome(); got.Kind != protocol.Cancelled {
		t.Fatalf("outcome %+v", got)
	}
	// The applications directory does not exist yet: it is watched through
	// its ancestor, and its new subdirectory through it.
	apps := filepath.Join(dataHome, "applications")
	if err := os.MkdirAll(filepath.Join(apps, "forge"), 0o755); err != nil {
		t.Fatal(err)
	}
	rite, err := os.ReadFile(filepath.Join(appFixtures(t), "scripted/applications/forge-rite.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apps, "forge/late.desktop"), rite, 0o644); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(patience); ; {
		c := r.openAppsOver(scratch)
		_ = c.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		var reply protocol.Reply
		if err := protocol.ReadFrame(c.conn, &reply); err == nil {
			if reply.Type != protocol.TypeResult || reply.Outcome.Kind != protocol.Closed {
				t.Fatalf("%+v", reply)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the new app was never indexed")
		}
		_ = c.conn.SetReadDeadline(time.Time{})
		c.send(protocol.Cancel())
		c.outcome()
	}
	if heard := recorded(t, scratch); !strings.Contains(heard, "arg --rite\n") {
		t.Errorf("%q", heard)
	}
}

// The config's terminal runs Terminal=true apps, from the start and after
// every applied edit.
func TestTheConfigsTerminalRunsTerminalAppsFromTheStartAndAfterAnEdit(t *testing.T) {
	r := newRig(t)
	scratch := t.TempDir()
	share := filepath.Join(scratch, "share")
	shell := "[Desktop Entry]\nType=Application\nName=>Cogitator shell\nTerminal=true\nExec=anoint --shell\n"
	if err := os.MkdirAll(filepath.Join(share, "applications"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(share, "applications/forge-shell.desktop"), []byte(shell), 0o644); err != nil {
		t.Fatal(err)
	}
	anoint := filepath.Join(appFixtures(t), "bin/anoint")
	glass := func(n string) string { return `{"terminal": ["` + anoint + `", "--glass=` + n + `"]}` }
	writeConfig(t, r.configFile(), glass("one"))
	source := r.named()
	d, _ := r.startConfigured(source, Options{Getenv: appsEnv(filepath.Join(scratch, "none"), share, filepath.Join(scratch, "state"))})
	summon := func(want string) {
		t.Helper()
		_ = os.Remove(filepath.Join(scratch, "record"))
		if got := r.openAppsOver(scratch).outcome(); got.Kind != protocol.Closed {
			t.Fatalf("outcome %+v", got)
		}
		if heard := recorded(t, scratch); !strings.Contains(heard, want) {
			t.Errorf("%q lacks %q", heard, want)
		}
	}
	summon("arg --glass=one\narg anoint\narg --shell\n")
	writeConfig(t, r.configFile(), glass("two"))
	waitUntil(t, "the edit to be applied", func() bool { return strings.Contains(d.stderr.String(), heeds) })
	summon("arg --glass=two\narg anoint\narg --shell\n")
}
