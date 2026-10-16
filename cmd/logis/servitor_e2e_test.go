package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// servitor mode through a real daemon: `logis --servitor` with the scripted
// resolver standing in for the window (it chooses the first settled row
// starting with '>' and types an inscription's placeholder after its '>';
// internal/daemon/scripted.go) and a fake `servitor` on the caller's PATH
// (tests/fixtures/servitor/servitor) telling census lines recorded from the
// real one and recording how it was invoked.

// world is a caller's world: a directory with bin/servitor (the fake) and
// the fake's records.
type world struct {
	t   *testing.T
	dir string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(servitorFixture(t, "servitor"), filepath.Join(bin, "servitor")); err != nil {
		t.Fatal(err)
	}
	return &world{t, dir}
}

func servitorFixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "servitor", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// env is this caller's environment for the fake telling census; the
// invocation labours seconds.
func (w *world) env(census, seconds string) []string {
	return []string{
		"PATH=" + filepath.Join(w.dir, "bin") + ":" + os.Getenv("PATH"),
		"FAKE_SERVITOR_DIR=" + w.dir,
		"FAKE_SERVITOR_CENSUS=" + servitorFixture(w.t, census),
		"FAKE_INVOKE_SECONDS=" + seconds,
		"FAKE_TOKEN=the caller's own",
	}
}

// servitor is `logis --servitor` as this caller in rig r.
func (w *world) servitor(r *rig, census, seconds string) *exec.Cmd {
	cmd := r.logis("--servitor")
	cmd.Env = append(cmd.Env, w.env(census, seconds)...)
	cmd.Dir = w.dir
	return cmd
}

func (w *world) record(name string) string {
	w.t.Helper()
	text, err := os.ReadFile(filepath.Join(w.dir, "invoke", name))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(text)
}

func (w *world) invoked() bool { return !gone(filepath.Join(w.dir, "invoke", "recorded")) }
func (w *world) done() bool    { return !gone(filepath.Join(w.dir, "invoke", "done")) }

func (w *world) censusPid() int { return pidIn(w.t, w.dir, "census.pid") }

func (w *world) openGate() {
	w.t.Helper()
	if err := os.WriteFile(filepath.Join(w.dir, "gate"), nil, 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// labouredToItsEnd waits until the invocation is done, so its records are
// whole before the test's directory goes.
func (w *world) labouredToItsEnd() {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !w.done() {
		if time.Now().After(deadline) {
			w.t.Fatal("the invocation never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func lines(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }

func TestServitorModeInvokesDetachedOnceSettledAndTheCallerDoesNotWait(t *testing.T) {
	r := newRig(t)
	r.start()
	w := newWorld(t)
	client := w.servitor(r, "chosen.jsonl", "3")
	var stdout, stderr strings.Builder
	client.Stdout, client.Stderr = &stdout, &stderr
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = client.Wait(); close(exited) }()
	defer func() { _ = client.Process.Kill(); <-exited }()

	// The chosen rite is still awaited: it is pending, so nothing is
	// invoked however long the auspex takes.
	w.censusPid()
	time.Sleep(400 * time.Millisecond)
	if w.invoked() {
		t.Fatal("a pending rite was invoked")
	}

	// Its auspex settles; the rite, its aspect and inscriptions follow,
	// and servitor is invoked. The caller is answered at once, exit 0.
	settled := time.Now().UnixNano()
	w.openGate()
	select {
	case <-exited:
	case <-time.After(2 * patience):
		t.Fatalf("the caller never ended; stderr %q", stderr.String())
	}
	answered := time.Now().UnixNano()
	if code := client.ProcessState.ExitCode(); code != 0 || stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
	}
	waitUntil(t, "the invocation to be recorded", w.invoked)
	started, err := strconv.ParseInt(strings.TrimSpace(w.record("start")), 10, 64)
	if err != nil || started <= settled {
		t.Fatalf("invoked at %d, before its rite settled at %d (%v)", started, settled, err)
	}
	if w.done() {
		t.Fatalf("logis waited for the invocation (%d ms after it began)", (answered-started)/1e6)
	}

	// How it was launched (servitor S7): the argv, a session and process
	// group of its own, stdio on /dev/null, the caller's environment and
	// working directory.
	if argv := lines(w.record("argv")); !reflect.DeepEqual(argv, []string{">dnd", ">on", "--reason", "the forge demands focus", "--origin", "a hotkey of the adept"}) {
		t.Fatalf("%q", argv)
	}
	pid := strings.TrimSpace(w.record("pid"))
	if ids := strings.Fields(w.record("ids")); !reflect.DeepEqual(ids, []string{pid, pid}) {
		t.Fatalf("its own session and process group: %q, pid %s", ids, pid)
	}
	if fds := lines(w.record("fds")); !reflect.DeepEqual(fds, []string{"/dev/null", "/dev/null", "/dev/null"}) {
		t.Fatalf("%q", fds)
	}
	if token := strings.TrimSpace(w.record("token")); token != "the caller's own" {
		t.Fatalf("%q", token)
	}
	cwd, _ := filepath.EvalSymlinks(w.dir)
	if got := strings.TrimSpace(w.record("cwd")); got != cwd {
		t.Fatalf("cwd %q, want %q", got, cwd)
	}

	// It labours on, unwatched, to its end.
	w.labouredToItsEnd()
}

func TestAnEdictIsInvokedWithoutAnAspect(t *testing.T) {
	r := newRig(t)
	r.start()
	w := newWorld(t)
	if got := r.runClient(w.servitor(r, "edict.jsonl", "0"), nil); got.code != 0 {
		t.Fatalf("%+v", got)
	}
	waitUntil(t, "the invocation to be recorded", w.invoked)
	if argv := lines(w.record("argv")); !reflect.DeepEqual(argv, []string{">screenshot", "--region", "whole"}) {
		t.Fatalf("%q", argv)
	}
	w.labouredToItsEnd()
}

func TestARiteThatNeverSettlesIsNeverInvokedAndLeavingEndsTheCensus(t *testing.T) {
	r := newRig(t)
	r.start()
	w := newWorld(t)
	client := w.servitor(r, "lingering.jsonl", "0")
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	census := w.censusPid()
	time.Sleep(500 * time.Millisecond)
	// The caller leaves, as a dismissal ends a session: logis cancels it.
	_ = client.Process.Kill()
	_ = client.Wait()
	waitUntil(t, "the census to end once its reader departed", func() bool { return !alive(census) })
	if w.invoked() {
		t.Fatal("a pending rite was invoked")
	}
}

func TestNoDismissalOrDepartureOfLogisAbortsARunningInvocation(t *testing.T) {
	r := newRig(t)
	daemon := r.start()
	w := newWorld(t)
	w.openGate()
	if got := r.runClient(w.servitor(r, "chosen.jsonl", "2"), nil); got.code != 0 {
		t.Fatalf("%+v", got)
	}
	waitUntil(t, "the invocation to be recorded", w.invoked)
	invocation, err := strconv.Atoi(strings.TrimSpace(w.record("pid")))
	if err != nil {
		t.Fatal(err)
	}

	// servitor mode opened again and dismissed (its caller leaves), then
	// the daemon itself goes: the invocation labours on.
	again := newWorld(t)
	client := again.servitor(r, "lingering.jsonl", "0")
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}
	again.censusPid()
	_ = client.Process.Kill()
	_ = client.Wait()
	_ = daemon.cmd.Process.Signal(syscall.SIGTERM)
	daemon.exitCode(t)
	if !alive(invocation) || w.done() {
		t.Fatal("the invocation was cut short")
	}
	w.labouredToItsEnd()
}

// provideServitor is `logis provide servitor` in w, the fake telling census.
func provideServitor(r *rig, w *world, census string) *exec.Cmd {
	cmd := exec.Command(r.bin, "provide", "servitor")
	cmd.Env = append(r.env(), w.env(census, "0")...)
	return cmd
}

func TestServitorModeRunsLogisItselfAsItsProvider(t *testing.T) {
	// `logis provide servitor` speaks the provider protocol on its own:
	// greeting, the rite stage at once, then the census as it is told.
	r := newRig(t)
	w := newWorld(t)
	cmd := provideServitor(r, w, "edict.jsonl")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if _, err := stdin.Write([]byte(`{"type":"hello","protocol":1}` + "\n")); err != nil {
		t.Fatal(err)
	}
	out := bufio.NewReader(stdout)
	next := func() map[string]any {
		t.Helper()
		line, err := out.ReadString('\n')
		var v map[string]any
		if err != nil || json.Unmarshal([]byte(line), &v) != nil {
			t.Fatalf("%q, %v", line, err)
		}
		return v
	}
	if m := next(); m["type"] != "hello" {
		t.Fatalf("%v", m)
	}
	if m := next(); m["type"] != "stage" || m["id"] != "rite" {
		t.Fatalf("%v", m)
	}
	if m := next(); m["type"] != "items" || m["items"].([]any)[0].(map[string]any)["id"] != ">screenshot" {
		t.Fatalf("%v", m)
	}
	if _, err := stdin.Write([]byte(`{"type":"cancel"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("%v", err)
	}
	if w.invoked() {
		t.Fatal("invoked after a cancel")
	}
}

// A provider whose logis is gone (its stdout read by nobody) ends quietly,
// exit 0, rather than killed by SIGPIPE.
func TestAProviderWhoseLogisIsGoneEndsWithExit0(t *testing.T) {
	r := newRig(t)
	w := newWorld(t)
	cmd := provideServitor(r, w, "lingering.jsonl")
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	cmd.Stdout = write
	err = cmd.Run()
	_ = write.Close()
	if err != nil {
		t.Fatalf("%v (%v)", err, cmd.ProcessState)
	}
}
