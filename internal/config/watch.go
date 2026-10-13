package config

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Watching the config file (or every theme in the themes directory) for the
// daemon's live reload, with inotify through package syscall: the inotify
// descriptor is non-blocking and read through the runtime's poller, so a
// waiting watcher costs no thread and no wake-up, and Close ends it.
//
// Two kinds of watch cover how editors save:
//   - the directory the files live in, for writes in place (IN_CLOSE_WRITE),
//     atomic replacement by rename (IN_MOVED_TO) and deletion; while that
//     directory does not exist yet, its nearest existing ancestor, for its
//     creation;
//   - each watched file itself, followed through a symlink, for edits to a
//     link's target (dotfile managers link the config from elsewhere).
//
// A burst of events (an editor's save is several) settles for a moment and
// is reported once. The watcher writes nothing: a daemon started by a client
// outlives that client's stderr.

const (
	// settle is how long a burst of events must be quiet to be reported.
	settle = 30 * time.Millisecond
	// maxSettle is the longest a burst is held back, however busy the file.
	maxSettle = 300 * time.Millisecond
)

// dirEvents are watched on the directory (ONLYDIR: it must stay one).
const dirEvents = syscall.IN_CLOSE_WRITE | syscall.IN_MOVED_TO | syscall.IN_MOVED_FROM |
	syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF |
	syscall.IN_ONLYDIR

// fileEvents are watched on each file (or a symlink's target).
const fileEvents = syscall.IN_CLOSE_WRITE | syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF

// Watcher watches one config file, or every theme in a directory. Run and
// Close may be called from different goroutines; nothing else is shared.
type Watcher struct {
	f *os.File
	// one is the watched file's name in dir; "" watches every file with
	// the extension ext.
	one, ext string
	// dir is where the files live; watched is dir or its nearest existing
	// ancestor, watched as dirWD (-1: none).
	dir, watched string
	dirWD        int
	fileWDs      []int
}

// Watch starts watching file, which need not exist yet. Changes from now on
// are reported once Run runs, so read the file after this.
func Watch(file string) (*Watcher, error) {
	dir, name := filepath.Split(filepath.Clean(file))
	if name == "" || name == "/" {
		return nil, fmt.Errorf("%s names no file in a directory", file)
	}
	return start(filepath.Clean(dir), name, "")
}

// WatchFiles starts watching every file in dir whose name ends in ext,
// hidden ones aside (the themes directory and its .jsonc); dir may not
// exist yet.
func WatchFiles(dir, ext string) (*Watcher, error) {
	return start(filepath.Clean(dir), "", ext)
}

func start(dir, one, ext string) (*Watcher, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%s is not an absolute directory", dir)
	}
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, os.NewSyscallError("inotify_init1", err)
	}
	w := &Watcher{f: os.NewFile(uintptr(fd), "inotify"), one: one, ext: ext, dir: dir, dirWD: -1}
	w.arm()
	if w.dirWD < 0 {
		_ = w.f.Close()
		return nil, fmt.Errorf("no directory on the way to %s can be watched", dir)
	}
	return w, nil
}

// Close stops the watcher; a running Run returns.
func (w *Watcher) Close() error { return w.f.Close() }

// Run calls changed after every settled burst of changes to the watched
// files, until changed returns false or the watcher is closed.
func (w *Watcher) Run(changed func() bool) {
	buf := make([]byte, 64*(syscall.SizeofInotifyEvent+syscall.NAME_MAX+1))
	for {
		relevant, err := w.nextBurst(buf)
		if err != nil {
			return
		}
		w.arm()
		if relevant && !changed() {
			return
		}
	}
}

// nextBurst waits for events until they settle; whether any concerned the
// watched files.
func (w *Watcher) nextBurst(buf []byte) (bool, error) {
	if err := w.f.SetReadDeadline(time.Time{}); err != nil {
		return false, err
	}
	relevant, err := w.read(buf)
	for start := time.Now(); err == nil && time.Since(start) < maxSettle; {
		if err := w.f.SetReadDeadline(time.Now().Add(settle)); err != nil {
			return false, err
		}
		var more bool
		if more, err = w.read(buf); errors.Is(err, os.ErrDeadlineExceeded) {
			return relevant, nil
		}
		relevant = relevant || more
	}
	return relevant, err
}

// read reads what is pending, waiting until something is.
func (w *Watcher) read(buf []byte) (bool, error) {
	n, err := w.f.Read(buf)
	if err != nil {
		return false, err
	}
	relevant := false
	for at := 0; at+syscall.SizeofInotifyEvent <= n; {
		wd := int(int32(binary.NativeEndian.Uint32(buf[at:])))
		mask := binary.NativeEndian.Uint32(buf[at+4:])
		size := int(binary.NativeEndian.Uint32(buf[at+12:]))
		at += syscall.SizeofInotifyEvent
		name := strings.TrimRight(string(buf[at:min(n, at+size)]), "\x00")
		at += size
		relevant = w.concerns(wd, mask, name) || relevant
	}
	return relevant, nil
}

// concerns says an event touches the watched files (or the way to them).
func (w *Watcher) concerns(wd int, mask uint32, name string) bool {
	switch {
	case mask&syscall.IN_Q_OVERFLOW != 0 || slices.Contains(w.fileWDs, wd):
		return true
	case wd != w.dirWD:
		return false
	case mask&(syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF) != 0:
		return true
	case name == "":
		return false
	case w.watched != w.dir:
		// An ancestor: the next directory on the way may have appeared.
		rel, _ := filepath.Rel(w.watched, w.dir)
		next, _, _ := strings.Cut(rel, string(filepath.Separator))
		return next == name && mask&(syscall.IN_CREATE|syscall.IN_MOVED_TO) != 0
	case !w.matches(name):
		return false
	}
	// A plain file just created is reported once written and closed; a
	// symlink is complete as soon as it exists.
	rewritten := uint32(syscall.IN_CLOSE_WRITE | syscall.IN_MOVED_TO | syscall.IN_MOVED_FROM | syscall.IN_DELETE)
	if mask&rewritten != 0 {
		return true
	}
	info, err := os.Lstat(filepath.Join(w.dir, name))
	return mask&syscall.IN_CREATE != 0 && err == nil && info.Mode()&os.ModeSymlink != 0
}

func (w *Watcher) matches(name string) bool {
	if w.one != "" {
		return name == w.one
	}
	return !strings.HasPrefix(name, ".") && strings.HasSuffix(name, w.ext)
}

// arm (re)places the watches: a file may be a new inode, and a directory on
// the way may have appeared or vanished.
func (w *Watcher) arm() {
	dir := w.dir
	for !isDir(dir) && dir != filepath.Dir(dir) {
		dir = filepath.Dir(dir)
	}
	dirWD := w.addWatch(dir, dirEvents)
	if dirWD >= 0 {
		w.watched = dir
	}
	if w.dirWD != dirWD {
		w.forget(w.dirWD)
	}
	w.dirWD = dirWD

	var files []string
	if w.one != "" {
		files = []string{filepath.Join(w.dir, w.one)}
	} else if entries, err := os.ReadDir(w.dir); err == nil {
		for _, e := range entries {
			if w.matches(e.Name()) {
				files = append(files, filepath.Join(w.dir, e.Name()))
			}
		}
	}
	var wds []int
	for _, f := range files {
		if wd := w.addWatch(f, fileEvents); wd >= 0 {
			wds = append(wds, wd)
		}
	}
	for _, old := range w.fileWDs {
		if !slices.Contains(wds, old) && old != w.dirWD {
			w.forget(old)
		}
	}
	w.fileWDs = wds
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// addWatch watches path for mask; -1 when it cannot (or is closed).
func (w *Watcher) addWatch(path string, mask uint32) int {
	wd := -1
	if raw, err := w.f.SyscallConn(); err == nil {
		_ = raw.Control(func(fd uintptr) {
			if n, err := syscall.InotifyAddWatch(int(fd), path, mask); err == nil {
				wd = n
			}
		})
	}
	return wd
}

// forget removes a watch that was replaced (one on a gone inode already is).
func (w *Watcher) forget(wd int) {
	if wd < 0 {
		return
	}
	if raw, err := w.f.SyscallConn(); err == nil {
		_ = raw.Control(func(fd uintptr) { _, _ = syscall.InotifyRmWatch(int(fd), uint32(wd)) })
	}
}
