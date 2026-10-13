package daemon

import (
	"sync"

	"github.com/nerdwave-nick/logis/internal/apps"
	"github.com/nerdwave-nick/logis/internal/dbus"
)

// The apps of a running daemon (Rust src/daemon/mod.rs apps_state and
// index_apps): frecency from $XDG_STATE_HOME/logis/ and the session bus's
// address at start; the index read on a goroutine of its own (so it never
// delays the first frame), and read again whenever the applications
// directories change. The directories, the current desktops (OnlyShowIn),
// PATH (TryExec) and the locale are the daemon's own environment's.

// startApps gives the core its apps from getenv and starts the indexer;
// stop ends the indexer and waits for frecency still being kept.
func (l *loop) startApps(getenv func(string) string) (stop func()) {
	s := newAppsState()
	s.bus = dbus.SessionAddress(getenv)
	if s.frecencyPath = apps.FrecencyPath(getenv); s.frecencyPath != "" {
		f, err := apps.LoadFrecency(s.frecencyPath)
		if err != nil {
			l.logf(err.Error() + "; frecency starts anew")
		} else {
			s.frecency = f
		}
	}
	s.keep = l.keeper.keep(l)
	l.core.apps = s
	stopIndexer := l.indexApps(getenv)
	return func() {
		stopIndexer()
		l.keeper.wg.Wait()
	}
}

// indexApps reads the index now and after every settled change, posting
// each to the loop, until stop.
func (l *loop) indexApps(getenv func(string) string) (stop func()) {
	dirs, cx := apps.DirsFromVars(getenv), apps.ContextFromVars(getenv)
	w, err := apps.NewWatcher()
	if err != nil {
		l.logf("applications are not watched, changes need a restart: " + err.Error())
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var read []string
		for {
			// Watched before reading, so no change falls between the two.
			if w != nil {
				w.Arm(read, dirs)
			}
			ix := apps.Scan(dirs, cx)
			read = ix.Dirs
			if w != nil {
				w.Arm(read, dirs)
			}
			l.post(func(l *loop) { l.core.indexed(ix) })
			if w == nil {
				return
			}
			if _, err := w.Wait(-1); err != nil {
				return // closed
			}
		}
	}()
	return func() {
		if w != nil {
			_ = w.Close()
		}
		<-done
	}
}

// keeper keeps frecency off the daemon's goroutine (the file is synced to
// disk), one save at a time and never an older snapshot over a newer one.
type keeper struct {
	wg sync.WaitGroup
	// seq numbers the snapshots (the loop's goroutine only).
	seq uint64
	mu  sync.Mutex
	// written is the newest snapshot written.
	written uint64
}

func (k *keeper) keep(l *loop) func(f *apps.Frecency, path string) {
	return func(f *apps.Frecency, path string) {
		k.seq++
		seq := k.seq
		k.wg.Add(1)
		go func() {
			defer k.wg.Done()
			k.mu.Lock()
			defer k.mu.Unlock()
			if seq < k.written {
				return
			}
			k.written = seq
			if err := f.Save(path); err != nil {
				l.post(func(l *loop) { l.logf("frecency is not kept in " + path + ": " + err.Error()) })
			}
		}()
	}
}
