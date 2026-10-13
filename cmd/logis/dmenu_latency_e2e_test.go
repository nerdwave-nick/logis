package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/e2e/latency"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// dmenu latency (spec §3 budgets; Rust tests/latency.rs dmenu_latency) in
// the test compositor, the T0_NS way: warm open is `logis --dmenu` from
// exec to the daemon's first commit of its window (and to `settled`);
// keystroke → commit with 3 items and with 100 000 lines, plain and in
// columns. Budgets: warm open 15 ms, a keystroke one frame. Run alone,
// under the shared measurement lock (see the worker brief).
func TestLatencyDmenu(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	watch := e2e.StartWatchdog(t, c.Compositor)
	runs := latency.Runs(t)
	frameMS := 1000 / c.Harness.RefreshHz()
	items := []byte("alpha\nbeta\ngamma\n")
	warm := latency.WarmOpen(t, c.Daemon, watch, runs, true, func() func() {
		client := c.DmenuAll(items)
		return func() { _ = client.Kill() }
	})

	client := c.DmenuAll(items)
	focused := c.Shown(3)
	few := keystrokes(t, c, focused, []uint32{code(t, 'a'), e2e.KeyBackspace}, runs)
	_ = client.Kill()
	c.Daemon.WaitFor("hide", e2e.Word("hide"))

	keys := []uint32{code(t, 'r'), code(t, 'i'), code(t, 't'), code(t, 'e'),
		e2e.KeyBackspace, e2e.KeyBackspace, e2e.KeyBackspace, e2e.KeyBackspace}
	many := func(columns bool) []float64 {
		client := c.Dmenu([]byte{}) // an empty first write: stdin stays open
		feeder := make(chan error, 1)
		go func() {
			_, err := client.Stdin.Write(manyLines(columns))
			if cerr := client.Stdin.Close(); err == nil {
				err = cerr
			}
			feeder <- err
		}()
		focused := c.Shown(100_000)
		if err := <-feeder; err != nil {
			t.Fatal(err)
		}
		times := keystrokes(t, c, focused, keys, max(runs, len(keys)))
		_ = client.Kill()
		c.Daemon.WaitFor("hide", e2e.Word("hide"))
		return times
	}
	plain, columned := many(false), many(true)

	series := []latency.Series{
		latency.NewSeries("dmenu warm open: start → first commit", latency.WarmOpenMS, warm.First),
		latency.NewSeries("dmenu warm open: start → open settled", latency.WarmOpenMS+150+2*frameMS, warm.Settled),
		latency.NewSeries("dmenu keystroke → commit, 3 items", frameMS, few),
		latency.NewSeries("dmenu keystroke → commit, 100 000 items", frameMS, plain),
		latency.NewSeries("dmenu keystroke → commit, 100 000 items in columns", frameMS, columned),
	}
	latency.Record(t, c.Compositor.Kind().Name(), series)
	warm.EnforceNoneSlow(t)
	latency.EnforceBudgets(t, series)
}

// keystrokes taps keys (cycled) runs times, each → the commit of the frame
// it caused.
func keystrokes(t *testing.T, c *e2e.Cogitator, f e2e.Focused, keys []uint32, runs int) []float64 {
	t.Helper()
	out := make([]float64, 0, runs)
	for i := range runs {
		// Idle first: the previous frame's callback has come back.
		time.Sleep(100 * time.Millisecond)
		t0 := trace.MonotonicNS()
		c.Tap(f, keys[i%len(keys)])
		out = append(out, latency.MsBetween(t0, c.Daemon.WaitForCommitAfter(t0).NS))
	}
	return out
}

// manyLines are 100 000 distinct path-like lines, as a big dmenu input
// might be; with columns, each a path, a word and a number, separated by
// tabs (Rust many_lines).
func manyLines(columns bool) []byte {
	words := []string{"rite", "aspect", "cogitator", "noosphere", "spirit", "litany", "forge"}
	var b strings.Builder
	for i := range 100_000 {
		path := fmt.Sprintf("/%s/%s/%s-%d", words[i%7], words[(i/7)%7], words[(i/49)%7], i)
		if columns {
			fmt.Fprintf(&b, "%s\t%s\t%d\n", path, words[(i/343)%7], i*7919%100_003)
		} else {
			b.WriteString(path + "\n")
		}
	}
	return []byte(b.String())
}

// A warm open shows its session without a single round trip to the
// compositor (Rust a_warm_open_waits_on_no_round_trip_before_it_is_shown):
// counted in the daemon's own WAYLAND_DEBUG log, from the last window gone
// (`faded`) to the next `show`; the daemon's first window may ask.
func TestE2EAWarmOpenWaitsOnNoRoundTripBeforeItIsShown(t *testing.T) {
	c := e2e.StartCogitator(t, []string{"WAYLAND_DEBUG=client"}, "")
	for run := range 3 {
		client := c.DmenuAll([]byte("alpha\n"))
		show := c.Daemon.WaitFor("show", e2e.Word("show"))
		c.Daemon.WaitForCommitAfter(show.NS)
		if run > 0 {
			lines := strings.Split(c.Daemon.Stderr(), "\n")
			shown, gone := -1, -1
			for i, l := range lines {
				if e, ok := trace.Parse(l); ok {
					switch {
					case e.What == show.What && e.NS == show.NS:
						shown = i
					case e.What == "faded" && shown < 0:
						gone = i
					}
				}
			}
			if shown < 0 || gone < 0 {
				t.Fatalf("show at %d, last faded at %d", shown, gone)
			}
			if n := latency.RoundTrips(lines[gone:shown]); n != 0 {
				t.Fatalf("warm open %d waited on %d round trips before it was shown", run, n)
			}
		}
		_ = client.Kill()
		c.Daemon.WaitFor("hide", e2e.Word("hide"))
		c.Daemon.WaitSeen("the window gone", func(e trace.Event) bool { return e.What == "faded" && e.NS >= show.NS })
	}
}
