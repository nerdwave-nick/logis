package apps

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Watching the applications directories, on Watcher.Scan: inotify queues
// what happens once a directory is watched, so a change made before Wait
// is still reported by it. afterRead puts a change exactly where a scan
// could lose it: right after a directory was read.

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

// onRead runs change once, right after the scan has read dir (or failed
// to).
func onRead(t *testing.T, dir string, change func()) {
	t.Helper()
	done := false
	afterRead = func(d string) {
		if d == dir && !done {
			done = true
			change()
		}
	}
	t.Cleanup(func() { afterRead = nil })
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, file string) {
	t.Helper()
	if err := os.WriteFile(file, []byte("[Desktop Entry]\nType=Application\nName=Late\nExec=late\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAWrittenOrRemovedEntryIsAChange(t *testing.T) {
	apps := filepath.Join(t.TempDir(), "applications")
	mkdir(t, filepath.Join(apps, "sub"))
	w := newWatcher(t)
	w.Scan(Dirs{apps}, Context{})
	if waited(t, w, quiet) {
		t.Fatal("nothing changed yet")
	}
	file := filepath.Join(apps, "sub/oil.desktop")
	write(t, file)
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
	w := newWatcher(t)
	w.Scan(Dirs{filepath.Join(dir, "share/applications")}, Context{})
	write(t, filepath.Join(dir, "unrelated"))
	if waited(t, w, quiet) {
		t.Fatal("other files in the ancestor are changes")
	}
	mkdir(t, filepath.Join(dir, "share"))
	if !waited(t, w, soon) {
		t.Fatal("the next directory on the way went unseen")
	}
}

// A burst (a package manager writing many files) is reported once.
func TestABurstIsReportedOnce(t *testing.T) {
	apps := filepath.Join(t.TempDir(), "applications")
	mkdir(t, apps)
	w := newWatcher(t)
	w.Scan(Dirs{apps}, Context{})
	for i := range 20 {
		write(t, filepath.Join(apps, string(rune('a'+i))+".desktop"))
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
	w.Scan(Dirs{t.TempDir()}, Context{})
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

// A file that appears in a directory the scan has just read (here on the
// very first scan) is not in the index, so it must be a change: the
// directory is watched before it is read.
func TestAFileAddedToADirectoryJustReadIsAChange(t *testing.T) {
	apps := filepath.Join(t.TempDir(), "applications")
	forge := filepath.Join(apps, "forge")
	mkdir(t, forge)
	onRead(t, forge, func() { write(t, filepath.Join(forge, "late.desktop")) })
	w := newWatcher(t)
	if ix := w.Scan(Dirs{apps}, Context{}); len(ix.Apps()) != 0 {
		t.Fatalf("the scan saw %d apps; the change came too early to test anything", len(ix.Apps()))
	}
	if !waited(t, w, soon) {
		t.Fatal("the file added after its directory was read went unseen")
	}
	if ix := w.Scan(Dirs{apps}, Context{}); len(ix.Apps()) != 1 {
		t.Errorf("the next scan saw %d apps", len(ix.Apps()))
	}
}

// A missing applications directory that appears right after the scan
// found it missing is a change, also on a later scan when it had been
// watched through its ancestor all along.
func TestAMissingDirectoryCreatedDuringTheScanIsAChange(t *testing.T) {
	for _, rescan := range []bool{false, true} {
		dir := t.TempDir()
		apps := filepath.Join(dir, "applications")
		w := newWatcher(t)
		if rescan {
			w.Scan(Dirs{apps}, Context{})
		}
		onRead(t, apps, func() { mkdir(t, filepath.Join(apps, "forge")) })
		w.Scan(Dirs{apps}, Context{})
		afterRead = nil
		if !waited(t, w, soon) {
			t.Fatalf("rescan %v: the directory created during the scan went unseen", rescan)
		}
		ix := w.Scan(Dirs{apps}, Context{})
		write(t, filepath.Join(apps, "forge/late.desktop"))
		if !waited(t, w, soon) {
			t.Fatalf("rescan %v: a file in the new directory went unseen (read %q)", rescan, ix.Dirs)
		}
	}
}

// A watch the scan no longer needs (the ancestor of a directory that now
// exists) is dropped: its other files are no changes any more.
func TestAWatchNoLongerNeededIsDropped(t *testing.T) {
	dir := t.TempDir()
	apps := filepath.Join(dir, "applications")
	w := newWatcher(t)
	w.Scan(Dirs{apps}, Context{})
	mkdir(t, apps)
	if !waited(t, w, soon) {
		t.Fatal("the new directory went unseen")
	}
	w.Scan(Dirs{apps}, Context{})
	if waited(t, w, quiet) {
		t.Fatal("a rescan of nothing new is a change")
	}
	write(t, filepath.Join(dir, "unrelated"))
	if waited(t, w, quiet) {
		t.Fatal("the dropped ancestor still reports")
	}
}
