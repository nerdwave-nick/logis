package window

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/wlproto"
	"github.com/nerdwave-nick/logis/internal/wltest"
)

// The pointer, cursor shape and paste on a fake compositor (Rust
// src/window/{pointer,paste}.rs).

// Server-created objects (data offers) take ids from here, in order
// (libwayland refuses a gap).
const serverID = 0xff000000

// inputShell is a fakeShell that also plays the seat's pointer, the
// cursor-shape manager and both selection devices: it logs their requests
// and keeps the write end of each pipe a client asks an offer to fill.
type inputShell struct {
	*fakeShell
	mu             sync.Mutex
	pointer, shape uint32
	data, primary  uint32
	offers         map[uint32]string // offer id → "clipboard"/"primary"
	log            []string
	pipes          []int
}

var withSelections = append(slices.Clone(niri),
	wltest.Global{Interface: "wl_data_device_manager", Version: 3},
	wltest.Global{Interface: "zwp_primary_selection_device_manager_v1", Version: 1},
	wltest.Global{Interface: "wp_cursor_shape_manager_v1", Version: 1},
)

func newInputShell(t *testing.T, globals []wltest.Global) *inputShell {
	t.Helper()
	f := &inputShell{fakeShell: newFakeShell(t, globals), offers: map[uint32]string{}}
	f.comp.OnRequest(func(cl *wltest.Client, r wltest.Request) {
		f.request(cl, r)
		f.fakeShell.request(cl, r) // closes the request's fds
	})
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, fd := range f.pipes {
			_ = syscall.Close(fd)
		}
	})
	return f
}

func (f *inputShell) logf(format string, args ...any) {
	f.log = append(f.log, fmt.Sprintf(format, args...))
}

func (f *inputShell) request(cl *wltest.Client, r wltest.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rd := wltest.NewReader(r)
	switch {
	case r.Object == cl.Bound("wl_seat") && r.Opcode == 0:
		f.pointer = rd.Uint()
	case r.Object == f.pointer && f.pointer != 0 && r.Opcode == 1:
		f.logf("release pointer")
	case r.Object == cl.Bound("wp_cursor_shape_manager_v1") && r.Opcode == 1:
		f.shape = rd.Uint()
		f.logf("shape device for %v", rd.Uint() == f.pointer)
	case r.Object == f.shape && f.shape != 0:
		switch r.Opcode {
		case 0:
			f.logf("destroy shape device")
		case 1:
			f.logf("set_shape %d %d", rd.Uint(), rd.Uint())
		}
	case r.Object == cl.Bound("wl_data_device_manager") && r.Opcode == 1:
		f.data = rd.Uint()
	case r.Object == cl.Bound("zwp_primary_selection_device_manager_v1") && r.Opcode == 1:
		f.primary = rd.Uint()
	case r.Object == f.data && f.data != 0 && r.Opcode == 2:
		f.logf("release data device")
	case r.Object == f.primary && f.primary != 0 && r.Opcode == 1:
		f.logf("destroy primary device")
	case f.offers[r.Object] != "":
		which := f.offers[r.Object]
		receive, destroy := uint16(1), uint16(2) // wl_data_offer
		if which == "primary" {
			receive, destroy = 0, 1
		}
		switch r.Opcode {
		case receive:
			f.logf("receive %s %d %s", which, r.Object-serverID, rd.String())
			if len(r.Fds) == 1 {
				fd, err := syscall.Dup(r.Fds[0])
				if err == nil {
					f.pipes = append(f.pipes, fd)
				}
			}
		case destroy:
			f.logf("destroy offer %d", r.Object-serverID)
		}
	}
}

func (f *inputShell) takeLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	log := f.log
	f.log = nil
	return log
}

// lastPipe is the write end of the latest receive's pipe, now the test's.
func (f *inputShell) lastPipe(t *testing.T) int {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pipes) == 0 {
		t.Fatal("no pipe was sent")
	}
	fd := f.pipes[len(f.pipes)-1]
	f.pipes = f.pipes[:len(f.pipes)-1]
	return fd
}

func (f *inputShell) id(get func(*inputShell) uint32) func(*fakeShell) uint32 {
	return func(*fakeShell) uint32 {
		f.mu.Lock()
		defer f.mu.Unlock()
		return get(f)
	}
}

func (f *inputShell) surfaceID() uint32 {
	f.fakeShell.mu.Lock()
	defer f.fakeShell.mu.Unlock()
	return f.surface
}

// sendPointer sends wl_pointer event opcode.
func (f *inputShell) sendPointer(opcode uint16, args ...any) {
	f.send(f.id(func(f *inputShell) uint32 { return f.pointer }), opcode, args...)
}

func fixed(v float64) int32 { return int32(v * 256) }

// offer announces a selection offer of which ("clipboard"/"primary")
// with mimes and makes it the selection.
func (f *inputShell) offer(t *testing.T, which string, n uint32, mimes ...string) {
	t.Helper()
	f.mu.Lock()
	f.offers[serverID+n] = which
	device := f.data
	selection := uint16(5)
	if which == "primary" {
		device, selection = f.primary, 1
	}
	f.mu.Unlock()
	dev := func(*fakeShell) uint32 { return device }
	f.send(dev, 0, uint32(serverID+n))
	for _, m := range mimes {
		f.send(func(*fakeShell) uint32 { return serverID + n }, 0, m)
	}
	f.send(dev, selection, uint32(serverID+n))
}

// sink collects the actions the window hands on.
type sink struct{ got []input.Action }

func (s *sink) take(a input.Action) { s.got = append(s.got, a) }

// pointerShown shows a configured panel on a seat with a pointer and a
// keyboard and waits until the window asked for the pointer.
func pointerShown(t *testing.T, globals []wltest.Global) (*inputShell, *Window, *sink) {
	t.Helper()
	f := newInputShell(t, globals)
	w, _ := openFake(t, f.fakeShell)
	s := &sink{}
	w.SetSink(s.take)
	showConfigured(t, f.fakeShell, w, &painter{})
	f.send(func(f *fakeShell) uint32 { return f.client.Bound("wl_seat") }, 0,
		uint32(wlproto.WlSeatCapabilityPointer|wlproto.WlSeatCapabilityKeyboard))
	waitFor(t, w, "the pointer", func() bool { return f.id(func(f *inputShell) uint32 { return f.pointer })(nil) != 0 })
	return f, w, s
}

func waitFor(t *testing.T, w *Window, what string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if done() {
			return
		}
		if err := w.Dispatch(2 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("no %s", what)
}

func point(kind input.PointKind, x, y float64) input.Action {
	return input.Action{Kind: input.PointAt, Point: input.Point{Kind: kind, X: x, Y: y}}
}

func scrolledBy(s input.Scroll, x, y float64) input.Action {
	a := point(input.PointScrolled, x, y)
	a.Point.Scroll = s
	return a
}

func wantActions(t *testing.T, s *sink, want ...input.Action) {
	t.Helper()
	if !slices.Equal(s.got, want) {
		t.Errorf("actions\n%+v\nwant\n%+v", s.got, want)
	}
	s.got = nil
}

func TestThePointerOverThePanelPointsAndGetsTheDefaultCursor(t *testing.T) {
	f, w, s := pointerShown(t, withSelections)
	surface := f.surfaceID()
	f.sendPointer(0, uint32(7), surface, fixed(10.5), fixed(20)) // enter
	f.sendPointer(5)
	f.sendPointer(2, uint32(1), fixed(11), fixed(21)) // motion
	f.sendPointer(5)
	f.sendPointer(3, uint32(8), uint32(2), uint32(input.BtnLeft), uint32(1)) // button
	f.sendPointer(5)
	f.sendPointer(6, uint32(wlproto.WlPointerAxisSourceWheel))
	f.sendPointer(4, uint32(3), uint32(wlproto.WlPointerAxisVerticalScroll), fixed(15))
	f.sendPointer(9, uint32(wlproto.WlPointerAxisVerticalScroll), int32(120)) // value120
	f.sendPointer(5)
	f.sendPointer(4, uint32(4), uint32(wlproto.WlPointerAxisHorizontalScroll), fixed(9))
	f.sendPointer(5) // horizontal: nothing
	f.sendPointer(3, uint32(9), uint32(5), uint32(input.BtnMiddle), uint32(1))
	f.sendPointer(1, uint32(10), surface) // leave
	settle(t, w)
	wantActions(t, s,
		point(input.PointMoved, 11, 21),
		point(input.PointPressed, 11, 21),
		scrolledBy(input.Scroll{Kind: input.ScrollWheel, Value120: 120}, 11, 21),
		input.Action{Kind: input.Paste, Selection: input.Primary},
		input.Action{Kind: input.PointAt, Point: input.Point{Kind: input.PointLeft}},
	)
	if log := f.takeLog(); !slices.Contains(log, "set_shape 7 1") || !slices.Contains(log, "shape device for true") {
		t.Errorf("no default cursor set on enter: %q", log)
	}
}

func TestAPointerEnteringAnotherSurfaceIsNotOverThePanel(t *testing.T) {
	f, w, s := pointerShown(t, withSelections)
	earlier := f.surfaceID()
	showConfigured(t, f.fakeShell, w, &painter{})
	f.sendPointer(0, uint32(7), earlier, fixed(1), fixed(2)) // not the panel now
	f.sendPointer(2, uint32(1), fixed(3), fixed(4))
	f.sendPointer(3, uint32(8), uint32(2), uint32(input.BtnLeft), uint32(1))
	f.sendPointer(5)
	settle(t, w)
	wantActions(t, s)
	if log := f.takeLog(); slices.ContainsFunc(log, func(l string) bool { return strings.HasPrefix(l, "set_shape") }) {
		t.Errorf("a cursor set off the panel: %q", log)
	}
}

func TestWithoutCursorShapeTheCursorIsLeftAlone(t *testing.T) {
	globals := slices.DeleteFunc(slices.Clone(withSelections), func(g wltest.Global) bool {
		return g.Interface == "wp_cursor_shape_manager_v1"
	})
	f, w, s := pointerShown(t, globals)
	f.sendPointer(0, uint32(7), f.surfaceID(), fixed(1), fixed(2))
	f.sendPointer(2, uint32(1), fixed(3), fixed(4))
	settle(t, w)
	wantActions(t, s, point(input.PointMoved, 3, 4))
	if log := f.takeLog(); slices.ContainsFunc(log, func(l string) bool { return strings.Contains(l, "shape") }) {
		t.Errorf("%q", log)
	}
}

// Before wl_pointer v5 there are no frames: each axis event is one.
func TestAnOldPointerScrollsOnEveryAxisEvent(t *testing.T) {
	globals := slices.Clone(withSelections)
	for i, g := range globals {
		if g.Interface == "wl_seat" {
			globals[i].Version = 4
		}
	}
	f, w, s := pointerShown(t, globals)
	f.sendPointer(0, uint32(7), f.surfaceID(), fixed(1), fixed(2))
	f.sendPointer(4, uint32(3), uint32(wlproto.WlPointerAxisVerticalScroll), fixed(10))
	f.sendPointer(4, uint32(4), uint32(wlproto.WlPointerAxisVerticalScroll), fixed(-5))
	settle(t, w)
	px := func(v float64) input.Scroll { return input.Scroll{Kind: input.ScrollPixels, Pixels: v} }
	wantActions(t, s, scrolledBy(px(10), 1, 2), scrolledBy(px(-5), 1, 2))
}

func TestDiscreteStepsAndAFingersStopScroll(t *testing.T) {
	f, w, s := pointerShown(t, withSelections)
	f.sendPointer(0, uint32(7), f.surfaceID(), fixed(1), fixed(2))
	f.sendPointer(4, uint32(3), uint32(wlproto.WlPointerAxisVerticalScroll), fixed(30))
	f.sendPointer(8, uint32(wlproto.WlPointerAxisVerticalScroll), int32(-2)) // discrete
	f.sendPointer(5)
	f.sendPointer(7, uint32(4), uint32(wlproto.WlPointerAxisVerticalScroll)) // stop
	f.sendPointer(5)
	settle(t, w)
	wantActions(t, s,
		scrolledBy(input.Scroll{Kind: input.ScrollWheel, Value120: -240}, 1, 2),
		scrolledBy(input.Scroll{Kind: input.ScrollStop}, 1, 2))
}

func TestShowingAPanelForgetsWhereThePointerWas(t *testing.T) {
	f, w, s := pointerShown(t, withSelections)
	f.sendPointer(0, uint32(7), f.surfaceID(), fixed(1), fixed(2))
	settle(t, w)
	showConfigured(t, f.fakeShell, w, &painter{})
	f.sendPointer(3, uint32(8), uint32(2), uint32(input.BtnLeft), uint32(1))
	settle(t, w)
	wantActions(t, s)
}

func TestTheSeatLosingItsPointerReleasesItAndItsShape(t *testing.T) {
	f, w, _ := pointerShown(t, withSelections)
	f.send(func(f *fakeShell) uint32 { return f.client.Bound("wl_seat") }, 0, uint32(wlproto.WlSeatCapabilityKeyboard))
	settle(t, w)
	log := f.takeLog()
	if !slices.Contains(log, "destroy shape device") || !slices.Contains(log, "release pointer") {
		t.Errorf("%q", log)
	}
}

// pasteShown is pointerShown with both selections offered.
func pasteShown(t *testing.T) (*inputShell, *Window, *sink) {
	t.Helper()
	f, w, s := pointerShown(t, withSelections)
	waitFor(t, w, "the selection devices", func() bool {
		return f.id(func(f *inputShell) uint32 { return min(f.data, f.primary) })(nil) != 0
	})
	return f, w, s
}

func write(t *testing.T, fd int, text string) {
	t.Helper()
	if _, err := syscall.Write(fd, []byte(text)); err != nil {
		t.Fatal(err)
	}
}

func waitLog(t *testing.T, w *Window, f *inputShell, want string) []string {
	t.Helper()
	var log []string
	waitFor(t, w, want, func() bool {
		log = append(log, f.takeLog()...)
		return slices.Contains(log, want)
	})
	return log
}

func TestCtrlVReadsTheClipboardsBestTextAndInsertsItsFirstLine(t *testing.T) {
	f, w, s := pasteShown(t)
	f.offer(t, "clipboard", 0, "image/png", "text/plain", "text/plain;charset=utf-8")
	settle(t, w)
	w.Paste(input.Clipboard)
	waitLog(t, w, f, "receive clipboard 0 text/plain;charset=utf-8")
	fd := f.lastPipe(t)
	write(t, fd, "Ave Omni")
	settle(t, w) // a slow owner: nothing yet, and nothing blocks
	wantActions(t, s)
	write(t, fd, "ssiah\nnot this")
	_ = syscall.Close(fd)
	waitFor(t, w, "the pasted text", func() bool { return len(s.got) > 0 })
	wantActions(t, s, input.Insert("Ave Omnissiah"))
}

func TestTheMiddleButtonsPrimarySelectionIsReadFromItsOwnDevice(t *testing.T) {
	f, w, s := pasteShown(t)
	f.offer(t, "clipboard", 0, "text/plain")
	f.offer(t, "primary", 1, "UTF8_STRING", "STRING")
	settle(t, w)
	w.Paste(input.Primary)
	waitLog(t, w, f, "receive primary 1 UTF8_STRING")
	fd := f.lastPipe(t)
	write(t, fd, "bet")
	_ = syscall.Close(fd)
	waitFor(t, w, "the pasted text", func() bool { return len(s.got) > 0 })
	wantActions(t, s, input.Insert("bet"))
}

func TestNothingIsReadWithoutTextOnOffer(t *testing.T) {
	f, w, s := pasteShown(t)
	w.Paste(input.Clipboard) // no selection at all
	f.offer(t, "primary", 0, "image/png")
	settle(t, w)
	w.Paste(input.Primary)
	settle(t, w)
	wantActions(t, s)
	if log := f.takeLog(); slices.ContainsFunc(log, func(l string) bool { return strings.HasPrefix(l, "receive") }) {
		t.Errorf("%q", log)
	}
}

func TestWithoutSelectionDevicesPasteDoesNothing(t *testing.T) {
	f, w, s := pointerShown(t, niri)
	w.Paste(input.Clipboard)
	w.Paste(input.Primary)
	settle(t, w)
	wantActions(t, s)
	_ = f
}

func TestANewerSelectionReplacesTheOfferAndAnEmptyOneClearsIt(t *testing.T) {
	f, w, s := pasteShown(t)
	f.offer(t, "clipboard", 0, "text/plain")
	f.offer(t, "clipboard", 1, "STRING")
	waitLog(t, w, f, "destroy offer 0")
	w.Paste(input.Clipboard)
	waitLog(t, w, f, "receive clipboard 1 STRING")
	_ = syscall.Close(f.lastPipe(t))
	settle(t, w)
	wantActions(t, s) // an empty paste inserts nothing
	f.send(f.id(func(f *inputShell) uint32 { return f.data }), 5, uint32(0))
	waitLog(t, w, f, "destroy offer 1")
	w.Paste(input.Clipboard)
	settle(t, w)
	if log := f.takeLog(); slices.ContainsFunc(log, func(l string) bool { return strings.HasPrefix(l, "receive") }) {
		t.Errorf("%q", log)
	}
}

func TestADragOfferIsDeclined(t *testing.T) {
	f, w, _ := pasteShown(t)
	f.mu.Lock()
	f.offers[serverID] = "clipboard"
	data := f.data
	f.mu.Unlock()
	dev := func(*fakeShell) uint32 { return data }
	f.send(dev, 0, uint32(serverID))
	f.send(dev, 1, uint32(1), f.surfaceID(), fixed(1), fixed(1), uint32(serverID)) // enter
	waitLog(t, w, f, "destroy offer 0")
	f.send(dev, 2) // leave
	settle(t, w)
}

func TestANewerPasteReplacesOneInProgressAndHidingDropsIt(t *testing.T) {
	f, w, s := pasteShown(t)
	f.offer(t, "clipboard", 0, "text/plain")
	settle(t, w)
	w.Paste(input.Clipboard)
	waitLog(t, w, f, "receive clipboard 0 text/plain")
	first := f.lastPipe(t)
	w.Paste(input.Clipboard)
	waitLog(t, w, f, "receive clipboard 0 text/plain")
	second := f.lastPipe(t)
	if _, err := syscall.Write(first, []byte("old")); err != syscall.EPIPE {
		t.Errorf("the replaced paste's pipe is still read: %v", err)
	}
	_ = syscall.Close(first)
	write(t, second, "new")
	_ = syscall.Close(second)
	waitFor(t, w, "the pasted text", func() bool { return len(s.got) > 0 })
	wantActions(t, s, input.Insert("new"))

	w.Paste(input.Clipboard)
	waitLog(t, w, f, "receive clipboard 0 text/plain")
	third := f.lastPipe(t)
	w.Hide()
	if _, err := syscall.Write(third, []byte("late")); err != syscall.EPIPE {
		t.Errorf("hiding kept the paste: %v", err)
	}
	_ = syscall.Close(third)
	settle(t, w)
	wantActions(t, s)
}

func TestReadingStopsAtTheLimitWhileTheOwnerKeepsWriting(t *testing.T) {
	f, w, s := pasteShown(t)
	f.offer(t, "clipboard", 0, "text/plain")
	settle(t, w)
	w.Paste(input.Clipboard)
	waitLog(t, w, f, "receive clipboard 0 text/plain")
	fd := f.lastPipe(t)
	defer func() { _ = syscall.Close(fd) }()
	chunk := strings.Repeat("a", 4096)
	written := 0
	_ = syscall.SetNonblock(fd, true)
	for written <= input.PasteLimit && len(s.got) == 0 {
		if n, err := syscall.Write(fd, []byte(chunk)); err == nil {
			written += n
		}
		if err := w.Dispatch(time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, w, "the pasted text", func() bool { return len(s.got) > 0 })
	if len(s.got) != 1 || len(s.got[0].Edit.Text) != input.PasteLimit {
		t.Errorf("%d actions, the first inserting %d bytes", len(s.got), len(s.got[0].Edit.Text))
	}
}

func TestAWithdrawnSeatTakesThePointerAndTheSelectionDevicesWithIt(t *testing.T) {
	f, w, _ := pasteShown(t)
	f.offer(t, "clipboard", 0, "text/plain")
	settle(t, w)
	live := w.conn.display.Live()
	f.comp.RemoveGlobal(uint32(len(niri))) // wl_seat
	settle(t, w)
	// The seat, its keyboard and pointer, the shape device, both selection
	// devices and the clipboard's offer.
	if got := w.conn.display.Live(); got != live-7 {
		t.Errorf("%d proxies alive, want %d", got, live-7)
	}
	w.Paste(input.Clipboard)
	settle(t, w)
}
