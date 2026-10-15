package window

import (
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// panel is one shown layer surface: its scaling objects, its EGL surface,
// the configure/scale/pacing state machine its events feed, and its open
// or close transition. It is the visible panel or one closing.
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
	paint     FramePainter
	// blur is the blur behind it, while on and offered.
	blur       *panelBlur
	transition motion.Transition
	// drawnOnce: a frame was shown; wasMoving: the last frame left
	// something moving (true at first, so the first still frame settles).
	drawnOnce, wasMoving bool
	// closed: the compositor sent `closed`; the panel goes after the
	// dispatch that brought it.
	closed bool
}

// newPanel creates an overlay layer surface asking for size logical
// pixels, with exclusive keyboard focus and blur behind it when on, and
// commits it without a buffer so the compositor configures it. It opens
// with transition.
func (w *Window) newPanel(size scale.Size, paint FramePainter, transition motion.Transition) *panel {
	p := &panel{state: newSurfaceState(size), paint: paint, gpu: w.gpu, transition: transition, wasMoving: true}
	p.surface = w.compositor.CreateSurface()
	p.surface.OnEnter = func(o *wlproto.WlOutput) { w.entered(p, o) }
	p.fractional = w.fractional.GetFractionalScale(p.surface)
	p.fractional.OnPreferredScale = func(n uint32) { p.state.setScale(scale.From120ths(n)) }
	p.viewport = w.viewporter.GetViewport(p.surface)
	if w.blur && w.effects != nil {
		p.blur = newPanelBlur(w.effects, p.surface)
	}
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
	return p
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

// close starts the close transition at now. The panel gives up keyboard
// focus and pointer input at once (an empty input region), so whatever
// comes next (the launched app, the next session) gets them; only its
// look lingers.
func (p *panel) close(m motion.Motion, now uint64, compositor *wlproto.WlCompositor) {
	p.transition.Close(m.CloseTo, m.Close, now)
	p.layer.SetKeyboardInteractivity(wlproto.ZwlrLayerSurfaceV1KeyboardInteractivityNone)
	nothing := compositor.CreateRegion()
	p.surface.SetInputRegion(nothing)
	nothing.Destroy()
	p.surface.Commit()
	p.state.setAnimating(true)
}

// destroy releases the panel: its EGL surface before the wl_surface it is
// on, the pending frame callback, the blur, the scaling objects, the
// layer surface, then the wl_surface.
func (p *panel) destroy() {
	if p.presenter != nil {
		p.presenter.destroy()
		p.presenter = nil
	}
	if p.frame != nil {
		p.frame.Destroy()
		p.frame = nil
	}
	if p.blur != nil {
		p.blur.destroy()
		p.blur = nil
	}
	p.viewport.Destroy()
	p.fractional.Destroy()
	p.layer.Destroy()
	p.surface.Destroy()
}
