package main

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/version"
)

// The caller contract (README "Caller contract") at the process boundary,
// ported from the Rust tests it names that no other Go test held at the
// same boundary: the built binary, a real daemon over its socket in a
// scratch runtime directory, the scripted resolver standing in for the
// window (it picks the first item whose shown text starts with '>', or
// dismisses). No compositor is needed.

// caller is a `logis --dmenu` client started with its stdin held open, so
// its session stays visible until something else ends it.
type caller struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout, stderr strings.Builder
	exited         chan struct{}
}

func (r *rig) startCaller(lines string) *caller {
	r.t.Helper()
	c := &caller{cmd: r.logis("--dmenu"), exited: make(chan struct{})}
	c.cmd.Stdout, c.cmd.Stderr = &c.stdout, &c.stderr
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		r.t.Fatal(err)
	}
	c.stdin = stdin
	if err := c.cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	go func() { _ = c.cmd.Wait(); close(c.exited) }()
	r.t.Cleanup(func() {
		_ = c.cmd.Process.Kill()
		<-c.exited
	})
	if _, err := io.WriteString(stdin, lines); err != nil {
		r.t.Fatal(err)
	}
	return c
}

// ended waits for the caller's exit and returns how it ended.
func (c *caller) ended(t *testing.T) ran {
	t.Helper()
	select {
	case <-c.exited:
	case <-time.After(patience):
		t.Fatal("the caller never ended")
	}
	return ran{c.cmd.ProcessState.ExitCode(), c.stdout.String(), c.stderr.String()}
}

func (c *caller) running() bool {
	select {
	case <-c.exited:
		return false
	default:
		return true
	}
}

// Ported from Rust tests/cli.rs version_flag_prints_name_and_version_and_succeeds.
func TestVersionFlagPrintsNameAndVersionAndSucceeds(t *testing.T) {
	r := newRig(t)
	for _, rune := range []string{"--version", "-V"} {
		got := r.runClient(r.logis(rune), nil)
		if got.code != 0 || got.stdout != "logis "+version.Version+"\n" || got.stderr != "" {
			t.Errorf("%s: %+v", rune, got)
		}
	}
}

// Ported from Rust tests/cli.rs unknown_argument_exits_2_naming_it.
func TestUnknownArgumentExits2NamingIt(t *testing.T) {
	r := newRig(t)
	got := r.runClient(r.logis("--heresy"), nil)
	if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, "--heresy") {
		t.Fatalf("%+v", got)
	}
}

// Ported from Rust tests/cli.rs daemon_detach_exits_0_at_once_when_a_daemon_already_answers.
func TestDaemonDetachExits0AtOnceWhenADaemonAlreadyAnswers(t *testing.T) {
	r := newRig(t)
	r.start("--detach")
	since := time.Now()
	got := r.runClient(r.logis("daemon", "--detach"), nil)
	if got.code != 0 || !strings.Contains(got.stdout, "already listens") {
		t.Fatalf("%+v", got)
	}
	if took := time.Since(since); testing.Short() {
		t.Logf("took %v; timing: \"at once\" (under %v) is a wall-clock check, not under -short", took, patience)
	} else if took >= patience {
		t.Fatalf("took %v", took)
	}
}

// Ported from Rust tests/daemon.rs the_original_bytes_of_the_chosen_item_are_returned_exactly.
func TestTheOriginalBytesOfTheChosenItemAreReturnedExactly(t *testing.T) {
	r := dmenuRig(t)
	r.dmenuRun([]byte("plain\n>\xff\xfe not utf-8\r\n\tlast")).printed(t, ">\xff\xfe not utf-8\r\n")
}

// Ported from Rust tests/daemon.rs a_replaced_dmenu_caller_exits_1_with_empty_stdout.
func TestAReplacedDmenuCallerExits1WithEmptyStdout(t *testing.T) {
	r := newRig(t)
	r.start()
	c := r.startCaller(">never chosen\n")
	r.waitForAVisibleSession()
	apps := r.connect()
	if err := apps.Send(protocol.Open(protocol.ModeApps, nil)); err != nil {
		t.Fatal(err)
	}
	if state, err := apps.Recv(); err != nil || state == nil || state.Type != protocol.TypeVisible {
		t.Fatalf("%+v, %v", state, err)
	}
	if got := c.ended(t); got.code != 1 || got.stdout != "" {
		t.Fatalf("%+v", got)
	}
}

// Ported from Rust tests/daemon.rs a_dmenu_caller_waits_in_the_queue_for_its_turn.
func TestADmenuCallerWaitsInTheQueueForItsTurn(t *testing.T) {
	r := newRig(t)
	r.start()
	visible := r.connect()
	if err := visible.Send(protocol.Open(protocol.ModeDmenu, nil)); err != nil {
		t.Fatal(err)
	}
	if state, err := visible.Recv(); err != nil || state == nil || state.Type != protocol.TypeVisible {
		t.Fatalf("%+v, %v", state, err)
	}
	waiting := r.startCaller(">mine\n")
	_ = waiting.stdin.Close()
	// Whether or not it has reached the daemon yet, it cannot answer before
	// the visible session ends. (Strict queue order: internal/daemon.)
	if !waiting.running() {
		t.Fatalf("it did not wait its turn: %+v", waiting.ended(t))
	}
	theirs := []protocol.Item{{Text: ">theirs", Raw: []byte(">theirs")}}
	for _, req := range []protocol.Request{protocol.Items(theirs), protocol.End()} {
		if err := visible.Send(req); err != nil {
			t.Fatal(err)
		}
	}
	if outcome, err := visible.Await(); err != nil || outcome.Kind != protocol.Selected || outcome.Item.Text != ">theirs" {
		t.Fatalf("%+v, %v", outcome, err)
	}
	waiting.ended(t).printed(t, ">mine\n")
}

// Ported from Rust tests/daemon.rs a_dmenu_caller_whose_daemon_dies_exits_2_with_empty_stdout,
// the caller's half: Rust's daemon dies of a lost compositor, which
// TestE2ELosingTheCompositorEndsTheDaemonWithExit2 holds in a nested niri;
// here it is killed.
func TestADmenuCallerWhoseDaemonDiesExits2WithEmptyStdout(t *testing.T) {
	r := newRig(t)
	d := r.start()
	c := r.startCaller("never chosen\n")
	r.waitForAVisibleSession()
	if err := d.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	got := c.ended(t)
	if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, "fell silent") {
		t.Fatalf("%+v", got)
	}
}

// Ported from Rust tests/daemon.rs a_caller_the_daemon_refuses_exits_2_with_the_daemons_reason:
// a daemon of another version, left running across an upgrade, reads the
// greeting and refuses it.
func TestACallerTheDaemonRefusesExits2WithTheDaemonsReason(t *testing.T) {
	r := newRig(t)
	listener, err := net.Listen("unix", r.socket())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	refused := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			refused <- err
			return
		}
		defer func() { _ = conn.Close() }()
		var hello protocol.Request
		if err := protocol.ReadRequest(conn, &hello); err != nil {
			refused <- err
			return
		}
		refused <- protocol.WriteFrame(conn, protocol.Error("this machine spirit speaks version 1: restart the daemon"))
	}()
	got := r.dmenuRun([]byte(">x\n"))
	if err := <-refused; err != nil {
		t.Fatal(err)
	}
	if got.code != 2 || got.stdout != "" || !strings.Contains(got.stderr, "restart the daemon") {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(r.socket()); err != nil {
		t.Fatalf("the refusing daemon's socket: %v", err)
	}
}

// Ported from Rust tests/daemon.rs an_auto_started_daemon_outlives_its_starter_and_logs_to_the_runtime_dir:
// the daemon a `logis --dmenu` caller starts serves on after its starter
// has gone, and its later diagnostics (Rust: the compositor's loss; here,
// with no compositor, its idle end) go to $XDG_RUNTIME_DIR/logis.log.
func TestAnAutoStartedDaemonOutlivesItsStarterAndLogsToTheRuntimeDir(t *testing.T) {
	r := newRig(t)
	r.stopsItsDaemon()
	if err := os.MkdirAll(filepath.Dir(r.configFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.configFile(), []byte(`{"idle-timeout": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r.dmenuRun([]byte(">x\n")).printed(t, ">x\n")
	c, err := client.Connect(client.Options{RuntimeDir: r.dir, Exe: "/nonexistent", Patience: time.Second})
	if err != nil {
		t.Fatalf("the daemon did not outlive its starter: %v", err)
	}
	pid := c.Daemon
	_ = c.Close()
	waitUntil(t, "the daemon to idle out", func() bool { return !alive(pid) })
	log, err := os.ReadFile(filepath.Join(r.dir, "logis.log"))
	if err != nil || !strings.Contains(string(log), "no summons came for 2s") {
		t.Fatalf("log %q, %v", log, err)
	}
}
