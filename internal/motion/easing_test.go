package motion

import (
	"math"
	"testing"
)

// Linear and cubic-bezier easings, against CSS's named curves.

var cssEase = CubicBezier(0.25, 0.1, 0.25, 1.0)

var allEasings = []Easing{Linear, EaseOut, EaseIn, EaseInOut, cssEase}

func near(a, b, tolerance float32) bool { return math.Abs(float64(a-b)) < float64(tolerance) }

func TestEveryCurveStartsAt0AndEndsAt1(t *testing.T) {
	for _, e := range allEasings {
		if e.Apply(0) != 0 || e.Apply(1) != 1 {
			t.Errorf("%+v: %v, %v", e, e.Apply(0), e.Apply(1))
		}
	}
}

func TestTimeOutsideTheTransitionIsClamped(t *testing.T) {
	for _, e := range allEasings {
		if e.Apply(-0.5) != 0 || e.Apply(7) != 1 || e.Apply(float32(math.NaN())) != 0 {
			t.Errorf("%+v: %v, %v, %v", e, e.Apply(-0.5), e.Apply(7), e.Apply(float32(math.NaN())))
		}
	}
}

func TestLinearIsTheIdentity(t *testing.T) {
	for _, v := range []float32{0.1, 0.25, 0.5, 0.9} {
		if Linear.Apply(v) != v {
			t.Errorf("%v → %v", v, Linear.Apply(v))
		}
	}
}

func TestCubicBezierMatchesCSSEase(t *testing.T) {
	// Browsers' cubic-bezier(0.25, 0.1, 0.25, 1) at 0.5: 0.8024033877.
	if got := cssEase.Apply(0.5); !near(got, 0.8024034, 1e-4) {
		t.Errorf("ease at 0.5: %v", got)
	}
	// easeOutCubic is 1 - (1 - t)^3; its Bézier fit is close to it.
	for _, v := range []float32{0.1, 0.3, 0.5, 0.8} {
		exact := 1 - float32(math.Pow(float64(1-v), 3))
		if got := EaseOut.Apply(v); !near(got, exact, 0.02) {
			t.Errorf("ease-out at %v: %v, want ≈%v", v, got, exact)
		}
	}
}

func TestEaseOutRunsAheadAndEaseInLagsBehind(t *testing.T) {
	for _, v := range []float32{0.1, 0.5, 0.9} {
		if EaseOut.Apply(v) <= v || EaseIn.Apply(v) >= v {
			t.Errorf("%v: out %v, in %v", v, EaseOut.Apply(v), EaseIn.Apply(v))
		}
	}
}

func TestEaseInOutIsSymmetric(t *testing.T) {
	for _, v := range []float32{0.1, 0.3, 0.45} {
		if sum := EaseInOut.Apply(v) + EaseInOut.Apply(1-v); !near(sum, 1, 1e-4) {
			t.Errorf("%v: %v", v, sum)
		}
	}
}

func TestStandardCurvesNeverGoBackwards(t *testing.T) {
	for _, e := range allEasings {
		last := float32(0)
		for i := 1; i <= 200; i++ {
			v := e.Apply(float32(i) / 200)
			if v < last-1e-5 {
				t.Fatalf("%+v at %d: %v < %v", e, i, v, last)
			}
			last = v
		}
	}
}

func TestOutOfRangeControlXValuesAreClamped(t *testing.T) {
	wild := CubicBezier(-1, 0, 2, 1)
	tame := CubicBezier(0, 0, 1, 1)
	if wild.Apply(0.3) != tame.Apply(0.3) {
		t.Errorf("%v vs %v", wild.Apply(0.3), tame.Apply(0.3))
	}
}
