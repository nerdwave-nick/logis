package servitor

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Ported from Rust src/servitor/invoke.rs tests.

func TestInvokeRunsInASessionOfItsOwnWithStdioOnDevNullAndUnawaited(t *testing.T) {
	dir := t.TempDir()
	// The fake servitor, made to labour 2 s once invoked.
	program := fake(t, dir, fixture(t, "vigil.jsonl"), "FAKE_INVOKE_SECONDS=2")
	args := []string{"invoke", "dnd", "on", "--reason", "-a word that looks like a rune"}
	since := time.Now()
	if err := Detached(program, args); err != nil {
		t.Fatal(err)
	}
	// Under -short the invocation still must not be done when logis is
	// (below); only the wall-clock limit is left out.
	if took := time.Since(since); testing.Short() {
		t.Logf("returned after %v; timing: the 1.5 s limit is a wall-clock check, not under -short", took)
	} else if took > 1500*time.Millisecond {
		t.Fatalf("logis waited for the invocation (%v)", took)
	}
	if !waitFor(filepath.Join(dir, "invoke", "recorded"), 5*time.Second) {
		t.Fatal("never invoked")
	}
	if waitFor(filepath.Join(dir, "invoke", "done"), 0) {
		t.Fatal("done already")
	}
	if argv := strings.Split(strings.TrimSuffix(record(t, dir, "argv"), "\n"), "\n"); !reflect.DeepEqual(argv, args[1:]) {
		t.Fatalf("%q", argv)
	}
	pid := strings.TrimSpace(record(t, dir, "pid"))
	if ids := strings.Fields(record(t, dir, "ids")); !reflect.DeepEqual(ids, []string{pid, pid}) {
		t.Fatalf("its own session and process group: %q, pid %s", ids, pid)
	}
	if fds := strings.Fields(record(t, dir, "fds")); !reflect.DeepEqual(fds, []string{"/dev/null", "/dev/null", "/dev/null"}) {
		t.Fatalf("stdin, stdout and stderr: %q", fds)
	}
	if !waitFor(filepath.Join(dir, "invoke", "done"), 5*time.Second) {
		t.Fatal("never done")
	}
}

func TestAServitorThatCannotBeStartedIsAnErrorAtOnce(t *testing.T) {
	err := Detached("/nonexistent/servitor", []string{"invoke"})
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("%v", err)
	}
}
