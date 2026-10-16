package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Watching the config file, or every theme in a directory.

const (
	// patience is how long a change may take to be reported.
	patience = 5 * time.Second
	// quiet is how long to listen for a report that should not come.
	quiet = 300 * time.Millisecond
)

// reports runs w until the test ends and hands out its reports.
func reports(t *testing.T, w *Watcher) <-chan struct{} {
	t.Helper()
	ch := make(chan struct{}, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(func() bool {
			ch <- struct{}{}
			return true
		})
	}()
	t.Cleanup(func() {
		_ = w.Close()
		select {
		case <-done:
		case <-time.After(patience):
			t.Error("Run did not return after Close")
		}
	})
	return ch
}

func watchFile(t *testing.T, file string) <-chan struct{} {
	t.Helper()
	w, err := Watch(file)
	if err != nil {
		t.Fatal(err)
	}
	return reports(t, w)
}

func watchDir(t *testing.T, dir string) <-chan struct{} {
	t.Helper()
	w, err := WatchFiles(dir, ".jsonc")
	if err != nil {
		t.Fatal(err)
	}
	return reports(t, w)
}

// reported waits for a report, then drains the rest of the burst if it was
// reported in parts.
func reported(ch <-chan struct{}) bool {
	select {
	case <-ch:
	case <-time.After(patience):
		return false
	}
	for {
		select {
		case <-ch:
		case <-time.After(quiet):
			return true
		}
	}
}

func silent(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return false
	case <-time.After(quiet):
		return true
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestEveryThemeInADirectoryIsWatchedAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "forge.jsonc"), "{}")
	ch := watchDir(t, dir)
	write(t, filepath.Join(dir, "forge.jsonc"), `{"glow": {}}`)
	if !reported(ch) {
		t.Error("an edit")
	}
	write(t, filepath.Join(dir, "amber.jsonc"), "{}")
	if !reported(ch) {
		t.Error("a new theme")
	}
	write(t, filepath.Join(dir, "notes.txt"), "brass")
	write(t, filepath.Join(dir, ".forge.jsonc.swp"), "x")
	if !silent(ch) {
		t.Error("other files")
	}
	if err := os.Remove(filepath.Join(dir, "amber.jsonc")); err != nil {
		t.Fatal(err)
	}
	if !reported(ch) {
		t.Error("a removed theme")
	}
}

func TestAThemesDirectoryCreatedLaterAndALinkedThemeAreFollowed(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "logis", "themes")
	ch := watchDir(t, dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "dotfiles-mine.jsonc")
	write(t, target, "{}")
	symlink(t, target, filepath.Join(dir, "mine.jsonc"))
	if !reported(ch) {
		t.Error("the linked theme appeared")
	}
	write(t, target, `{"glow": {}}`)
	if !reported(ch) {
		t.Error("its target was edited")
	}
}

func TestAWriteInPlaceIsReportedOnce(t *testing.T) {
	file := filepath.Join(t.TempDir(), "logis.jsonc")
	write(t, file, "{}")
	ch := watchFile(t, file)
	write(t, file, `{"theme": "x"}`)
	select {
	case <-ch:
	case <-time.After(patience):
		t.Fatal("not reported")
	}
	if !silent(ch) {
		t.Error("one save was reported twice")
	}
}

func TestAnAtomicReplacementIsReportedAndStillWatchedAfter(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "logis.jsonc")
	write(t, file, "{}")
	ch := watchFile(t, file)
	for round := range 2 {
		tmp := filepath.Join(dir, ".logis.jsonc."+string(rune('0'+round)))
		write(t, tmp, `{"theme": "y"}`)
		if err := os.Rename(tmp, file); err != nil {
			t.Fatal(err)
		}
		if !reported(ch) {
			t.Errorf("round %d", round)
		}
	}
}

func TestADeletedFileIsReported(t *testing.T) {
	file := filepath.Join(t.TempDir(), "logis.jsonc")
	write(t, file, "{}")
	ch := watchFile(t, file)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if !reported(ch) {
		t.Error("not reported")
	}
}

func TestAFileCreatedLaterIsReported(t *testing.T) {
	file := filepath.Join(t.TempDir(), "logis.jsonc")
	ch := watchFile(t, file)
	write(t, file, "{}")
	if !reported(ch) {
		t.Error("its creation")
	}
	write(t, file, `{"theme": "z"}`)
	if !reported(ch) {
		t.Error("the new file is not watched")
	}
}

func TestADirectoryCreatedLaterIsFollowed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config", "logis", "logis.jsonc")
	ch := watchFile(t, file)
	write(t, file, "{}")
	if !reported(ch) {
		t.Error("its creation")
	}
	write(t, file, `{"theme": "z"}`)
	if !reported(ch) {
		t.Error("the new directory is not watched")
	}
}

func TestOtherFilesBesideItAreIgnored(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "logis.jsonc")
	write(t, file, "{}")
	ch := watchFile(t, file)
	write(t, filepath.Join(dir, "frecency.json"), "{}")
	if err := os.Mkdir(filepath.Join(dir, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !silent(ch) {
		t.Error("reported")
	}
}

func TestAnEditToASymlinkedFilesTargetIsReported(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "logis.jsonc")
	write(t, target, "{}")
	file := filepath.Join(dir, "config", "logis.jsonc")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	symlink(t, target, file)
	ch := watchFile(t, file)
	write(t, target, `{"theme": "z"}`)
	if !reported(ch) {
		t.Error("not reported")
	}
}

func TestANewSymlinkInItsPlaceIsReported(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.jsonc")
	write(t, target, "{}")
	file := filepath.Join(dir, "logis.jsonc")
	ch := watchFile(t, file)
	symlink(t, target, file)
	if !reported(ch) {
		t.Error("not reported")
	}
}

func TestRunEndsWhenChangedSaysSo(t *testing.T) {
	file := filepath.Join(t.TempDir(), "logis.jsonc")
	w, err := Watch(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(func() bool { return false })
	}()
	write(t, file, "{}")
	select {
	case <-done:
	case <-time.After(patience):
		t.Error("Run went on")
	}
}

func TestAFileWithoutAPlaceCannotBeWatched(t *testing.T) {
	if _, err := Watch("/"); err == nil {
		t.Error("watched /")
	}
	if _, err := WatchFiles("relative/themes", ".jsonc"); err == nil {
		t.Error("watched a relative directory")
	}
}
