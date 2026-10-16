package render

import (
	"testing"
)

// Colours, paints and the instance layout the sdf shader reads.

func rect(w, h float32) RoundedRect {
	return RoundedRect{
		Rect:   [4]float32{10, 20, w, h},
		Radius: 8,
		Fill:   Solid(RGBA(1, 0.5, 0, 0.5)),
	}
}

func eq(t *testing.T, what string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: %v, want %v", what, got, want)
			return
		}
	}
}

func TestColoursPremultiplyAndClamp(t *testing.T) {
	if got := RGBA(1, 0.5, 0, 0.5).Premultiplied(); got != [4]float32{0.5, 0.25, 0, 0.5} {
		t.Errorf("got %v", got)
	}
	if got := RGBA(2, -1, 0.5, 1.5).Premultiplied(); got != [4]float32{1, 0, 0.5, 1} {
		t.Errorf("got %v", got)
	}
}

func TestHexColoursAreOpaqueSRGBBytes(t *testing.T) {
	if got, want := Hex(0xff8000), RGBA(1, 128.0/255, 0, 1); got != want {
		t.Errorf("Hex(0xff8000) = %v, want %v", got, want)
	}
	if got := Hex(0x123456).WithAlpha(0.25).A; got != 0.25 {
		t.Errorf("WithAlpha: %v", got)
	}
}

func TestAShapePacksRectParamsAndPremultipliedPaint(t *testing.T) {
	packed := pack(rect(100, 40))
	eq(t, "rect", packed[0:4], []float32{10, 20, 100, 40})
	eq(t, "params", packed[4:8], []float32{8, 0, 0, 0})
	eq(t, "the one fill stop", packed[8:12], []float32{0.5, 0.25, 0, 0.5})
	eq(t, "one fill stop, one border stop", packed[56:58], []float32{1, 1})
	eq(t, "no glow", packed[48:52], []float32{0, 0, 0, 0})
}

func TestTheRadiusAndBorderAreClampedToHalfTheShortSide(t *testing.T) {
	shape := rect(100, 10)
	shape.Radius = 50
	shape.Border = Border{Width: 30, Paint: Solid(Hex(0xffffff))}
	packed := pack(shape)
	eq(t, "radius, border", packed[4:6], []float32{5, 5})
}

func TestNegativeSizesAndReachesBecomeZero(t *testing.T) {
	shape := rect(-5, 10)
	shape.Radius = -3
	shape.Glow = Glow{Radius: -4, Color: Hex(0xff0000)}
	packed := pack(shape)
	eq(t, "width", packed[2:3], []float32{0})
	eq(t, "radius", packed[4:5], []float32{0})
	eq(t, "glow radius", packed[6:7], []float32{0})
	eq(t, "glow colour", packed[48:52], []float32{1, 0, 0, 1})
}

func TestAGradientPacksItsLineAndBothEnds(t *testing.T) {
	shape := rect(100, 40)
	shape.Fill = Linear([2]float32{10, 20}, [2]float32{110, 20}, Hex(0xff0000), Hex(0x0000ff).WithAlpha(0.5))
	packed := pack(shape)
	eq(t, "from", packed[8:12], []float32{1, 0, 0, 1})
	eq(t, "to", packed[12:16], []float32{0, 0, 0.5, 0.5})
	eq(t, "line", packed[24:28], []float32{10, 20, 110, 20})
	eq(t, "two stops", packed[56:57], []float32{2})
}

func TestARampPacksItsStopsAndAHighlightItsAngleAndWidth(t *testing.T) {
	shape := rect(100, 40)
	stops := []Color{Hex(0xff0000), Hex(0x00ff00), Hex(0x0000ff)}
	shape.Border = Border{
		Width:     2,
		Paint:     Ramp([2]float32{0, 20}, [2]float32{0, 60}, stops...),
		Highlight: Highlight{Angle: -1.5, Reach: 35, Color: Hex(0xffffff)},
	}
	packed := pack(shape)
	eq(t, "first stop", packed[28:32], []float32{1, 0, 0, 1})
	eq(t, "third stop", packed[36:40], []float32{0, 0, 1, 1})
	eq(t, "border line", packed[44:48], []float32{0, 20, 0, 60})
	eq(t, "three border stops", packed[57:58], []float32{3})
	eq(t, "the angle", packed[7:8], []float32{-1.5})
	// 35 px along a box whose mean half extent is (100 + 40) / 4 = 35.
	eq(t, "the width", packed[58:59], []float32{1})
	eq(t, "the colour", packed[52:56], []float32{1, 1, 1, 1})
}

func TestWithoutAHighlightTheWidthIsOneAndTheColourTransparent(t *testing.T) {
	packed := pack(rect(100, 40))
	eq(t, "angle", packed[7:8], []float32{0})
	eq(t, "width", packed[58:59], []float32{1})
	eq(t, "colour", packed[52:56], []float32{0, 0, 0, 0})
}

// A ramp of one stop is solid, of two linear, and keeps four at most.
func TestOneOrTwoStopsAreThePaintsThereWere(t *testing.T) {
	red, green, blue, white := Hex(0xff0000), Hex(0x00ff00), Hex(0x0000ff), Hex(0xffffff)
	start, end := [2]float32{0, 0}, [2]float32{0, 10}
	if got := Ramp(start, end, red); got != Solid(red) {
		t.Errorf("one stop: %+v", got)
	}
	if got := Ramp(start, end, red, blue); got != Linear(start, end, red, blue) {
		t.Errorf("two stops: %+v", got)
	}
	ramp := Ramp(start, end, red, green, blue, white, red)
	got := ramp.Stops()
	if want := []Color{red, green, blue, white}; len(got) != 4 || [4]Color(got) != [4]Color(want) {
		t.Errorf("four at most: %v", got)
	}
	if got := Ramp(start, end).Stops(); len(got) != 1 || got[0] != Transparent {
		t.Errorf("no stop is transparent: %v", got)
	}
}
