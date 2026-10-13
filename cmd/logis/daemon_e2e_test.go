package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// These tests run the built logis binary as the daemon over a real socket in
// a scratch runtime directory (never the real one), answering sessions with
// the scripted resolver.

const patience = 5 * time.Second

type rig struct {
	t   *testing.T
	dir string
	bin string
	// own are the daemons this test started as its children; it stops
	// them itself.
	own map[int]bool
}

func newRig(t *testing.T) *rig {
	t.Helper()
	bin := buildLogis(t)
	dir, err := os.MkdirTemp("", "logis-e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// A daemon a client starts inherits the test's environment.
	t.Setenv("LOGIS_RESOLVER", "scripted")
	return &rig{t, dir, bin, map[int]bool{}}
}

func (r *rig) socket() string { return filepath.Join(r.dir, protocol.SocketName) }

// env is this rig's environment: no inherited socket activation, no
// compositor, the scratch runtime directory.
func (r *rig) env(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES", "WAYLAND_DISPLAY", "WAYLAND_SOCKET":
		default:
			env = append(env, kv)
		}
	}
	return append(append(env, "XDG_RUNTIME_DIR="+r.dir, "LOGIS_RESOLVER=scripted"), extra...)
}

func (r *rig) logis(args ...string) *exec.Cmd {
	cmd := exec.Command(r.bin, args...)
	cmd.Env = r.env()
	return cmd
}

// daemonProc is a logis daemon this test started; killed at the end.
type daemonProc struct {
	cmd    *exec.Cmd
	stderr io.ReadCloser
	// stdoutEnded is closed once nobody holds the daemon's stdout any more.
	stdoutEnded chan struct{}
	exited      chan struct{}
}

// start starts `logis daemon ARGS` and waits for its readiness line.
func (r *rig) start(args ...string) *daemonProc {
	r.t.Helper()
	return r.startCmd(r.logis(append([]string{"daemon"}, args...)...))
}

func (r *rig) startCmd(cmd *exec.Cmd) *daemonProc {
	r.t.Helper()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	d := &daemonProc{cmd: cmd, stderr: stderr, stdoutEnded: make(chan struct{}), exited: make(chan struct{})}
	r.own[cmd.Process.Pid] = true
	r.t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-d.exited
	})
	// One goroutine owns stdout: the readiness line, then the rest.
	ready := make(chan string, 1)
	go func() {
		out := bufio.NewReader(stdout)
		line, _ := out.ReadString('\n')
		ready <- line
		_, _ = io.Copy(io.Discard, out)
		close(d.stdoutEnded)
		_ = cmd.Wait()
		close(d.exited)
	}()
	var line string
	select {
	case line = <-ready:
	case <-time.After(patience):
		r.t.Fatal("the daemon did not get ready in time")
	}
	if !strings.Contains(line, "listens at "+r.socket()) && !strings.Contains(line, "already listens") {
		<-d.stdoutEnded
		text, _ := io.ReadAll(stderr)
		r.t.Fatalf("the daemon did not get ready: %q; stderr %q", line, text)
	}
	return d
}

// exitCode waits for the daemon to exit on its own.
func (d *daemonProc) exitCode(t *testing.T) int {
	t.Helper()
	select {
	case <-d.exited:
		return d.cmd.ProcessState.ExitCode()
	case <-time.After(patience):
		t.Fatal("the daemon did not exit")
		return -1
	}
}

func (d *daemonProc) running() bool {
	select {
	case <-d.exited:
		return false
	default:
		return true
	}
}

// connect greets the daemon; a daemon it had to start is stopped at the end.
func (r *rig) connect() *client.Conn {
	r.t.Helper()
	c, err := client.Connect(client.Options{RuntimeDir: r.dir, Exe: r.bin})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() {
		_ = c.Close()
		if !r.own[c.Daemon] {
			stopDaemon(c.Daemon)
		}
	})
	return c
}

// stopDaemon ends a daemon this test did not start as its own child and
// waits until it is gone.
func stopDaemon(pid int) {
	if syscall.Kill(pid, syscall.SIGTERM) != nil {
		return
	}
	deadline := time.Now().Add(patience)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
}

// dmenu runs a dmenu session of these items to its outcome.
func (r *rig) dmenu(texts ...string) protocol.Outcome {
	r.t.Helper()
	c := r.connect()
	var items []protocol.Item
	for _, t := range texts {
		items = append(items, protocol.Item{Text: t, Raw: []byte(t)})
	}
	for _, req := range []protocol.Request{protocol.Open(protocol.ModeDmenu, nil), protocol.Items(items), protocol.End()} {
		if err := c.Send(req); err != nil {
			r.t.Fatal(err)
		}
	}
	outcome, err := c.Await()
	if err != nil {
		r.t.Fatal(err)
	}
	return outcome
}

func waitUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("waited in vain for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func gone(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

// Ported from Rust tests/daemon.rs a_client_starts_the_daemon_when_none_answers_and_later_clients_reuse_it.
func TestE2EAClientStartsTheDaemonAndLaterClientsReuseIt(t *testing.T) {
	r := newRig(t)
	if !gone(r.socket()) {
		t.Fatal("a socket before the first client")
	}
	if got := r.dmenu("first", ">second", "third"); got.Kind != protocol.Selected || got.Index != 1 {
		t.Errorf("outcome %+v", got)
	}
	pid := r.connect().Daemon
	if got := r.dmenu(">again"); string(got.Item.Raw) != ">again" {
		t.Errorf("outcome %+v", got)
	}
	if again := r.connect().Daemon; again != pid {
		t.Errorf("a second daemon was started: %d, then %d", pid, again)
	}
}

// Ported from a_dismissed_dmenu_session (the daemon's half): no marked item.
func TestE2EADmenuSessionWithoutAMarkedItemIsDismissed(t *testing.T) {
	r := newRig(t)
	if got := r.dmenu("nothing", "marked"); got.Kind != protocol.Dismissed {
		t.Errorf("outcome %+v", got)
	}
}

// Ported from an_auto_started_daemon_outlives_its_starter_and_logs_to_the_runtime_dir:
// detached, the daemon holds neither of its starter's pipes, and logs to
// logis.log.
func TestE2EADetachedDaemonLeavesItsStartersStdioAndLogsToTheRuntimeDir(t *testing.T) {
	r := newRig(t)
	d := r.start("--detach", "--idle-timeout", "0.3")
	c := r.connect()
	stderr, _ := io.ReadAll(d.stderr) // ends only once the daemon let go of it
	if len(stderr) != 0 {
		t.Errorf("stderr %q", stderr)
	}
	select {
	case <-d.stdoutEnded:
	case <-time.After(patience):
		t.Fatal("the daemon kept its starter's stdout")
	}
	if !d.running() {
		t.Fatal("the daemon left with a client connected")
	}
	_ = c.Close()
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d", code)
	}
	log, err := os.ReadFile(filepath.Join(r.dir, "logis.log"))
	if err != nil || !strings.Contains(string(log), "no summons came for 300ms") {
		t.Errorf("log %q, %v", log, err)
	}
}

// Ported from with_an_idle_timeout_the_daemon_exits_once_no_client_is_connected.
func TestE2EWithAnIdleTimeoutTheDaemonExitsOnceNoClientIsConnected(t *testing.T) {
	r := newRig(t)
	d := r.start("--idle-timeout", "0.3")
	c := r.connect()
	time.Sleep(600 * time.Millisecond) // timing is the behaviour under test
	if !d.running() {
		t.Fatal("it left while a client was connected")
	}
	_ = c.Close()
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d", code)
	}
	if !gone(r.socket()) {
		t.Error("the socket was left behind")
	}
}

// Ported from a_second_daemon_defers_to_the_one_already_listening.
func TestE2EASecondDaemonDefersToTheOneAlreadyListening(t *testing.T) {
	r := newRig(t)
	r.start()
	pid := r.connect().Daemon
	out, err := r.logis("daemon").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(string(out), "another machine spirit already listens; this one returns to rest") {
		t.Errorf("output %q", out)
	}
	if again := r.connect().Daemon; again != pid {
		t.Errorf("pid %d, want %d", again, pid)
	}
}

// `logis daemon --detach` only ensures the daemon: it returns once one
// listens, whether it started it or not.
func TestE2EDaemonDetachOnlyEnsuresTheDaemon(t *testing.T) {
	r := newRig(t)
	first := r.start("--detach")
	second := r.start("--detach")
	if code := second.exitCode(t); code != 0 {
		t.Errorf("the second exits %d", code)
	}
	if pid := r.connect().Daemon; pid != first.cmd.Process.Pid {
		t.Errorf("pid %d answered, want %d", pid, first.cmd.Process.Pid)
	}
}

// Ported from an_unknown_idle_timeout_value_is_an_error.
func TestE2EAnUnknownIdleTimeoutValueIsAnError(t *testing.T) {
	r := newRig(t)
	out, err := r.logis("daemon", "--idle-timeout", "soon").CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "'soon'") {
		t.Errorf("%v: %q", err, out)
	}
}

func TestE2EADaemonWithoutARuntimeDirExits2(t *testing.T) {
	r := newRig(t)
	cmd := r.logis("daemon")
	cmd.Env = append(cmd.Env, "XDG_RUNTIME_DIR=")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 ||
		!strings.Contains(string(out), "the machine spirit has no dwelling") {
		t.Errorf("%v: %q", err, out)
	}
}

func TestE2EAnUnknownResolverIsAnError(t *testing.T) {
	r := newRig(t)
	cmd := r.logis("daemon")
	cmd.Env = append(cmd.Env, "LOGIS_RESOLVER=heresy")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 ||
		string(out) != "logis: LOGIS_RESOLVER names no known resolver: 'heresy'\n" {
		t.Errorf("%v: %q", err, out)
	}
	if !gone(r.socket()) {
		t.Error("the socket was left behind")
	}
}

// Without LOGIS_RESOLVER (and until the window exists) sessions are refused.
func TestE2EWithoutAWindowSessionsAreRefused(t *testing.T) {
	r := newRig(t)
	cmd := r.logis("daemon")
	var env []string
	for _, kv := range cmd.Env {
		if !strings.HasPrefix(kv, "LOGIS_RESOLVER=") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	r.startCmd(cmd)
	c := r.connect()
	if err := c.Send(protocol.Open(protocol.ModeDmenu, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Await(); err == nil || !strings.Contains(err.Error(), "no window to manifest in") {
		t.Errorf("%v", err)
	}
}

// SIGTERM (systemctl stop) ends the daemon cleanly: exit 0, socket removed.
func TestE2ESIGTERMEndsTheDaemonCleanly(t *testing.T) {
	r := newRig(t)
	d := r.start()
	_ = d.cmd.Process.Signal(syscall.SIGTERM)
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d", code)
	}
	if !gone(r.socket()) {
		t.Error("the socket was left behind")
	}
}

// socketActivated runs the daemon as logis.socket would, via
// systemd-socket-activate (skipped where it is not installed).
func (r *rig) socketActivated(args ...string) *daemonProc {
	r.t.Helper()
	const tool = "/usr/bin/systemd-socket-activate"
	if _, err := os.Stat(tool); err != nil {
		r.t.Skipf("%s is not installed", tool)
	}
	cmd := exec.Command(tool, append([]string{"--listen", r.socket(),
		"-E", "XDG_RUNTIME_DIR", "-E", "LOGIS_RESOLVER", r.bin, "daemon"}, args...)...)
	cmd.Env = r.env()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	d := &daemonProc{cmd: cmd, exited: make(chan struct{})}
	r.own[cmd.Process.Pid] = true
	go func() {
		_ = cmd.Wait()
		close(d.exited)
	}()
	r.t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-d.exited
	})
	waitUntil(r.t, "the activation socket", func() bool { return !gone(r.socket()) })
	return d
}

// Ported from a_socket_activated_daemon_serves_the_socket_it_was_given.
func TestE2EASocketActivatedDaemonServesTheSocketItWasGiven(t *testing.T) {
	r := newRig(t)
	d := r.socketActivated()
	if got := r.dmenu(">activated"); string(got.Item.Raw) != ">activated" {
		t.Errorf("outcome %+v", got)
	}
	if pid := r.connect().Daemon; pid != d.cmd.Process.Pid {
		t.Errorf("pid %d answered, want the activated %d", pid, d.cmd.Process.Pid)
	}
}

// Ported from a_socket_activated_daemon_leaves_the_socket_to_systemd_when_idle.
func TestE2EASocketActivatedDaemonLeavesTheSocketToSystemdWhenIdle(t *testing.T) {
	r := newRig(t)
	d := r.socketActivated("--idle-timeout", "0.2")
	c, err := client.Connect(client.Options{RuntimeDir: r.dir, Exe: "/nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if code := d.exitCode(t); code != 0 {
		t.Errorf("exit %d", code)
	}
	if gone(r.socket()) {
		t.Error("the daemon removed systemd's socket")
	}
}
