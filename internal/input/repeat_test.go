package input

import (
	"testing"
	"time"
)

// Tests of client-side key repeat.

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

// repeat is 25 per second (40 ms) after 300 ms.
func repeat() *Repeat {
	r := NewRepeat()
	r.SetRate(25, 300)
	return r
}

func wantDeadline(t *testing.T, r *Repeat, want time.Time) {
	t.Helper()
	got, ok := r.Deadline()
	if want.IsZero() {
		if ok {
			t.Errorf("a deadline %v, want none", got)
		}
		return
	}
	if !ok || !got.Equal(want) {
		t.Errorf("deadline %v %v, want %v", got, ok, want)
	}
}

func wantDue(t *testing.T, r *Repeat, at time.Time, code uint32, n int) {
	t.Helper()
	gotCode, gotN := r.Due(at)
	if gotN != n || n > 0 && gotCode != code {
		t.Errorf("due: key %d ×%d, want key %d ×%d", gotCode, gotN, code, n)
	}
}

func TestNothingIsDueAndNothingWaitsWhileNoKeyIsHeld(t *testing.T) {
	r, t0 := repeat(), time.Now()
	wantDeadline(t, r, time.Time{})
	wantDue(t, r, t0.Add(10*time.Second), 0, 0)
}

func TestAHeldKeyRepeatsAfterTheDelayAtTheRate(t *testing.T) {
	r, t0 := repeat(), time.Now()
	r.Press(30, t0)
	wantDeadline(t, r, t0.Add(ms(300)))
	wantDue(t, r, t0.Add(ms(299)), 0, 0)
	wantDue(t, r, t0.Add(ms(300)), 30, 1)
	wantDeadline(t, r, t0.Add(ms(340)))
	wantDue(t, r, t0.Add(ms(339)), 0, 0)
	wantDue(t, r, t0.Add(ms(420)), 30, 3)
	wantDeadline(t, r, t0.Add(ms(460)))
}

func TestALateLoopGetsABoundedBurstAndSkipsAhead(t *testing.T) {
	r, t0 := repeat(), time.Now()
	r.Press(30, t0)
	late := t0.Add(time.Minute)
	wantDue(t, r, late, 30, maxBurst)
	wantDeadline(t, r, late.Add(ms(40)))
}

func TestRateZeroDisablesRepeat(t *testing.T) {
	r, t0 := repeat(), time.Now()
	r.SetRate(0, 300)
	if r.Enabled() {
		t.Error("enabled at rate 0")
	}
	r.Press(30, t0)
	wantDeadline(t, r, time.Time{})
	wantDue(t, r, t0.Add(10*time.Second), 0, 0)
}

func TestReleasingTheHeldKeyStopsItAndAnotherKeyDoesNot(t *testing.T) {
	r, t0 := repeat(), time.Now()
	r.Press(30, t0)
	r.Release(31)
	if _, ok := r.Deadline(); !ok {
		t.Error("another key's release stopped the repeat")
	}
	r.Release(30)
	wantDeadline(t, r, time.Time{})
}

func TestANewPressReplacesTheHeldKey(t *testing.T) {
	r, t0 := repeat(), time.Now()
	r.Press(30, t0)
	r.Press(31, t0.Add(ms(100)))
	wantDue(t, r, t0.Add(ms(400)), 31, 1)
}

func TestResetAndNewRepeatInfoStopTheHeldKey(t *testing.T) {
	r, t0 := repeat(), time.Now()
	r.Press(30, t0)
	r.Reset()
	wantDeadline(t, r, time.Time{})
	r.Press(30, t0)
	r.SetRate(30, 200)
	wantDeadline(t, r, time.Time{})
}

func TestWithoutRepeatInfoTheXDefaultsApply(t *testing.T) {
	r, t0 := NewRepeat(), time.Now()
	r.Press(30, t0)
	wantDeadline(t, r, t0.Add(ms(600)))
	wantDue(t, r, t0.Add(ms(640)), 30, 2)
}
