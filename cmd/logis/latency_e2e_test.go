package main

import (
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/e2e/latency"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// Latency (spec §3 budgets, §6) in the test compositor, the research/bench
// T0_NS way, against `logis daemon --debug-panel` (the only window logis
// shows until the daemon renders sessions; then latency.WarmOpen measures
// `logis --dmenu` from exec to the daemon's first commit):
//
//   - debug panel warm open: the panel's `show` → its first `commit`: the
//     surface made, configured, drawn and presented on a GPU already woken
//     and shaders already compiled. Exec, connect and the GPU's wake are
//     not in it: that is cold start, which the owner measures separately.
//   - keystroke → commit: the harness's clock just before it injects Down
//     or Up → the commit of the frame showing the moved selection; budget
//     one frame at the output's refresh rate.
//
// LOGIS_E2E=niri|sway runs it, LOGIS_LATENCY_RUNS sets the samples (10),
// LOGIS_LATENCY_REPORT appends JSON lines, LOGIS_LATENCY_BUDGETS=enforce
// fails a median over budget or a slow open. Run measurements one at a
// time under the shared lock (see the worker brief), the nested niri in
// view.
func TestLatencyDebugPanel(t *testing.T) {
	c := e2e.Start(t, e2e.Requested(t))
	h := e2e.Connect(t, c)
	watch := e2e.StartWatchdog(t, c)
	runs := latency.Runs(t)
	frameMS := 1000 / h.RefreshHz()

	var opens []float64
	var slow []string
	for run := 0; run <= runs; run++ {
		time.Sleep(100 * time.Millisecond)
		panel := e2e.Spawn(t, c.Command(buildLogis(t), "daemon", "--debug-panel"))
		show := panel.WaitFor("show", e2e.Is("show debug-panel"))
		commit := panel.WaitForCommitAfter(show.NS)
		_ = syscall.Kill(panel.PID(), syscall.SIGTERM)
		if code := panel.WaitExit(); code != 0 {
			t.Fatalf("the panel exited %d on SIGTERM", code)
		}
		if run == 0 {
			continue // warming: the compositor's first client
		}
		ms := latency.MsBetween(show.NS, commit.NS)
		if ms > latency.SlowOpenMS {
			stall, stalled := watch.StalledWithin(show.NS, commit.NS)
			slow = append(slow, latency.SlowOpen(ms, stall, stalled))
		}
		opens = append(opens, ms)
	}

	kb := h.VirtualKeyboard()
	panel := e2e.Spawn(t, c.Command(buildLogis(t), "daemon", "--debug-panel"))
	focused := panel.WaitForFocus()
	panel.WaitForFirstCommit()
	var keys []float64
	for i := range runs {
		// Idle first: the previous frame's callback has come back.
		time.Sleep(100 * time.Millisecond)
		key := uint32(e2e.KeyDown)
		if i%2 == 1 {
			key = e2e.KeyUp
		}
		t0 := trace.MonotonicNS()
		kb.Tap(focused, key)
		keys = append(keys, latency.MsBetween(t0, panel.WaitForCommitAfter(t0).NS))
	}

	series := []latency.Series{
		latency.NewSeries("debug panel warm open: show → first commit", latency.WarmOpenMS, opens),
		latency.NewSeries("debug panel keystroke → commit", frameMS, keys),
	}
	latency.Record(t, c.Kind().Name(), series)
	for _, s := range slow {
		t.Log("SLOW OPEN: " + s)
	}
	latency.WarmOpens{Slow: slow}.EnforceNoneSlow(t)
	latency.EnforceBudgets(t, series)
}
