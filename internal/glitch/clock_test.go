package glitch

import (
	"math"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
)

// The glitch's clock with a fake clock (ns) and fixed random numbers: the
// ambient clock from the panel's opening, bursts from events and at
// random, the envelope sin(πt)^0.6 with its stutter (the effects lab's
// scheduling), and when the panel must keep drawing.

const ms = uint64(1_000_000)

// fixed returns the numbers given in turn, then the last again.
func fixed(n ...float64) func() float64 {
	return func() float64 {
		v := n[0]
		if len(n) > 1 {
			n = n[1:]
		}
		return v
	}
}

// still is a glitch that does not move at rest, without ambient episodes
// or random bursts: only events make it move.
func still() effects.Glitch {
	g := effects.GlitchDefaults()
	g.Strength = 0.5
	g.Wobble.Rest, g.Tracking.Px, g.Static.Rest, g.LinePulse.Rest = 0, 0, 0, 0
	g.Episodes.PeriodS = 0
	g.Bursts.Stutter = 0
	return g
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }

func TestTheOpenBurstRisesAndFallsOverItsLength(t *testing.T) {
	g := still()
	c := New(fixed(0.9))
	open := 1000 * ms
	c.Open(open, g)
	for _, p := range []struct {
		at   uint64
		want float32
	}{{0, 0}, {105, float32(math.Pow(math.Sin(math.Pi*0.25), 0.6))}, {210, 1}, {315, float32(math.Pow(math.Sin(math.Pi*0.75), 0.6))}, {420, 0}, {800, 0}} {
		m := c.At(open+p.at*ms, g)
		if !near(m.Event, p.want) {
			t.Errorf("%d ms: %v, want %v", p.at, m.Event, p.want)
		}
		if want := float32(p.at) / 1000; !near(m.Time, want) {
			t.Errorf("%d ms: time %v, want %v", p.at, m.Time, want)
		}
		if moving := c.Moving(open+p.at*ms, g); moving != (p.at < 420) {
			t.Errorf("%d ms: moving %v", p.at, moving)
		}
	}
}

// Without "open" among the events, opening fires nothing; a tab burst
// fires only with "tab" among them, and a burst replaces the one running.
func TestOnlyTheChosenEventsFireBursts(t *testing.T) {
	g := still()
	g.Bursts.On = effects.EventTab
	c := New(fixed(0.9))
	c.Open(0, g)
	if m := c.At(100*ms, g); m.Event != 0 || c.Moving(100*ms, g) {
		t.Fatalf("an open burst without open: %+v", m)
	}
	c.Fire(effects.EventTab, 200*ms, g)
	if m := c.At(320*ms, g); !near(m.Event, 1) {
		t.Errorf("mid tab burst: %v", m.Event)
	}
	c.Fire(effects.EventOpen, 330*ms, g)
	if m := c.At(340*ms, g); m.Event == 0 {
		t.Error("an open event (not chosen) ended the tab burst")
	}
	g.Bursts.On = effects.EventOpen | effects.EventTab
	c.Fire(effects.EventTab, 400*ms, g)
	if m := c.At(400*ms, g); m.Event != 0 {
		t.Errorf("a new burst starts at 0: %v", m.Event)
	}
	if m := c.At(520*ms, g); !near(m.Event, 1) {
		t.Errorf("the new burst's peak: %v", m.Event)
	}
}

// Stutter damps a share of the burst's 33 ms steps to a quarter, each
// step drawn once.
func TestStutterDampsStepsToAQuarter(t *testing.T) {
	g := still()
	g.Bursts.Stutter = 0.5
	// Step 0 draws 0.9 (kept), step 1 0.1 (damped), then 0.9 on.
	c := New(fixed(0.9, 0.1, 0.9))
	c.Open(0, g)
	full := func(at uint64) float32 {
		return float32(math.Pow(math.Sin(math.Pi*float64(at)/420), 0.6))
	}
	if m := c.At(20*ms, g); !near(m.Event, full(20)) {
		t.Errorf("step 0: %v", m.Event)
	}
	if m := c.At(40*ms, g); !near(m.Event, full(40)/4) {
		t.Errorf("step 1: %v, want %v", m.Event, full(40)/4)
	}
	if m := c.At(50*ms, g); !near(m.Event, full(50)/4) {
		t.Errorf("step 1 again: %v (drawn anew?)", m.Event)
	}
	if m := c.At(70*ms, g); !near(m.Event, full(70)) {
		t.Errorf("step 2: %v", m.Event)
	}
}

// Random bursts come as a Poisson process: the wait is -ln(1-u)·60/rate
// seconds from the opening and from each random burst on, each burst at
// its time.
func TestRandomBurstsComeAtExponentialIntervals(t *testing.T) {
	g := still()
	g.Bursts.On, g.Bursts.RandomPerMinute, g.Bursts.RandomMS = 0, 6, 300
	u := 1 - math.Exp(-1) // a wait of exactly 60/6 = 10 s
	c := New(fixed(u))
	c.Open(0, g)
	if next, ok := c.Next(0, g); !ok || next != 10_000*ms {
		t.Fatalf("next %v %v", next, ok)
	}
	if c.Moving(9_999*ms, g) || c.At(9_999*ms, g).Event != 0 {
		t.Fatal("a random burst came early")
	}
	if m := c.At(10_150*ms, g); !near(m.Event, 1) {
		t.Errorf("mid random burst: %v", m.Event)
	}
	if !c.Moving(10_200*ms, g) {
		t.Error("not moving in a random burst")
	}
	if next, ok := c.Next(10_200*ms, g); ok {
		t.Errorf("a wake while moving: %v", next)
	}
	if next, ok := c.Next(10_500*ms, g); !ok || next != 20_000*ms {
		t.Errorf("the next one: %v %v", next, ok)
	}
	g.Bursts.RandomPerMinute = 0
	if next, ok := c.Next(10_500*ms, g); ok {
		t.Errorf("random bursts turned off still wake: %v", next)
	}
}

// Ambient episodes need no drawing between them: the panel draws in each
// episode and wakes for the next one.
func TestAmbientEpisodesWakeThePanelAndEnd(t *testing.T) {
	g := still()
	g.Bursts.On = 0
	g.Episodes = effects.Episodes{PeriodS: 9, Duty: 0.1}
	c := New(fixed(0.5))
	open := 5_000 * ms
	c.Open(open, g)
	if c.Moving(open+1_000*ms, g) {
		t.Error("moving between episodes")
	}
	if next, ok := c.Next(open+1_000*ms, g); !ok || next != open+8_100*ms {
		t.Errorf("next %v %v", next, ok)
	}
	if !c.Moving(open+8_100*ms, g) || !c.Moving(open+8_999*ms, g) {
		t.Error("not moving in the episode")
	}
	if c.Moving(open+9_000*ms, g) {
		t.Error("moving after the episode")
	}
	if next, ok := c.Next(open+9_000*ms, g); !ok || next != open+17_100*ms {
		t.Errorf("next %v %v", next, ok)
	}
}

// A glitch that moves at rest keeps the panel drawing; one turned off
// (strength 0, reduce-motion) never does, and fires nothing.
func TestRestMotionKeepsDrawingAndOffNeverDoes(t *testing.T) {
	g := effects.GlitchDefaults()
	c := New(fixed(0.5))
	c.Open(0, g)
	if !c.Moving(5_000*ms, g) {
		t.Error("the defaults move at rest")
	}
	off := g.Stilled()
	c = New(fixed(0.5))
	c.Open(0, off)
	if c.Moving(10*ms, off) {
		t.Error("an off glitch keeps drawing")
	}
	if _, ok := c.Next(10*ms, off); ok {
		t.Error("an off glitch wakes the panel")
	}
}

// The pass's time wraps once a whole number of periods (about an hour)
// has passed, so its float keeps its precision on a panel left open.
func TestTheTimeWrapsAtAPeriodBoundary(t *testing.T) {
	g := still()
	g.Episodes = effects.Episodes{PeriodS: 7, Duty: 0.1}
	c := New(fixed(0.5))
	c.Open(0, g)
	wrap := uint64(7*514) * 1000 * ms // 3598 s
	if m := c.At(wrap+1_500*ms, g); !near(m.Time, 1.5) {
		t.Errorf("after the wrap: %v", m.Time)
	}
	if m := c.At(wrap-1_000*ms, g); !near(m.Time, 3597) {
		t.Errorf("before the wrap: %v", m.Time)
	}
	g.Episodes.PeriodS = 0
	if m := c.At(3_600_000*ms+250*ms, g); !near(m.Time, 0.25) {
		t.Errorf("without a period, the hour: %v", m.Time)
	}
}

// Before the panel opened there is no time and nothing moves.
func TestAClockNotYetOpenedStandsStill(t *testing.T) {
	g := effects.GlitchDefaults()
	c := New(fixed(0.5))
	if c.Opened() || c.Moving(1_000*ms, g) {
		t.Error("moving before the opening")
	}
	if m := c.At(1_000*ms, g); m != (effects.Moment{}) {
		t.Errorf("%+v", m)
	}
}
