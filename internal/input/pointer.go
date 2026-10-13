package input

import "math"

// The pointer as the visible session sees it (spec feat/logis §3 story 16;
// Rust src/input/pointer.rs): the wl_pointer events in, Points out. The
// window's seat forwards the protocol events; this model groups them into
// frames and decides what they mean.
//
//   - Position: motion reports where the pointer is, in the surface's
//     logical pixels; leave ends it. Enter only records the position: a
//     window that opens under a pointer at rest must not move the
//     selection (nor undo a preselection), so only a pointer that moves
//     points.
//   - Buttons: the primary button's press and release are reported where
//     they happen (the list accepts a row pressed and released on the same
//     item); the middle button pastes the primary selection, as it does
//     everywhere on a Unix desktop.
//   - Scrolling: the vertical axis of one frame becomes one Scroll. A
//     wheel reports in 120ths of a notch: axis_value120 (wl_pointer v8,
//     high-resolution wheels send fractions), else axis_discrete (v5–7) in
//     whole notches. A touchpad finger, a continuous device, or a wheel
//     without either reports logical pixels; axis_stop ends a finger's
//     scroll. Whatever the axis_source, whole notches are used when the
//     compositor counts them, pixels otherwise. Horizontal scrolling means
//     nothing to a list.
//
// Before wl_pointer v5 there are no frames: the seat ends a frame after
// every axis event itself.

// Evdev button codes (linux/input-event-codes.h).
const (
	// BtnLeft is the primary button.
	BtnLeft = 0x110
	// BtnMiddle pastes the primary selection.
	BtnMiddle = 0x112
)

// PointKind is what the pointer did.
type PointKind int

const (
	// PointMoved: the pointer moved to (X, Y).
	PointMoved PointKind = iota + 1
	// PointLeft: the pointer left the window.
	PointLeft
	// PointPressed: the primary button went down at (X, Y).
	PointPressed
	// PointReleased: the primary button came up at (X, Y).
	PointReleased
	// PointScrolled: the list scrolls by Scroll, the pointer at (X, Y).
	PointScrolled
)

// Point is what the pointer did over the visible session, in surface-local
// logical pixels.
type Point struct {
	Kind   PointKind
	X, Y   float64
	Scroll Scroll // for PointScrolled
}

// ScrollKind is how one frame scrolls.
type ScrollKind int

const (
	// ScrollWheel: a wheel, by Value120 (120ths of a notch).
	ScrollWheel ScrollKind = iota + 1
	// ScrollPixels: a finger or a continuous device, by Pixels (logical).
	ScrollPixels
	// ScrollStop: the finger lifted; a continuous scroll ended.
	ScrollStop
)

// Scroll is one frame's vertical scroll; positive moves towards later
// items.
type Scroll struct {
	Kind     ScrollKind
	Value120 int32
	Pixels   float64
}

// pointerFrame is the vertical axis events of the frame being collected.
type pointerFrame struct {
	value    float64
	hasValue bool
	value120 int32
	discrete int32
	stop     bool
}

// Pointer is the pointer's position over the visible window and the frame
// in progress. It is owned by the window's goroutine.
type Pointer struct {
	x, y  float64
	over  bool
	frame pointerFrame
}

// NewPointer is a pointer that is not over the window.
func NewPointer() *Pointer { return &Pointer{} }

// Position is where the pointer is, while it is over the window.
func (p *Pointer) Position() (x, y float64, ok bool) { return p.x, p.y, p.over }

// Reset forgets the position and any frame in progress (the window was
// shown or hidden; the compositor sends a new enter for a new one).
func (p *Pointer) Reset() { *p = Pointer{} }

// Enter: the pointer entered the window at (x, y); recorded, not
// reported.
func (p *Pointer) Enter(x, y float64) {
	p.frame = pointerFrame{}
	p.x, p.y, p.over = x, y, true
}

// Motion: the pointer moved to (x, y); nothing while it is not over the
// window.
func (p *Pointer) Motion(x, y float64) (Point, bool) {
	if !p.over {
		return Point{}, false
	}
	p.x, p.y = x, y
	return Point{Kind: PointMoved, X: x, Y: y}, true
}

// Leave: the pointer left the window.
func (p *Pointer) Leave() (Point, bool) {
	was := p.over
	p.over = false
	p.frame = pointerFrame{}
	return Point{Kind: PointLeft}, was
}

// Button: evdev button code button was pressed or released.
func (p *Pointer) Button(button uint32, pressed bool) (Action, bool) {
	if !p.over {
		return Action{}, false
	}
	switch {
	case button == BtnLeft && pressed:
		return Action{Kind: PointAt, Point: Point{Kind: PointPressed, X: p.x, Y: p.y}}, true
	case button == BtnLeft:
		return Action{Kind: PointAt, Point: Point{Kind: PointReleased, X: p.x, Y: p.y}}, true
	case button == BtnMiddle && pressed:
		return Action{Kind: Paste, Selection: Primary}, true
	}
	return Action{}, false
}

// Axis is a vertical axis value in logical pixels (continuous).
func (p *Pointer) Axis(value float64) {
	p.frame.value += value
	p.frame.hasValue = true
}

// AxisDiscrete is a vertical wheel movement in whole notches
// (wl_pointer v5–7).
func (p *Pointer) AxisDiscrete(discrete int32) {
	p.frame.discrete = saturate(int64(p.frame.discrete) + int64(discrete))
}

// AxisValue120 is a vertical wheel movement in 120ths of a notch
// (wl_pointer v8+).
func (p *Pointer) AxisValue120(value120 int32) {
	p.frame.value120 = saturate(int64(p.frame.value120) + int64(value120))
}

// AxisStop: the vertical axis stopped (a finger lifted).
func (p *Pointer) AxisStop() { p.frame.stop = true }

// Frame ends the frame: what its vertical axis events scroll, if anything.
func (p *Pointer) Frame() []Point {
	f := p.frame
	p.frame = pointerFrame{}
	if !p.over {
		return nil
	}
	var points []Point
	scrolled := func(s Scroll) { points = append(points, Point{Kind: PointScrolled, X: p.x, Y: p.y, Scroll: s}) }
	switch {
	case f.value120 != 0:
		scrolled(Scroll{Kind: ScrollWheel, Value120: f.value120})
	case f.discrete != 0:
		scrolled(Scroll{Kind: ScrollWheel, Value120: saturate(int64(f.discrete) * 120)})
	case f.hasValue && f.value != 0:
		scrolled(Scroll{Kind: ScrollPixels, Pixels: f.value})
	}
	if f.stop {
		scrolled(Scroll{Kind: ScrollStop})
	}
	return points
}

// saturate clamps v to int32, as Rust's saturating arithmetic.
func saturate(v int64) int32 { return int32(max(min(v, math.MaxInt32), math.MinInt32)) }
