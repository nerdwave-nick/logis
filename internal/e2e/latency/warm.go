package latency

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// WarmOpens is the samples of WarmOpen.
type WarmOpens struct {
	// First is client start → the window's first commit, ms.
	First []float64
	// Settled is client start → the open settled, ms (none for a window
	// whose theme moves on its own: it never settles).
	Settled []float64
	// Slow says what to say of each open over SlowOpenMS.
	Slow []string
}

// EnforceNoneSlow fails t for every slow open, when Enforce says so.
func (w WarmOpens) EnforceNoneSlow(t testing.TB) {
	t.Helper()
	if Enforce() && len(w.Slow) > 0 {
		t.Error(strings.Join(w.Slow, "\n"))
	}
}

// WarmOpen measures runs warm opens of the daemon's window (Rust
// tests/latency.rs warm_open; the hook for the daemon's sessions): open
// starts a client (logis --dmenu …) and returns how to end its session.
// T0 is read just before open; the sample is T0 → the daemon's first
// `commit` at or after it, after its `show`, and (if settles) T0 → its
// `settled`. A first, warming run is not counted. Each session is ended
// and gone (`hide`, then `faded`) 100 ms before the next starts, as a warm
// open finds nothing on screen. With a watchdog, a slow open says whether
// the compositor itself stalled meanwhile.
func WarmOpen(t testing.TB, daemon *e2e.Traced, watch *e2e.Watchdog, runs int, settles bool, open func() (end func())) WarmOpens {
	t.Helper()
	var w WarmOpens
	for run := 0; run <= runs; run++ {
		time.Sleep(100 * time.Millisecond)
		t0 := trace.MonotonicNS()
		end := open()
		daemon.WaitFor("show", e2e.Word("show"))
		commit := daemon.WaitFor("a first frame", func(e trace.Event) bool { return e.What == "commit" && e.NS >= t0 })
		if run > 0 {
			ms := MsBetween(t0, commit.NS)
			if ms > SlowOpenMS {
				w.Slow = append(w.Slow, slowOpen(t, daemon, watch, t0, commit.NS, ms))
			}
			w.First = append(w.First, ms)
		}
		if settles {
			rest := daemon.WaitFor("the open settles", e2e.Is("settled"))
			if run > 0 {
				w.Settled = append(w.Settled, MsBetween(t0, rest.NS))
			}
		}
		end()
		daemon.WaitFor("hide", e2e.Word("hide"))
		// Gone, not only hidden: its close transition has run.
		daemon.WaitSeen("the window gone", func(e trace.Event) bool { return e.What == "faded" && e.NS >= t0 })
	}
	return w
}

// slowOpen logs the window's trace of a slow open and says whose it was.
func slowOpen(t testing.TB, daemon *e2e.Traced, watch *e2e.Watchdog, t0, commit uint64, ms float64) string {
	var steps []string
	for _, e := range daemon.Events() {
		if e.NS >= t0 && e.NS <= commit {
			steps = append(steps, fmt.Sprintf("+%.1f %s", MsBetween(t0, e.NS), e.What))
		}
	}
	var stall float64
	var stalled bool
	if watch != nil {
		stall, stalled = watch.StalledWithin(t0, commit)
	}
	verdict := SlowOpen(ms, stall, stalled)
	t.Logf("SLOW OPEN %.1f ms: %q\n  %s", ms, steps, verdict)
	return verdict
}
