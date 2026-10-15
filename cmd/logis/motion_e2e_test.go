package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/wltest"
)

// Motion and blur end to end (spec §4.2, §4.3; Rust tests/motion.rs): a
// daemon in the test compositor opens, moves and closes its window paced
// by frame callbacks, draws nothing once settled, answers a dmenu caller
// before its close transition ends, shows a waiting session without
// waiting for it, and blurs behind the body where the compositor offers
// ext-background-effect. LOGIS_MOTION=4 slows every transition fourfold
// where a test must see one in progress, so that machine load cannot hide
// it.

var motionItems = []byte("alpha\nbeta\ngamma\n")

// slowCloseMS is the default close transition, fourfold.
const slowCloseMS = 4 * 110

func commitsBetween(events []trace.Event, from, to uint64) int {
	n := 0
	for _, e := range events {
		if e.What == "commit" && e.NS >= from && e.NS <= to {
			n++
		}
	}
	return n
}

// quietFor waits span and returns the trace events that arrived meanwhile.
func quietFor(c *e2e.Cogitator, span time.Duration) []trace.Event {
	since := trace.MonotonicNS()
	time.Sleep(span) // a span to watch for frames in, not a wait for a condition
	var after []trace.Event
	for _, e := range c.Daemon.Events() {
		if e.NS >= since {
			after = append(after, e)
		}
	}
	return after
}

func drewIn(events []trace.Event) bool {
	for _, e := range events {
		if strings.HasPrefix(e.What, "commit") {
			return true
		}
	}
	return false
}

func TestE2ETheWindowOpensOverSeveralFramesThenDrawsNothing(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	client := c.DmenuAll(motionItems)
	c.Shown(3)
	first := c.Daemon.WaitForFirstCommit()
	settled := c.Daemon.WaitSeen("the open settles", e2e.Is("settled"))
	// How many frames the open gets depends on the parent session pacing
	// the nested compositor.
	if frames := commitsBetween(c.Daemon.Events(), first.NS, settled.NS); frames < 2 {
		t.Fatalf("the open transition drew %d frames", frames)
	}
	// The first frame already shows the open one frame in; a slow first
	// frame may take some more of it.
	if ms := float64(settled.NS-first.NS) / 1e6; ms < 75 || ms >= 1000 {
		t.Fatalf("the 150 ms open took %.1f ms from the first commit", ms)
	}
	if after := quietFor(c, 400*time.Millisecond); drewIn(after) {
		t.Fatalf("an idle window drew: %v", after)
	}
	_ = client.Kill()
}

func TestE2EMovingTheSelectionGlidesOverFramesAndSettles(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	client := c.DmenuAll(motionItems)
	focused := c.Shown(3)
	c.Daemon.WaitSeen("the open settles", e2e.Is("settled"))
	c.Tap(focused, e2e.KeyDown)
	selected := c.Daemon.WaitFor("select", e2e.Is("select 1"))
	settled := c.Daemon.WaitFor("the glide settles", e2e.Is("settled"))
	if frames := commitsBetween(c.Daemon.Events(), selected.NS, settled.NS); frames < 2 {
		t.Fatalf("the band moved in %d frames", frames)
	}
	if after := quietFor(c, 300*time.Millisecond); drewIn(after) {
		t.Fatalf("%v", after)
	}
	_ = client.Kill()
}

func TestE2ETheAnswerIsSentAtAcceptWhileTheWindowStillFades(t *testing.T) {
	c := e2e.StartCogitator(t, []string{"LOGIS_MOTION=4"}, "")
	client := c.DmenuAll(motionItems)
	focused := c.Shown(3)
	c.Tap(focused, e2e.KeyEnter)
	accept := c.Daemon.WaitFor("accept", e2e.Is("accept 0"))
	ended := client.Ended()
	answered := trace.MonotonicNS()
	if ended.Code != 0 || string(ended.Stdout) != "alpha\n" {
		t.Fatalf("%+v", ended)
	}
	faded := c.Daemon.WaitFor("the close to end", e2e.Is("faded"))
	fading := 0
	for _, e := range c.Daemon.Events() {
		if e.What == "commit closing" {
			fading++
		}
	}
	if fading < 1 {
		t.Fatalf("the close drew %d frames", fading)
	}
	if answered >= faded.NS {
		t.Fatalf("the caller had its answer %.1f ms after accept, the window faded at %.1f ms",
			float64(answered-accept.NS)/1e6, float64(faded.NS-accept.NS)/1e6)
	}
	if faded.NS-accept.NS < (slowCloseMS-40)*uint64(time.Millisecond) {
		t.Fatalf("the close ended %.1f ms after accept", float64(faded.NS-accept.NS)/1e6)
	}
	if after := quietFor(c, 300*time.Millisecond); drewIn(after) {
		t.Fatalf("%v", after)
	}
}

func TestE2EAWaitingSessionIsShownWithoutWaitingForTheClose(t *testing.T) {
	c := e2e.StartCogitator(t, []string{"LOGIS_MOTION=4"}, "")
	first := c.DmenuAll([]byte("one\ntwo\n"))
	focused := c.Shown(2)
	second := c.DmenuAll([]byte("three\n"))
	c.Tap(focused, e2e.KeyEnter)
	accept := c.Daemon.WaitFor("accept", e2e.Is("accept 0"))
	started := time.Now()
	focused = c.Shown(1)
	shown := c.Daemon.WaitSeen("the second show", func(e trace.Event) bool {
		return strings.HasPrefix(e.What, "show ") && e.NS > accept.NS
	})
	if waited := shown.NS - accept.NS; waited >= slowCloseMS*uint64(time.Millisecond)/2 {
		t.Fatalf("the next session waited %.1f ms for a %d ms close", float64(waited)/1e6, slowCloseMS)
	}
	if time.Since(started) >= 5*time.Second {
		t.Fatal("the second session took too long")
	}
	for _, e := range c.Daemon.Events() {
		if e.What == "faded" && e.NS < shown.NS {
			t.Fatal("the closing window was waited for, not cut short")
		}
	}
	// Destroying the old window costs the compositor a frame and the GPU
	// its buffers: it goes only once the new window's first frame is out.
	frame := c.Daemon.WaitSeen("the new first frame", func(e trace.Event) bool { return e.What == "commit" && e.NS > shown.NS })
	cut := c.Daemon.WaitSeen("the old window cut", e2e.Is("cut"))
	if cut.NS <= frame.NS {
		t.Fatal("cut before the new window's first frame")
	}
	if got := first.Ended(); string(got.Stdout) != "one\n" {
		t.Fatalf("%+v", got)
	}
	c.Tap(focused, e2e.KeyEnter)
	if got := second.Ended(); string(got.Stdout) != "three\n" {
		t.Fatalf("%+v", got)
	}
}

func blurs(c *e2e.Cogitator) []string {
	var out []string
	for _, e := range c.Daemon.Events() {
		if strings.HasPrefix(e.What, "blur ") {
			out = append(out, e.What)
		}
	}
	return out
}

func TestE2EBlurFollowsTheBodyWhereTheCompositorOffersIt(t *testing.T) {
	c := e2e.StartCogitator(t, nil, "")
	client := c.DmenuAll(motionItems)
	c.Shown(3)
	c.Daemon.WaitSeen("the open settles", e2e.Is("settled"))
	got := blurs(c)
	switch c.Compositor.Kind() {
	case e2e.NestedNiri:
		// niri offers ext_background_effect_manager_v1: the body's rounded
		// rectangle, stepped at its corners (14 rows each, and the middle).
		if len(got) == 0 {
			t.Fatal("no blur region was set")
		}
		for _, b := range got {
			if b != "blur 29" {
				t.Fatalf("%q", got)
			}
		}
	case e2e.HeadlessSway:
		// sway does not: no blur, and no error either.
		if len(got) != 0 {
			t.Fatalf("%q", got)
		}
	}
	_ = client.Kill()
}

func TestE2EBlurOffInTheConfigSetsNoRegion(t *testing.T) {
	c := e2e.StartCogitator(t, nil, `{ "blur": false }`)
	client := c.DmenuAll(motionItems)
	c.Shown(3)
	c.Daemon.WaitSeen("the open settles", e2e.Is("settled"))
	if got := blurs(c); len(got) != 0 {
		t.Fatalf("blur was set although the config forbids it: %q", got)
	}
	_ = client.Kill()
}

func TestE2EAConfigEditTurnsBlurOnForTheVisibleWindow(t *testing.T) {
	c := e2e.StartCogitator(t, nil, `{ "blur": false }`)
	if c.Compositor.Kind() != e2e.NestedNiri {
		t.Skip("only niri offers blur")
	}
	client := c.DmenuAll(motionItems)
	c.Shown(3)
	c.Daemon.WaitSeen("the open settles", e2e.Is("settled"))
	writeTheme(t, filepath.Join(c.Compositor.ConfigHome(), "logis", "logis.jsonc"), e2e.Still(`{ "blur": true }`))
	c.Daemon.WaitFor("a blur region", e2e.Word("blur"))
	_ = client.Kill()
}

// Watched for 2.5 s (Rust: 1 s), past the 2 s write deadline of the last
// reply: no timer may outlive what armed it. The daemon runs without the
// garbage collector: a cycle the open started may otherwise end within
// the span with the runtime's own housekeeping (sweeping, then returning
// freed memory to the OS), which wakes threads once and is not ours.
//
// The span starts once the compositor has answered every frame and the
// daemon has come to rest (Rust: a 200 ms sleep). A nested compositor
// shows the last frame when the parent session lets it: under load, or
// with its window out of sight, seconds later. The daemon then wakes to
// read the answers, which is not idling wrongly.
func TestE2EAnIdleWindowWakesNoThreadOfTheDaemon(t *testing.T) {
	c := e2e.StartCogitator(t, []string{"GOGC=off"}, "")
	client := c.DmenuAll(motionItems)
	c.Shown(3)
	c.Daemon.WaitSeen("the open settles", e2e.Is("settled"))
	c.Daemon.WaitUntil("the compositor's answer to every frame", everyFrameAnswered)
	pid := c.Daemon.PID()
	atRest(t, pid)
	before := threadSwitches(pid)
	time.Sleep(2500 * time.Millisecond) // the span watched
	after := threadSwitches(pid)
	var moved []string
	for tid, n := range after {
		if n != before[tid] {
			wchan, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/task/" + tid + "/wchan")
			moved = append(moved, fmt.Sprintf("thread %s: %d (now in %s)", tid, n-before[tid], wchan))
		}
	}
	if len(moved) > 0 {
		t.Fatalf("context switches while idle: %v", moved)
	}
	_ = client.Kill()
}

// everyFrameAnswered: the compositor answered each frame of the visible
// window, both its frame callback and its presentation feedback (the
// test compositors offer wp_presentation), so it owes the daemon nothing.
func everyFrameAnswered(events []trace.Event) bool {
	commits, done, reported := 0, 0, 0
	for _, e := range events {
		switch e.What {
		case "commit":
			commits++
		case "frame-done":
			done++
		case "presented", "discarded":
			reported++
		}
	}
	return done == commits && reported == commits
}

// restSpan is how long no thread of an idle daemon switches before it
// counts as at rest: after its last work the Go runtime's monitor thread
// (sysmon) backs off in sleeps of at most 10 ms before it parks.
const restSpan = 50 * time.Millisecond

// atRest waits until no thread of pid has switched for restSpan, within
// e2e.Patience.
func atRest(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(e2e.Patience)
	last, quiet := threadSwitches(pid), time.Now()
	for time.Since(quiet) < restSpan {
		if time.Now().After(deadline) {
			t.Fatalf("the daemon never rested for %v in %v", restSpan, e2e.Patience)
		}
		time.Sleep(5 * time.Millisecond) // the poll interval
		if now := threadSwitches(pid); !maps.Equal(now, last) {
			last, quiet = now, time.Now()
		}
	}
}

// threadSwitches is the context switches of each thread of pid.
func threadSwitches(pid int) map[string]uint64 {
	out := map[string]uint64{}
	tasks, _ := os.ReadDir("/proc/" + strconv.Itoa(pid) + "/task")
	for _, task := range tasks {
		status, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/task/" + task.Name() + "/status")
		for line := range strings.Lines(string(status)) {
			for _, prefix := range []string{"voluntary_ctxt_switches:", "nonvoluntary_ctxt_switches:"} {
				if v, ok := strings.CutPrefix(line, prefix); ok {
					n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
					out[task.Name()] += n
				}
			}
		}
	}
	return out
}

// A LOGIS_MOTION the cogitator cannot read stops the daemon before it
// serves (exit 2, why), as Rust's window start refuses it: after the
// session's preflight, which a fake compositor offering every global
// passes.
func TestE2EARefusedMotionStopsTheDaemon(t *testing.T) {
	r := newRig(t)
	c, err := wltest.Serve(r.dir, "wayland-test", withLayerShell)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	cmd := r.logis("daemon")
	var env []string
	for _, kv := range cmd.Env {
		if !strings.HasPrefix(kv, "LOGIS_RESOLVER=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "LOGIS_MOTION=fast", "WAYLAND_DISPLAY=wayland-test")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 ||
		string(out) != "logis: LOGIS_MOTION=fast is no rite of motion: use 'off' or a factor such as 4\n" {
		t.Errorf("%v: %q", err, out)
	}
	if !gone(r.socket()) {
		t.Error("the socket was left behind")
	}
}
