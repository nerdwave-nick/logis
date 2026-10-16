package input

import (
	"slices"
	"testing"
)

// Ported from Rust src/input/pointer.rs.

func over(x, y float64) *Pointer {
	p := NewPointer()
	p.Enter(x, y)
	return p
}

func moved(x, y float64) Point     { return Point{Kind: PointMoved, X: x, Y: y} }
func pressedAt(x, y float64) Point { return Point{Kind: PointPressed, X: x, Y: y} }

func wheel(v int32, x, y float64) Point {
	return Point{Kind: PointScrolled, X: x, Y: y, Scroll: Scroll{Kind: ScrollWheel, Value120: v}}
}

func pixels(px, x, y float64) Point {
	return Point{Kind: PointScrolled, X: x, Y: y, Scroll: Scroll{Kind: ScrollPixels, Pixels: px}}
}

func stopped(x, y float64) Point {
	return Point{Kind: PointScrolled, X: x, Y: y, Scroll: Scroll{Kind: ScrollStop}}
}

func wantFrame(t *testing.T, p *Pointer, want ...Point) {
	t.Helper()
	if got := p.Frame(); !slices.Equal(got, want) {
		t.Errorf("frame %+v, want %+v", got, want)
	}
}

func TestMotionReportsThePositionFromEnterUntilLeave(t *testing.T) {
	p := NewPointer()
	if _, ok := p.Motion(1, 2); ok {
		t.Error("not over the window yet")
	}
	p.Enter(10, 20)
	if x, y, ok := p.Position(); !ok || x != 10 || y != 20 {
		t.Errorf("recorded, not reported: %v %v %v", x, y, ok)
	}
	if got, ok := p.Motion(11.5, 21); !ok || got != moved(11.5, 21) {
		t.Errorf("motion %+v %v", got, ok)
	}
	if x, y, ok := p.Position(); !ok || x != 11.5 || y != 21 {
		t.Errorf("position %v %v %v", x, y, ok)
	}
	if got, ok := p.Leave(); !ok || got.Kind != PointLeft {
		t.Errorf("leave %+v %v", got, ok)
	}
	if _, ok := p.Leave(); ok {
		t.Error("a second leave")
	}
	if _, ok := p.Motion(1, 2); ok {
		t.Error("motion after leave")
	}
	if _, _, ok := p.Position(); ok {
		t.Error("a position after leave")
	}
}

func TestThePrimaryButtonReportsPressAndReleaseWhereTheyHappen(t *testing.T) {
	p := over(5, 6)
	if a, ok := p.Button(BtnLeft, true); !ok || a != (Action{Kind: PointAt, Point: pressedAt(5, 6)}) {
		t.Errorf("press %+v %v", a, ok)
	}
	p.Motion(7, 8)
	want := Action{Kind: PointAt, Point: Point{Kind: PointReleased, X: 7, Y: 8}}
	if a, ok := p.Button(BtnLeft, false); !ok || a != want {
		t.Errorf("release %+v %v", a, ok)
	}
}

func TestTheMiddleButtonPastesThePrimarySelectionOnPress(t *testing.T) {
	p := over(5, 6)
	if a, ok := p.Button(BtnMiddle, true); !ok || a != (Action{Kind: Paste, Selection: Primary}) {
		t.Errorf("middle press %+v %v", a, ok)
	}
	if _, ok := p.Button(BtnMiddle, false); ok {
		t.Error("middle release")
	}
	if _, ok := p.Button(0x111, true); ok {
		t.Error("the right button does nothing")
	}
}

func TestButtonsOutsideTheWindowDoNothing(t *testing.T) {
	p := NewPointer()
	if _, ok := p.Button(BtnLeft, true); ok {
		t.Error("left")
	}
	if _, ok := p.Button(BtnMiddle, true); ok {
		t.Error("middle")
	}
}

func TestAWheelFrameScrollsByValue120AndIgnoresItsPixels(t *testing.T) {
	p := over(1, 2)
	p.Axis(15)
	p.AxisValue120(120)
	wantFrame(t, p, wheel(120, 1, 2))
	wantFrame(t, p) // a frame is used once
}

func TestAHighResolutionWheelReportsFractionsOfANotch(t *testing.T) {
	p := over(0, 0)
	p.AxisValue120(-30)
	wantFrame(t, p, wheel(-30, 0, 0))
}

func TestDiscreteStepsCountWholeNotchesBeforeV8(t *testing.T) {
	p := over(0, 0)
	p.Axis(-30)
	p.AxisDiscrete(-2)
	wantFrame(t, p, wheel(-240, 0, 0))
}

func TestAFingerScrollsByPixelsAndItsStopEndsTheScroll(t *testing.T) {
	p := over(3, 4)
	p.Axis(4.5)
	p.Axis(2)
	wantFrame(t, p, pixels(6.5, 3, 4))
	p.AxisStop()
	wantFrame(t, p, stopped(3, 4))
}

func TestAFrameWithoutVerticalMovementScrollsNothing(t *testing.T) {
	p := over(0, 0)
	p.Axis(0)
	wantFrame(t, p)
	away := NewPointer()
	away.AxisValue120(120)
	wantFrame(t, away) // not over the window
}

func TestLeavingDropsTheFrameInProgress(t *testing.T) {
	p := over(0, 0)
	p.AxisValue120(120)
	p.Leave()
	p.Enter(1, 1)
	wantFrame(t, p)
}

func TestHugeWheelValuesSaturate(t *testing.T) {
	p := over(0, 0)
	p.AxisDiscrete(1 << 30)
	wantFrame(t, p, wheel(1<<31-1, 0, 0))
	p.AxisValue120(1<<31 - 1)
	p.AxisValue120(1)
	wantFrame(t, p, wheel(1<<31-1, 0, 0))
}

func TestResetForgetsThePositionAndTheFrame(t *testing.T) {
	p := over(1, 1)
	p.AxisValue120(120)
	p.Reset()
	if _, _, ok := p.Position(); ok {
		t.Error("a position after reset")
	}
	wantFrame(t, p)
}
