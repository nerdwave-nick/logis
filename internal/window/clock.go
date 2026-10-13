package window

// When the frame being drawn will be seen (Rust src/window/clock.rs): the
// time motion evaluates its transitions at. With wp_presentation (spec
// §4.2: used when offered) the compositor reports when each frame reached
// the screen and the output's refresh interval, so while frames flow the
// next frame's moment is the next refresh after now. Without it, or after
// a pause, "now" stands in (with a 60 Hz interval if none is known). Only
// CLOCK_MONOTONIC reports are used: the clock the trace and motion count
// in, and what wp_presentation's clock_id normally names.

// FrameTime is a frame's moment and the interval between frames, in
// CLOCK_MONOTONIC nanoseconds.
type FrameTime struct {
	// At is when the frame is expected on screen.
	At uint64
	// Interval is the time between frames (the output's refresh interval).
	Interval uint64
}

// fallbackInterval is the interval assumed without presentation feedback:
// 60 Hz.
const fallbackInterval = 16_666_667

// frameClock is the output timing learned from presentation feedback.
type frameClock struct {
	// lastAt and refresh are the last report (refresh 0: none).
	lastAt, refresh uint64
	// drawn is when the last frame drawn is expected on screen (0: none).
	drawn uint64
}

// presented: a frame reached the screen at at; the output refreshes every
// refresh nanoseconds (0: unknown, e.g. a variable refresh rate).
func (c *frameClock) presented(at, refresh uint64) { c.lastAt, c.refresh = at, refresh }

// drew: a frame expected on screen at at was drawn; the next one is shown
// no sooner than an interval after it.
func (c *frameClock) drew(at uint64) { c.drawn = at }

// frame is the moment of a frame drawn at now: the first refresh at or
// after it while frames are flowing, or now itself without timing or after
// a pause (with a variable refresh rate, the first frame after idle is
// shown when it is ready, off the old grid); in any case no sooner than an
// interval after the frame drawn before it.
func (c *frameClock) frame(now uint64) FrameTime {
	t := c.timed(now)
	if c.drawn != 0 {
		t.At = max(t.At, c.drawn+t.Interval)
	}
	return t
}

// timed is frame from the presentation reports alone.
func (c *frameClock) timed(now uint64) FrameTime {
	at, refresh := c.lastAt, c.refresh
	switch {
	case refresh == 0:
		return FrameTime{At: now, Interval: fallbackInterval}
	case now > at+2*refresh:
		return FrameTime{At: now, Interval: refresh}
	case now > at:
		steps := (now - at + refresh - 1) / refresh
		return FrameTime{At: at + steps*refresh, Interval: refresh}
	}
	return FrameTime{At: at, Interval: refresh}
}
