package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/client"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Provider sessions through a real daemon and real provider processes, with
// the scripted resolver standing in for the window: it chooses the first
// settled item marked '>'. The fixture provider is
// tests/fixtures/providers/forge.sh. No compositor is needed;
// provider_window_e2e_test.go covers the window itself.

func forge(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../../tests/fixtures/providers/forge.sh")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// provider starts `logis --provider forge.sh mode` in this rig, its fixture
// files in dir.
func (r *rig) provider(dir, mode string) *exec.Cmd {
	r.t.Helper()
	cmd := r.logis("--provider", forge(r.t), mode)
	cmd.Env = append(cmd.Env, "FORGE_DIR="+dir)
	return cmd
}

func heardBy(dir string) string {
	text, _ := os.ReadFile(filepath.Join(dir, "heard"))
	return string(text)
}

// pidIn is the pid forge.sh wrote to dir/name.
func pidIn(t *testing.T, dir, name string) int {
	t.Helper()
	var text []byte
	waitUntil(t, name+" to be written", func() bool {
		var err error
		text, err = os.ReadFile(filepath.Join(dir, name))
		return err == nil && strings.HasSuffix(string(text), "\n")
	})
	pid, err := strconv.Atoi(strings.TrimSpace(string(text)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// alive: process pid still runs (a zombie counts as gone).
func alive(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(stat)
	return !strings.HasPrefix(strings.TrimSpace(s[strings.LastIndexByte(s, ')')+1:]), "Z")
}

// endOwn ends a process this test's provider started, by its pid.
func endOwn(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }

// probe opens a dmenu session only to see whether it would be shown at
// once ("visible") or wait ("queued"), then cancels it and waits for the
// cancellation.
func (r *rig) probe() string {
	r.t.Helper()
	c, err := client.Connect(client.Options{RuntimeDir: r.dir, Exe: "/nonexistent", Patience: time.Second})
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Send(protocol.Open(protocol.ModeDmenu, nil)); err != nil {
		r.t.Fatal(err)
	}
	first, err := c.Recv()
	if err != nil || first == nil {
		r.t.Fatalf("%+v, %v", first, err)
	}
	if err := c.Send(protocol.Cancel()); err != nil {
		r.t.Fatal(err)
	}
	if outcome, err := c.Await(); err != nil || outcome.Kind != protocol.Cancelled {
		r.t.Fatalf("%+v, %v", outcome, err)
	}
	return first.Type
}

func (r *rig) waitForAVisibleSession() {
	r.t.Helper()
	waitUntil(r.t, "a session to become visible", func() bool { return r.probe() == protocol.TypeQueued })
}

func (r *rig) waitUntilNoSessionRemains() {
	r.t.Helper()
	waitUntil(r.t, "every session to end", func() bool { return r.probe() == protocol.TypeVisible })
}

func TestATwoStageFlowRunsToTheProvidersClose(t *testing.T) {
	r := newRig(t)
	r.stopsItsDaemon()
	dir := t.TempDir()
	cmd := r.provider(dir, "flow")
	cmd.Env = append(cmd.Env, "FORGE_TOKEN=the caller's own")
	cmd.Dir = dir
	got := r.runClient(cmd, nil)
	if got.code != 0 || got.stdout != "" {
		t.Fatalf("%+v", got)
	}
	heard := heardBy(dir)
	cwd, _ := filepath.EvalSymlinks(dir)
	if !strings.HasPrefix(heard, "cwd "+cwd+"\ntoken the caller's own\n") {
		t.Fatalf("%q", heard)
	}
	lines := strings.Split(strings.TrimSuffix(heard, "\n"), "\n")[2:]
	want := []string{
		`{"type":"hello","protocol":1}`,
		`{"type":"activate","stage":"rite","id":"oil"}`,
		`{"type":"activate","stage":"aspect","id":"on"}`,
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the pending rite was not chosen only once settled, then its aspect: %q", lines)
	}
}

func TestADismissedProviderThatActedIsNeverSignalledNorItsAction(t *testing.T) {
	r := newRig(t)
	r.stopsItsDaemon()
	dir := t.TempDir()
	cmd := r.provider(dir, "act")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	action, provider := pidIn(t, dir, "action"), pidIn(t, dir, "provider")
	defer endOwn(action)
	defer endOwn(provider)
	// The caller leaves (as a dismissal would end it): the session is over.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	r.waitUntilNoSessionRemains()
	// Past both graces a provider that never acted would get.
	time.Sleep(2500 * time.Millisecond)
	if !alive(action) {
		t.Fatal("the action was killed")
	}
	if !alive(provider) {
		t.Fatal("the provider that started an action was signalled")
	}
}

func TestAProviderThatNeverActedAndLingersAfterCancelIsEnded(t *testing.T) {
	r := newRig(t)
	r.stopsItsDaemon()
	dir := t.TempDir()
	cmd := r.provider(dir, "stubborn")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	provider := pidIn(t, dir, "provider")
	defer endOwn(provider)
	since := time.Now()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	deadline := time.Now().Add(patience)
	for alive(provider) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if alive(provider) {
		t.Fatal("the stubborn provider was never ended")
	}
	if took := time.Since(since); took < 1800*time.Millisecond {
		t.Fatalf("ended after %v: before its graces (it ignores SIGTERM)", took)
	}
}

func TestAReplacedProviderCallerExits1WithEmptyStdout(t *testing.T) {
	r := newRig(t)
	r.stopsItsDaemon()
	dir := t.TempDir()
	cmd := r.provider(dir, "stubborn")
	var stdout strings.Builder
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() { _ = cmd.Process.Kill(); <-exited }()
	provider := pidIn(t, dir, "provider")
	defer endOwn(provider)
	r.waitForAVisibleSession()
	apps := r.connect()
	if err := apps.Send(protocol.Open(protocol.ModeApps, nil)); err != nil {
		t.Fatal(err)
	}
	if state, err := apps.Recv(); err != nil || state == nil || state.Type != protocol.TypeVisible {
		t.Fatalf("%+v, %v", state, err)
	}
	select {
	case <-exited:
	case <-time.After(patience):
		t.Fatal("the replaced caller never ended")
	}
	if code := cmd.ProcessState.ExitCode(); code != 1 || stdout.String() != "" {
		t.Fatalf("exit %d, stdout %q", code, stdout.String())
	}
}

func TestAProviderThatShowsNothingInTimeFailsItsSession(t *testing.T) {
	r := newRig(t)
	r.stopsItsDaemon()
	since := time.Now()
	got := r.runClient(r.provider(t.TempDir(), "mute"), nil)
	if got.code != 2 || !strings.Contains(got.stderr, "showed nothing within 5 s") {
		t.Fatalf("%+v", got)
	}
	if took := time.Since(since); took < 5*time.Second {
		t.Fatalf("failed after %v", took)
	}
}

func TestAProviderThatBreaksTheProtocolOrDepartsFailsItsSession(t *testing.T) {
	r := newRig(t)
	r.stopsItsDaemon()
	dir := t.TempDir()
	if got := r.runClient(r.provider(dir, "garbage"), nil); got.code != 2 || !strings.Contains(got.stderr, "not a JSON line") {
		t.Fatalf("%+v", got)
	}
	if got := r.runClient(r.provider(dir, "departs"), nil); got.code != 2 || !strings.Contains(got.stderr, "exit status: 3") {
		t.Fatalf("%+v", got)
	}
}

func TestAProviderThatCannotStartIsAnError(t *testing.T) {
	r := newRig(t)
	r.stopsItsDaemon()
	got := r.runClient(r.logis("--provider", "/nonexistent/rite"), nil)
	if got.code != 2 || !strings.Contains(got.stderr, "/nonexistent/rite") {
		t.Fatalf("%+v", got)
	}
}
