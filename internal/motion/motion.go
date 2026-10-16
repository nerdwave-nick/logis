// Package motion is the cogitator's motion (spec §4.3): the open and close
// transitions and the selection band's movement. Pure values and
// arithmetic; the window paces them by its frame callbacks (and
// wp_presentation timing when the compositor offers it), and draws nothing
// once they have settled.
//
// Durations come from the theme (Timed); LOGIS_MOTION (FromEnv) turns
// motion off ("off", for deterministic tests and the motion-averse) or
// slows it by a factor ("4" runs every transition four times longer), and
// outranks the theme.
package motion

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Nanos is a point in time: CLOCK_MONOTONIC in nanoseconds, the clock
// wp_presentation reports in and the test trace stamps with.
type Nanos = uint64

// Curve is how long a transition takes and how its progress is eased.
type Curve struct {
	// Duration is from start to end; zero means "at once".
	Duration time.Duration
	Easing   Easing
}

// Instant is no transition: always at the end.
var Instant = Curve{}

// Millis is easing over n milliseconds.
func Millis(n int64, easing Easing) Curve {
	return Curve{Duration: time.Duration(n) * time.Millisecond, Easing: easing}
}

// Progress is the eased progress (0..1) elapsed nanoseconds after the
// start.
func (c Curve) Progress(elapsed Nanos) float32 {
	if c.Duration <= 0 {
		return 1
	}
	total := uint64(c.Duration)
	return c.Easing.Apply(float32(float64(min(elapsed, total)) / float64(total)))
}

// Done reports whether the transition is over elapsed nanoseconds after
// its start.
func (c Curve) Done(elapsed Nanos) bool { return c.Duration <= 0 || elapsed >= uint64(c.Duration) }

// Motion is every motion parameter of the cogitator.
type Motion struct {
	// Open is the open transition, from OpenFrom to rest. It starts with
	// the window's first frame, which already shows the panel (the "first
	// frame" of the latency budget is that first commit).
	Open     Curve
	OpenFrom Pose
	// Close is the close transition, from the pose last shown to CloseTo
	// (then the panel is gone); the session's answer is sent before it
	// starts.
	Close   Curve
	CloseTo Pose
	// Selection is how the selection band moves to a newly selected row.
	Selection Curve
}

// Env is the variable FromEnv reads.
const Env = "LOGIS_MOTION"

// Off is no motion at all: panels appear and vanish at once, the band
// jumps.
var Off = Motion{OpenFrom: Rest, CloseTo: Rest}

// Default is quick and calm: the panel rises into place and fades in over
// 150 ms (starting at 40 % so its first frame is already plainly there),
// sinks and fades out over 110 ms, and the band glides in 90 ms.
func Default() Motion {
	return Motion{
		Open:      Millis(150, EaseOut),
		OpenFrom:  Pose{Opacity: 0.4, Offset: [2]float32{0, 10}},
		Close:     Millis(110, EaseIn),
		CloseTo:   Pose{Opacity: 0, Offset: [2]float32{0, 6}},
		Selection: Millis(90, EaseOut),
	}
}

// IsOff reports whether nothing ever moves.
func (m Motion) IsOff() bool {
	return m.Open.Duration <= 0 && m.Close.Duration <= 0 && m.Selection.Duration <= 0
}

// Slowed is m with every duration multiplied by factor (0 turns motion
// off); the poses stay.
func (m Motion) Slowed(factor float64) Motion {
	scale := func(c Curve) Curve {
		c.Duration = time.Duration(float64(c.Duration) * max(factor, 0))
		return c
	}
	m.Open, m.Close, m.Selection = scale(m.Open), scale(m.Close), scale(m.Selection)
	return m
}

// Timed is the default motion with these durations (a theme's timings):
// its poses and easing curves stay.
func Timed(open, close, selection time.Duration) Motion {
	m := Default()
	m.Open.Duration, m.Close.Duration, m.Selection.Duration = open, close, selection
	return m
}

// FromEnvValue is the motion LOGIS_MOTION=value asks for: empty for the
// default, "off" (or 0) for none, a factor up to 100 to slow it down.
func FromEnvValue(value string) (Motion, error) {
	if value == "" {
		return Default(), nil
	}
	if value == "off" {
		return Off, nil
	}
	refused := fmt.Errorf("%s=%s is no rite of motion: use 'off' or a factor such as 4", Env, value)
	// Plain decimal numbers only: no hex, no digit separators (inf and NaN
	// parse, and are refused below).
	if strings.ContainsAny(value, "xX_") {
		return Motion{}, refused
	}
	factor, err := strconv.ParseFloat(value, 64)
	switch {
	case err != nil && factor == 0:
		return Motion{}, refused
	case factor == 0:
		return Off, nil
	case err == nil && factor > 0 && factor <= 100:
		return Default().Slowed(factor), nil
	}
	return Motion{}, refused
}

// FromEnv is the motion the process environment asks for, and whether
// LOGIS_MOTION is set at all (it then outranks a theme's timings).
func FromEnv() (m Motion, set bool, err error) {
	value := os.Getenv(Env)
	if value == "" {
		return Default(), false, nil
	}
	m, err = FromEnvValue(value)
	return m, true, err
}
