package window

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/wltest"
)

// The window's motion, blur and frame timing (Rust src/window/panel/paint.rs,
// windows/motion.rs, blur.rs, clock.rs) on the fake compositor, with the
// window's clock and trace in the test's hands.

var (
	withEffects      = append(slices.Clone(niri), wltest.Global{Interface: "ext_background_effect_manager_v1", Version: 1})
	withPresentation = append(slices.Clone(niri), wltest.Global{Interface: "wp_presentation", Version: 2})
)

// steady is a motion of plain numbers: linear opens and closes of 100 ms
// from and to half opacity, 10 px down.
var steady = motion.Motion{
	Open:      motion.Millis(100, motion.Linear),
	OpenFrom:  motion.Pose{Opacity: 0.5, Offset: [2]float32{0, 10}},
	Close:     motion.Millis(100, motion.Linear),
	CloseTo:   motion.Pose{Opacity: 0, Offset: [2]float32{0, 10}},
	Selection: motion.Millis(100, motion.Linear),
}

// moved is a fake shell that also logs what motion and blur ask of the
// compositor: regions, the input region, blur, presentation feedback, and
// the requests of surfaces shown before the current one ("… old").
type moved struct {
	*fakeShell
	surfaces  []uint32
	regions   []uint32
	effects   []uint32
	feedbacks []uint32
}

func newMoved(t *testing.T, globals []wltest.Global) *moved {
	m := &moved{fakeShell: newFakeShell(t, globals)}
	m.comp.OnRequest(func(cl *wltest.Client, r wltest.Request) {
		m.request(cl, r)
		m.more(cl, r)
	})
	return m
}

func (m *moved) more(cl *wltest.Client, r wltest.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd := wltest.NewReader(r)
	switch {
	case r.Object == cl.Bound("wl_compositor") && r.Opcode == 0:
		m.surfaces = append(m.surfaces, m.surface)
	case r.Object == cl.Bound("wl_compositor") && r.Opcode == 1:
		m.regions = append(m.regions, rd.Uint())
		m.logf("region")
	case slices.Contains(m.regions, r.Object) && r.Opcode == 1:
		m.logf("add %d %d %d %d", int32(rd.Uint()), int32(rd.Uint()), int32(rd.Uint()), int32(rd.Uint()))
	case r.Object == m.surface && r.Opcode == 5:
		m.logf("input region")
	case slices.Contains(m.surfaces, r.Object) && r.Object != m.surface:
		switch r.Opcode {
		case 0:
			m.logf("destroy old surface")
		case 3:
			m.frames = append(m.frames, rd.Uint())
			m.logf("frame old")
		case 6:
			m.logf("commit old")
		}
	case r.Object == cl.Bound("ext_background_effect_manager_v1") && r.Opcode == 1:
		m.effects = append(m.effects, rd.Uint())
		m.logf("background effect")
	case slices.Contains(m.effects, r.Object):
		switch r.Opcode {
		case 0:
			m.logf("destroy effect")
		case 1:
			m.logf("blur region")
		}
	case r.Object == cl.Bound("wp_presentation") && r.Opcode == 1:
		_ = rd.Uint() // the surface
		m.feedbacks = append(m.feedbacks, rd.Uint())
		m.logf("feedback")
	}
}

// to sends an event to the object id the caller reads under the lock.
func (m *moved) to(id func(*moved) uint32, opcode uint16, args ...any) {
	m.t.Helper()
	m.mu.Lock()
	cl, object := m.client, id(m)
	m.mu.Unlock()
	if err := cl.Send(object, opcode, args...); err != nil {
		m.t.Fatal(err)
	}
}

// clock is the window's CLOCK_MONOTONIC, stepped by the test.
type clock struct{ ns uint64 }

func (c *clock) now() uint64        { return c.ns }
func (c *clock) advance(ms float64) { c.ns += uint64(ms * 1e6) }

// traced records what the window emits to the test trace.
type traced struct {
	mu     sync.Mutex
	events []string
}

func traceOf(t *testing.T) *traced {
	tr := &traced{}
	before := emit
	emit = func(format string, args ...any) {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		tr.events = append(tr.events, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { emit = before })
	return tr
}

func (tr *traced) take() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	e := tr.events
	tr.events = nil
	return e
}

func count(events []string, what string) int {
	n := 0
	for _, e := range events {
		if e == what {
			n++
		}
	}
	return n
}

// framed records the frames it drew and draws a body with a backdrop.
type framed struct {
	frames   []Frame
	backdrop *Backdrop
	moving   bool
}

func (p *framed) paint(f Frame) Drawing {
	p.frames = append(p.frames, f)
	return Drawing{Moving: p.moving, Backdrop: p.backdrop}
}

func (p *framed) last() Frame { return p.frames[len(p.frames)-1] }

// openMoving opens a window on m with motion and a clock at 1 s.
func openMoving(t *testing.T, m *moved, mo motion.Motion) (*Window, *clock) {
	t.Helper()
	w, _ := openFake(t, m.fakeShell)
	c := &clock{ns: 1_000_000_000}
	w.now = c.now
	w.SetMotion(mo)
	return w, c
}

// shown shows p and draws its first frame.
func shown(t *testing.T, m *moved, w *Window, p *framed) {
	t.Helper()
	if err := w.ShowFrames(size(300, 200), p.paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, m.fakeShell, "commit")
	m.configure(1, 0, 0)
	until(t, w, m.fakeShell, "ack 1", "frame", "commit")
}

// next lets the last frame's callback come after ms and waits for the
// frame it brings (commit, or "commit old" for an old surface); what the
// compositor saw meanwhile.
func next(t *testing.T, m *moved, w *Window, c *clock, ms float64, commit string) []string {
	t.Helper()
	c.advance(ms)
	m.frameDone()
	return until(t, w, m.fakeShell, commit)
}

func TestAnOpenDrawsOnEachFrameCallbackUntilItRestsThenSettles(t *testing.T) {
	m := newMoved(t, niri)
	tr := traceOf(t)
	w, c := openMoving(t, m, steady)
	p := &framed{}
	shown(t, m, w, p)
	first := p.last()
	// The first frame is shown one (60 Hz) interval into the open.
	if first.At != c.ns || first.Interval != fallbackInterval || !nearly(first.Pose.Opacity, 0.5+0.5*1/6.0, 1e-3) {
		t.Fatalf("first frame %+v", first)
	}
	for range 5 {
		next(t, m, w, c, 1000/60.0, "commit")
	}
	if got := p.last().Pose; got != motion.Rest {
		t.Fatalf("after the open: %+v", got)
	}
	poses := make([]float32, len(p.frames))
	for i, f := range p.frames {
		poses[i] = f.Pose.Opacity
	}
	if !slices.IsSorted(poses) || poses[0] == poses[len(poses)-1] {
		t.Errorf("the open does not brighten: %v", poses)
	}
	events := tr.take()
	if count(events, "commit") != 6 || count(events, "settled") != 1 || events[len(events)-1] != "settled" {
		t.Fatalf("%q", events)
	}
	m.frameDone()
	settle(t, w)
	if log := m.takeLog(); slices.Contains(log, "commit") || len(p.frames) != 6 {
		t.Errorf("an open at rest drew again: %q", log)
	}
}

func TestAStillPanelSettlesWithItsFirstFrameOnly(t *testing.T) {
	m := newMoved(t, niri)
	tr := traceOf(t)
	w, _ := openMoving(t, m, motion.Off)
	p := &framed{}
	shown(t, m, w, p)
	if got := p.last().Pose; got != motion.Rest {
		t.Fatalf("%+v", got)
	}
	m.frameDone()
	w.RequestRedraw()
	until(t, w, m.fakeShell, "commit")
	if events := tr.take(); !slices.Equal(events, []string{"commit", "settled", "frame-done", "commit"}) {
		t.Errorf("%q", events)
	}
}

func TestHideFadesThePanelOutWithoutTheKeyboardThenItGoes(t *testing.T) {
	m := newMoved(t, niri)
	tr := traceOf(t)
	w, c := openMoving(t, m, motion.Off)
	p := &framed{}
	shown(t, m, w, p)
	w.SetMotion(steady)
	tr.take()
	w.Hide()
	if w.Visible() {
		t.Fatal("a closing panel is still visible")
	}
	until(t, w, m.fakeShell, "keyboard 0", "region", "input region", "commit")
	m.frameDone() // the last open frame's
	until(t, w, m.fakeShell, "frame", "commit")
	half := 0
	for range 10 {
		c.advance(25)
		m.frameDone()
		settle(t, w)
		if half == 0 && len(p.frames) > 2 {
			half = len(p.frames)
		}
		if slices.Contains(m.takeLog(), "destroy surface") {
			break
		}
	}
	events := tr.take()
	if count(events, "commit closing") < 3 || count(events, "faded") != 1 || events[len(events)-1] != "faded" {
		t.Fatalf("%q", events)
	}
	if count(events, "commit") != 0 || count(events, "settled") != 0 {
		t.Errorf("a closing panel reported visible frames: %q", events)
	}
	fading := p.frames[1:]
	for i := 1; i < len(fading); i++ {
		if fading[i].Pose.Opacity > fading[i-1].Pose.Opacity {
			t.Errorf("the close brightens: %+v", fading)
		}
	}
	if first, last := fading[0].Pose, fading[len(fading)-1].Pose; last.Opacity >= first.Opacity || last.Offset[1] <= first.Offset[1] {
		t.Errorf("the close went from %+v to %+v", first, last)
	}
}

func TestHideIsAtOnceWithoutMotionOrBeforeTheFirstFrame(t *testing.T) {
	m := newMoved(t, niri)
	tr := traceOf(t)
	w, _ := openMoving(t, m, motion.Off)
	shown(t, m, w, &framed{})
	tr.take()
	w.Hide()
	until(t, w, m.fakeShell, "destroy surface")
	if events := tr.take(); !slices.Equal(events, []string{"faded"}) {
		t.Errorf("%q", events)
	}
	w.SetMotion(steady)
	if err := w.ShowFrames(size(300, 200), (&framed{}).paint); err != nil {
		t.Fatal(err)
	}
	w.Hide() // never drawn
	until(t, w, m.fakeShell, "destroy surface")
	if events := tr.take(); !slices.Equal(events, []string{"faded"}) {
		t.Errorf("%q", events)
	}
}

func TestAShowCutsAClosingPanelShortOnceItsOwnFirstFrameIsOut(t *testing.T) {
	m := newMoved(t, niri)
	tr := traceOf(t)
	w, c := openMoving(t, m, steady)
	shown(t, m, w, &framed{})
	w.Hide()
	until(t, w, m.fakeShell, "commit")
	tr.take()
	next(t, m, w, c, 10, "commit")
	if err := w.ShowFrames(size(300, 200), (&framed{}).paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, m.fakeShell, "commit")
	m.frameDone() // the old panel's callback: it is not drawn again
	settle(t, w)
	if log := m.takeLog(); slices.Contains(log, "destroy old surface") || slices.Contains(log, "commit old") {
		t.Fatalf("the closing panel went or drew before the new one showed: %q", log)
	}
	m.configure(2, 0, 0)
	log := until(t, w, m.fakeShell, "ack 2", "commit", "destroy old surface")
	if slices.Index(log, "destroy old surface") < slices.Index(log, "commit") {
		t.Errorf("cut before the new first frame: %q", log)
	}
	if events := tr.take(); !slices.Contains(events, "cut") || slices.Contains(events, "faded") ||
		slices.Index(events, "cut") < slices.Index(events, "commit") {
		t.Errorf("%q", events)
	}
}

// A closing panel whose frame callbacks stopped (its output went dark)
// still goes: a Dispatch that would wait for events wakes at its end plus
// a grace.
func TestAClosingPanelWhoseCallbacksStopGoesAfterItsGrace(t *testing.T) {
	m := newMoved(t, niri)
	tr := traceOf(t)
	w, c := openMoving(t, m, steady)
	shown(t, m, w, &framed{})
	w.Hide()
	until(t, w, m.fakeShell, "commit")
	if left, ok := w.motionDeadline(); !ok || left.Milliseconds() != 200 {
		t.Fatalf("%v %v, want the 100 ms close and its 100 ms grace", left, ok)
	}
	tr.take()
	c.advance(190)
	start := time.Now()
	if err := w.Dispatch(2 * time.Second); err != nil { // wakes 10 ms on
		t.Fatal(err)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("waited %v for a panel due in 10 ms", waited)
	}
	until(t, w, m.fakeShell, "destroy surface")
	if events := tr.take(); !slices.Equal(events, []string{"faded"}) {
		t.Errorf("%q", events)
	}
	if _, ok := w.motionDeadline(); ok {
		t.Error("a deadline without a closing panel")
	}
}

func TestBlurFollowsTheBodyAndItsPoseWhereOffered(t *testing.T) {
	m := newMoved(t, withEffects)
	tr := traceOf(t)
	w, c := openMoving(t, m, steady)
	w.SetBlur(true)
	p := &framed{backdrop: &Backdrop{Rect: [4]float32{24, 24, 300, 100}, Radius: 14}}
	if err := w.ShowFrames(size(348, 148), p.paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, m.fakeShell, "background effect", "commit")
	m.configure(1, 0, 0)
	log := until(t, w, m.fakeShell, "ack 1", "region", "blur region", "commit")
	adds := slices.DeleteFunc(slices.Clone(log), func(e string) bool { return !strings.HasPrefix(e, "add ") })
	// The first frame is one sixth into the open, 8.33 px below rest: the
	// region follows, its top row at 33 rather than 24.
	if len(adds) != 29 || !strings.HasPrefix(adds[0], "add 38 33 ") || adds[14] != "add 24 47 300 71" {
		t.Fatalf("%d rectangles: %q", len(adds), adds)
	}
	if events := tr.take(); !slices.Contains(events, "blur 29") {
		t.Fatalf("%q", events)
	}
	for range 5 {
		next(t, m, w, c, 1000/60.0, "commit")
	}
	if p.last().Pose != motion.Rest {
		t.Fatalf("not at rest: %+v", p.last().Pose)
	}
	w.RequestRedraw()
	if log := next(t, m, w, c, 1000/60.0, "commit"); slices.Contains(log, "blur region") {
		t.Errorf("an unchanged region was set again: %q", log)
	}
	w.SetBlur(false)
	until(t, w, m.fakeShell, "destroy effect")
	m.frameDone()
	until(t, w, m.fakeShell, "commit")
}

func TestWithoutTheProtocolOrWithBlurOffNothingIsBlurred(t *testing.T) {
	for name, globals := range map[string][]wltest.Global{"not offered": niri, "off": withEffects} {
		t.Run(name, func(t *testing.T) {
			m := newMoved(t, globals)
			tr := traceOf(t)
			w, _ := openMoving(t, m, motion.Off)
			w.SetBlur(name == "not offered")
			p := &framed{backdrop: &Backdrop{Rect: [4]float32{24, 24, 300, 100}, Radius: 14}}
			shown(t, m, w, p)
			if log := m.takeLog(); slices.Contains(log, "background effect") || slices.Contains(log, "blur region") {
				t.Errorf("%q", log)
			}
			for _, e := range tr.take() {
				if strings.HasPrefix(e, "blur ") {
					t.Errorf("%q", e)
				}
			}
		})
	}
}

func TestTheCompositorSaysWhetherItCanBlur(t *testing.T) {
	m := newMoved(t, withEffects)
	tr := traceOf(t)
	w, _ := openMoving(t, m, motion.Off)
	shown(t, m, w, &framed{})
	m.to(func(m *moved) uint32 { return m.client.Bound("ext_background_effect_manager_v1") }, 0, uint32(1))
	settle(t, w)
	if events := tr.take(); !slices.Contains(events, "blur-capable true") {
		t.Errorf("%q", events)
	}
}

func TestPresentationFeedbackPutsFramesOnTheRefreshGrid(t *testing.T) {
	m := newMoved(t, withPresentation)
	w, c := openMoving(t, m, motion.Off)
	p := &framed{}
	shown(t, m, w, p)
	m.to(func(m *moved) uint32 { return m.client.Bound("wp_presentation") }, 0, uint32(clockMonotonic))
	if log := m.takeLog(); len(m.feedbacks) != 1 {
		t.Fatalf("%d feedbacks: %q", len(m.feedbacks), log)
	}
	// Shown at 1.004 s; the output refreshes every 8 ms.
	at := c.ns + 4_000_000
	secs := at / 1_000_000_000
	m.to(func(m *moved) uint32 { return m.feedbacks[0] }, 1,
		uint32(secs>>32), uint32(secs), uint32(at%1_000_000_000), uint32(8_000_000), uint32(0), uint32(1), uint32(0))
	c.advance(5)
	w.RequestRedraw()
	next(t, m, w, c, 0, "commit")
	if got := p.last().FrameTime; got != (FrameTime{At: at + 8_000_000, Interval: 8_000_000}) {
		t.Errorf("%+v, want the refresh after now", got)
	}
}

func nearly(a, b, tolerance float32) bool { return a-b < tolerance && b-a < tolerance }

// The compositor's answers to the visible window's frames (its frame
// callback, its presentation feedback) are reported, so that a test can
// tell when the compositor owes the window nothing more; a closing
// window's are not, as its frames are `commit closing`.
func TestTheCompositorsAnswersToTheVisibleWindowsFramesAreReported(t *testing.T) {
	m := newMoved(t, withPresentation)
	tr := traceOf(t)
	w, c := openMoving(t, m, motion.Off)
	p := &framed{}
	shown(t, m, w, p)
	tr.take()
	m.to(func(m *moved) uint32 { return m.feedbacks[0] }, 1,
		uint32(0), uint32(1), uint32(0), uint32(8_000_000), uint32(0), uint32(1), uint32(0))
	m.frameDone()
	settle(t, w)
	if events := tr.take(); !slices.Equal(events, []string{"presented", "frame-done"}) {
		t.Fatalf("the first frame's answers: %q", events)
	}
	w.RequestRedraw()
	until(t, w, m.fakeShell, "feedback", "commit")
	m.to(func(m *moved) uint32 { return m.feedbacks[1] }, 2)
	settle(t, w)
	if events := tr.take(); !slices.Equal(events, []string{"commit", "discarded"}) {
		t.Fatalf("a discarded frame: %q", events)
	}
	w.SetMotion(steady)
	w.Hide()
	m.frameDone() // the last open frame's
	until(t, w, m.fakeShell, "frame", "commit")
	tr.take()
	c.advance(25)
	m.to(func(m *moved) uint32 { return m.feedbacks[len(m.feedbacks)-1] }, 2)
	m.frameDone()
	settle(t, w)
	for _, e := range tr.take() {
		if e == "frame-done" || e == "presented" || e == "discarded" {
			t.Errorf("a closing window's answer reported: %q", e)
		}
	}
}
