package apps

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Ported from Rust src/apps/watch.rs. inotify queues what happens after
// Arm, so a change made before Wait is still reported by it.

const (
	soon  = 3 * time.Second
	quiet = 200 * time.Millisecond
)

func newWatcher(t *testing.T) *Watcher {
	t.Helper()
	w, err := NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func waited(t *testing.T, w *Watcher, timeout time.Duration) bool {
	t.Helper()
	changed, err := w.Wait(timeout)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func TestAWrittenOrRemovedEntryIsAChange(t *testing.T) {
	apps := filepath.Join(t.TempDir(), "applications")
	if err := os.MkdirAll(filepath.Join(apps, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := newWatcher(t)
	w.Arm([]string{apps, filepath.Join(apps, "sub")}, []string{apps})
	if waited(t, w, quiet) {
		t.Fatal("nothing changed yet")
	}
	file := filepath.Join(apps, "sub/oil.desktop")
	if err := os.WriteFile(file, []byte("[Desktop Entry]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !waited(t, w, soon) {
		t.Fatal("a new file in a subdirectory went unseen")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if !waited(t, w, soon) {
		t.Fatal("a removed file went unseen")
	}
}

func TestAMissingApplicationsDirectoryIsWatchedThroughItsAncestor(t *testing.T) {
	dir := t.TempDir()
	apps := filepath.Join(dir, "share/applications")
	w := newWatcher(t)
	w.Arm(nil, []string{apps})
	if err := os.WriteFile(filepath.Join(dir, "unrelated"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if waited(t, w, quiet) {
		t.Fatal("other files in the ancestor are changes")
	}
	if err := os.Mkdir(filepath.Join(dir, "share"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !waited(t, w, soon) {
		t.Fatal("the next directory on the way went unseen")
	}
}

// A burst (a package manager writing many files) is reported once.
func TestABurstIsReportedOnce(t *testing.T) {
	apps := filepath.Join(t.TempDir(), "applications")
	if err := os.Mkdir(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	w := newWatcher(t)
	w.Arm([]string{apps}, []string{apps})
	for i := range 20 {
		name := filepath.Join(apps, string(rune('a'+i))+".desktop")
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !waited(t, w, soon) {
		t.Fatal("the burst went unseen")
	}
	if waited(t, w, quiet) {
		t.Fatal("the burst was reported twice")
	}
}

// Close ends a Wait on another goroutine.
func TestCloseEndsAWait(t *testing.T) {
	w := newWatcher(t)
	w.Arm([]string{t.TempDir()}, nil)
	ended := make(chan error, 1)
	go func() { _, err := w.Wait(-1); ended <- err }()
	_ = w.Close()
	select {
	case err := <-ended:
		if err == nil {
			t.Error("Wait ended without an error")
		}
	case <-time.After(soon):
		t.Fatal("Wait outlived Close")
	}
}
