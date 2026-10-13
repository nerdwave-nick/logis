package window

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// Repeater is a Keyboard that repeats keys itself (client-side repeat,
// wl_keyboard before version 10): the window's repeat timer is armed for
// Deadline while it reports one and disarmed otherwise, and calls Repeat
// when it fires. Its goroutine is the window's.
type Repeater interface {
	Deadline() (time.Time, bool)
	Repeat(now time.Time)
}

// Resetter is a Keyboard that starts over when a panel is shown or hidden
// (no repeat or Compose sequence carries over).
type Resetter interface{ Reset() }

// repeatTimer is the window's key-repeat timer: a timerfd (CLOCK_MONOTONIC,
// as time.Now's monotonic reading) in the display's poll, armed only while
// a key repeats, so an idle window never wakes. golang.org/x/sys/unix
// provides timerfd; the standard library's syscall package does not.
type repeatTimer struct {
	fd    int
	armed bool
	at    time.Time
}

func newRepeatTimer() (repeatTimer, error) {
	fd, err := unix.TimerfdCreate(unix.CLOCK_MONOTONIC, unix.TFD_NONBLOCK|unix.TFD_CLOEXEC)
	if err != nil {
		return repeatTimer{fd: -1}, fmt.Errorf("the cogitator's chronometer fails: timerfd_create: %w", err)
	}
	return repeatTimer{fd: fd}, nil
}

// set arms the timer for at, or disarms it (on false); unchanged, it
// leaves the timer alone.
func (r *repeatTimer) set(at time.Time, on bool) {
	if on == r.armed && (!on || at.Equal(r.at)) {
		return
	}
	var spec unix.ItimerSpec
	if on {
		// Relative to now: a due deadline still fires (1 ns, not 0, which
		// would disarm).
		spec.Value = unix.NsecToTimespec(max(time.Until(at), time.Nanosecond).Nanoseconds())
	}
	if err := unix.TimerfdSettime(r.fd, 0, &spec, nil); err != nil {
		on = false
	}
	r.armed, r.at = on, at
}

// fired consumes the expiry; the timer is one-shot, so it is disarmed now.
func (r *repeatTimer) fired() {
	var buf [8]byte
	_, _ = unix.Read(r.fd, buf[:])
	r.armed = false
}

func (r *repeatTimer) close() {
	if r.fd >= 0 {
		_ = unix.Close(r.fd)
		r.fd = -1
	}
}

// armRepeat arms the repeat timer for the keyboard's deadline, or
// disarms it.
func (w *Window) armRepeat() {
	if r, ok := w.seat.consumer.(Repeater); ok {
		w.repeat.set(r.Deadline())
		return
	}
	w.repeat.set(time.Time{}, false)
}

// repeatDue runs when the repeat timer fires.
func (w *Window) repeatDue() {
	w.repeat.fired()
	if r, ok := w.seat.consumer.(Repeater); ok {
		r.Repeat(time.Now())
	}
}

// resetKeyboard starts the keyboard over (a panel was shown or hidden).
func (w *Window) resetKeyboard() {
	if r, ok := w.seat.consumer.(Resetter); ok {
		r.Reset()
	}
	w.armRepeat()
}
