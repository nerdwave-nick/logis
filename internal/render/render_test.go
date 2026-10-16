package render_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// The offscreen path read back pixel by pixel — SDF coverage, borders,
// gradients, ramps, highlights, glows, premultiplied alpha, scales
// 1.0/1.3/2.0. One GL path, run on i915 and on llvmpipe
// (LIBGL_ALWAYS_SOFTWARE=1).

var logical = scale.Size{W: 100, H: 80}

var blue = render.Hex(0x40a0ff)

func at(n uint32) scale.Scale { return scale.From120ths(n) }

func renderAt(t *testing.T, sc *render.Scene, s scale.Scale) *render.Image {
	t.Helper()
	img, err := rendertest.Headless(t).Offscreen(sc, logical, s)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func shape(rect [4]float32, radius float32, fill render.Paint) render.RoundedRect {
	return render.RoundedRect{Rect: rect, Radius: radius, Fill: fill}
}

func shapes(s ...render.RoundedRect) *render.Scene { return &render.Scene{Shapes: s} }

func near(t *testing.T, what string, actual, expected [4]byte, tolerance uint8) {
	t.Helper()
	for i := range 4 {
		if max(actual[i], expected[i])-min(actual[i], expected[i]) > tolerance {
			t.Errorf("%s: pixel %v is not within %d of %v", what, actual, tolerance, expected)
			return
		}
	}
}

func premultiplied(t *testing.T, img *render.Image) {
	t.Helper()
	for i := 0; i < len(img.Pixels); i += 4 {
		px := img.Pixels[i : i+4]
		if px[0] > px[3] || px[1] > px[3] || px[2] > px[3] {
			t.Fatalf("pixel (%d, %d) = %v is not premultiplied", i/4%img.Width, i/4/img.Width, px)
		}
	}
}

// reference is an opaque blue rounded rectangle, radius 20.
func reference() *render.Scene {
	return shapes(shape([4]float32{10, 10, 80, 60}, 20, render.Solid(blue)))
}

func checkReference(t *testing.T, img *render.Image, s scale.Scale) {
	t.Helper()
	f := s.Factor()
	px := func(x, y float32) [4]byte { return img.Pixel(int(x*f), int(y*f)) }
	if want := s.Physical(logical); img.Width != int(want.W) || img.Height != int(want.H) {
		t.Fatalf("image %dx%d, want %v", img.Width, img.Height, want)
	}
	near(t, "interior", px(50, 40), [4]byte{0x40, 0xa0, 0xff, 255}, 1)
	near(t, "straight top edge", px(30, 12), [4]byte{0x40, 0xa0, 0xff, 255}, 1)
	near(t, "outside the corner's arc", px(12, 12), [4]byte{}, 0)
	near(t, "left of the shape", px(5, 40), [4]byte{}, 0)
	near(t, "below the shape", px(50, 75), [4]byte{}, 0)
	premultiplied(t, img)
}

func TestARoundedRectCoversItsInteriorAndSparesItsCorners(t *testing.T) {
	checkReference(t, renderAt(t, reference(), scale.One), scale.One)
}

func TestScalesOneOnePointThreeAndTwoSizeAndPlaceTheShape(t *testing.T) {
	for _, n := range []uint32{120, 156, 240} {
		s := at(n)
		t.Run(fmt.Sprint(s.Factor()), func(t *testing.T) {
			img := renderAt(t, reference(), s)
			checkReference(t, img, s)
			// The right edge sits at 90 logical pixels at every scale.
			edge := 90 * s.Factor()
			y := int(40 * s.Factor())
			if a := img.Pixel(int(edge)-2, y)[3]; a != 255 {
				t.Errorf("inside the right edge: alpha %d", a)
			}
			if a := img.Pixel(int(edge)+2, y)[3]; a != 0 {
				t.Errorf("outside the right edge: alpha %d", a)
			}
		})
	}
}

func TestATranslucentFillIsPremultiplied(t *testing.T) {
	img := renderAt(t, shapes(shape([4]float32{10, 10, 80, 60}, 8, render.Solid(render.RGBA(1, 0, 0, 0.5)))), scale.One)
	near(t, "interior", img.Pixel(50, 40), [4]byte{128, 0, 0, 128}, 1)
	premultiplied(t, img)
}

func TestAnEdgeBetweenPixelsIsHalfCovered(t *testing.T) {
	// The left edge runs through the middle of pixel column 10.
	img := renderAt(t, shapes(shape([4]float32{10.5, 10, 60, 60}, 0, render.Solid(render.Hex(0xffffff)))), scale.One)
	near(t, "the edge", img.Pixel(10, 40), [4]byte{128, 128, 128, 128}, 2)
	near(t, "left of it", img.Pixel(9, 40), [4]byte{}, 0)
	near(t, "right of it", img.Pixel(11, 40), [4]byte{255, 255, 255, 255}, 0)
}

func TestABorderPaintsTheRingAndTheFillTheInterior(t *testing.T) {
	framed := shape([4]float32{10, 10, 80, 60}, 0, render.Solid(render.Hex(0)))
	framed.Border = render.Border{Width: 4, Paint: render.Solid(render.Hex(0xd4a017))}
	img := renderAt(t, shapes(framed), scale.One)
	brass := [4]byte{0xd4, 0xa0, 0x17, 255}
	near(t, "in the ring", img.Pixel(11, 40), brass, 1)
	near(t, "in the ring", img.Pixel(13, 40), brass, 1)
	near(t, "inside it", img.Pixel(15, 40), [4]byte{0, 0, 0, 255}, 1)
	near(t, "top ring", img.Pixel(50, 12), brass, 1)
}

func TestALinearGradientRunsFromItsStartToItsEnd(t *testing.T) {
	gradient := render.Linear([2]float32{0, 0}, [2]float32{100, 0}, render.Hex(0xff0000), render.Hex(0x0000ff))
	img := renderAt(t, shapes(shape([4]float32{0, 0, 100, 80}, 0, gradient)), scale.One)
	near(t, "start", img.Pixel(0, 40), [4]byte{255, 0, 0, 255}, 2)
	near(t, "middle", img.Pixel(50, 40), [4]byte{127, 0, 128, 255}, 2)
	near(t, "end", img.Pixel(99, 40), [4]byte{0, 0, 255, 255}, 2)
}

func TestAGlowFadesOutsideTheEdgeAndStaysOutOfTheInterior(t *testing.T) {
	lit := shape([4]float32{40, 30, 20, 20}, 4, render.Solid(render.Hex(0)))
	lit.Glow = render.Glow{Radius: 20, Color: render.Hex(0xb30f0d)}
	img := renderAt(t, shapes(lit), scale.One)
	alphaRightOfEdge := func(gap int) byte { return img.Pixel(60+gap, 40)[3] }
	if alphaRightOfEdge(2) <= alphaRightOfEdge(10) || alphaRightOfEdge(10) == 0 {
		t.Errorf("alpha 2 px out %d, 10 px out %d: not fading", alphaRightOfEdge(2), alphaRightOfEdge(10))
	}
	if a := alphaRightOfEdge(21); a != 0 {
		t.Errorf("alpha %d beyond the reach", a)
	}
	near(t, "opaque interior", img.Pixel(50, 40), [4]byte{0, 0, 0, 255}, 1)
	premultiplied(t, img)
}

func TestLaterShapesPaintOverEarlierOnesAndTheClearColourShowsThrough(t *testing.T) {
	sc := shapes(
		shape([4]float32{0, 0, 60, 80}, 0, render.Solid(render.Hex(0xff0000))),
		shape([4]float32{40, 0, 60, 40}, 0, render.Solid(render.Hex(0x00ff00))),
	)
	sc.Clear = render.Hex(0x101010)
	img := renderAt(t, sc, scale.One)
	near(t, "overlap: top shape", img.Pixel(50, 20), [4]byte{0, 255, 0, 255}, 1)
	near(t, "only the first", img.Pixel(50, 60), [4]byte{255, 0, 0, 255}, 1)
	near(t, "clear colour", img.Pixel(80, 60), [4]byte{16, 16, 16, 255}, 1)
}

// Ramps and highlights on a 80×60 box at (10, 10).

var (
	red   = render.Hex(0xff0000)
	green = render.Hex(0x00ff00)
	blu   = render.Hex(0x0000ff)
	white = render.Hex(0xffffff)
)

func box(fill render.Paint, border render.Border) *render.Scene {
	b := shape([4]float32{10, 10, 80, 60}, 0, fill)
	b.Border = border
	sc := shapes(b)
	sc.Clear = render.Hex(0)
	return sc
}

func TestARampPassesThroughEachOfItsStopsEvenlySpaced(t *testing.T) {
	fill := render.Ramp([2]float32{10, 0}, [2]float32{90, 0}, red, green, blu)
	img := renderAt(t, box(fill, render.Border{}), scale.One)
	near(t, "first stop", img.Pixel(10, 40), [4]byte{255, 0, 0, 255}, 8)
	near(t, "second stop", img.Pixel(50, 40), [4]byte{0, 255, 0, 255}, 8)
	near(t, "third stop", img.Pixel(89, 40), [4]byte{0, 6, 249, 255}, 8)
	near(t, "between the first two", img.Pixel(30, 40), [4]byte{128, 128, 0, 255}, 8)
	near(t, "between the last two", img.Pixel(70, 40), [4]byte{0, 128, 128, 255}, 8)
}

func TestABorderRampRunsTopToBottomThroughFourStops(t *testing.T) {
	paint := render.Ramp([2]float32{0, 10}, [2]float32{0, 70}, red, green, blu, white)
	img := renderAt(t, box(render.Solid(render.Hex(0)), render.Border{Width: 4, Paint: paint}), scale.One)
	// The left border at the stops' heights: 10, 30, 50, 70.
	near(t, "at 10", img.Pixel(11, 10), [4]byte{255, 0, 0, 255}, 16)
	near(t, "at 30", img.Pixel(11, 30), [4]byte{0, 255, 0, 255}, 16)
	near(t, "at 50", img.Pixel(11, 50), [4]byte{0, 0, 255, 255}, 16)
	near(t, "at 70", img.Pixel(11, 69), [4]byte{255, 255, 255, 255}, 20)
}

func highlighted(angle float32) *render.Scene {
	return box(render.Solid(render.Hex(0)), render.Border{
		Width:     4,
		Paint:     render.Solid(render.Hex(0x400000)),
		Highlight: render.Highlight{Angle: angle, Reach: 15, Color: white},
	})
}

func TestAHighlightBrightensTheBorderWhereItStandsOnly(t *testing.T) {
	dark := [4]byte{0x40, 0, 0, 255}
	// At the top (-π/2: angles turn clockwise from the right, y down).
	top := renderAt(t, highlighted(-math.Pi/2), scale.One)
	if lit := top.Pixel(50, 11); lit[1] <= 200 || lit[2] <= 200 {
		t.Errorf("the top centre is %v", lit)
	}
	near(t, "bottom", top.Pixel(50, 68), dark, 2)
	near(t, "left", top.Pixel(11, 40), dark, 2)
	// At the right.
	right := renderAt(t, highlighted(0), scale.One)
	if lit := right.Pixel(88, 40); lit[1] <= 200 {
		t.Errorf("the right middle is %v", lit)
	}
	near(t, "top", right.Pixel(50, 11), dark, 2)
	// Only the border: the inside stays black.
	near(t, "inside", right.Pixel(80, 40), [4]byte{0, 0, 0, 255}, 0)
}
