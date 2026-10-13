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
// (sway's default background is dark grey, niri's is configurable). One
// black wl_shm pixel, stretched by wp_viewporter to the configured size,
// so it is exact at any scale.
type backdrop struct {
	surface  *wlproto.WlSurface
	layer    *wlproto.ZwlrLayerSurfaceV1
	viewport *wlproto.WpViewport
	pool     *wlproto.WlShmPool
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
	// One XRGB pixel of zeros: opaque black. Read-write: compositors map
	// shm pools with write access.
	path := filepath.Join(h.runtimeDir, "backdrop.shm")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err == nil {
		_, err = f.Write(make([]byte, 4))
	}
	if err != nil {
		h.t.Fatal(err)
	}
	b.pool = h.shm.CreatePool(int(f.Fd()), 4)
	b.buffer = b.pool.CreateBuffer(0, 1, 1, 4, wlproto.WlShmFormatXrgb8888)
	b.layer.OnConfigure = func(serial, w, ht uint32) {
		b.layer.AckConfigure(serial)
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
	err = h.dispatchUntil("the backdrop's configure", func() bool { return b.painted || b.closed })
	// libwayland sent a duplicate of the pool's fd with the request.
	_ = f.Close()
	_ = os.Remove(path)
	if err != nil {
		h.t.Fatal(err)
	}
	h.Roundtrip()
}

// checkBackdrop fails the test once the compositor closed the backdrop.
func (h *Harness) checkBackdrop() {
	h.t.Helper()
	if h.backdrop != nil && h.backdrop.closed {
		h.t.Fatal("the compositor closed the harness's backdrop")
	}
}
