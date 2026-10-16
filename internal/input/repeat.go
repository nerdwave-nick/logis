package input

import "time"

// maxBurst bounds the repeats emitted at once; a loop that fell further
// behind (a stalled frame, a suspend) skips ahead instead.
const maxBurst = 4

// Repeat is client-side key repeat (wl_keyboard before version 10): the
// compositor announces a rate and a delay, and the client repeats the last
// pressed key itself. It is only a deadline: the window arms its timerfd
// for Deadline while a key is held and disarms it otherwise, so nothing
// wakes the CPU when no key is held.
type Repeat struct {
	enabled         bool
	delay, interval time.Duration
	held            bool
	code            uint32
	next            time.Time
}

// NewRepeat has the X server's defaults (25 per second after 600 ms), for
// a wl_keyboard older than version 4, which sends no repeat info.
func NewRepeat() *Repeat {
	r := &Repeat{}
	r.SetRate(25, 600)
	return r
}

// SetRate applies the compositor's repeat info: rate per second after
// delay ms; a rate of 0 (or less) disables repeat. A key held at the time
// stops repeating.
func (r *Repeat) SetRate(rate, delay int32) {
	r.held = false
	r.enabled = rate > 0
	if r.enabled {
		r.delay = time.Duration(max(delay, 0)) * time.Millisecond
		r.interval = time.Second / time.Duration(rate)
	}
}

// Enabled says whether repeat is enabled.
func (r *Repeat) Enabled() bool { return r.enabled }

// Press: key code was pressed at now; it repeats from now on, replacing
// any other.
func (r *Repeat) Press(code uint32, now time.Time) {
	if r.enabled {
		r.held, r.code, r.next = true, code, now.Add(r.delay)
	}
}

// Release: key code was released; it stops repeating if it was the one.
func (r *Repeat) Release(code uint32) {
	if r.held && r.code == code {
		r.held = false
	}
}

// Reset stops any repeat (focus left, keymap changed, panel hidden).
func (r *Repeat) Reset() { r.held = false }

// Deadline is when the next repeat is due; false when nothing repeats.
func (r *Repeat) Deadline() (time.Time, bool) {
	if !r.held {
		return time.Time{}, false
	}
	return r.next, true
}

// Due is the held key and how many repeats are due at now (at most
// maxBurst); n is 0 when none is.
func (r *Repeat) Due(now time.Time) (code uint32, n int) {
	if !r.enabled || !r.held {
		return 0, 0
	}
	for !r.next.After(now) && n < maxBurst {
		r.next = r.next.Add(r.interval)
		n++
	}
	if !r.next.After(now) {
		r.next = now.Add(r.interval)
	}
	return r.code, n
}
