package e2e

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/trace"
)

func TestMain(m *testing.M) { Main(m) }

// The harness in a real compositor (LOGIS_E2E=niri|sway): Rust tests/e2e.rs's
// harness scenario with the debug panel (the only window logis shows yet),
// and checks of the pointer, the selection owner and the watchdog against
// probe clients. Skipped without LOGIS_E2E.

func black(px [4]byte) bool { return px == [4]byte{0, 0, 0, 255} }

func TestE2EThePanelTakesFocusReceivesKeysAndShowsThem(t *testing.T) {
	c := Start(t, Requested(t))
	h := Connect(t, c)
	kb := h.VirtualKeyboard()
	panel := Spawn(t, c.Logis("daemon", "--debug-panel"))
	focused := panel.WaitForFocus()
	panel.WaitForFirstCommit()

	before := h.SettledCapture()
	w, ht := before.Width, before.Height
	for _, at := range [][2]int{{0, 0}, {w - 1, ht - 1}} {
		if px := before.Pixel(at[0], at[1]); !black(px) {
			t.Errorf("the backdrop at %v is %v, not opaque black", at, px)
		}
	}
	if px := before.Pixel(w/2, ht/2); black(px) {
		t.Error("no panel at the output's centre")
	}

	// A real Shift press plus its mask around the letter, as a user types.
	kb.Type(focused, "A")
	for _, step := range []string{"key 42 pressed", "modifiers 1 0 0 0", "key 30 pressed",
		"key 30 released", "key 42 released", "modifiers 0 0 0 0"} {
		panel.WaitFor(step, Is(step))
	}
	kb.Hold(focused, Control)
	panel.WaitFor("Control's mask", Is("modifiers 4 0 0 0"))
	kb.Release(focused, Control)

	for _, row := range []string{"select 1", "select 2"} {
		kb.Tap(focused, KeyDown)
		at := panel.WaitFor(row, Is(row))
		panel.WaitForCommitAfter(at.NS)
	}
	if after := h.SettledCapture(); bytes.Equal(after.Pixels, before.Pixels) {
		t.Error("the moved selection is not on screen")
	}

	kb.Tap(focused, KeyEsc)
	panel.WaitFor("dismiss", Is("dismiss"))
	if code := panel.WaitExit(); code != 0 {
		t.Errorf("Escape closes the panel with exit %d, want 0", code)
	}
}

func TestE2ETypingWithoutFocusFailsTheTest(t *testing.T) {
	c := Start(t, Requested(t))
	kb := Connect(t, c).VirtualKeyboard()
	fake := &recorder{TB: t}
	kb.h.t = fake
	func() {
		defer func() { _ = recover() }()
		kb.Tap(Focused{}, KeyEsc)
	}()
	if !strings.Contains(strings.Join(fake.fatals, ""), "before the client reported keyboard focus") {
		t.Errorf("a key went out without focus: %q", fake.fatals)
	}
}

// recorder records Fatal instead of ending the test.
type recorder struct {
	testing.TB
	fatals []string
}

func (r *recorder) Helper() {}
func (r *recorder) Fatal(args ...any) {
	r.fatals = append(r.fatals, args[0].(string))
	panic("fatal")
}

func TestE2ETheGuardRefusesTheLiveSessionAndStrangers(t *testing.T) {
	c := Start(t, Requested(t))
	if _, err := ConnectVerified(c.Socket(), os.Getpid()); err == nil || !strings.Contains(err.Error(), "refusing to inject") {
		t.Errorf("the test compositor was accepted as another process: %v", err)
	}
	if c.Kind() != NestedNiri {
		return
	}
	// The running session's own socket: refused before a request is sent.
	if _, err := ConnectVerified(parentSocket(t), c.PID()); err == nil || !strings.Contains(err.Error(), "refusing to inject") {
		t.Errorf("the live session was accepted: %v", err)
	}
}

func TestE2EThePointerMovesClicksAndScrollsOverASurface(t *testing.T) {
	c := Start(t, Requested(t))
	h := Connect(t, c)
	vp := h.VirtualPointer()
	p := newProbe(t, c)
	p.surface(c.RuntimeDir(), 200, 100)

	w, ht := h.OutputSize()
	cx, cy := float64(w)/2, float64(ht)/2
	var seen [2][2]float64
	vp.MoveTo(cx, cy)
	p.until("the pointer on the probe", func() bool { return slices.Contains(p.pointer, "enter") })
	seen[0] = p.at
	vp.MoveTo(cx+20, cy+10)
	p.until("motion", func() bool { return p.at != seen[0] })
	seen[1] = p.at
	panel := Calibrate([2][2]float64{{cx, cy}, {cx + 20, cy + 10}}, seen)
	if x, y := panel.At(seen[1][0], seen[1][1]); x != cx+20 || y != cy+10 {
		t.Errorf("calibrated (%v, %v) for (%v, %v)", x, y, cx+20, cy+10)
	}

	vp.Click(BtnLeft)
	p.until("a click", func() bool {
		return slices.Contains(p.pointer, "button 272 1") && slices.Contains(p.pointer, "button 272 0")
	})
	vp.Wheel(2)
	p.until("two notches", func() bool { return slices.Contains(p.pointer, "discrete 2") })
	vp.Finger(12)
	p.until("a finger", func() bool { return slices.Contains(p.pointer, "axis 12") })
}

func TestE2ETheSelectionOwnerServesItsTexts(t *testing.T) {
	c := Start(t, Requested(t))
	owner := OwnSelection(t, c, "brass", "rune")
	p := newProbe(t, c)
	p.selections()
	p.until("both selections", func() bool { return p.selection != nil && p.primary != nil })
	if got := p.mimes[p.selection.ID()]; !slices.Equal(got, Offered) {
		t.Errorf("offered %q, want %q", got, Offered)
	}
	if got := p.receive(p.selection, "text/plain;charset=utf-8"); got != "brass" {
		t.Errorf("clipboard %q", got)
	}
	if got := p.receive(p.primary, "UTF8_STRING"); got != "rune" {
		t.Errorf("primary %q", got)
	}
	want := []string{"clipboard text/plain;charset=utf-8", "primary UTF8_STRING"}
	if got := owner.Requests(); !slices.Equal(got, want) {
		t.Errorf("requests %q, want %q", got, want)
	}
}

// msFrom is the time from from to at in ms (negative: before).
func msFrom(at, from uint64) float64 { return (float64(at) - float64(from)) / 1e6 }

// Start returns a compositor that has finished starting up (it answers
// promptly), so a stall seen before the stop would be the watchdog's
// invention.
func TestE2ETheWatchdogSeesTheCompositorStall(t *testing.T) {
	c := Start(t, Requested(t))
	w := StartWatchdog(t, c)
	time.Sleep(50 * time.Millisecond)
	from := trace.MonotonicNS()
	_ = syscall.Kill(c.PID(), syscall.SIGSTOP)
	time.Sleep(300 * time.Millisecond)
	_ = syscall.Kill(c.PID(), syscall.SIGCONT)
	time.Sleep(100 * time.Millisecond)
	ms, stalled := w.StalledWithin(from, trace.MonotonicNS())
	if !stalled || ms < 250 {
		t.Errorf("a 300 ms stop seen as %.1f ms (stalled %v)", ms, stalled)
	}
	if _, stalled := w.StalledWithin(0, from-StallNS); stalled {
		t.Error("a stall before the stop")
	}
	if t.Failed() {
		w.mu.Lock()
		for _, s := range w.stalls {
			t.Logf("stall: asked %+.1f ms, answered %+.1f ms from the stop", msFrom(s[0], from), msFrom(s[1], from))
		}
		w.mu.Unlock()
	}
}

func TestE2EACogitatorsDaemonGreetsAndAnswersAClient(t *testing.T) {
	c := StartCogitator(t, nil, "")
	client := c.DmenuAll([]byte("alpha\nbeta\n"))
	focused := c.Shown(2)
	c.Tap(focused, KeyEsc)
	c.Next("dismiss")
	if ended := client.Ended(); ended.Code != 1 || len(ended.Stdout) != 0 {
		t.Errorf("the dmenu client ended %+v, want exit 1 and no stdout", ended)
	}
}
