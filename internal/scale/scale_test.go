package scale

import "testing"

// Scales in 120ths and the logical → buffer size rule.

func TestIntegerScalesMultiplyExactly(t *testing.T) {
	if got := One.Physical(Size{640, 360}); got != (Size{640, 360}) {
		t.Errorf("1.0: %v", got)
	}
	if got := FromInteger(2).Physical(Size{640, 360}); got != (Size{1280, 720}) {
		t.Errorf("2.0: %v", got)
	}
}

func TestFractionalScalesRoundHalfAwayFromZero(t *testing.T) {
	s := From120ths(156) // 1.3
	if got := s.Physical(Size{640, 360}); got != (Size{832, 468}) {
		t.Errorf("1.3: %v", got)
	}
	// 1.25 × 3 = 3.75 → 4; 1.25 × 2 = 2.5 → 3.
	if got := From120ths(150).Physical(Size{3, 2}); got != (Size{4, 3}) {
		t.Errorf("1.25: %v", got)
	}
}

func TestAZeroScaleIsTreatedAsOne(t *testing.T) {
	if From120ths(0) != One || FromInteger(0) != One {
		t.Errorf("From120ths(0) = %v, FromInteger(0) = %v", From120ths(0), FromInteger(0))
	}
	var zero Scale
	if got := zero.Physical(Size{640, 360}); got != (Size{640, 360}) {
		t.Errorf("the zero Scale scales %v", got)
	}
}

func TestAPhysicalSideIsNeverZero(t *testing.T) {
	if got := One.Physical(Size{0, 0}); got != (Size{1, 1}) {
		t.Errorf("%v", got)
	}
}

func TestTheFactorMatchesTheNumerator(t *testing.T) {
	s := From120ths(156)
	if s.Factor() != 1.3 || s.As120ths() != 156 {
		t.Errorf("factor %v, numerator %d", s.Factor(), s.As120ths())
	}
}

func TestAHugeScaleSaturates(t *testing.T) {
	if got := From120ths(1 << 31).Physical(Size{1 << 31, 1}); got.W != 1<<32-1 {
		t.Errorf("%v", got)
	}
}
