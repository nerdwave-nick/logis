// Package window is logis's layer-shell window (spec §4.2, D1, D5; Rust
// src/window): the compositor connection through wlproto, the GPU woken on
// it at start (EGL Wayland platform, OpenGL ES 3), and at most one panel —
// an overlay layer surface in namespace "logis" with exclusive keyboard
// focus, drawn at the compositor's fractional scale through a viewport and
// paced by frame callbacks.
//
// A Window is owned by one goroutine: Open locks it to its OS thread (the
// GL context is current there) and Close unlocks it. Only Wake may be
// called from other goroutines. Every C object is released by Close or
// Hide, never by finalizers.
package window

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/nerdwave-nick/logis/internal/egl"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// NAMESPACE is the layer-shell namespace compositors see (spec §4.2).
const NAMESPACE = "logis"

// Painter draws one frame of geometry g into the current GL context (its
// viewport already covers the buffer) and reports whether it animates: an
// animating panel is drawn again on every frame callback.
type Painter func(g Geometry) (animating bool)

// Window is the daemon's compositor connection, its GPU and its panel.
type Window struct {
	conn *connection
	// display is conn's, set once at Open: Wake reads it from other
	// goroutines, also after Close (wlproto's Wake allows that).
	display    *wlproto.Display
	closed     bool
	compositor *wlproto.WlCompositor
	layerShell *wlproto.ZwlrLayerShellV1
	fractional *wlproto.WpFractionalScaleManagerV1
	viewporter *wlproto.WpViewporter
	seat       *seat
	clipboard  *clipboard
	activation activation
	gpu        gpu
	panel      *panel
	log        io.Writer
	repeat     repeatTimer
}

// Open connects to the session's compositor, binds the globals the window
// needs (refusing a compositor without them) and wakes the GPU: at start,
// never lazily on the first Show. The calling goroutine owns the window
// from now on.
func Open() (*Window, error) { return openWith(openEGL) }

func openWith(open openGPU) (w *Window, err error) {
	runtime.LockOSThread()
	defer func() {
		if err != nil {
			runtime.UnlockOSThread()
		}
	}()
	c, err := connect()
	if err != nil {
		return nil, err
	}
	if err := required(c.interfaces()); err != nil {
		c.display.Close()
		return nil, err
	}
	w = &Window{conn: c, display: c.display, log: io.Discard}
	w.compositor, _ = c.bind(wlproto.WlCompositorInterface, 6).(*wlproto.WlCompositor)
	w.layerShell, _ = c.bind(wlproto.ZwlrLayerShellV1Interface, 4).(*wlproto.ZwlrLayerShellV1)
	w.fractional, _ = c.bind(wlproto.WpFractionalScaleManagerV1Interface, 1).(*wlproto.WpFractionalScaleManagerV1)
	w.viewporter, _ = c.bind(wlproto.WpViewporterInterface, 1).(*wlproto.WpViewporter)
	w.seat = bindSeat(c, w.shows)
	w.seat.pointer.shapes, _ = w.Bind(wlproto.WpCursorShapeManagerV1Interface, 1).(*wlproto.WpCursorShapeManagerV1)
	w.clipboard = bindClipboard(w, w.seat.seat)
	w.activation.manager, _ = w.Bind(wlproto.XdgActivationV1Interface, 1).(*wlproto.XdgActivationV1)
	w.seat.pressed, w.seat.pointer.pressed = w.activation.press, w.activation.press
	c.removed = func(g Global) {
		if w.seat.removed(g) {
			w.clipboard.releaseDevices()
		}
	}
	if w.repeat, err = newRepeatTimer(); err != nil {
		w.releaseInput()
		c.display.Close()
		return nil, err
	}
	c.display.Watch(w.repeat.fd, w.repeatDue)
	if w.gpu, err = open(c.display.Ptr()); err != nil {
		w.releaseInput()
		c.display.Close()
		w.repeat.close()
		return nil, err
	}
	return w, nil
}

// GPU names the GL implementation the window draws with.
func (w *Window) GPU() egl.Info { return w.gpu.info() }

// SetLog reports every buffer (re)configuration to log ("cogitator glass"
// lines, as Rust's verbose debug panel does).
func (w *Window) SetLog(log io.Writer) { w.log = log }

// SetKeyboard sends the seat's keyboard events to k from now on; without
// one, keymaps are closed unread. A Repeater is woken for its repeats, a
// Resetter starts over when a panel is shown or hidden.
func (w *Window) SetKeyboard(k Keyboard) {
	w.seat.consumer = k
	w.armRepeat()
}

// SetSink sends what the pointer does over the visible panel and the text
// of each paste (as an Insert edit) to sink, on the window's goroutine.
// Sharing the keyboard's sink keeps their order.
func (w *Window) SetSink(sink func(input.Action)) {
	w.seat.pointer.sink = sink
	w.clipboard.sink = sink
}

// Paste starts reading selection s's text, replacing a paste still being
// read; the text reaches the sink once its owner has written it all.
// Hiding or showing a panel drops it.
func (w *Window) Paste(s input.Selection) { w.clipboard.start(s) }

// Seat is the seat the window listens to (the first one offered), or nil.
func (w *Window) Seat() *wlproto.WlSeat { return w.seat.seat }

// Bind binds the advertised global of iface at its version up to max, for
// the protocols later parts of the window use when offered; nil if the
// compositor does not offer it.
func (w *Window) Bind(iface *wlproto.Interface, max uint32) wlproto.Object {
	return w.conn.bind(iface, max)
}

// Show shows a panel of size logical pixels drawn by paint, replacing any
// visible one. It is drawn once the compositor configures it.
func (w *Window) Show(size scale.Size, paint Painter) error {
	w.Hide()
	p, err := w.newPanel(size, paint)
	if err != nil {
		return err
	}
	w.panel = p
	w.resetInput()
	return w.flush()
}

// Hide hides the visible panel at once, if any. With the test trace on it
// reports `faded`: the window is gone (at once: there is no close
// transition yet).
func (w *Window) Hide() {
	if w.panel != nil {
		w.panel.destroy()
		w.panel = nil
		w.resetInput()
		trace.Emit("faded")
	}
}

// ReleaseKeyboard lets the visible panel go of the keyboard: it stays in
// view, but keys go to whatever has focus beneath it (a toast left to
// read). The keyboard starts over.
func (w *Window) ReleaseKeyboard() {
	if w.panel != nil {
		w.panel.layer.SetKeyboardInteractivity(wlproto.ZwlrLayerSurfaceV1KeyboardInteractivityNone)
		w.panel.surface.Commit()
		_ = w.flush() // a lost compositor shows at the next Dispatch
	}
	w.resetKeyboard()
}

// Visible reports whether a panel is shown (and not closed by the
// compositor).
func (w *Window) Visible() bool { return w.panel != nil }

// Size is the visible panel's size in logical pixels, once configured.
func (w *Window) Size() (scale.Size, bool) {
	if w.panel == nil {
		return scale.Size{}, false
	}
	g, ok := w.panel.state.geometry()
	return g.Logical, ok
}

// RequestRedraw asks for one more frame of the visible panel.
func (w *Window) RequestRedraw() {
	if w.panel != nil {
		w.panel.state.requestRedraw()
	}
}

// Dispatch waits up to timeout (negative: no limit) for compositor events
// or Wake, handles them, and draws the panel if a frame is due: event
// handlers only update state, so a preferred scale and the configure after
// it in one batch make one frame. A lost compositor is a
// DisconnectedError.
func (w *Window) Dispatch(timeout time.Duration) error {
	if err := w.conn.display.Dispatch(timeout); err != nil {
		return disconnected(err)
	}
	if w.panel != nil && w.panel.closed {
		w.Hide()
	}
	w.armRepeat()
	if err := w.draw(); err != nil {
		return err
	}
	return w.flush()
}

// Wake makes a waiting Dispatch return; any goroutine may call it.
func (w *Window) Wake() { w.display.Wake() }

func (w *Window) flush() error {
	if err := w.conn.display.Flush(); err != nil {
		return disconnected(err)
	}
	return nil
}

func disconnected(err error) error {
	var wlErr *wlproto.Error
	if errors.As(err, &wlErr) {
		return &DisconnectedError{Reason: wlErr.Error()}
	}
	return &DisconnectedError{Reason: err.Error()}
}

// draw draws the visible panel if a frame is due. A frame that fails
// because the connection broke under EGL is the compositor's loss.
func (w *Window) draw() error {
	p := w.panel
	if p == nil || !p.state.shouldDraw() {
		return nil
	}
	g, reconfigured := p.state.pendingReconfigure()
	if reconfigured {
		p.reconfigure(g)
	}
	if err := p.present(); err != nil {
		if lost := w.conn.display.Err(); lost != nil {
			return disconnected(lost)
		}
		return &SurfaceError{Reason: err.Error()}
	}
	if reconfigured {
		_, _ = fmt.Fprintf(w.log, "logis: cogitator glass %dx%d px for %dx%d logical at scale %g (EGL Wayland platform)\n",
			g.Physical.W, g.Physical.H, g.Logical.W, g.Logical.H, g.Scale.Factor())
	}
	return nil
}

// shows reports whether s is the visible panel's surface.
func (w *Window) shows(s *wlproto.WlSurface) bool {
	return s != nil && w.panel != nil && w.panel.surface == s
}

// Close hides the panel and releases the seat, the globals, the GPU and
// the connection; the owner goroutine is unlocked from its thread.
func (w *Window) Close() {
	if w.closed {
		return
	}
	w.closed = true
	w.Hide()
	w.releaseInput()
	w.gpu.close()
	w.conn.display.Close()
	w.repeat.close()
	runtime.UnlockOSThread()
}

// resetInput: nothing typed, pointed at or pasted carries over to another
// panel.
func (w *Window) resetInput() {
	w.resetKeyboard()
	w.seat.pointer.reset()
	w.clipboard.cancel()
}

// releaseInput releases the selection devices, the seat with its keyboard
// and pointer, the cursor-shape manager and the activation global.
func (w *Window) releaseInput() {
	w.activation.release()
	w.clipboard.close()
	w.seat.release()
	if shapes := w.seat.pointer.shapes; shapes != nil {
		shapes.Destroy()
		w.seat.pointer.shapes = nil
	}
}
