package e2e

import (
	"math"
	"time"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// evdev button codes (linux/input-event-codes.h).
const (
	BtnLeft   = 0x110
	BtnMiddle = 0x112
)

// notchPixels is what wlroots and libinput report per wheel notch as a
// continuous value.
const notchPixels = 15

// VirtualPointer is a zwlr_virtual_pointer_v1 on the harness connection:
// absolute motion over the test output, buttons, wheel notches and finger
// scrolling, each followed by a frame. Like the keyboard it exists only on
// a verified connection, so it can never move the live session's pointer.
type VirtualPointer struct {
	h       *Harness
	pointer *wlproto.ZwlrVirtualPointerV1
	epoch   time.Time
}

// VirtualPointer creates a virtual pointer on the seat, bound to the
// harness's output where the compositor supports that (v2), so absolute
// motion spans it.
func (h *Harness) VirtualPointer() *VirtualPointer {
	h.t.Helper()
	if h.pointers == nil {
		h.t.Fatal("the test compositor does not offer zwlr_virtual_pointer_manager_v1")
	}
	p := &VirtualPointer{h: h, epoch: time.Now()}
	if h.pointers.Version() >= 2 {
		p.pointer = h.pointers.CreateVirtualPointerWithOutput(h.seat, h.output)
	} else {
		p.pointer = h.pointers.CreateVirtualPointer(h.seat)
	}
	h.Roundtrip()
	return p
}

func (p *VirtualPointer) time() uint32 { return uint32(time.Since(p.epoch).Milliseconds()) }

func (p *VirtualPointer) frame() {
	p.pointer.Frame()
	p.h.Roundtrip()
}

// MoveTo moves the pointer to the output point (x, y).
func (p *VirtualPointer) MoveTo(x, y float64) {
	w, h := p.h.OutputSize()
	clamp := func(v float64, extent int) uint32 {
		return uint32(min(max(math.Round(v), 0), float64(extent-1)))
	}
	p.pointer.MotionAbsolute(p.time(), clamp(x, w), clamp(y, h), uint32(w), uint32(h))
	p.frame()
}

// Button presses or releases button.
func (p *VirtualPointer) Button(button uint32, pressed bool) {
	state := uint32(wlproto.WlPointerButtonStateReleased)
	if pressed {
		state = wlproto.WlPointerButtonStatePressed
	}
	p.pointer.Button(p.time(), button, state)
	p.frame()
}

// Click presses and releases button where the pointer is.
func (p *VirtualPointer) Click(button uint32) {
	p.Button(button, true)
	p.Button(button, false)
}

// Wheel turns the wheel by notches (positive: down), one frame.
func (p *VirtualPointer) Wheel(notches int32) {
	t := p.time()
	p.pointer.AxisSource(wlproto.WlPointerAxisSourceWheel)
	p.pointer.AxisDiscrete(t, wlproto.WlPointerAxisVerticalScroll, wlproto.FixedFrom(float64(notches)*notchPixels), notches)
	p.frame()
}

// Finger moves a touchpad finger by dy logical pixels, one frame.
func (p *VirtualPointer) Finger(dy float64) {
	p.pointer.AxisSource(wlproto.WlPointerAxisSourceFinger)
	p.pointer.Axis(p.time(), wlproto.WlPointerAxisVerticalScroll, wlproto.FixedFrom(dy))
	p.frame()
}

// Close destroys the virtual pointer.
func (p *VirtualPointer) Close() {
	p.pointer.Destroy()
	p.h.Roundtrip()
}

// Panel maps the harness's output points onto a client's surface, learnt
// from where the client itself said the pointer entered it and then
// moved, so no test assumes how a compositor places the surface or maps
// absolute motion (nested niri maps it through its output's flip).
type Panel struct {
	// Anchor is an output point the pointer was sent to and the surface
	// point the client reported for it.
	Anchor [2][2]float64
	// Sign is per axis +1 if surface coordinates grow with output
	// coordinates, -1 if the compositor mirrored that axis.
	Sign [2]float64
}

// Calibrate makes a Panel from two output points sent and the surface
// points the client reported for them; both must differ on both axes.
func Calibrate(sent, seen [2][2]float64) Panel {
	sign := func(axis int) float64 {
		a, b, c, d := sent[0][axis], sent[1][axis], seen[0][axis], seen[1][axis]
		if a == b || c == d {
			panic("calibration needs movement on both axes")
		}
		return math.Copysign(1, (d-c)/(b-a))
	}
	return Panel{Anchor: [2][2]float64{sent[0], seen[0]}, Sign: [2]float64{sign(0), sign(1)}}
}

// At is the output point over the surface-local point (x, y).
func (p Panel) At(x, y float64) (float64, float64) {
	o, s := p.Anchor[0], p.Anchor[1]
	return o[0] + p.Sign[0]*(x-s[0]), o[1] + p.Sign[1]*(y-s[1])
}
