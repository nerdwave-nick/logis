package window

import (
	"math"
	"slices"
	"testing"
)

// Tests of the blur region: a rounded rectangle in whole pixels.

func covers(rects [][4]int32, x, y int32) bool {
	return slices.ContainsFunc(rects, func(r [4]int32) bool {
		return x >= r[0] && x < r[0]+r[2] && y >= r[1] && y < r[1]+r[3]
	})
}

func TestASquareCorneredBodyIsOneRectangle(t *testing.T) {
	got := regionRects(Backdrop{Rect: [4]float32{24, 24, 300, 100}})
	if !slices.Equal(got, [][4]int32{{24, 24, 300, 100}}) {
		t.Errorf("%v", got)
	}
}

func TestRoundedCornersAreSteppedInsideTheCurve(t *testing.T) {
	rects := regionRects(Backdrop{Rect: [4]float32{24, 24, 300, 100}, Radius: 14})
	// 14 rows at the top, the middle, 14 rows at the bottom.
	if len(rects) != 29 {
		t.Fatalf("%d rectangles: %v", len(rects), rects)
	}
	if rects[14] != [4]int32{24, 38, 300, 72} {
		t.Errorf("middle %v", rects[14])
	}
	// The top row is inset the most, the 14th row hardly at all.
	if rects[0][0] <= rects[13][0] || rects[0][1] != 24 || rects[28][1]+rects[28][3] != 124 {
		t.Errorf("%v", rects)
	}
	// Every covered pixel's square lies inside the rounded rectangle…
	cx, cy := 24.0+14.0, 24.0+14.0
	for _, r := range rects {
		for px := r[0]; px < r[0]+r[2]; px++ {
			for py := r[1]; py < r[1]+r[3]; py++ {
				fx, fy := float64(px), float64(py)
				if fx < cx && fy < cy && math.Hypot(cx-fx, cy-fy) > 14+1e-3 {
					t.Fatalf("(%d,%d) is outside the arc", px, py)
				}
			}
		}
	}
	// …and the body's centre and edge midpoints are blurred.
	if !covers(rects, 174, 74) || !covers(rects, 24, 74) || !covers(rects, 174, 24) {
		t.Error("the body is not covered")
	}
	if covers(rects, 24, 24) {
		t.Error("the corner pixel is covered")
	}
}

func TestAFractionalBodyIsRoundedInwards(t *testing.T) {
	got := regionRects(Backdrop{Rect: [4]float32{24.4, 23.6, 100.2, 50}})
	if !slices.Equal(got, [][4]int32{{25, 24, 99, 49}}) {
		t.Errorf("%v", got)
	}
}

func TestARadiusBeyondHalfTheShortSideIsClamped(t *testing.T) {
	rects := regionRects(Backdrop{Rect: [4]float32{0, 0, 40, 10}, Radius: 50})
	if len(rects) != 10 {
		t.Fatalf("%d rows, want every row a corner row: %v", len(rects), rects)
	}
	for _, r := range rects {
		if r[0] < 0 || r[0]+r[2] > 40 {
			t.Errorf("%v leaves the body", r)
		}
	}
}

func TestAnEmptyBodyBlursNothing(t *testing.T) {
	if got := regionRects(Backdrop{Rect: [4]float32{10, 10, 0.5, 30}, Radius: 4}); len(got) != 0 {
		t.Errorf("%v", got)
	}
}
