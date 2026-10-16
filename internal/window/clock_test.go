package window

import "testing"

// Tests of the frame clock: when a frame being drawn will be seen.

const msNS uint64 = 1_000_000

func TestWithoutFeedbackAFrameIsSeenNowAt60Hz(t *testing.T) {
	var c frameClock
	if got := c.frame(5 * msNS); got != (FrameTime{At: 5 * msNS, Interval: fallbackInterval}) {
		t.Errorf("%+v", got)
	}
}

func TestWithFeedbackAFrameIsSeenAtTheNextRefresh(t *testing.T) {
	var c frameClock
	c.presented(100*msNS, 8*msNS)
	for now, want := range map[uint64]uint64{101 * msNS: 108 * msNS, 108 * msNS: 108 * msNS, 113 * msNS: 116 * msNS} {
		if got := c.frame(now); got.At != want || got.Interval != 8*msNS {
			t.Errorf("at %d: %+v, want %d", now, got, want)
		}
	}
}

func TestAfterAPauseTheOldRefreshGridIsNotTrusted(t *testing.T) {
	// Variable refresh rate: after idle, the next frame is shown as soon as
	// it is ready, not on the grid of the last report.
	var c frameClock
	c.presented(100*msNS, 8*msNS)
	if got := c.frame(400*msNS + 3); got != (FrameTime{At: 400*msNS + 3, Interval: 8 * msNS}) {
		t.Errorf("%+v", got)
	}
	if got := c.frame(115 * msNS); got.At != 116*msNS {
		t.Errorf("recent: %+v, want on the grid", got)
	}
}

func TestFramesAreAtLeastOneIntervalApart(t *testing.T) {
	// The second frame is drawn as soon as the first one's callback comes,
	// but reaches the screen no sooner than a refresh later.
	var c frameClock
	c.presented(100*msNS, 8*msNS)
	first := c.frame(500 * msNS)
	c.drew(first.At)
	if got := c.frame(502 * msNS); got.At != 508*msNS {
		t.Errorf("%+v", got)
	}
	if got := c.frame(511 * msNS); got.At != 511*msNS {
		t.Errorf("later than that: %+v, want now", got)
	}
	var fresh frameClock
	fresh.drew(10 * msNS)
	if got := fresh.frame(11 * msNS); got.At != 10*msNS+fallbackInterval {
		t.Errorf("%+v", got)
	}
}

func TestAReportFromTheFutureOfNowIsItsOwnMoment(t *testing.T) {
	var c frameClock
	c.presented(100*msNS, 8*msNS)
	if got := c.frame(99 * msNS); got.At != 100*msNS {
		t.Errorf("%+v", got)
	}
}

func TestAnUnknownRefreshForgetsTheTiming(t *testing.T) {
	var c frameClock
	c.presented(100*msNS, 8*msNS)
	c.presented(120*msNS, 0)
	if got := c.frame(130 * msNS); got.At != 130*msNS {
		t.Errorf("%+v", got)
	}
}

func TestFramesFasterThanTheAssumedIntervalDoNotRunAhead(t *testing.T) {
	// Without the output's refresh (nested niri reports 0), 60 Hz is a
	// guess: when the compositor asks for frames faster than that, each
	// frame is still seen within an interval of being drawn, not an
	// interval after the last guess, which would run ever further ahead.
	var c frameClock
	c.presented(90*msNS, 0)
	for i := range uint64(30) {
		now := 100*msNS + i*10*msNS
		f := c.frame(now)
		if f.At > now+fallbackInterval {
			t.Fatalf("frame %d drawn at %d ms is seen %.1f ms later", i, now/msNS, float64(f.At-now)/float64(msNS))
		}
		c.drew(f.At)
	}
}

func TestABurstOfCallbacksGetsNoFrameMoreThanARefreshAheadOfTheGrid(t *testing.T) {
	// Callbacks that come faster than the refresh (a burst after a stall)
	// are drawn for the refresh after the next one at the latest: frames
	// queue behind one another, not behind guesses.
	var c frameClock
	c.presented(100*msNS, 8*msNS)
	var last uint64
	for i := range uint64(5) {
		now := 101*msNS + i*msNS
		f := c.frame(now)
		if f.At > 116*msNS || f.At < last {
			t.Fatalf("callback %d at %d ms: frame at %d ns (the one before at %d)", i, now/msNS, f.At, last)
		}
		last = f.At
		c.drew(f.At)
	}
}
