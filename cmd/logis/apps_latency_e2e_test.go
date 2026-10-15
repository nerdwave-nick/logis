package main

import (
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/e2e/latency"
)

// The app launcher's warm open (story 5, budget 15 ms; Rust
// tests/latency.rs apps_latency) in the test compositor, the T0_NS way:
// with the daemon running and its index resident (the applications of the
// system this runs on, and their icons, rasterised by a first, warming
// session), `logis --apps` from exec to the daemon's first commit of its
// window, and to `settled`. Each session ends with its client: nothing is
// ever launched. Run alone, under the shared measurement lock (see the
// worker brief).
func TestLatencyApps(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	watch := e2e.StartWatchdog(t, c.Compositor)
	runs := latency.Runs(t)
	frameMS := 1000 / c.Harness.RefreshHz()
	// The index is read on a goroutine of its own at start; let it finish.
	time.Sleep(500 * time.Millisecond)
	warm := latency.WarmOpen(t, c.Daemon, watch, runs, true, func() func() {
		client := c.Apps()
		return func() { _ = client.Kill() }
	})
	series := []latency.Series{
		latency.NewSeries("apps warm open: start → first commit", latency.WarmOpenMS, warm.First),
		latency.NewSeries("apps warm open: start → open settled", latency.WarmOpenMS+150+2*frameMS, warm.Settled),
	}
	latency.Record(t, c.Compositor.Kind().Name(), series)
	warm.EnforceNoneSlow(t)
	latency.EnforceBudgets(t, series)
}
