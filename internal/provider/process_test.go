package provider

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Provider processes, with real sh processes.

const patience = 5 * time.Second

func sh(script string) Launch {
	return Launch{Argv: []string{"/bin/sh", "-c", script}, Cwd: "/", Env: []string{"PATH=/usr/bin:/bin", "OMEN=a=b"}}
}

func start(t *testing.T, l Launch) (*Process, chan Heard) {
	t.Helper()
	heard := make(chan Heard, 1024)
	p, err := Spawn(l, func(h Heard) bool { heard <- h; return true })
	if err != nil {
		t.Fatal(err)
	}
	return p, heard
}

func next(t *testing.T, heard chan Heard) Heard {
	t.Helper()
	select {
	case h := <-heard:
		return h
	case <-time.After(patience):
		t.Fatal("the provider said nothing")
		return Heard{}
	}
}

func message(t *testing.T, heard chan Heard) Message {
	t.Helper()
	h := next(t, heard)
	if h.Exited || h.Err != nil {
		t.Fatalf("%+v", h)
	}
	return h.Message
}

func exited(t *testing.T, heard chan Heard) string {
	t.Helper()
	for {
		if h := next(t, heard); h.Exited {
			return h.Status
		}
	}
}

// killOwn ends a process this test's provider started, by its pid.
func killOwn(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }

func TestLinesAreHeardInOrderThenTheExit(t *testing.T) {
	_, heard := start(t, sh(`echo '{"type":"hello","protocol":1}'; echo; printf '{"type":"close"}'`))
	if m := message(t, heard); m != (Hello{Protocol: 1}) {
		t.Fatalf("%#v", m)
	}
	if m := message(t, heard); m != (Close{}) {
		t.Fatalf("blank lines skipped, last line unended: %#v", m)
	}
	if s := exited(t, heard); s != "exit status: 0" {
		t.Fatal(s)
	}
}

func TestTheProviderHearsWhatLogisSends(t *testing.T) {
	p, heard := start(t, sh(`read line; echo "$line"`))
	if err := p.Send(Greeting()); err != nil {
		t.Fatal(err)
	}
	if m := message(t, heard); m != (Hello{Protocol: 1}) {
		t.Fatalf("%#v", m)
	}
	exited(t, heard)
	// Gone: its input is broken, and that is an error, not a hang.
	time.Sleep(50 * time.Millisecond)
	if err := p.Send(Cancel{}); err == nil {
		t.Fatal("a gone provider took a line")
	}
}

func TestItRunsInTheCallersDirectoryAndEnvironmentAndItsOwnGroup(t *testing.T) {
	_, heard := start(t, sh(`printf '{"type":"toast","text":"%s %s %s"}\n' "$(pwd)" "$OMEN" "$(ps -o pid=,pgid= $$ | tr -s ' ' | sed 's/^ //')"`))
	toast, ok := message(t, heard).(Toast)
	if !ok {
		t.Fatal("no toast")
	}
	parts := strings.Split(toast.Text, " ")
	if len(parts) != 4 || parts[0] != "/" || parts[1] != "a=b" {
		t.Fatalf("%q", toast.Text)
	}
	if parts[2] != parts[3] {
		t.Fatalf("not its own process group: %q", toast.Text)
	}
	if parts[3] == strconv.Itoa(syscall.Getpgrp()) {
		t.Fatalf("in the daemon's group: %q", toast.Text)
	}
}

func TestTheProgramIsFoundOnTheCallersPathAndAMissingDirectoryIsRoot(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '{\"type\":\"toast\",\"text\":\"%s\"}\\n' \"$(pwd)\"\n"
	if err := os.WriteFile(filepath.Join(dir, "forge-rite"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	l := Launch{Argv: []string{"forge-rite"}, Cwd: filepath.Join(dir, "gone"), Env: []string{"PATH=/nowhere:" + dir}}
	_, heard := start(t, l)
	if toast, ok := message(t, heard).(Toast); !ok || toast.Text != "/" {
		t.Fatalf("%#v", toast)
	}
	if s := exited(t, heard); s != "exit status: 0" {
		t.Fatal(s)
	}
}

func TestClosingItsInputLetsItEnd(t *testing.T) {
	p, heard := start(t, sh("cat >/dev/null"))
	p.CloseInput()
	p.CloseInput() // harmless twice
	if s := exited(t, heard); s != "exit status: 0" {
		t.Fatal(s)
	}
}

func TestASignalReachesTheProviderAlone(t *testing.T) {
	// The provider starts a child in its group, then waits.
	p, heard := start(t, sh(`sleep 30 & echo "{\"type\":\"toast\",\"text\":\"$!\"}"; wait`))
	toast, _ := message(t, heard).(Toast)
	child, err := strconv.Atoi(toast.Text)
	if err != nil {
		t.Fatal(err)
	}
	defer killOwn(child)
	p.Signal(syscall.SIGTERM)
	if s := exited(t, heard); !strings.Contains(s, "SIGTERM") {
		t.Fatal(s)
	}
	if syscall.Kill(child, 0) != nil {
		t.Fatal("its child was signalled too")
	}
	p.Signal(syscall.SIGKILL) // harmless once it is gone
}

func TestItsExitIsHeardWhileAChildStillHoldsItsOutput(t *testing.T) {
	_, heard := start(t, sh(`sleep 30 & echo "{\"type\":\"toast\",\"text\":\"$!\"}"; printf '{"type":"close"}'`))
	toast, _ := message(t, heard).(Toast)
	child, err := strconv.Atoi(toast.Text)
	if err != nil {
		t.Fatal(err)
	}
	defer killOwn(child)
	if m := message(t, heard); m != (Close{}) {
		t.Fatalf("%#v", m)
	}
	if s := exited(t, heard); s != "exit status: 0" {
		t.Fatal(s)
	}
}

func TestGarbageIsReportedAndUnheardOutputIsDrained(t *testing.T) {
	heard := make(chan Heard, 16)
	first := true
	_, err := Spawn(sh(`echo nonsense; yes '{"type":"ready"}' | head -n 100000`), func(h Heard) bool {
		goOn := !first
		first = false
		heard <- h
		return goOn
	})
	if err != nil {
		t.Fatal(err)
	}
	if h := next(t, heard); h.Err == nil || !strings.Contains(h.Err.Error(), "JSON") {
		t.Fatalf("not an error first: %+v", h)
	}
	if s := exited(t, heard); s != "exit status: 0" {
		t.Fatalf("stuck on a full pipe? %s", s)
	}
}

func TestALineTooLongIsAnError(t *testing.T) {
	heard := make(chan Heard, 16)
	_, err := Spawn(sh(`head -c 17000000 /dev/zero | tr '\0' x`), func(h Heard) bool { heard <- h; return h.Err == nil })
	if err != nil {
		t.Fatal(err)
	}
	if h := next(t, heard); h.Err == nil || !strings.Contains(h.Err.Error(), "longer than") {
		t.Fatalf("%+v", h)
	}
	if s := exited(t, heard); s != "exit status: 0" {
		t.Fatal(s)
	}
}

func TestAProgramThatDoesNotExistIsAnError(t *testing.T) {
	l := sh("")
	l.Argv = []string{"/nonexistent/provider"}
	if _, err := Spawn(l, func(Heard) bool { return true }); err == nil || !strings.Contains(err.Error(), "/nonexistent/provider") {
		t.Fatalf("%v", err)
	}
	l.Argv = []string{"no-such-rite-anywhere"}
	if _, err := Spawn(l, func(Heard) bool { return true }); err == nil || !strings.Contains(err.Error(), "no-such-rite-anywhere") {
		t.Fatalf("%v", err)
	}
	l.Argv = nil
	if _, err := Spawn(l, func(Heard) bool { return true }); err == nil {
		t.Fatal("an empty command line started")
	}
}

func TestALaunchCarriesTheClientsBytesAndEnvironmentPairsSplitAtTheFirstEquals(t *testing.T) {
	l := LaunchOf(protocol.Launch{Argv: [][]byte{[]byte("rite"), []byte("\xff")}, Cwd: []byte("/x"),
		Env: [][]byte{[]byte("A=1=2"), []byte("NOEQ"), []byte("B=")}})
	want := Launch{Argv: []string{"rite", "\xff"}, Cwd: "/x", Env: []string{"A=1=2", "B="}}
	if !reflect.DeepEqual(l, want) {
		t.Fatalf("%q", l)
	}
}
