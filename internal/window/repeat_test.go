package window

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// repeatingKeys is a keyboard that repeats keys itself: it holds a key
// until its deadline, then repeats it once and lets go.
type repeatingKeys struct {
	keys
	deadline time.Time
	held     bool
	fired    []time.Time
	resets   int
}

func (k *repeatingKeys) Deadline() (time.Time, bool) { return k.deadline, k.held }
func (k *repeatingKeys) Repeat(now time.Time) {
	k.fired = append(k.fired, now)
	k.held = false
}
func (k *repeatingKeys) Reset() { k.resets++ }

// armed is the window's repeat timer's time left, 0 when disarmed.
func armed(t *testing.T, w *Window) time.Duration {
	t.Helper()
	var spec unix.ItimerSpec
	if err := unix.TimerfdGettime(w.repeat.fd, &spec); err != nil {
		t.Fatal(err)
	}
	return time.Duration(spec.Value.Nano())
}

func TestAHeldKeyIsRepeatedWhenItsDeadlineComes(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	k := &repeatingKeys{}
	w.SetKeyboard(k)
	settle(t, w)
	k.deadline, k.held = time.Now().Add(40*time.Millisecond), true
	if err := w.Dispatch(0); err != nil { // as after the key event
		t.Fatal(err)
	}
	start := time.Now()
	if err := w.Dispatch(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	waited := time.Since(start)
	if len(k.fired) != 1 || k.fired[0].Before(k.deadline) || waited > 4*time.Second {
		t.Fatalf("repeats at %v for the deadline %v; Dispatch returned after %v", k.fired, k.deadline, waited)
	}
	if left := armed(t, w); left != 0 {
		t.Errorf("the timer is still armed (%v) with no key held", left)
	}
}

// Idle means no wake-ups: the timer is disarmed whenever no key repeats,
// also when the key is released before its deadline.
func TestTheRepeatTimerIsDisarmedWhileNoKeyRepeats(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	k := &repeatingKeys{}
	w.SetKeyboard(k)
	settle(t, w)
	if left := armed(t, w); left != 0 {
		t.Fatalf("armed (%v) with no key held", left)
	}
	k.deadline, k.held = time.Now().Add(50*time.Millisecond), true
	if err := w.Dispatch(0); err != nil {
		t.Fatal(err)
	}
	if left := armed(t, w); left <= 0 || left > 50*time.Millisecond {
		t.Fatalf("armed for %v, want up to 50 ms", left)
	}
	k.held = false // released
	if err := w.Dispatch(0); err != nil {
		t.Fatal(err)
	}
	if left := armed(t, w); left != 0 {
		t.Errorf("still armed (%v) after the release", left)
	}
	start := time.Now()
	if err := w.Dispatch(100 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); len(k.fired) != 0 || waited < 100*time.Millisecond {
		t.Errorf("repeated %v; Dispatch returned after %v", k.fired, waited)
	}
}

// A shown or hidden panel starts the keyboard over: no repeat or Compose
// sequence carries over (Rust's seat reset).
func TestShowAndHideResetTheKeyboard(t *testing.T) {
	f := newFakeShell(t, niri)
	w, _ := openFake(t, f)
	k := &repeatingKeys{}
	w.SetKeyboard(k)
	showConfigured(t, f, w, &painter{})
	if k.resets != 1 {
		t.Errorf("%d resets after Show", k.resets)
	}
	w.Hide()
	if k.resets != 2 {
		t.Errorf("%d resets after Hide", k.resets)
	}
}
