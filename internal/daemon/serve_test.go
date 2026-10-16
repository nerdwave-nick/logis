package daemon

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Dmenu sessions queue, and each caller gets its own answer in order.
func TestDmenuSessionsQueueAndEachCallerGetsItsOwnAnswerInOrder(t *testing.T) {
	r := newRig(t)
	r.start(0)
	first, state := r.open(protocol.ModeDmenu)
	if state != protocol.TypeVisible {
		t.Fatalf("first is %s", state)
	}
	second, state := r.open(protocol.ModeDmenu)
	if state != protocol.TypeQueued {
		t.Fatalf("second is %s", state)
	}
	third, state := r.open(protocol.ModeDmenu)
	if state != protocol.TypeQueued {
		t.Fatalf("third is %s", state)
	}
	third.itemsThenEnd(">three")
	second.itemsThenEnd("x", ">two")

	first.itemsThenEnd(">one")
	if got := first.outcome(); string(got.Item.Raw) != ">one" {
		t.Errorf("first chose %+v", got)
	}
	if got := second.recvType(); got != protocol.TypeVisible {
		t.Errorf("second is %s", got)
	}
	if got := second.outcome(); got.Index != 1 || got.Item.Text != ">two" {
		t.Errorf("second chose %+v", got)
	}
	if got := third.recvType(); got != protocol.TypeVisible {
		t.Errorf("third is %s", got)
	}
	if got := third.outcome(); got.Item.Text != ">three" {
		t.Errorf("third chose %+v", got)
	}
}

// Ported from an_interactive_session_replaces_the_visible_one.
func TestOverTheSocketAnInteractiveSessionReplacesTheVisibleOne(t *testing.T) {
	r := newRig(t)
	r.start(0)
	dmenuSession, _ := r.open(protocol.ModeDmenu)
	apps, state := r.open(protocol.ModeApps)
	if state != protocol.TypeVisible {
		t.Fatalf("apps is %s", state)
	}
	if got := dmenuSession.outcome(); got.Kind != protocol.Replaced {
		t.Errorf("the dmenu session got %+v", got)
	}
	servitor, state := r.open(protocol.ModeServitor)
	if state != protocol.TypeVisible {
		t.Fatalf("servitor is %s", state)
	}
	if got := apps.outcome(); got.Kind != protocol.Replaced {
		t.Errorf("the apps session got %+v", got)
	}
	servitor.send(protocol.Cancel())
	if got := servitor.outcome(); got.Kind != protocol.Cancelled {
		t.Errorf("the servitor session got %+v", got)
	}
}

// Ported from a_client_disconnect_cancels_its_session_and_the_next_one_is_shown.
func TestAClientDisconnectCancelsItsSessionAndTheNextOneIsShown(t *testing.T) {
	r := newRig(t)
	r.start(0)
	caller, _ := r.open(protocol.ModeDmenu)
	queued, state := r.open(protocol.ModeDmenu)
	if state != protocol.TypeQueued {
		t.Fatalf("second is %s", state)
	}
	_ = caller.conn.Close()
	if got := queued.recvType(); got != protocol.TypeVisible {
		t.Errorf("the queued session is %s", got)
	}
	_ = queued.conn.Close()
	r.waitUntilNoSessionRemains()
}

// Ported from a_greeting_in_another_protocol_version_is_refused.
func TestOverTheSocketAGreetingInAnotherVersionIsRefused(t *testing.T) {
	r := newRig(t)
	r.start(0)
	c := r.connect()
	c.send(protocol.Request{Type: protocol.TypeHello, Version: 999})
	reply := c.recv()
	if reply == nil || reply.Type != protocol.TypeError || !strings.Contains(reply.Message, "999") {
		t.Errorf("reply %+v", reply)
	}
	if c.recv() != nil {
		t.Error("the connection stays open")
	}
}

// A frame that is not a request ends the connection with the reason.
func TestAGarbledFrameEndsTheConnectionWithTheReason(t *testing.T) {
	r := newRig(t)
	r.start(0)
	c := r.connect()
	c.greet()
	if _, err := c.conn.Write([]byte("\x00\x00\x00\x03{x}")); err != nil {
		t.Fatal(err)
	}
	reply := c.recv()
	if reply == nil || !strings.HasPrefix(reply.Message, "malformed message: ") {
		t.Errorf("reply %+v", reply)
	}
}

// An idle daemon exits after the timeout, and its socket is not left
// behind.
func TestAnIdleDaemonExitsAfterTheTimeoutAndRemovesItsSocket(t *testing.T) {
	r := newRig(t)
	start := time.Now()
	d := r.start(50 * time.Millisecond)
	if status := d.exit(t); status != 0 {
		t.Errorf("exit %d, stderr %q", status, d.stderr.String())
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("left after %v", elapsed)
	}
	if _, err := os.Stat(r.socket()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket was left behind: %v", err)
	}
	if !strings.Contains(d.stderr.String(), "returns to rest") {
		t.Errorf("log %q", d.stderr.String())
	}
}

// Ported from the_idle_timeout_waits_for_the_last_client_to_leave and
// with_an_idle_timeout_the_daemon_exits_once_no_client_is_connected.
func TestTheIdleTimeoutWaitsForTheLastClientToLeave(t *testing.T) {
	r := newRig(t)
	const idle = 100 * time.Millisecond
	d := r.start(idle)
	c := r.connect()
	c.greet() // the daemon has seen the client, not only the kernel
	// Timing is the behaviour under test: three timeouts must pass.
	time.Sleep(3 * idle)
	if d.exited() {
		t.Fatal("it left while a client was connected")
	}
	left := time.Now()
	_ = c.conn.Close()
	if status := d.exit(t); status != 0 {
		t.Errorf("exit %d", status)
	}
	if elapsed := time.Since(left); elapsed < idle {
		t.Errorf("left %v after the client", elapsed)
	}
}

// Ported from without_an_idle_timeout_the_daemon_stays.
func TestWithoutAnIdleTimeoutTheDaemonStays(t *testing.T) {
	r := newRig(t)
	d := r.start(0)
	// Timing is the behaviour under test: nothing may end it in this time.
	time.Sleep(300 * time.Millisecond)
	if d.exited() {
		t.Error("it left")
	}
}

// A component (the Wayland connection, for one) ends the daemon at once with
// its status and reason; its clients see the end of the stream.
func TestAComponentCanEndTheDaemonWithAStatus(t *testing.T) {
	r := newRig(t)
	d := r.start(0)
	caller, _ := r.open(protocol.ModeDmenu)
	d.host.Exit(2, "the noosphere fell silent")
	if status := d.exit(t); status != 2 {
		t.Errorf("exit %d", status)
	}
	if !strings.Contains(d.stderr.String(), "logis: the noosphere fell silent\n") {
		t.Errorf("log %q", d.stderr.String())
	}
	if reply := caller.recv(); reply != nil {
		t.Errorf("the caller got %+v", reply)
	}
	if _, err := os.Stat(r.socket()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket was left behind: %v", err)
	}
	d.host.Exit(3, "too late") // never blocks once the daemon has stopped
	d.host.Answer(1, Answer{Kind: AnswerDismissed})
}

// A resolver that cannot answer a session reports it, and the next is shown.
func TestAFailedSessionReachesItsClientOverTheSocket(t *testing.T) {
	r := newRig(t)
	d := r.start(0)
	failing, _ := r.open(protocol.ModeDmenu)
	waiting, _ := r.open(protocol.ModeDmenu)
	d.host.Fail(1, "the glass cracked")
	if reply := failing.recv(); reply == nil || reply.Message != "the glass cracked" {
		t.Errorf("reply %+v", reply)
	}
	if got := waiting.recvType(); got != protocol.TypeVisible {
		t.Errorf("the next is %s", got)
	}
}

// Ported from a_stale_socket_is_cleared_by_the_next_daemon.
func TestAStaleSocketIsClearedByTheNextDaemon(t *testing.T) {
	r := newRig(t)
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: r.socket(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	_ = stale.Close()
	r.start(0)
	if _, state := r.open(protocol.ModeDmenu); state != protocol.TypeVisible {
		t.Errorf("state %s", state)
	}
}

// Ported from a_second_daemon_defers_to_the_one_already_listening.
func TestASecondDaemonDefersToTheOneAlreadyListening(t *testing.T) {
	r := newRig(t)
	r.start(0)
	pid := r.connect().greet().PID
	second := r.run(Options{})
	if status := second.exit(t); status != 0 {
		t.Errorf("exit %d, stderr %q", status, second.stderr.String())
	}
	if !strings.Contains(second.stdout.String(), "another machine spirit already listens") {
		t.Errorf("stdout %q", second.stdout.String())
	}
	if again := r.connect().greet().PID; again != pid {
		t.Errorf("pid %d, want %d", again, pid)
	}
}

// A daemon that holds the lock but never answers is reported, not waited on
// for ever.
func TestADaemonHoldingTheLockWithoutAnsweringIsAnError(t *testing.T) {
	r := newRig(t)
	lock, err := os.OpenFile(filepath.Join(r.dir, "logis.lock"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	d := r.run(Options{})
	if status := d.exit(t); status != 2 {
		t.Errorf("exit %d", status)
	}
	if !strings.Contains(d.stderr.String(), "holds "+lock.Name()+" but does not answer") {
		t.Errorf("stderr %q", d.stderr.String())
	}
}

// Without an absolute runtime directory there is no socket to claim.
func TestADaemonWithoutARuntimeDirFails(t *testing.T) {
	d := (&rig{t: t}).run(Options{})
	if status := d.exit(t); status != 2 {
		t.Errorf("exit %d", status)
	}
	want := "logis: the machine spirit has no dwelling: XDG_RUNTIME_DIR is not set or not absolute\n"
	if got := d.stderr.String(); got != want {
		t.Errorf("stderr %q", got)
	}
}

// A resolver that cannot start ends the daemon with its reason, and leaves
// no socket behind.
func TestAResolverThatCannotStartEndsTheDaemon(t *testing.T) {
	r := newRig(t)
	d := r.run(Options{Resolver: func(Host) (Resolver, error) {
		return nil, errors.New("the noosphere is silent")
	}})
	if status := d.exit(t); status != 2 {
		t.Errorf("exit %d", status)
	}
	if got := d.stderr.String(); got != "logis: the noosphere is silent\n" {
		t.Errorf("stderr %q", got)
	}
	if d.stdout.String() != "" {
		t.Errorf("it announced %q", d.stdout.String())
	}
	if _, err := os.Stat(r.socket()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket was left behind: %v", err)
	}
}

// Windowless listens but refuses every session with its reason.
func TestWindowlessListensButRefusesEverySession(t *testing.T) {
	r := newRig(t)
	d := r.run(Options{Resolver: func(Host) (Resolver, error) { return Windowless{}, nil }})
	waitUntil(t, "the daemon to listen", func() bool { return d.stdout.String() != "" })
	c := r.connect()
	c.greet()
	c.send(protocol.Open(protocol.ModeDmenu, nil))
	if reply := c.recv(); reply == nil || !strings.Contains(reply.Message, "no window") {
		t.Errorf("reply %+v", reply)
	}
}

// The daemon's start-up order (part of the caller contract): the runtime
// dir, then the session's preflight, then the config, then the socket. A
// session that cannot host the window is named before a broken config, and
// a second daemon in such a session does not defer.
func TestThePreflightComesAfterTheRuntimeDirAndBeforeTheConfigAndTheSocket(t *testing.T) {
	silent := func() error { return errors.New("the noosphere is silent") }
	d := (&rig{t: t}).run(Options{Preflight: func() error { t.Error("preflight without a dwelling"); return nil }})
	if status := d.exit(t); status != 2 || !strings.Contains(d.stderr.String(), "no dwelling") {
		t.Errorf("exit %d, stderr %q", status, d.stderr.String())
	}

	r := newRig(t)
	missing := config.NewSource(filepath.Join(r.dir, "missing.jsonc"))
	d = r.run(Options{Preflight: silent, Config: &missing})
	if status := d.exit(t); status != 2 || d.stderr.String() != "logis: the noosphere is silent\n" {
		t.Errorf("exit %d, stderr %q", status, d.stderr.String())
	}
	if _, err := os.Stat(r.socket()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the socket was taken: %v", err)
	}

	r.start(0)
	second := r.run(Options{Preflight: silent})
	if status := second.exit(t); status != 2 || second.stdout.String() != "" {
		t.Errorf("exit %d, stdout %q", status, second.stdout.String())
	}
}
