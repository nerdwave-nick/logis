package e2e

import (
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/trace"
)

// StallNS: a round trip longer than this is a stall.
const StallNS = 50_000_000

// watchEvery is how often the watchdog asks the compositor.
const watchEvery = 10 * time.Millisecond

// Watchdog watches the test compositor itself: does it answer at all? A
// goroutine of its own makes a wl_display.sync round trip on a verified
// connection of its own every 10 ms and keeps each that took longer than
// StallNS. A slow measurement that overlaps such a stall was the
// compositor's, not the client's: a nested niri whose window the session
// does not draw (another workspace, out of view) gets frame callbacks
// about once a second and blocks in its buffer swap meanwhile, answering
// nobody for up to two seconds (Rust ticket d49bceee).
type Watchdog struct {
	mu     sync.Mutex
	stalls [][2]uint64 // (asked, answered), CLOCK_MONOTONIC ns
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

// StartWatchdog starts watching c; t's cleanup stops it.
func StartWatchdog(t testing.TB, c *Compositor) *Watchdog {
	t.Helper()
	w := &Watchdog{stop: make(chan struct{}), done: make(chan struct{})}
	started := make(chan error, 1)
	go w.watch(c.Socket(), c.PID(), started)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Stop)
	return w
}

// watch owns its connection: made, used and closed on this goroutine.
func (w *Watchdog) watch(socket string, pid int, started chan<- error) {
	defer close(w.done)
	d, err := ConnectVerified(socket, pid)
	started <- err
	if err != nil {
		return
	}
	defer d.Close()
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		asked := trace.MonotonicNS()
		if d.Roundtrip() != nil {
			return // the compositor is gone
		}
		if answered := trace.MonotonicNS(); answered-asked > StallNS {
			w.mu.Lock()
			w.stalls = append(w.stalls, [2]uint64{asked, answered})
			w.mu.Unlock()
		}
		time.Sleep(watchEvery)
	}
}

// Stop ends the watch and waits for it (at most Patience: a stalled
// compositor may hold its last round trip).
func (w *Watchdog) Stop() {
	w.once.Do(func() { close(w.stop) })
	select {
	case <-w.done:
	case <-time.After(Patience):
	}
}

// StalledWithin is the longest time in ms the compositor did not answer
// within [from, to] (CLOCK_MONOTONIC ns), and whether it stalled then.
func (w *Watchdog) StalledWithin(from, to uint64) (ms float64, stalled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range w.stalls {
		a, b := max(s[0], from), min(s[1], to)
		if a < b {
			ms, stalled = max(ms, float64(b-a)/1e6), true
		}
	}
	return ms, stalled
}
