package main

import (
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/e2e/latency"
)

// Warm open in the themes as worn (spec §3 budget 15 ms; feat/logis-go
// G18): the bundled default with its continuous effects moving (the
// default config: no reduce-motion), and cogitator-phosphor, whose bloom
// and curved glass are the heaviest post-processing pass. A moving theme
// never settles, so only exec → first commit is measured. Run alone, under
// the shared measurement lock (see the worker brief).
func TestLatencyThemedWarmOpen(t *testing.T) {
	for _, c := range []struct{ name, config string }{
		{"forge-crimson-brass, moving", `{ "reduce-motion": false }`},
		{"cogitator-phosphor, moving", `{ "reduce-motion": false, "theme": "cogitator-phosphor" }`},
	} {
		t.Run(filepath.Base(c.name), func(t *testing.T) {
			cog := e2e.StartCogitator(t, nil, c.config)
			watch := e2e.StartWatchdog(t, cog.Compositor)
			warm := latency.WarmOpen(t, cog.Daemon, watch, latency.Runs(t), false, func() func() {
				client := cog.DmenuAll(dmenuItems)
				return func() { _ = client.Kill() }
			})
			series := []latency.Series{
				latency.NewSeries("dmenu warm open in "+c.name+": start → first commit", latency.WarmOpenMS, warm.First),
			}
			latency.Record(t, cog.Compositor.Kind().Name(), series)
			warm.EnforceNoneSlow(t)
			latency.EnforceBudgets(t, series)
		})
	}
}
