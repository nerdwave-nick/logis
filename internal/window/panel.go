package window

import (
	"github.com/nerdwave-nick/logis/internal/gles"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// panel is one shown layer surface: its scaling objects, its EGL surface
// and the configure/scale/pacing state machine its events feed.
type panel struct {
	surface    *wlproto.WlSurface
	fractional *wlproto.WpFractionalScaleV1
	viewport   *wlproto.WpViewport
	layer      *wlproto.ZwlrLayerSurfaceV1
	gpu        gpu
	// presenter is made at the first frame, at that frame's buffer size.
	presenter presenter
	frame     *wlproto.WlCallback // the pending frame callback, if any
	state     *surfaceState
	paint     Painter
	// closed: the compositor sent `closed`; the panel goes after the
	// dispatch that brought it.
	closed bool
}

// newPanel creates an overlay layer surface asking for size logical
// pixels, with exclusive keyboard focus, and commits it without a buffer
// so the compositor configures it.
func (w *Window) newPanel(size scale.Size, paint Painter) (*panel, error) {
	p := &panel{state: newSurfaceState(size), paint: paint, gpu: w.gpu}
	p.surface = w.compositor.CreateSurface()
	p.fractional = w.fractional.GetFractionalScale(p.surface)
	p.fractional.OnPreferredScale = func(n uint32) { p.state.setScale(scale.From120ths(n)) }
	p.viewport = w.viewporter.GetViewport(p.surface)
	p.layer = w.layerShell.GetLayerSurface(p.surface, nil, wlproto.ZwlrLayerShellV1LayerOverlay, NAMESPACE)
	p.layer.OnConfigure = func(serial, width, height uint32) {
		// Acked at once: the next commit (the next frame) carries it.
		p.layer.AckConfigure(serial)
		p.state.configure(scale.Size{W: width, H: height})
	}
	p.layer.OnClosed = func() { p.closed = true }
	p.layer.SetSize(size.W, size.H)
	p.layer.SetKeyboardInteractivity(wlproto.ZwlrLayerSurfaceV1KeyboardInteractivityExclusive)
	p.surface.Commit()
	return p, nil
}

// reconfigure resizes the buffers to g and sets the viewport to its
// logical size; both take effect with the next commit. Before the first
// frame there are no buffers yet: they are made at g's size.
func (p *panel) reconfigure(g Geometry) {
	if p.presenter != nil {
		p.presenter.resize(g.Physical)
	}
	p.viewport.SetDestination(int32(g.Logical.W), int32(g.Logical.H))
	p.state.applied(g)
}

// present draws one frame and presents it with a frame callback requested,
// so the next frame waits for the compositor. The first frame makes the EGL
// surface, at its own buffer size: Mesa defers a wl_egl_window resize while
// it holds a back buffer, so a surface made at the requested size and
// resized before its first frame could draw that frame at the old size.
func (p *panel) present() error {
	g, _ := p.state.geometry()
	if p.presenter == nil {
		var err error
		if p.presenter, err = p.gpu.newSurface(p.surface, g.Physical); err != nil {
			return err
		}
	}
	if err := p.presenter.makeCurrent(); err != nil {
		return err
	}
	gles.Viewport(0, 0, int(g.Physical.W), int(g.Physical.H))
	animating := p.paint(g)
	p.state.setAnimating(animating)
	p.frame = p.surface.Frame()
	frame := p.frame
	frame.OnDone = func(uint32) {
		if p.frame == frame {
			p.frame = nil
			p.state.frameDone()
		}
	}
	if err := p.presenter.swap(); err != nil {
		return err
	}
	trace.Emit("commit")
	if !animating {
		// The frame leaves nothing moving (Rust: `settled` after the first
		// such frame once motion ends; until motion is ported every change
		// is instant, so each still frame is one).
		trace.Emit("settled")
	}
	p.state.drew()
	return nil
}

// destroy releases the panel: its EGL surface before the wl_surface it is
// on, the pending frame callback, the scaling objects, the layer surface,
// then the wl_surface.
func (p *panel) destroy() {
	if p.presenter != nil {
		p.presenter.destroy()
		p.presenter = nil
	}
	if p.frame != nil {
		p.frame.Destroy()
		p.frame = nil
	}
	p.viewport.Destroy()
	p.fractional.Destroy()
	p.layer.Destroy()
	p.surface.Destroy()
}
