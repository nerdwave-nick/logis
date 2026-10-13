package apps

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Watching the applications directories so the daemon's app index stays
// current (feat/logis §4.7; Rust src/apps/watch.rs), with inotify through
// package syscall as the config watcher does: the descriptor is read
// through the runtime's poller, so a waiting watcher costs no thread and
// no wake-up, and Close ends a Wait.
//
//   - Every directory the last scan read is watched for files created,
//     written, moved or deleted (inotify does not recurse: subdirectories
//     are watched one by one, and a new one shows up as a change of its
//     parent).
//   - An applications directory that does not exist yet is watched through
//     its nearest existing ancestor, for the creation of the next directory
//     on the way.
//
// A burst of events (a package manager writes many files) is reported
// once, when it has been quiet for Settle (at most MaxSettle after it
// began). Arm first reads what is pending under the watches it replaces: a
// change that came meanwhile (while the index was read) is reported by the
// next Wait at once, never lost with its watch.

const (
	// Settle is how long a burst must be quiet before it is reported.
	Settle = 100 * time.Millisecond
	// MaxSettle is the longest a burst is held back.
	MaxSettle = time.Second
)

const watchEvents = syscall.IN_CLOSE_WRITE | syscall.IN_CREATE | syscall.IN_DELETE |
	syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_DELETE_SELF |
	syscall.IN_MOVE_SELF | syscall.IN_ONLYDIR

// watched is what a watch descriptor watches: a directory entries are read
// from, or (next not nil) an ancestor of missing applications directories,
// with the names of the next directories on the way to them.
type watched struct {
	ancestor bool
	next     []string
}

// Watcher watches the app index's directories. Arm and Wait belong to one
// goroutine; Close may come from another.
type Watcher struct {
	f       *os.File
	watches map[int]*watched
	buf     []byte
	// changed: Arm found a change pending; the next Wait reports it.
	changed bool
}

// NewWatcher is a watcher with nothing watched yet.
func NewWatcher() (*Watcher, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil, os.NewSyscallError("inotify_init1", err)
	}
	return &Watcher{
		f:       os.NewFile(uintptr(fd), "inotify"),
		watches: map[int]*watched{},
		buf:     make([]byte, 64*(syscall.SizeofInotifyEvent+syscall.NAME_MAX+1)),
	}, nil
}

// Close stops the watcher; a Wait returns with an error.
func (w *Watcher) Close() error { return w.f.Close() }

// Arm watches read (the directories a scan read) and, for each of roots
// that does not exist, its nearest existing ancestor. It replaces what was
// watched before.
func (w *Watcher) Arm(read, roots []string) {
	w.changed = w.drain() || w.changed
	for wd := range w.watches {
		w.control(func(fd int) { _, _ = syscall.InotifyRmWatch(fd, uint32(wd)) })
	}
	clear(w.watches)
	for _, dir := range read {
		if wd, ok := w.add(dir); ok {
			w.watches[wd] = &watched{}
		}
	}
	for _, root := range roots {
		if isDir(root) {
			continue
		}
		ancestor := filepath.Dir(root)
		for !isDir(ancestor) && ancestor != filepath.Dir(ancestor) {
			ancestor = filepath.Dir(ancestor)
		}
		wd, ok := w.add(ancestor)
		if !ok {
			continue
		}
		entry, known := w.watches[wd]
		if !known {
			entry = &watched{ancestor: true}
			w.watches[wd] = entry
		}
		rel, err := filepath.Rel(ancestor, root)
		if entry.ancestor && err == nil {
			name, _, _ := strings.Cut(rel, string(filepath.Separator))
			entry.next = append(entry.next, name)
		}
	}
}

// add adds a watch on dir; its descriptor.
func (w *Watcher) add(dir string) (int, bool) {
	wd := -1
	w.control(func(fd int) {
		if n, err := syscall.InotifyAddWatch(fd, dir, watchEvents); err == nil {
			wd = n
		}
	})
	return wd, wd >= 0
}

// control runs f on the descriptor, which Close cannot pull away meanwhile.
func (w *Watcher) control(f func(fd int)) {
	if raw, err := w.f.SyscallConn(); err == nil {
		_ = raw.Control(func(fd uintptr) { f(int(fd)) })
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Wait waits for a settled burst of changes; false if timeout passed first
// (a negative timeout waits for ever).
func (w *Watcher) Wait(timeout time.Duration) (bool, error) {
	if w.changed {
		w.changed = false
		return true, nil
	}
	var deadline time.Time
	if timeout >= 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		if err := w.f.SetReadDeadline(deadline); err != nil {
			return false, err
		}
		relevant, err := w.read()
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return false, nil
		}
		for start := time.Now(); err == nil && time.Since(start) < MaxSettle; {
			if err = w.f.SetReadDeadline(time.Now().Add(Settle)); err != nil {
				return false, err
			}
			var more bool
			if more, err = w.read(); errors.Is(err, os.ErrDeadlineExceeded) {
				err = nil
				break
			}
			relevant = relevant || more
		}
		if err != nil {
			return false, err
		}
		if relevant {
			return true, nil
		}
	}
}

// read reads what is pending, waiting until something is; whether any of
// it is a change of the index.
func (w *Watcher) read() (bool, error) {
	n, err := w.f.Read(w.buf)
	if err != nil {
		return false, err
	}
	return w.relevant(w.buf[:n]), nil
}

// drain reads what is pending without waiting; whether any of it is a
// change of the index.
func (w *Watcher) drain() bool {
	raw, err := w.f.SyscallConn()
	if err != nil || w.f.SetReadDeadline(time.Time{}) != nil {
		return false
	}
	relevant := false
	for more := true; more; {
		more = false
		_ = raw.Read(func(fd uintptr) bool {
			n, err := syscall.Read(int(fd), w.buf)
			if err == nil && n > 0 {
				relevant = w.relevant(w.buf[:n]) || relevant
				more = true
			}
			return true // never wait
		})
	}
	return relevant
}

// relevant says whether any event in b is a change of the index.
func (w *Watcher) relevant(b []byte) bool {
	relevant := false
	for len(b) >= syscall.SizeofInotifyEvent {
		wd := int(int32(binary.NativeEndian.Uint32(b[0:])))
		mask := binary.NativeEndian.Uint32(b[4:])
		size := int(binary.NativeEndian.Uint32(b[12:]))
		end := min(syscall.SizeofInotifyEvent+size, len(b))
		name := strings.TrimRight(string(b[syscall.SizeofInotifyEvent:end]), "\x00")
		relevant = relevant || w.concerns(wd, mask, name)
		b = b[end:]
	}
	return relevant
}

func (w *Watcher) concerns(wd int, mask uint32, name string) bool {
	if mask&syscall.IN_Q_OVERFLOW != 0 {
		return true
	}
	entry, ok := w.watches[wd]
	switch {
	case !ok:
		return false
	case !entry.ancestor:
		return true
	case mask&(syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF) != 0:
		return true
	}
	return mask&(syscall.IN_CREATE|syscall.IN_MOVED_TO) != 0 && slices.Contains(entry.next, name)
}
