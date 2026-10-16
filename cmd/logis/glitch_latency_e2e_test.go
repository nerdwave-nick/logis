package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/e2e/latency"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// Keystroke → commit with the glitch's key burst on and off (8ad70fee),
// the same session and theme apart from `on`: the bundled forge, whose
// glitch moves at rest, and a glitch calm at rest (keyGlitch, its key
// burst 400 ms), where a key wakes a resting panel. A first session,
// not recorded, warms up; each look is measured off, then on, keys 100 ms
// apart; the calm glitch's key burst also with each key from rest. Budget
// one frame. Run alone, under the
// shared measurement lock (see the worker brief).
func TestLatencyKeyBurst(t *testing.T) {
	c := e2e.StartCogitator(t, nil, `{ "reduce-motion": false, "theme": "keys" }`)
	frameMS := 1000 / c.Harness.RefreshHz()
	runs := latency.Runs(t)
	theme := filepath.Join(themesDir(t, c), "keys.jsonc")
	var series []latency.Series
	for i, look := range []struct{ name, text string }{
		{"warming up", `{ "extends": "forge-crimson-brass" }`},
		{"forge, key burst off", `{ "extends": "forge-crimson-brass", "effects": { "glitch": { "bursts": { "on": ["open", "tab"] } } } }`},
		{"forge, key burst on", `{ "extends": "forge-crimson-brass" }`},
		{"calm glitch, key burst off", keyGlitch(`[]`)},
		{"calm glitch, key burst on", keyGlitch(`["key"]`)},
	} {
		writeTheme(t, theme, look.text)
		c.Daemon.WaitFor("the theme "+look.name, e2e.Is("theme keys"))
		client := c.DmenuAll(dmenuItems)
		focused := c.Shown(3)
		c.Daemon.WaitForCommit()
		times := keystrokes(t, c, focused, []uint32{e2e.KeyDown, e2e.KeyUp}, runs)
		if i > 0 { // the first session warms the daemon and the GPU up
			series = append(series, latency.NewSeries("dmenu keystroke → commit, "+look.name, frameMS, times))
		}
		if i == 4 {
			// Each key from rest: the panel woke by the key is drawn at once.
			rested := make([]float64, 0, runs)
			for j := range runs {
				time.Sleep(600 * time.Millisecond) // past the 400 ms burst
				t0 := trace.MonotonicNS()
				c.Tap(focused, []uint32{e2e.KeyDown, e2e.KeyUp}[j%2])
				rested = append(rested, latency.MsBetween(t0, c.Daemon.WaitForCommitAfter(t0).NS))
			}
			series = append(series, latency.NewSeries("dmenu keystroke → commit, "+look.name+", from rest", frameMS, rested))
		}
		_ = client.Kill()
		c.Daemon.WaitFor("hide", e2e.Word("hide"))
	}
	latency.Record(t, c.Compositor.Kind().Name(), series)
	latency.EnforceBudgets(t, series)
}
