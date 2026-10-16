package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// Themes end to end (spec §4.9): a daemon in the test compositor wears the
// theme its config names, wears an edited theme at once on a visible
// window, keeps the last sound theme when an edit is broken and says so
// over the list, and keeps a window drawing only while the theme's
// continuous effects move (reduce-motion stills them, and then an idle
// window draws nothing). Runs only with LOGIS_E2E=niri|sway.

func themesDir(t *testing.T, c *e2e.Cogitator) string {
	t.Helper()
	dir := filepath.Join(c.Compositor.ConfigHome(), "logis", "themes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeTheme(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// commitsFor is the daemon's commits from now on for span.
func commitsFor(c *e2e.Cogitator, span time.Duration) int {
	since := trace.MonotonicNS()
	time.Sleep(span) // a span to count frames in, not a wait for a condition
	n := 0
	for _, e := range c.Daemon.Events() {
		if e.NS >= since && e.What == "commit" {
			n++
		}
	}
	return n
}

// switches is the context switches of every thread of pid, summed.
func switches(pid int) uint64 {
	tasks, _ := os.ReadDir("/proc/" + strconv.Itoa(pid) + "/task")
	var n uint64
	for _, task := range tasks {
		status, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/task/" + task.Name() + "/status")
		for line := range strings.Lines(string(status)) {
			for _, prefix := range []string{"voluntary_ctxt_switches:", "nonvoluntary_ctxt_switches:"} {
				if v, ok := strings.CutPrefix(line, prefix); ok {
					count, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
					n += count
				}
			}
		}
	}
	return n
}

// cpuTicks is pid's CPU time so far in clock ticks (utime + stime).
func cpuTicks(pid int) uint64 {
	stat, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	s := string(stat)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+2:])
	u, _ := strconv.ParseUint(fields[11], 10, 64)
	k, _ := strconv.ParseUint(fields[12], 10, 64)
	return u + k
}

// wakeups is the daemon's wake-ups per second and CPU (percent of one
// core at 100 ticks a second) over span.
func wakeups(c *e2e.Cogitator, span time.Duration) (float64, float64) {
	pid := c.Daemon.PID()
	before, ticks := switches(pid), cpuTicks(pid)
	time.Sleep(span) // the span measured
	seconds := span.Seconds()
	return float64(switches(pid)-before) / seconds, float64(cpuTicks(pid)-ticks) / seconds
}

func TestE2EAnEditedThemeReachesTheVisibleWindowAndABrokenOneIsReported(t *testing.T) {
	c := e2e.StartCogitator(t, nil, `{ "theme": "mine" }`)
	mine := filepath.Join(themesDir(t, c), "mine.jsonc")
	writeTheme(t, mine, `{ "extends": "cogitator-phosphor" }`)
	c.Daemon.WaitFor("the theme the config names", e2e.Is("theme mine"))
	opened := trace.MonotonicNS()
	client := c.DmenuAll(dmenuItems, "--lines=3")
	c.Shown(3)
	// It may settle before the focus Shown waits for arrives.
	c.Daemon.WaitSeen("the open settles", func(e trace.Event) bool { return e.NS >= opened && e.What == "settled" })

	// An edit: worn at once, and the visible window draws again.
	writeTheme(t, mine, `{ "extends": "cogitator-phosphor", "palette": { "phosphor": "#ffb000" } }`)
	c.Daemon.WaitFor("the edited theme", e2e.Is("theme mine"))
	c.Daemon.WaitFor("a frame in it", e2e.Is("commit"))

	// A broken edit: the last sound theme stays, a notice says where.
	broken := trace.MonotonicNS()
	writeTheme(t, mine, "{\n  \"glow\": { \"radius\": \"wide\" }\n}")
	toast := c.Daemon.WaitFor("the theme's notice", e2e.Word("toast"))
	if !strings.Contains(toast.What, "the theme is heretical") || !strings.Contains(toast.What, "mine.jsonc:2:") {
		t.Fatalf("%s", toast.What)
	}
	c.Daemon.WaitFor("the notice drawn", e2e.Is("commit"))
	c.Daemon.WaitFor("the notice goes", e2e.Is("untoast"))
	for _, e := range c.Daemon.Events() {
		if e.NS >= broken && strings.HasPrefix(e.What, "theme ") {
			t.Errorf("a broken theme changed what is worn: %s", e.What)
		}
	}
	_ = client.Kill()
}

func TestE2EContinuousEffectsKeepAWindowDrawingUntilMotionIsReduced(t *testing.T) {
	c := e2e.StartCogitator(t, nil, `{ "reduce-motion": false }`)
	client := c.DmenuAll(dmenuItems)
	c.Shown(3)
	time.Sleep(300 * time.Millisecond) // past the first frames
	span := time.Second
	moving := commitsFor(c, span)
	// Any steady drawing will do: a software renderer on a CI runner is slow.
	if moving < 5 {
		t.Fatalf("only %d frames in a second of pulse and highlight", moving)
	}
	busy, busyCPU := wakeups(c, 2*time.Second)

	config := filepath.Join(c.Compositor.ConfigHome(), "logis", "logis.jsonc")
	writeTheme(t, config, `{ "reduce-motion": true }`)
	c.Daemon.WaitFor("the stilled theme", e2e.Is("theme forge-crimson-brass"))
	c.Daemon.WaitFor("the window settles", e2e.Is("settled"))
	time.Sleep(200 * time.Millisecond) // past a frame in flight
	if still := commitsFor(c, span); still != 0 {
		t.Fatalf("a stilled window kept drawing: %d frames", still)
	}
	idle, idleCPU := wakeups(c, 2*time.Second)
	t.Logf("REPORT idle visible window: %d frames/s, %.0f daemon wake-ups/s, %.1f%% CPU with continuous effects; "+
		"0 frames/s, %.1f wake-ups/s, %.1f%% CPU with reduce-motion", moving, busy, busyCPU, idle, idleCPU)
	_ = client.Kill()
}
