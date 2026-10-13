package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/dbus"
	"github.com/nerdwave-nick/logis/internal/dbus/dbustest"
)

// The app launcher through a real daemon (Rust tests/apps.rs), with the
// scripted resolver standing in for the window: it chooses the app whose
// name starts with '>' and hands out a stand-in token, scripted-<n>. The
// daemon reads fixture applications directories only, keeps frecency in a
// scratch state dir and talks to a fake session bus (never the real one:
// it records the scope it is asked for and refuses the user manager's
// environment, so the app keeps the client's). The launched program is the
// fixture tests/fixtures/apps/bin/anoint, which records how it was started
// and runs until its hold file is gone.

func appFixtures(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../../tests/fixtures/apps")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

type launcher struct {
	r      *rig
	dir    string
	daemon *daemonProc
	bus    *dbustest.Bus
}

// startLauncher starts a daemon whose data dirs are dataHome and dataDirs
// ("" for a missing data home and the scripted fixtures).
func startLauncher(t *testing.T, dataHome, dataDirs string) *launcher {
	t.Helper()
	r := newRig(t)
	l := &launcher{r: r, dir: t.TempDir()}
	if dataHome == "" {
		dataHome = filepath.Join(l.dir, "no-data-home")
	}
	if dataDirs == "" {
		dataDirs = filepath.Join(appFixtures(t), "scripted")
	}
	l.bus = dbustest.Start(t, func(call dbus.Message) dbustest.Answer {
		if call.Member == "StartTransientUnit" {
			return dbustest.Return(dbus.ObjectPath("/org/freedesktop/systemd1/job/7"))
		}
		return dbustest.Fail("org.freedesktop.DBus.Error.UnknownMethod", "no")
	})
	cmd := r.logis("daemon")
	cmd.Env = append(cmd.Env, "XDG_DATA_HOME="+dataHome, "XDG_DATA_DIRS="+dataDirs,
		"XDG_STATE_HOME="+filepath.Join(l.dir, "state"), "PATH=/usr/bin:/bin",
		"DBUS_SESSION_BUS_ADDRESS="+l.bus.Address)
	l.daemon = r.startCmd(cmd)
	return l
}

func (l *launcher) record() string { return filepath.Join(l.dir, "record") }
func (l *launcher) hold() string   { return filepath.Join(l.dir, "hold") }

// client is `logis args…` in the environment an app gets without the
// user manager's: the fixture program on the PATH, its record and hold.
func (l *launcher) client(args ...string) *exec.Cmd {
	l.r.t.Helper()
	if err := os.WriteFile(l.hold(), nil, 0o600); err != nil {
		l.r.t.Fatal(err)
	}
	cmd := l.r.logis(args...)
	cmd.Env = append(cmd.Env, "PATH="+filepath.Join(appFixtures(l.r.t), "bin")+":/usr/bin:/bin",
		"HOME="+l.dir, "ANOINT_RECORD="+l.record(), "ANOINT_HOLD="+l.hold(), "ANOINT_MARKER=client")
	return cmd
}

// heard is what anoint recorded, by key.
func (l *launcher) heard(t *testing.T) map[string][]string {
	t.Helper()
	waitUntil(t, "the app to record itself", func() bool { _, err := os.Stat(l.record()); return err == nil })
	text, err := os.ReadFile(l.record())
	if err != nil {
		t.Fatal(err)
	}
	heard := map[string][]string{}
	for line := range strings.Lines(string(text)) {
		k, v, _ := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
		heard[k] = append(heard[k], v)
	}
	return heard
}

// runToEnd runs a client to its end: its exit status, stdout and stderr.
func runToEnd(t *testing.T, cmd *exec.Cmd) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !asExit(err, &exit) {
		t.Fatal(err)
	}
	return cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

func asExit(err error, exit **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	*exit = e
	return ok
}

func TestTheChosenAppIsLaunchedWithItsTokenInAScopeAndCountsForFrecency(t *testing.T) {
	l := startLauncher(t, "", "")
	code, stdout, stderr := runToEnd(t, l.client("--apps"))
	if code != 0 || stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	heard := l.heard(t)
	if got := heard["arg"]; !slices.Equal(got, []string{"--rite", "--name=>Rite of Anointing"}) {
		t.Errorf("args %q", got)
	}
	if got := heard["marker"]; !slices.Equal(got, []string{"client"}) {
		t.Errorf("marker %q: not the client's environment", got)
	}
	if got := heard["token"]; len(got) != 1 || !strings.HasPrefix(got[0], "scripted-") {
		t.Errorf("token %q", got)
	}
	pid, err := strconv.Atoi(heard["pid"][0])
	if err != nil || !slices.Equal(heard["sid"], heard["pid"]) {
		t.Errorf("pid %q, sid %q: not a session of its own", heard["pid"], heard["sid"])
	}
	unit := l.bus.WaitFor(t, "StartTransientUnit")
	if name, _ := unit.Body[0].(dbus.Str); string(name) != "app-logis-forge\\x2drite-"+strconv.Itoa(pid)+".scope" {
		t.Errorf("unit %q", name)
	}
	frecency := filepath.Join(l.dir, "state/logis/frecency.json")
	waitUntil(t, "frecency to be kept", func() bool {
		b, err := os.ReadFile(frecency)
		return err == nil && strings.Contains(string(b), "forge-rite.desktop")
	})

	// The app outlives the daemon that launched it.
	_ = l.daemon.cmd.Process.Kill()
	<-l.daemon.exited
	if !alive(pid) {
		t.Fatal("the app died with the daemon")
	}
	if err := os.Remove(l.hold()); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the app to end", func() bool { return !alive(pid) })
}

// Plain logis, without arguments, is the app launcher, as in fuzzel.
func TestPlainLogisIsTheAppLauncher(t *testing.T) {
	l := startLauncher(t, "", "")
	code, stdout, stderr := runToEnd(t, l.client())
	if code != 0 || stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if got := l.heard(t)["arg"]; len(got) == 0 || got[0] != "--rite" {
		t.Errorf("args %q", got)
	}
	_ = os.Remove(l.hold())
}

func TestAnAppInstalledWhileTheDaemonRunsIsFoundByTheNextSummons(t *testing.T) {
	dataHome, nothing := t.TempDir(), t.TempDir()
	l := startLauncher(t, dataHome, nothing)
	// No marked app anywhere: a session now waits for one. The
	// applications directory does not exist yet: it is watched through its
	// ancestor, and its new subdirectory through it.
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
		cmd := l.client("--apps")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan struct{})
		go func() { _ = cmd.Wait(); close(exited) }()
		select {
		case <-exited:
		case <-time.After(500 * time.Millisecond):
			_ = cmd.Process.Kill() // our own client, still waiting
			<-exited
		}
		if cmd.ProcessState.Exited() {
			if code := cmd.ProcessState.ExitCode(); code != 0 {
				t.Fatalf("exit %d", code)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the new app was never indexed")
		}
	}
	if got := l.heard(t)["arg"]; len(got) == 0 || got[0] != "--rite" {
		t.Errorf("args %q", got)
	}
	_ = os.Remove(l.hold())
}
