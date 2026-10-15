package client

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// asDaemon makes this test binary the daemon a client starts: `daemon
// --detach [--idle-timeout SECONDS]`, answering by script.
const asDaemon = "LOGIS_CLIENT_TEST_DAEMON"

func TestMain(m *testing.M) {
	if os.Getenv(asDaemon) == "1" {
		os.Exit(fakeDaemon(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeDaemon(args []string) int {
	if len(args) < 2 || args[0] != "daemon" || args[1] != "--detach" {
		fmt.Fprintf(os.Stderr, "logis: the fake daemon got %q\n", args)
		return 2
	}
	var idle time.Duration
	if len(args) == 4 && args[2] == "--idle-timeout" {
		idle, _ = time.ParseDuration(args[3] + "s")
	}
	return daemon.Run(context.Background(), daemon.Options{
		RuntimeDir:  os.Getenv("XDG_RUNTIME_DIR"),
		IdleTimeout: idle,
		Detach:      true,
		Resolver:    func(h daemon.Host) (daemon.Resolver, error) { return daemon.NewScripted(h), nil },
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	})
}

// scratch is a runtime directory with a short path (sockets need one).
func scratch(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "logis-c-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// script writes an executable shell script standing in for logis.
func script(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-logis")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantError(t *testing.T, err error, needle string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), needle) {
		t.Errorf("error %v, want one containing %q", err, needle)
	}
}

func TestWithoutARuntimeDirThereIsNoDaemonToReach(t *testing.T) {
	for _, dir := range []string{"", "run/user"} {
		_, err := Connect(Options{RuntimeDir: dir})
		wantError(t, err, "the machine spirit has no dwelling: XDG_RUNTIME_DIR is not set or not absolute")
	}
}

// A client starts the daemon when none answers; later clients reuse it.
// The daemon outlives its starter and logs to the runtime directory.
func TestAClientStartsTheDaemonWhenNoneAnswersAndLaterClientsReuseIt(t *testing.T) {
	dir := scratch(t)
	t.Setenv(asDaemon, "1")
	o := Options{RuntimeDir: dir, Exe: os.Args[0], DaemonArgs: []string{"--idle-timeout", "0.3"}}
	first, err := Connect(o)
	if err != nil {
		t.Fatal(err)
	}
	pid := first.Daemon
	if err := first.Send(protocol.Open(protocol.ModeDmenu, nil)); err != nil {
		t.Fatal(err)
	}
	if err := first.Send(protocol.Items([]protocol.Item{{Text: ">x", Raw: []byte(">x\xff")}})); err != nil {
		t.Fatal(err)
	}
	if err := first.Send(protocol.End()); err != nil {
		t.Fatal(err)
	}
	outcome, err := first.Await()
	if err != nil || outcome.Kind != protocol.Selected || string(outcome.Item.Raw) != ">x\xff" {
		t.Fatalf("outcome %+v, %v", outcome, err)
	}
	_ = first.Close()

	second, err := Connect(o)
	if err != nil {
		t.Fatal(err)
	}
	if second.Daemon != pid {
		t.Errorf("a second daemon was started: %d, then %d", pid, second.Daemon)
	}
	_ = second.Close()
	// Idle, it leaves; its diagnostics went to the runtime directory.
	log := filepath.Join(dir, "logis.log")
	deadline := time.Now().Add(5 * time.Second)
	for {
		text, _ := os.ReadFile(log)
		if strings.Contains(string(text), "returns to rest") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the log says %q", text)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Ported from Rust tests/daemon.rs a_client_whose_daemon_cannot_start_exits_2_with_the_reason.
func TestADaemonThatCannotStartIsReportedWithItsReason(t *testing.T) {
	dir := scratch(t)
	exe := script(t, dir, `echo "logis: the noosphere is silent: no WAYLAND_DISPLAY" >&2; exit 2`)
	_, err := Connect(Options{RuntimeDir: dir, Exe: exe})
	wantError(t, err, "the machine spirit could not be roused: the noosphere is silent: no WAYLAND_DISPLAY")
}

func TestADaemonThatDiesSilentlyIsReportedWithItsStatus(t *testing.T) {
	dir := scratch(t)
	exe := script(t, dir, `exit 3`)
	_, err := Connect(Options{RuntimeDir: dir, Exe: exe})
	wantError(t, err, "the machine spirit could not be roused (exit status 3)")
}

// The daemon gets the runtime directory the client resolved, and the
// arguments the client passes on.
func TestTheStartedDaemonGetsTheClientsRuntimeDirAndArguments(t *testing.T) {
	dir := scratch(t)
	exe := script(t, dir, `echo "logis: $XDG_RUNTIME_DIR $*" >&2; exit 2`)
	_, err := Connect(Options{RuntimeDir: dir, Exe: exe, DaemonArgs: []string{"--config", "/x"}})
	wantError(t, err, "roused: "+dir+" daemon --detach --config /x")
}

// A daemon that never reports readiness is given up on, and killed.
func TestADaemonThatNeverWakesIsGivenUpOn(t *testing.T) {
	dir := scratch(t)
	exe := script(t, dir, `exec sleep 30`)
	start := time.Now()
	_, err := Connect(Options{RuntimeDir: dir, Exe: exe, Patience: 200 * time.Millisecond})
	wantError(t, err, "the machine spirit did not wake within 0.2 s")
	if elapsed := time.Since(start); testing.Short() {
		t.Logf("gave up after %v; timing: the 2 s limit is a wall-clock check, not under -short", elapsed)
	} else if elapsed > 2*time.Second {
		t.Errorf("gave up after %v", elapsed)
	}
}

// listen serves the runtime directory's socket with handle per connection.
func listen(t *testing.T, dir string, handle func(net.Conn)) {
	t.Helper()
	ln, err := net.Listen("unix", filepath.Join(dir, protocol.SocketName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			handle(c)
		}
	}()
}

// Ported from a_caller_the_daemon_refuses_exits_2_with_the_daemons_reason.
func TestADaemonsRefusalIsTheClientsError(t *testing.T) {
	dir := scratch(t)
	listen(t, dir, func(c net.Conn) {
		defer func() { _ = c.Close() }()
		var hello map[string]any
		_ = protocol.ReadFrame(c, &hello)
		_ = protocol.WriteFrame(c, protocol.Error("this machine spirit speaks version 1: restart the daemon"))
	})
	_, err := Connect(Options{RuntimeDir: dir, Exe: "/nonexistent"})
	wantError(t, err, "restart the daemon")
}

// A daemon leaving as the client arrives is retried, but not for ever.
func TestADaemonThatKeepsHangingUpIsGivenUpOn(t *testing.T) {
	dir := scratch(t)
	var hangUps atomic.Int32
	listen(t, dir, func(c net.Conn) {
		hangUps.Add(1)
		_ = c.Close()
	})
	_, err := Connect(Options{RuntimeDir: dir, Exe: "/nonexistent"})
	wantError(t, err, "hangs up before greeting; if systemd starts it, see journalctl --user -u logis")
	if n := hangUps.Load(); n != 4 {
		t.Errorf("%d hang-ups, want 4", n)
	}
}

func TestADaemonSpeakingOutOfTurnIsAnError(t *testing.T) {
	dir := scratch(t)
	listen(t, dir, func(c net.Conn) {
		var hello map[string]any
		_ = protocol.ReadFrame(c, &hello)
		_ = protocol.WriteFrame(c, protocol.Visible())
	})
	_, err := Connect(Options{RuntimeDir: dir, Exe: "/nonexistent"})
	wantError(t, err, "the machine spirit spoke out of turn: 'visible'")
}

// Await reads through queued and visible to the outcome, the daemon's error,
// or its silence.
func TestAwaitEndsWithTheOutcomeTheErrorOrTheSilence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replies []protocol.Reply
		want    string
	}{
		{"outcome", []protocol.Reply{protocol.Queued(), protocol.Visible(),
			protocol.Result(protocol.Outcome{Kind: protocol.Dismissed})}, ""},
		{"error", []protocol.Reply{protocol.Queued(), protocol.Error("the glass cracked")}, "the glass cracked"},
		{"silence", []protocol.Reply{protocol.Visible()}, "the machine spirit fell silent before answering"},
		{"welcome", []protocol.Reply{protocol.Welcome(1)}, "spoke out of turn: 'welcome'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ours, theirs := net.Pipe()
			go func() {
				for _, r := range tc.replies {
					_ = protocol.WriteFrame(theirs, r)
				}
				_ = theirs.Close()
			}()
			outcome, err := newConn(ours).Await()
			if tc.want == "" {
				if err != nil || outcome.Kind != protocol.Dismissed {
					t.Errorf("%+v, %v", outcome, err)
				}
				return
			}
			wantError(t, err, tc.want)
		})
	}
}
