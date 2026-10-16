package window

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/nerdwave-nick/logis/internal/egl"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/wlproto"
	"github.com/nerdwave-nick/logis/internal/wltest"
)

var niri = append(slices.Clone(withLayerShell), wltest.Global{Interface: "wl_seat", Version: 9})

// fakeShell plays the compositor side of a layer surface on a wltest
// compositor: it logs the requests a window sends and sends configures,
// preferred scales, frame callbacks and `closed` when the test says so.
type fakeShell struct {
	t    *testing.T
	comp *wltest.Compositor

	mu                                   sync.Mutex
	log                                  []string
	client                               *wltest.Client
	surface, layer, fractional, viewport uint32
	frames                               []uint32
	keyboard                             uint32
}

func newFakeShell(t *testing.T, globals []wltest.Global) *fakeShell {
	t.Helper()
	dir := t.TempDir()
	f := &fakeShell{t: t, comp: serve(t, dir, globals)}
	f.comp.OnRequest(f.request)
	session(t, dir, "wayland-test")
	return f
}

func (f *fakeShell) logf(format string, args ...any) {
	f.log = append(f.log, fmt.Sprintf(format, args...))
}

func (f *fakeShell) request(cl *wltest.Client, r wltest.Request) {
	for _, fd := range r.Fds {
		_ = syscall.Close(fd)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.client = cl
	rd := wltest.NewReader(r)
	switch {
	case r.Object == cl.Bound("wl_compositor") && r.Opcode == 0:
		f.surface = rd.Uint()
	case r.Object == cl.Bound("wp_fractional_scale_manager_v1") && r.Opcode == 1:
		f.fractional = rd.Uint()
	case r.Object == cl.Bound("wp_viewporter") && r.Opcode == 1:
		f.viewport = rd.Uint()
	case r.Object == cl.Bound("wl_seat") && r.Opcode == 1:
		f.keyboard = rd.Uint()
	case r.Object == cl.Bound("zwlr_layer_shell_v1") && r.Opcode == 0:
		f.layer = rd.Uint()
		surface, output, layer := rd.Uint(), rd.Uint(), rd.Uint()
		f.logf("layer_surface surface=%v output=%d layer=%d ns=%s", surface == f.surface, output, layer, rd.String())
	case r.Object == f.surface && f.surface != 0:
		switch r.Opcode {
		case 0:
			f.logf("destroy surface")
		case 3:
			f.frames = append(f.frames, rd.Uint())
			f.logf("frame")
		case 6:
			f.logf("commit")
		}
	case r.Object == f.viewport && r.Opcode == 2:
		f.logf("destination %d %d", rd.Uint(), rd.Uint())
	case r.Object == f.layer && f.layer != 0:
		switch r.Opcode {
		case 0:
			f.logf("size %d %d", rd.Uint(), rd.Uint())
		case 4:
			f.logf("keyboard %d", rd.Uint())
		case 6:
			f.logf("ack %d", rd.Uint())
		case 7:
			f.logf("destroy layer")
		}
	}
}

func (f *fakeShell) send(object func(*fakeShell) uint32, opcode uint16, args ...any) {
	f.t.Helper()
	f.mu.Lock()
	cl, id := f.client, object(f)
	f.mu.Unlock()
	if cl == nil || id == 0 {
		f.t.Fatalf("nothing to send event %d to", opcode)
	}
	if err := cl.Send(id, opcode, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeShell) configure(serial, w, h uint32) {
	f.send(func(f *fakeShell) uint32 { return f.layer }, 0, serial, w, h)
}

func (f *fakeShell) preferScale(n uint32) {
	f.send(func(f *fakeShell) uint32 { return f.fractional }, 0, n)
}

func (f *fakeShell) closed() { f.send(func(f *fakeShell) uint32 { return f.layer }, 1) }

func (f *fakeShell) frameDone() {
	f.mu.Lock()
	frames, cl := f.frames, f.client
	f.frames = nil
	f.mu.Unlock()
	for _, id := range frames {
		_ = cl.Send(id, 0, uint32(0))
		_ = cl.Send(1, 1, id) // wl_display.delete_id
	}
}

func (f *fakeShell) takeLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	log := f.log
	f.log = nil
	return log
}

// fakeGPU stands in for EGL: its surfaces commit the wl_surface on swap,
// as eglSwapBuffers does.
type fakeGPU struct {
	opened int
	// created and resizes are the buffer sizes surfaces were made with
	// and later resized to.
	created []scale.Size
	resizes []scale.Size
	swaps   int
	live    int
	// fail, if set, makes newSurface fail with what it returns.
	fail func() error
}

type fakePresenter struct {
	g       *fakeGPU
	surface *wlproto.WlSurface
}

func (g *fakeGPU) open(unsafe.Pointer) (gpu, error) { g.opened++; return g, nil }
func (g *fakeGPU) info() egl.Info                   { return egl.Info{Renderer: "fake"} }
func (g *fakeGPU) close()                           {}
func (g *fakeGPU) newSurface(s *wlproto.WlSurface, size scale.Size) (presenter, error) {
	if g.fail != nil {
		return nil, g.fail()
	}
	g.live++
	g.created = append(g.created, size)
	return &fakePresenter{g, s}, nil
}
func (p *fakePresenter) resize(size scale.Size) { p.g.resizes = append(p.g.resizes, size) }
func (p *fakePresenter) makeCurrent() error     { return nil }
func (p *fakePresenter) swap() error {
	p.g.swaps++
	p.surface.Commit()
	return nil
}
func (p *fakePresenter) destroy() { p.g.live-- }

// painter records the geometries frames were drawn at.
type painter struct{ frames []Geometry }

func (p *painter) paint(g Geometry) bool {
	p.frames = append(p.frames, g)
	return false
}

func openFake(t *testing.T, f *fakeShell) (*Window, *fakeGPU) {
	t.Helper()
	g := &fakeGPU{}
	w, err := openWith(g.open)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w, g
}

// until dispatches until the compositor logged want (in this order, among
// other requests) or fails after a while.
func until(t *testing.T, w *Window, f *fakeShell, want ...string) []string {
	t.Helper()
	var log []string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := w.Dispatch(5 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		log = append(log, f.takeLog()...)
		rest := want
		for _, entry := range log {
			if len(rest) > 0 && entry == rest[0] {
				rest = rest[1:]
			}
		}
		if len(rest) == 0 {
			return log
		}
	}
	t.Fatalf("the compositor saw %q, want %q in order", log, want)
	return nil
}

// settle dispatches for a while, for "nothing more happens" checks.
func settle(t *testing.T, w *Window) {
	t.Helper()
	for range 10 {
		if err := w.Dispatch(2 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
}

const requested = 688 // × 408, the debug panel's size

func showConfigured(t *testing.T, f *fakeShell, w *Window, p *painter) {
	t.Helper()
	if err := w.Show(size(requested, 408), p.paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, f, "commit")
	f.configure(5, 0, 0)
	until(t, w, f, "ack 5", "frame", "commit")
}

func TestTheGPUWakesWhenTheWindowOpensNotOnShow(t *testing.T) {
	f := newFakeShell(t, niri)
	_ = f
	_, g := openFake(t, f)
	if g.opened != 1 {
		t.Errorf("GPU opened %d times at open, want 1", g.opened)
	}
}

func TestOpenRefusesACompositorWithoutLayerShellBeforeWakingTheGPU(t *testing.T) {
	newFakeShell(t, withoutLayerShell)
	g := &fakeGPU{}
	_, err := openWith(g.open)
	var noAltar *NoLayerShellError
	if !errors.As(err, &noAltar) {
		t.Errorf("%v", err)
	}
	if g.opened != 0 {
		t.Error("the GPU was woken for a compositor that cannot host the window")
	}
}

func TestShowAsksForAnOverlayLayerSurfaceWithTheKeyboard(t *testing.T) {
	f := newFakeShell(t, niri)
	w, g := openFake(t, f)
	p := &painter{}
	if err := w.Show(size(requested, 408), p.paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, f,
		fmt.Sprintf("layer_surface surface=true output=0 layer=%d ns=logis", wlproto.ZwlrLayerShellV1LayerOverlay),
		"size 688 408",
		fmt.Sprintf("keyboard %d", wlproto.ZwlrLayerSurfaceV1KeyboardInteractivityExclusive),
		"commit")
	settle(t, w)
	if len(p.frames) != 0 || len(g.created) != 0 {
		t.Errorf("drew %d frames, made %d buffers before the first configure", len(p.frames), len(g.created))
	}
	if !w.Visible() {
		t.Error("not visible after Show")
	}
}

func TestReleaseKeyboardKeepsThePanelButLetsTheKeyboardGo(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	w.ReleaseKeyboard() // nothing shown: harmless
	p := &painter{}
	if err := w.Show(size(requested, 408), p.paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, f, "commit")
	w.ReleaseKeyboard()
	until(t, w, f, fmt.Sprintf("keyboard %d", wlproto.ZwlrLayerSurfaceV1KeyboardInteractivityNone), "commit")
	if !w.Visible() {
		t.Error("the panel went with the keyboard")
	}
}

func TestTheFirstConfigureIsAckedAndDrawnAtItsSize(t *testing.T) {
	f := newFakeShell(t, niri)
	w, g := openFake(t, f)
	p := &painter{}
	if err := w.Show(size(requested, 408), p.paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, f, "commit")
	f.configure(5, 0, 0)
	log := until(t, w, f, "ack 5", "destination 688 408", "frame", "commit")
	if ack, commit := slices.Index(log, "ack 5"), slices.Index(log, "commit"); ack > commit {
		t.Errorf("committed before acking: %q", log)
	}
	want := Geometry{Logical: size(688, 408), Physical: size(688, 408), Scale: scale.One}
	if !slices.Equal(p.frames, []Geometry{want}) {
		t.Errorf("frames %+v, want one at %+v", p.frames, want)
	}
	if !slices.Equal(g.created, []scale.Size{size(688, 408)}) || len(g.resizes) != 0 {
		t.Errorf("buffers created %v, resized %v", g.created, g.resizes)
	}
	if got, ok := w.Size(); !ok || got != size(688, 408) {
		t.Errorf("Size() = %v, %v", got, ok)
	}
}

func TestAScaleAndItsConfigureInOneBatchDrawOneFrame(t *testing.T) {
	f := newFakeShell(t, niri)
	w, g := openFake(t, f)
	p := &painter{}
	if err := w.Show(size(requested, 408), p.paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, f, "commit")
	f.preferScale(156)
	f.configure(1, 0, 0)
	until(t, w, f, "ack 1", "destination 688 408", "frame", "commit")
	settle(t, w)
	// 688 × 1.3 = 894.4 → 894; 408 × 1.3 = 530.4 → 530.
	want := Geometry{Logical: size(688, 408), Physical: size(894, 530), Scale: scale.From120ths(156)}
	if !slices.Equal(p.frames, []Geometry{want}) {
		t.Errorf("frames %+v, want exactly one at %+v", p.frames, want)
	}
	// The first buffer has the configured size: one made at the requested
	// size and resized before the first frame may be drawn at the old size
	// (Mesa defers a resize while it holds a back buffer).
	if !slices.Equal(g.created, []scale.Size{size(894, 530)}) || len(g.resizes) != 0 {
		t.Errorf("buffers created %v, resized %v", g.created, g.resizes)
	}
}

func TestFramesWaitForTheFrameCallbackAndIdleDrawsNothing(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	p := &painter{}
	showConfigured(t, f, w, p)
	w.RequestRedraw()
	settle(t, w)
	if len(p.frames) != 1 {
		t.Fatalf("drew %d frames while the compositor held the first", len(p.frames))
	}
	f.frameDone()
	until(t, w, f, "frame", "commit")
	if len(p.frames) != 2 {
		t.Errorf("drew %d frames after the callback, want 2", len(p.frames))
	}
	f.frameDone()
	settle(t, w)
	if len(p.frames) != 2 {
		t.Errorf("drew %d frames while idle, want 2", len(p.frames))
	}
}

func TestAnAnimationDrawsOnEachFrameCallback(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	frames := 0
	if err := w.Show(size(requested, 408), func(Geometry) bool { frames++; return frames < 3 }); err != nil {
		t.Fatal(err)
	}
	until(t, w, f, "commit")
	f.configure(1, 0, 0)
	until(t, w, f, "ack 1", "frame", "commit")
	for range 3 {
		f.frameDone()
		settle(t, w)
	}
	if frames != 3 {
		t.Errorf("drew %d frames, want 3: two animated, then rest", frames)
	}
}

func TestAScaleChangeRedrawsAtTheNewBufferSize(t *testing.T) {
	f := newFakeShell(t, niri)
	w, g := openFake(t, f)
	p := &painter{}
	showConfigured(t, f, w, p)
	f.frameDone()
	f.preferScale(240)
	until(t, w, f, "frame", "commit")
	last := p.frames[len(p.frames)-1]
	if last.Physical != size(1376, 816) || last.Logical != size(688, 408) {
		t.Errorf("redrawn at %+v", last)
	}
	if got := g.resizes[len(g.resizes)-1]; got != size(1376, 816) {
		t.Errorf("buffers resized to %v", got)
	}
}

func TestANewConfigureLeavesNoStaleSize(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	p := &painter{}
	showConfigured(t, f, w, p)
	f.frameDone()
	// The output changed: a new scale and a smaller size in one batch.
	f.preferScale(156)
	f.configure(6, 500, 300)
	until(t, w, f, "ack 6", "destination 500 300", "frame", "commit")
	want := Geometry{Logical: size(500, 300), Physical: size(650, 390), Scale: scale.From120ths(156)}
	if last := p.frames[len(p.frames)-1]; last != want {
		t.Errorf("redrawn at %+v, want %+v", last, want)
	}
}

func TestClosedHidesThePanelAndReleasesItsObjects(t *testing.T) {
	f := newFakeShell(t, niri)
	w, g := openFake(t, f)
	before := w.conn.display.Live()
	p := &painter{}
	showConfigured(t, f, w, p)
	f.closed()
	until(t, w, f, "destroy layer", "destroy surface")
	if w.Visible() {
		t.Error("still visible after closed")
	}
	if g.live != 0 {
		t.Errorf("%d EGL surfaces left", g.live)
	}
	if after := w.conn.display.Live(); after != before {
		t.Errorf("%d proxies alive after closed, %d before Show", after, before)
	}
}

func TestHideWhileAFrameIsPendingLeavesNothingBehind(t *testing.T) {
	f := newFakeShell(t, niri)
	w, g := openFake(t, f)
	before := w.conn.display.Live()
	showConfigured(t, f, w, &painter{})
	w.Hide()
	until(t, w, f, "destroy layer", "destroy surface")
	if after := w.conn.display.Live(); after != before || g.live != 0 {
		t.Errorf("%d proxies and %d EGL surfaces alive after Hide, %d before Show", after, g.live, before)
	}
}

func TestLosingTheCompositorIsDisconnected(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	showConfigured(t, f, w, &painter{})
	f.comp.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err := w.Dispatch(10 * time.Millisecond)
		if err == nil {
			continue
		}
		var gone *DisconnectedError
		if !errors.As(err, &gone) || !strings.HasPrefix(err.Error(), "the noosphere fell silent mid-rite: ") {
			t.Errorf("%v", err)
		}
		return
	}
	t.Fatal("the window did not notice the compositor leave")
}

func TestWakeEndsAWaitingDispatch(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	settle(t, w) // nothing queued: only Wake can end the wait
	woke := make(chan struct{})
	go func() {
		defer close(woke)
		time.Sleep(20 * time.Millisecond)
		w.Wake()
	}()
	start := time.Now()
	// The window's own goroutine waits; Wake comes from another.
	err := w.Dispatch(5 * time.Second)
	waited := time.Since(start)
	<-woke
	if err != nil || waited < 15*time.Millisecond || waited > 4*time.Second {
		t.Errorf("Dispatch returned %v after %v, want nil after the 20 ms wake", err, waited)
	}
}

// Wake may come from another goroutine at any time, also after Close (a
// signal arriving as the debug panel ends).
func TestWakeAfterCloseIsHarmless(t *testing.T) {
	newFakeShell(t, niri)
	g := &fakeGPU{}
	w, err := openWith(g.open)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Wake()
	}()
	<-done
	w.Close() // twice is harmless too
}

type keys struct {
	log    []string
	keymap string
}

func (k *keys) Keymap(format uint32, fd int, size uint32) {
	f := os.NewFile(uintptr(fd), "keymap")
	defer func() { _ = f.Close() }()
	buf := make([]byte, size)
	_, _ = f.ReadAt(buf, 0)
	k.keymap = string(buf)
	k.log = append(k.log, "keymap")
}
func (k *keys) Enter(serial uint32, pressed []uint32) {
	k.log = append(k.log, fmt.Sprintf("enter %v", pressed))
}
func (k *keys) Leave(serial uint32) { k.log = append(k.log, "leave") }
func (k *keys) Key(serial, time, key, state uint32) {
	k.log = append(k.log, fmt.Sprintf("key %d %d", key, state))
}
func (k *keys) Modifiers(serial, depressed, latched, locked, group uint32) {
	k.log = append(k.log, fmt.Sprintf("modifiers %d %d %d %d", depressed, latched, locked, group))
}
func (k *keys) RepeatInfo(rate, delay int32) {
	k.log = append(k.log, fmt.Sprintf("repeat %d %d", rate, delay))
}

func TestKeyboardEventsOnThePanelReachTheConsumer(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	k := &keys{}
	w.SetKeyboard(k)
	showConfigured(t, f, w, &painter{})
	f.send(func(f *fakeShell) uint32 { return f.client.Bound("wl_seat") }, 0, uint32(wlproto.WlSeatCapabilityKeyboard))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		kb := f.keyboard
		f.mu.Unlock()
		if kb != 0 {
			break
		}
		settle(t, w)
	}
	mem, err := os.CreateTemp(t.TempDir(), "keymap")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = mem.WriteString("xkb_keymap {};")
	pressed := []byte{30, 0, 0, 0}
	kb := func(f *fakeShell) uint32 { return f.keyboard }
	surface := func() uint32 { f.mu.Lock(); defer f.mu.Unlock(); return f.surface }()
	f.send(kb, 0, uint32(wlproto.WlKeyboardKeymapFormatXkbV1), wltest.Fd(mem.Fd()), uint32(14))
	_ = mem.Close()
	f.send(kb, 1, uint32(1), surface, pressed)
	f.send(kb, 5, int32(25), int32(600))
	f.send(kb, 4, uint32(2), uint32(1), uint32(0), uint32(2), uint32(0))
	f.send(kb, 3, uint32(3), uint32(99), uint32(1), uint32(1))
	f.send(kb, 2, uint32(4), surface)
	settle(t, w)
	want := []string{"keymap", "enter [30]", "repeat 25 600", "modifiers 1 0 2 0", "key 1 1", "leave"}
	if !slices.Equal(k.log, want) {
		t.Errorf("keyboard saw %q, want %q", k.log, want)
	}
	if k.keymap != "xkb_keymap {};" {
		t.Errorf("keymap %q", k.keymap)
	}
}

// A surface that cannot be made because the compositor is gone (Mesa
// fails its own roundtrip) is the compositor's loss, not a GPU fault.
func TestASurfaceLostWithTheCompositorIsDisconnected(t *testing.T) {
	f := newFakeShell(t, niri)
	w, g := openFake(t, f)
	if err := w.Show(size(requested, 408), (&painter{}).paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, f, "commit")
	g.fail = func() error {
		f.comp.Close()
		for w.conn.display.Roundtrip() == nil {
		}
		return errors.New("eglCreatePlatformWindowSurface: EGL_SUCCESS")
	}
	f.configure(1, 0, 0)
	var err error
	for deadline := time.Now().Add(5 * time.Second); err == nil && time.Now().Before(deadline); {
		err = w.Dispatch(10 * time.Millisecond)
	}
	var gone *DisconnectedError
	if !errors.As(err, &gone) {
		t.Errorf("%v", err)
	}
}

// A surface the GPU refuses on a healthy connection is a surface error.
func TestASurfaceTheGPURefusesIsASurfaceError(t *testing.T) {
	f := newFakeShell(t, niri)
	w, g := openFake(t, f)
	g.fail = func() error { return errors.New("eglCreatePlatformWindowSurface: EGL_BAD_ALLOC") }
	if err := w.Show(size(requested, 408), (&painter{}).paint); err != nil {
		t.Fatal(err)
	}
	until(t, w, f, "commit")
	f.configure(1, 0, 0)
	var err error
	for deadline := time.Now().Add(5 * time.Second); err == nil && time.Now().Before(deadline); {
		err = w.Dispatch(10 * time.Millisecond)
	}
	var surface *SurfaceError
	if !errors.As(err, &surface) || !strings.Contains(err.Error(), "EGL_BAD_ALLOC") {
		t.Errorf("%v", err)
	}
}

// A withdrawn seat (global_remove) takes its keyboard with it: both are
// released, and the window keeps working.
func TestAWithdrawnSeatIsReleasedWithItsKeyboard(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	showConfigured(t, f, w, &painter{})
	f.send(func(f *fakeShell) uint32 { return f.client.Bound("wl_seat") }, 0, uint32(wlproto.WlSeatCapabilityKeyboard))
	for deadline := time.Now().Add(5 * time.Second); w.seat.keyboard == nil && time.Now().Before(deadline); {
		settle(t, w)
	}
	if w.Seat() == nil || w.seat.keyboard == nil {
		t.Fatal("no seat with a keyboard to withdraw")
	}
	live := w.conn.display.Live()
	f.comp.RemoveGlobal(uint32(len(niri))) // wl_seat is the last global
	settle(t, w)
	if w.Seat() != nil || w.seat.keyboard != nil {
		t.Error("the seat or its keyboard survived its global")
	}
	if got := w.conn.display.Live(); got != live-2 {
		t.Errorf("%d proxies alive, want %d", got, live-2)
	}
	w.RequestRedraw()
	f.frameDone()
	until(t, w, f, "frame", "commit")
}
