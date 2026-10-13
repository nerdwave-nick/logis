package window

import (
	"strconv"

	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/trace"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// pointer is the seat's pointer while it has one, its cursor-shape device
// and the pointer model (Rust src/window/pointer.rs): it forwards the
// wl_pointer events over the visible panel to input.Pointer and hands the
// actions it yields to the window's sink, beside the keyboard's, so their
// order is kept.
//
// The cursor is the compositor's default arrow, set through
// wp_cursor_shape_v1 on every enter when the compositor offers it. Without
// it logis sets no cursor image at all (no cursor theme is loaded
// in-process): the compositor keeps showing the cursor it had, which is
// what a missing cursor-shape protocol gracefully allows.
//
// Trace events: `pointer-enter <x> <y>` and `pointer-motion <x> <y>`
// (surface coordinates), `pointer-leave`, `cursor default` or `cursor
// unchanged` (no cursor-shape protocol), `button <code> pressed|released`,
// and per frame `scroll wheel <120ths>`, `scroll pixels <px>` or `scroll
// stop`.
type pointer struct {
	wl     *wlproto.WlPointer
	shapes *wlproto.WpCursorShapeManagerV1 // nil if not offered
	shape  *wlproto.WpCursorShapeDeviceV1
	model  *input.Pointer
	shows  func(*wlproto.WlSurface) bool
	// sink receives the pointer's actions (nil: they are dropped).
	sink func(input.Action)
	// pressed is told the serial of each button press (xdg-activation's).
	pressed func(serial uint32)
}

// capability: the seat gained or lost its pointer.
func (p *pointer) capability(seat *wlproto.WlSeat, has bool) {
	switch {
	case has && p.wl == nil:
		p.wl = seat.GetPointer()
		if p.shapes != nil {
			p.shape = p.shapes.GetPointer(p.wl)
		}
		p.listen(p.wl)
	case !has && p.wl != nil:
		p.release()
	}
}

// reset forgets where the pointer was (the panel was shown or hidden).
func (p *pointer) reset() { p.model.Reset() }

// release lets go of the pointer and its cursor-shape device.
func (p *pointer) release() {
	p.model.Reset()
	if p.shape != nil {
		p.shape.Destroy()
		p.shape = nil
	}
	if p.wl == nil {
		return
	}
	if p.wl.Version() >= 3 {
		p.wl.Release()
	} else {
		p.wl.Destroy()
	}
	p.wl = nil
}

// setCursor sets the default arrow for the enter with serial.
func (p *pointer) setCursor(serial uint32) {
	if p.shape == nil {
		trace.Emit("cursor unchanged")
		return
	}
	p.shape.SetShape(serial, wlproto.WpCursorShapeDeviceV1ShapeDefault)
	trace.Emit("cursor default")
}

func (p *pointer) act(a input.Action) {
	if p.sink != nil {
		p.sink(a)
	}
}

func (p *pointer) point(pt input.Point) { p.act(input.Action{Kind: input.PointAt, Point: pt}) }

// frame ends a frame: each scroll is traced and pointed.
func (p *pointer) frame() {
	for _, pt := range p.model.Frame() {
		switch s := pt.Scroll; s.Kind {
		case input.ScrollWheel:
			trace.Emit("scroll wheel %d", s.Value120)
		case input.ScrollPixels:
			trace.Emit("scroll pixels %s", num(s.Pixels))
		case input.ScrollStop:
			trace.Emit("scroll stop")
		}
		p.point(pt)
	}
}

func (p *pointer) listen(wl *wlproto.WlPointer) {
	// Before v5 there are no frames: every axis event is one.
	unframed := wl.Version() < 5
	vertical := func(axis uint32) bool { return axis == wlproto.WlPointerAxisVerticalScroll }
	wl.OnEnter = func(serial uint32, s *wlproto.WlSurface, x, y wlproto.Fixed) {
		if !p.shows(s) {
			return
		}
		trace.Emit("pointer-enter %s %s", num(x.Float()), num(y.Float()))
		p.setCursor(serial)
		p.model.Enter(x.Float(), y.Float())
	}
	wl.OnLeave = func(uint32, *wlproto.WlSurface) {
		if left, ok := p.model.Leave(); ok {
			trace.Emit("pointer-leave")
			p.point(left)
		}
	}
	wl.OnMotion = func(_ uint32, x, y wlproto.Fixed) {
		if moved, ok := p.model.Motion(x.Float(), y.Float()); ok {
			trace.Emit("pointer-motion %s %s", num(x.Float()), num(y.Float()))
			p.point(moved)
		}
	}
	wl.OnButton = func(serial, _, button, state uint32) {
		pressed := state == wlproto.WlPointerButtonStatePressed
		if pressed && p.pressed != nil {
			p.pressed(serial)
		}
		if _, _, over := p.model.Position(); over {
			word := "released"
			if pressed {
				word = "pressed"
			}
			trace.Emit("button %d %s", button, word)
		}
		if a, ok := p.model.Button(button, pressed); ok {
			p.act(a)
		}
	}
	wl.OnAxis = func(_, axis uint32, value wlproto.Fixed) {
		if vertical(axis) {
			p.model.Axis(value.Float())
			if unframed {
				p.frame()
			}
		}
	}
	wl.OnAxisDiscrete = func(axis uint32, discrete int32) {
		if vertical(axis) {
			p.model.AxisDiscrete(discrete)
		}
	}
	wl.OnAxisValue120 = func(axis uint32, value120 int32) {
		if vertical(axis) {
			p.model.AxisValue120(value120)
		}
	}
	wl.OnAxisStop = func(_, axis uint32) {
		if vertical(axis) {
			p.model.AxisStop()
		}
	}
	wl.OnFrame = p.frame
}

// num writes a coordinate as Rust's Display does for f64: the shortest
// decimal that reads back the same, never an exponent.
func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
