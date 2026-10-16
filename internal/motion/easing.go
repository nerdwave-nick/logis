package motion

import "math"

// Easing is a progress curve from (0, 0) to (1, 1): how a transition's
// progress in time (0 → 1) maps to how far it has moved (0 → 1). Linear, or
// CSS's cubic-bezier(x1, y1, x2, y2), so a theme can name the curves
// designers already know. The zero Easing is Linear.
type Easing struct {
	bezier bool
	// x1, y1, x2, y2: the control points of a cubic Bézier from (0, 0) to
	// (1, 1).
	points [4]float32
}

// Linear: value follows time.
var Linear = Easing{}

var (
	// EaseOut is a fast start and a gentle stop: things arriving
	// (easeOutCubic).
	EaseOut = CubicBezier(0.33, 1, 0.68, 1)
	// EaseIn is a gentle start and a fast end: things leaving
	// (easeInCubic).
	EaseIn = CubicBezier(0.32, 0, 0.67, 0)
	// EaseInOut is gentle at both ends (CSS ease-in-out).
	EaseInOut = CubicBezier(0.42, 0, 0.58, 1)
)

// CubicBezier is CSS cubic-bezier(x1, y1, x2, y2). x1 and x2 are clamped
// to 0..1 when applied, so the curve stays a function of time.
func CubicBezier(x1, y1, x2, y2 float32) Easing {
	return Easing{bezier: true, points: [4]float32{x1, y1, x2, y2}}
}

// Apply is the value at time t; t is clamped to 0..1 (NaN is 0), and the
// ends are exact.
func (e Easing) Apply(t float32) float32 {
	if t != t { // NaN
		t = 0
	}
	t = min(max(t, 0), 1)
	if t == 0 || t == 1 || !e.bezier {
		return t
	}
	x1, y1, x2, y2 := e.points[0], e.points[1], e.points[2], e.points[3]
	s := solveX(min(max(x1, 0), 1), min(max(x2, 0), 1), t)
	return bezier(y1, y2, s)
}

// bezier is one coordinate of the Bézier with control values a, b at s.
func bezier(a, b, s float32) float32 {
	r := 1 - s
	return 3*r*r*s*a + 3*r*s*s*b + s*s*s
}

// slope is its derivative in s.
func slope(a, b, s float32) float32 {
	r := 1 - s
	return 3*r*r*a + 6*r*s*(b-a) + 3*s*s*(1-b)
}

// solveX is the curve parameter whose x is x: Newton's method from x, and
// bisection where the slope is too flat (x is monotonic in s for control x
// values in 0..1, so the bisection always converges).
func solveX(x1, x2, x float32) float32 {
	s := x
	for range 8 {
		err := bezier(x1, x2, s) - x
		if abs(err) < 1e-6 {
			return s
		}
		d := slope(x1, x2, s)
		if abs(d) < 1e-6 {
			break
		}
		s = min(max(s-err/d, 0), 1)
	}
	low, high := float32(0), float32(1)
	s = x
	for range 40 {
		at := bezier(x1, x2, s)
		if abs(at-x) < 1e-6 {
			break
		}
		if at < x {
			low = s
		} else {
			high = s
		}
		s = (low + high) / 2
	}
	return s
}

func abs(v float32) float32 { return float32(math.Abs(float64(v))) }
