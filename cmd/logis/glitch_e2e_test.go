package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// The glitch end to end (33cfcf44): a glitch that is calm at rest draws
// its open burst, then the window settles and draws nothing, until a
// random burst wakes it, after which it settles again. Runs only with
// LOGIS_E2E=niri|sway.

// calmGlitch is the forge with nothing moving but the glitch's bursts:
// an open burst of 600 ms and random bursts at perMinute.
func calmGlitch(perMinute string) string {
	return `{ "extends": "forge-crimson-brass", "glow": { "pulse": 0 }, "border": { "highlight": false },
	  "effects": { "scanlines": { "roll": 0 }, "aberration": { "wobble": 0 }, "grain": { "rate": 0 }, "flicker": 0,
	    "glitch": { "episodes": { "period-s": 0 }, "wobble": { "rest": 0 }, "tracking": { "px": 0 },
	      "noise-band": { "rest": 0 }, "static": { "rest": 0 }, "line-pulse": { "rest": 0 },
	      "bursts": { "open-ms": 600, "random-per-minute": ` + perMinute + `, "random-ms": 300 } } } }`
}

func TestE2EAGlitchBurstDrawsThenTheWindowSleepsUntilTheNext(t *testing.T) {
	c := e2e.StartCogitator(t, nil, `{ "reduce-motion": false, "theme": "calm" }`)
	writeTheme(t, filepath.Join(themesDir(t, c), "calm.jsonc"), calmGlitch("0"))
	c.Daemon.WaitFor("the theme the config names", e2e.Is("theme calm"))
	opened := trace.MonotonicNS()
	client := c.DmenuAll(dmenuItems)
	c.Shown(3)
	settled := c.Daemon.WaitSeen("the open burst settles", func(e trace.Event) bool { return e.NS >= opened && e.What == "settled" })
	frames := 0
	for _, e := range c.Daemon.Events() {
		if e.NS >= opened && e.NS <= settled.NS && e.What == "commit" {
			frames++
		}
	}
	burst := time.Duration(settled.NS - opened)
	if frames < 5 || burst < 600*time.Millisecond {
		t.Fatalf("the burst drew %d frames and settled after %v", frames, burst)
	}
	time.Sleep(200 * time.Millisecond) // past a frame in flight
	if still := commitsFor(c, time.Second); still != 0 {
		t.Fatalf("a calm glitch kept drawing: %d frames", still)
	}
	_ = client.Kill()

	// Random bursts, 30 a minute: the window wakes for one, draws it, and
	// settles again.
	writeTheme(t, filepath.Join(themesDir(t, c), "calm.jsonc"), calmGlitch("30"))
	c.Daemon.WaitFor("the edited theme", e2e.Is("theme calm"))
	opened = trace.MonotonicNS()
	client = c.DmenuAll(dmenuItems)
	c.Shown(3)
	first := c.Daemon.WaitSeen("the open burst settles", func(e trace.Event) bool { return e.NS >= opened && e.What == "settled" })
	woken := c.Daemon.WaitSeen("a random burst draws", func(e trace.Event) bool {
		return e.NS > first.NS+uint64(100*time.Millisecond) && e.What == "commit"
	})
	again := c.Daemon.WaitSeen("it settles again", func(e trace.Event) bool { return e.NS > woken.NS && e.What == "settled" })
	t.Logf("REPORT glitch: open burst %d frames, settled %v after the show; a random burst woke the window %v later and settled after %v",
		frames, burst, time.Duration(woken.NS-first.NS), time.Duration(again.NS-woken.NS))
	_ = client.Kill()
}

// keyGlitch is calmGlitch's forge with no open or random bursts and no
// selection glide: only a key press's burst (on, as `on` names it; 400 ms
// long) moves it.
func keyGlitch(on string) string {
	return `{ "extends": "forge-crimson-brass", "glow": { "pulse": 0 }, "border": { "highlight": false },
	  "motion": { "select-ms": 0 },
	  "effects": { "scanlines": { "roll": 0 }, "aberration": { "wobble": 0 }, "grain": { "rate": 0 }, "flicker": 0,
	    "glitch": { "episodes": { "period-s": 0 }, "wobble": { "rest": 0 }, "tracking": { "px": 0 },
	      "noise-band": { "rest": 0 }, "static": { "rest": 0 }, "line-pulse": { "rest": 0 },
	      "bursts": { "on": ` + on + `, "key-ms": 400, "key-strength": 0.3, "random-per-minute": 0 } } } }`
}

// burstAfterKey presses Down in the resting panel and waits until it
// rests again: how long it drew after the press and how many frames.
func burstAfterKey(t *testing.T, c *e2e.Cogitator, focused e2e.Focused) (time.Duration, int) {
	t.Helper()
	since := trace.MonotonicNS()
	c.Tap(focused, e2e.KeyDown)
	pressed := c.Daemon.WaitSeen("the press", func(e trace.Event) bool {
		return e.NS >= since && e.What == "key 108 pressed"
	})
	settled := c.Daemon.WaitSeen("the panel rests again", func(e trace.Event) bool {
		return e.NS > pressed.NS && e.What == "settled"
	})
	frames := 0
	for _, e := range c.Daemon.Events() {
		if e.NS >= pressed.NS && e.NS <= settled.NS && e.What == "commit" {
			frames++
		}
	}
	return time.Duration(settled.NS - pressed.NS), frames
}

// A key press wakes a resting panel for about key-ms of frames, then lets
// it rest again (8ad70fee); without "key" among the events the same press
// draws only its change.
func TestE2EAKeyPressWakesARestingPanelForItsBurst(t *testing.T) {
	c := e2e.StartCogitator(t, nil, `{ "reduce-motion": false, "theme": "calm" }`)
	writeTheme(t, filepath.Join(themesDir(t, c), "calm.jsonc"), keyGlitch(`["key"]`))
	c.Daemon.WaitFor("the theme the config names", e2e.Is("theme calm"))
	client := c.DmenuAll(dmenuItems)
	focused := c.Shown(3)
	c.OpenSettled()
	time.Sleep(200 * time.Millisecond) // past a frame in flight
	if still := commitsFor(c, 500*time.Millisecond); still != 0 {
		t.Fatalf("the panel does not rest before the key: %d frames", still)
	}
	burst, frames := burstAfterKey(t, c, focused)
	// About key-ms: a frame is stamped with when it is expected on screen,
	// so the last one of the burst may commit up to a frame or two early.
	if burst < 350*time.Millisecond || burst > 700*time.Millisecond || frames < 5 {
		t.Fatalf("a key burst of 400 ms drew %d frames and rested after %v", frames, burst)
	}
	time.Sleep(200 * time.Millisecond)
	if still := commitsFor(c, time.Second); still != 0 {
		t.Fatalf("the panel kept drawing after the key burst: %d frames", still)
	}

	// Without "key" among the events: the press draws its change and rests.
	writeTheme(t, filepath.Join(themesDir(t, c), "calm.jsonc"), keyGlitch(`["open"]`))
	c.Daemon.WaitFor("the edited theme", e2e.Is("theme calm"))
	time.Sleep(300 * time.Millisecond) // the retheme's own frame
	since := trace.MonotonicNS()
	c.Tap(focused, e2e.KeyUp)
	pressed := c.Daemon.WaitSeen("the press", func(e trace.Event) bool {
		return e.NS >= since && e.What == "key 103 pressed"
	})
	c.Daemon.WaitSeen("its change drawn", func(e trace.Event) bool { return e.NS > pressed.NS && e.What == "commit" })
	time.Sleep(600 * time.Millisecond) // a span to count frames in
	plain := 0
	for _, e := range c.Daemon.Events() {
		if e.NS >= pressed.NS && e.NS <= pressed.NS+uint64(600*time.Millisecond) && e.What == "commit" {
			plain++
		}
	}
	if plain > 3 {
		t.Fatalf("without the key burst the press drew %d frames in 600 ms", plain)
	}
	t.Logf("REPORT key burst: a 400 ms burst drew %d frames and rested %v after the press; without it %d frames",
		frames, burst, plain)
	_ = client.Kill()
}
