package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// Harness is the harness's own Wayland connection to a test compositor
// and nothing else: the globals the injectors and screen capture need,
// and the backdrop. It belongs to the test's goroutine.
//
// Event handlers run inside libwayland's dispatch (a cgo callback), where
// t.Fatal must not be called: they only record, and the harness fails the
// test after the dispatch returns.
type Harness struct {
	t          testing.TB
	display    *wlproto.Display
	runtimeDir string
	globals    map[string]global

	output *wlproto.WlOutput
	// transform is the output's: screencopy delivers buffer contents,
	// which a capture turns upright (niri's nested output is flipped).
	transform  int32
	refreshMHz int32
	mode       [2]int32 // the current mode in pixels

	shm        *wlproto.WlShm
	seat       *wlproto.WlSeat
	compositor *wlproto.WlCompositor
	screencopy *wlproto.ZwlrScreencopyManagerV1
	keyboards  *wlproto.ZwpVirtualKeyboardManagerV1
	pointers   *wlproto.ZwlrVirtualPointerManagerV1 // nil if not offered
	backdrop   *backdrop
}

type global struct{ name, version uint32 }

// Connect connects to c's socket after ConnectVerified proved its server
// is the compositor process (failing the test otherwise) and paints the
// black backdrop. t's cleanup closes the connection.
func Connect(t testing.TB, c *Compositor) *Harness {
	t.Helper()
	d, err := ConnectVerified(c.Socket(), c.PID())
	if err != nil {
		t.Fatal(err)
	}
	h := &Harness{t: t, display: d, runtimeDir: c.RuntimeDir(), globals: map[string]global{}}
	t.Cleanup(d.Close)
	registry := d.Registry()
	registry.OnGlobal = func(name uint32, iface string, version uint32) {
		h.globals[iface] = global{name, version}
	}
	h.Roundtrip()
	h.output = must[*wlproto.WlOutput](h, registry, wlproto.WlOutputInterface, 4)
	h.output.OnGeometry = func(_, _, _, _, _ int32, _, _ string, transform int32) { h.transform = transform }
	h.output.OnMode = func(flags uint32, w, ht, refresh int32) {
		if flags&wlproto.WlOutputModeCurrent != 0 {
			h.mode, h.refreshMHz = [2]int32{w, ht}, refresh
		}
	}
	h.shm = must[*wlproto.WlShm](h, registry, wlproto.WlShmInterface, 1)
	h.seat = must[*wlproto.WlSeat](h, registry, wlproto.WlSeatInterface, 7)
	h.compositor = must[*wlproto.WlCompositor](h, registry, wlproto.WlCompositorInterface, 4)
	h.screencopy = must[*wlproto.ZwlrScreencopyManagerV1](h, registry, wlproto.ZwlrScreencopyManagerV1Interface, 3)
	h.keyboards = must[*wlproto.ZwpVirtualKeyboardManagerV1](h, registry, wlproto.ZwpVirtualKeyboardManagerV1Interface, 1)
	if _, ok := h.globals[wlproto.ZwlrVirtualPointerManagerV1Interface.Name]; ok {
		h.pointers = must[*wlproto.ZwlrVirtualPointerManagerV1](h, registry, wlproto.ZwlrVirtualPointerManagerV1Interface, 2)
	}
	layerShell := must[*wlproto.ZwlrLayerShellV1](h, registry, wlproto.ZwlrLayerShellV1Interface, 4)
	viewporter := must[*wlproto.WpViewporter](h, registry, wlproto.WpViewporterInterface, 1)
	h.Roundtrip()
	h.paintBackdrop(layerShell, viewporter)
	return h
}

// must binds the global iface at its version up to most, or fails.
func must[T wlproto.Object](h *Harness, r *wlproto.WlRegistry, iface *wlproto.Interface, most uint32) T {
	h.t.Helper()
	g, ok := h.globals[iface.Name]
	if !ok {
		h.t.Fatalf("the test compositor does not offer %s", iface.Name)
	}
	obj, _ := r.Bind(g.name, iface, min(g.version, most)).(T)
	return obj
}

// Roundtrip sends everything and waits until the compositor handled it.
func (h *Harness) Roundtrip() {
	h.t.Helper()
	if err := h.display.Roundtrip(); err != nil {
		h.t.Fatalf("the test compositor does not answer: %v", err)
	}
	h.checkBackdrop()
}

// dispatchUntil dispatches events until done holds, for at most Patience.
func (h *Harness) dispatchUntil(what string, done func() bool) error {
	deadline := time.Now().Add(Patience)
	for !done() {
		left := time.Until(deadline)
		if left <= 0 {
			return fmt.Errorf("%s: nothing after %v", what, Patience)
		}
		if err := h.display.Dispatch(left); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}
	h.checkBackdrop()
	return nil
}

// RefreshHz is the output's refresh rate (60 Hz if it announces none).
func (h *Harness) RefreshHz() float64 {
	if h.refreshMHz > 0 {
		return float64(h.refreshMHz) / 1000
	}
	return 60
}

// OutputSize is the output's current mode in pixels (logical pixels at
// the default scale 1).
func (h *Harness) OutputSize() (w, ht int) {
	h.t.Helper()
	if h.mode[0] <= 0 || h.mode[1] <= 0 {
		h.t.Fatal("the test output announced no mode")
	}
	return int(h.mode[0]), int(h.mode[1])
}
