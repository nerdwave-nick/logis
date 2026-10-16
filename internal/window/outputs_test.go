package window

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/wltest"
)

// How much room a panel has (the list fits its rows into it): the logical
// heights of the compositor's outputs (xdg-output), and the output the
// compositor put the last panel on (wl_surface.enter). Before any panel
// has shown, the shortest output; a panel entering an output, or that
// output changing size under it, tells the room sink.

var withOutputs = append(slices.Clone(niri),
	wltest.Global{Interface: "zxdg_output_manager_v1", Version: 3},
	wltest.Global{Interface: "wl_output", Version: 4},
	wltest.Global{Interface: "wl_output", Version: 4},
)

// screens is a fake shell that also hands out xdg outputs: each
// get_xdg_output's new id, by the wl_output it describes.
type screens struct {
	*fakeShell
	smu   sync.Mutex
	xdg   map[uint32]uint32 // wl_output id → its zxdg_output_v1 id
	order []uint32          // the wl_output ids, in the order described
}

func newScreens(t *testing.T, globals []wltest.Global) *screens {
	s := &screens{fakeShell: newFakeShell(t, globals), xdg: map[uint32]uint32{}}
	s.comp.OnRequest(func(cl *wltest.Client, r wltest.Request) {
		s.request(cl, r)
		s.more(cl, r)
	})
	return s
}

func (s *screens) more(cl *wltest.Client, r wltest.Request) {
	s.smu.Lock()
	defer s.smu.Unlock()
	rd := wltest.NewReader(r)
	manager := cl.Bound("zxdg_output_manager_v1")
	switch {
	case r.Object == manager && r.Opcode == 1:
		id, output := rd.Uint(), rd.Uint()
		s.xdg[output] = id
		s.order = append(s.order, output)
	}
}

// output is the i-th wl_output the window described (0-based), once it
// has.
func (s *screens) output(t *testing.T, w *Window, i int) uint32 {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.smu.Lock()
		n := len(s.order)
		var id uint32
		if n > i {
			id = s.order[i]
		}
		s.smu.Unlock()
		if id != 0 {
			return id
		}
		if err := w.Dispatch(5 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("the window described no output %d", i)
	return 0
}

// logicalSize sends output i's logical size.
func (s *screens) logicalSize(t *testing.T, w *Window, i int, width, height int32) {
	t.Helper()
	output := s.output(t, w, i)
	s.smu.Lock()
	id := s.xdg[output]
	s.smu.Unlock()
	s.send(func(*fakeShell) uint32 { return id }, 1, width, height)
	s.send(func(*fakeShell) uint32 { return id }, 2) // done
}

// enter puts the panel's surface on output i.
func (s *screens) enter(t *testing.T, w *Window, i int) {
	t.Helper()
	output := s.output(t, w, i)
	s.send(func(f *fakeShell) uint32 { return f.surface }, 0, output)
}

// roomUntil dispatches until the window's room is want.
func roomUntil(t *testing.T, w *Window, want uint32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Room() == want {
			return
		}
		if err := w.Dispatch(5 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("room %d, want %d", w.Room(), want)
}

func TestBeforeAnyPanelTheRoomIsTheShortestOutputs(t *testing.T) {
	s := newScreens(t, withOutputs)
	w, _ := openFake(t, s.fakeShell)
	if w.Room() != 0 {
		t.Fatalf("room %d before any output is known", w.Room())
	}
	s.logicalSize(t, w, 0, 1920, 1080)
	roomUntil(t, w, 1080)
	s.logicalSize(t, w, 1, 1280, 400)
	roomUntil(t, w, 400)
}

func TestThePanelsOutputIsTheRoomAndTellsTheSink(t *testing.T) {
	s := newScreens(t, withOutputs)
	w, _ := openFake(t, s.fakeShell)
	var told []uint32
	w.SetRoomSink(func(room uint32) { told = append(told, room) })
	s.logicalSize(t, w, 0, 1920, 1080)
	s.logicalSize(t, w, 1, 1280, 400)
	roomUntil(t, w, 400)
	showConfigured(t, s.fakeShell, w, &painter{})
	s.enter(t, w, 0)
	roomUntil(t, w, 1080)
	if !slices.Equal(told, []uint32{1080}) {
		t.Fatalf("told %v", told)
	}
	// Its output changes size under it (another scale): told again.
	s.logicalSize(t, w, 0, 1536, 864)
	roomUntil(t, w, 864)
	settle(t, w)
	if !slices.Equal(told, []uint32{1080, 864}) {
		t.Fatalf("told %v", told)
	}
	// Another output changing tells nothing; the next panel starts on the
	// output the last one showed on.
	s.logicalSize(t, w, 1, 1280, 300)
	settle(t, w)
	w.Hide()
	if w.Room() != 864 || len(told) != 2 {
		t.Fatalf("room %d, told %v", w.Room(), told)
	}
}

func TestAWithdrawnOutputIsForgottenAndAnAddedOneCounts(t *testing.T) {
	s := newScreens(t, withOutputs)
	w, _ := openFake(t, s.fakeShell)
	s.logicalSize(t, w, 0, 1920, 1080)
	s.logicalSize(t, w, 1, 1280, 400)
	roomUntil(t, w, 400)
	showConfigured(t, s.fakeShell, w, &painter{})
	s.enter(t, w, 1)
	settle(t, w)
	live := w.conn.display.Live()
	s.comp.RemoveGlobal(uint32(len(withOutputs))) // the second wl_output
	roomUntil(t, w, 1080)
	if got := w.conn.display.Live(); got != live-2 {
		t.Errorf("%d proxies alive, want %d (its wl_output and xdg output released)", got, live-2)
	}
	s.comp.AddGlobal(wltest.Global{Interface: "wl_output", Version: 4})
	s.logicalSize(t, w, 2, 1024, 600)
	roomUntil(t, w, 600)
}

func TestWithoutXdgOutputTheRoomIsUnknown(t *testing.T) {
	globals := append(slices.Clone(niri), wltest.Global{Interface: "wl_output", Version: 4})
	f := newFakeShell(t, globals)
	w, _ := openFake(t, f)
	told := false
	w.SetRoomSink(func(uint32) { told = true })
	showConfigured(t, f, w, &painter{})
	settle(t, w)
	if w.Room() != 0 || told {
		t.Fatalf("room %d, told %v", w.Room(), told)
	}
}

func TestClosingReleasesTheOutputs(t *testing.T) {
	s := newScreens(t, withOutputs)
	w, _ := openFake(t, s.fakeShell)
	s.logicalSize(t, w, 1, 1280, 400)
	roomUntil(t, w, 400)
	live := w.conn.display.Live()
	w.releaseOutputs()
	// The manager, both wl_outputs and their xdg outputs.
	if got := w.conn.display.Live(); got != live-5 || w.Room() != 0 {
		t.Fatalf("%d proxies alive, want %d; room %d", got, live-5, w.Room())
	}
}
