package e2e

import (
	"math"
	"os"
	"path/filepath"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// BackdropNamespace is the namespace compositors see for the backdrop.
const BackdropNamespace = "logis-test-backdrop"

// backdrop is the harness's own opaque black layer surface on the
// background layer, covering the whole output, so whatever a client draws
// translucently is composited over the same black in every compositor
// (sway's default background is dark grey, niri's is configurable). A
// black wl_shm buffer of the output's physical size, mapped by
// wp_viewporter onto the configured logical size, so the compositor
// scales it by about 1:1 at any output scale. A smaller buffer stretched
// further is not exact: a bilinear filter reads past its edges into
// transparency (sway 1.9's pixman renderer at 1.3 showed its grey through
// a stretched single pixel as a vignette, and it rounds and misplaces a
// viewport's source box when scaling); at about 1:1 that edge weight
// stays under half a level.
type backdrop struct {
	surface  *wlproto.WlSurface
	layer    *wlproto.ZwlrLayerSurfaceV1
	viewport *wlproto.WpViewport
	buffer   *wlproto.WlBuffer
	painted  bool
	closed   bool
}

// paintBackdrop puts the black backdrop under everything and waits until
// it is painted at the size the compositor configured.
func (h *Harness) paintBackdrop(layerShell *wlproto.ZwlrLayerShellV1, viewporter *wlproto.WpViewporter) {
	h.t.Helper()
	b := &backdrop{surface: h.compositor.CreateSurface()}
	b.layer = layerShell.GetLayerSurface(b.surface, h.output, wlproto.ZwlrLayerShellV1LayerBackground, BackdropNamespace)
	b.layer.SetAnchor(wlproto.ZwlrLayerSurfaceV1AnchorTop | wlproto.ZwlrLayerSurfaceV1AnchorBottom |
		wlproto.ZwlrLayerSurfaceV1AnchorLeft | wlproto.ZwlrLayerSurfaceV1AnchorRight)
	b.layer.SetSize(0, 0)
	b.layer.SetExclusiveZone(-1)
	b.viewport = viewporter.GetViewport(b.surface)
	var failed error
	b.layer.OnConfigure = func(serial, w, ht uint32) {
		b.layer.AckConfigure(serial)
		if b.buffer == nil {
			b.buffer, failed = h.blackBuffer(int32(w), int32(ht))
			if failed != nil {
				b.closed = true
				return
			}
		}
		if w > 0 && ht > 0 {
			b.viewport.SetDestination(int32(w), int32(ht))
		}
		b.surface.Attach(b.buffer, 0, 0)
		b.surface.Damage(0, 0, math.MaxInt32, math.MaxInt32)
		b.surface.Commit()
		b.painted = true
	}
	b.layer.OnClosed = func() { b.closed = true }
	b.surface.Commit()
	h.backdrop = b
	err := h.dispatchUntil("the backdrop's configure", func() bool { return b.painted || b.closed })
	if err == nil {
		err = failed
	}
	if err != nil {
		h.t.Fatal(err)
	}
	h.Roundtrip()
}

// blackBuffer is an opaque black buffer of the output's physical size (the
// configured logical w×ht before the output announced its mode).
func (h *Harness) blackBuffer(w, ht int32) (*wlproto.WlBuffer, error) {
	if h.mode[0] > 0 && h.mode[1] > 0 {
		w, ht = h.mode[0], h.mode[1]
		if h.transform%2 == 1 { // 90° and 270°, flipped or not
			w, ht = ht, w
		}
	}
	w, ht = max(w, 1), max(ht, 1)
	// XRGB zeros: opaque black. Read-write: compositors map shm pools with
	// write access.
	path := filepath.Join(h.runtimeDir, "backdrop.shm")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	// libwayland sends a duplicate of the pool's fd with the request.
	defer func() { _ = f.Close(); _ = os.Remove(path) }()
	size := w * ht * 4
	if err := f.Truncate(int64(size)); err != nil {
		return nil, err
	}
	pool := h.shm.CreatePool(int(f.Fd()), size)
	return pool.CreateBuffer(0, w, ht, w*4, wlproto.WlShmFormatXrgb8888), nil
}

// checkBackdrop fails the test once the compositor closed the backdrop.
func (h *Harness) checkBackdrop() {
	h.t.Helper()
	if h.backdrop != nil && h.backdrop.closed {
		h.t.Fatal("the compositor closed the harness's backdrop")
	}
}
