package effects

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

// The glitch table is the clean room's glitch-controls.json (testdata, as
// delivered): the same uniforms, ranges and defaults in its order. A
// theme writes each under its part (the uniform's prefix, as the owner's
// looks do) and the rest of the uniform with dashes.
func TestTheGlitchControlsAreTheDeliveredOnes(t *testing.T) {
	b, err := os.ReadFile("testdata/glitch-controls.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []struct {
		Uniform           string
		Min, Max, Default float64
	}
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if len(GlitchControls) != len(want) || len(want) != 43 {
		t.Fatalf("%d controls, delivered %d", len(GlitchControls), len(want))
	}
	parts := map[string]string{
		"u_episode_": "episodes", "u_wobble_": "wobble", "u_tracking_": "tracking", "u_fringe_": "fringe",
		"u_slice_": "slices", "u_block_": "blocks", "u_flash_": "flashes", "u_band_": "noise-band",
		"u_static_": "static", "u_pulse_": "line-pulse",
	}
	defaults := GlitchDefaults()
	for i, w := range want {
		k := GlitchControls[i]
		part, key := "", "strength"
		for prefix, p := range parts {
			if rest, ok := strings.CutPrefix(w.Uniform, prefix); ok {
				part, key = p, strings.ReplaceAll(rest, "_", "-")
			}
		}
		if k.Uniform != w.Uniform || k.Part != part || k.Key != key {
			t.Errorf("%d: %s.%s (%s), delivered %s.%s (%s)", i, k.Part, k.Key, k.Uniform, part, key, w.Uniform)
		}
		if k.Min != float32(w.Min) || k.Max != float32(w.Max) || k.Value(defaults) != float32(w.Default) {
			t.Errorf("%s: %v to %v default %v, delivered %v to %v default %v",
				k.Uniform, k.Min, k.Max, k.Value(defaults), w.Min, w.Max, w.Default)
		}
	}
}

// Every control reads and writes its own field.
func TestEachGlitchControlIsItsOwnField(t *testing.T) {
	var g Glitch
	for i, k := range GlitchControls {
		k.Set(&g, float32(i+1))
	}
	for i, k := range BurstControls {
		k.Set(&g.Bursts, float32(100+i))
	}
	for i, k := range GlitchControls {
		if got := k.Value(g); got != float32(i+1) {
			t.Errorf("%s: %v, want %v", k.Uniform, got, i+1)
		}
	}
	for i, k := range BurstControls {
		if got := k.Value(g.Bursts); got != float32(100+i) {
			t.Errorf("bursts.%s: %v", k.Key, got)
		}
	}
}

// The bursts default to the effects lab's: open 420 ms, tab 240 ms, no
// random bursts (each 300 ms), stutter 0.28, on open and tab.
func TestTheBurstsDefaultToTheLabs(t *testing.T) {
	want := Bursts{OpenMS: 420, TabMS: 240, RandomPerMinute: 0, RandomMS: 300, Stutter: 0.28, On: EventOpen | EventTab}
	if got := GlitchDefaults().Bursts; got != want {
		t.Errorf("%+v", got)
	}
	b := want
	b.On = EventTab
	if b.Length(EventOpen) != 0 || b.Length(EventTab) != 240 {
		t.Errorf("open %v tab %v", b.Length(EventOpen), b.Length(EventTab))
	}
}

// The ambient bell lies over the last duty share of each period: calm at
// the period's start, its peak (1) in the episode's middle, 0 at both
// ends, and none without a period or a duty.
func TestTheAmbientCycleIsABellAtEachPeriodsEnd(t *testing.T) {
	e := Episodes{PeriodS: 9, Duty: 0.1}
	near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }
	for at, want := range map[float32]float32{0: 0, 1: 0, 8.1: 0, 8.55: 1, 9: 0, 17.55: 1, 8.325: 0.5625} {
		if got := e.Ambient(at); !near(got, want) {
			t.Errorf("t=%v: %v, want %v", at, got, want)
		}
	}
	for _, off := range []Episodes{{PeriodS: 0, Duty: 0.1}, {PeriodS: 9, Duty: 0}} {
		if got := off.Ambient(8.55); got != 0 {
			t.Errorf("%+v: %v", off, got)
		}
	}
}

// The intensity the pass draws: the larger of the ambient cycle and the
// burst, times the master strength.
func TestTheIntensityIsTheLargerOfAmbientAndBurstTimesStrength(t *testing.T) {
	g := GlitchDefaults()
	g.Strength = 0.5
	for _, c := range []struct {
		m    Moment
		want float32
	}{{Moment{1, 0}, 0}, {Moment{1, 0.6}, 0.3}, {Moment{8.55, 0.2}, 0.5}, {Moment{1, 3}, 0.5}} {
		if got := g.Intensity(c.m); math.Abs(float64(got-c.want)) > 1e-6 {
			t.Errorf("%+v: %v, want %v", c.m, got, c.want)
		}
	}
}

// What moves at rest: a part with a rest amount drawn anew over time, or
// the tracking sweeps; nothing with the strength at 0, and stilling turns
// the glitch off.
func TestWhatOfTheGlitchMovesAtRest(t *testing.T) {
	quiet := func() Glitch {
		g := GlitchDefaults()
		g.Wobble.Rest, g.Tracking.Px, g.Static.Rest, g.LinePulse.Rest = 0, 0, 0, 0
		return g
	}
	cases := map[string]struct {
		set   func(*Glitch)
		moves bool
	}{
		"quiet":            {func(*Glitch) {}, false},
		"defaults":         {func(g *Glitch) { *g = GlitchDefaults() }, true},
		"wobble":           {func(g *Glitch) { g.Wobble.Rest = 0.2 }, true},
		"frozen wobble":    {func(g *Glitch) { g.Wobble.Rest, g.Wobble.SpeedHz = 0.2, 0 }, false},
		"tracking":         {func(g *Glitch) { g.Tracking.Px = 3 }, true},
		"tracking stopped": {func(g *Glitch) { g.Tracking.Px, g.Tracking.AHz, g.Tracking.BHz = 3, 0, 0 }, false},
		"slices":           {func(g *Glitch) { g.Slices.Rest = 0.1 }, true},
		"blocks":           {func(g *Glitch) { g.Blocks.Rest = 0.1 }, true},
		"flashes":          {func(g *Glitch) { g.Flashes.Rest = 0.1 }, true},
		"noise band":       {func(g *Glitch) { g.NoiseBand.Rest = 0.1 }, true},
		"static":           {func(g *Glitch) { g.Static.Rest = 0.1 }, true},
		"frozen static":    {func(g *Glitch) { g.Static.Rest, g.Static.RateHz = 0.1, 0 }, false},
		"line pulse":       {func(g *Glitch) { g.LinePulse.Rest = 0.1 }, true},
		"still line pulse": {func(g *Glitch) { g.LinePulse.Rest, g.LinePulse.DriftPxS = 0.1, 0 }, false},
		"off":              {func(g *Glitch) { *g = GlitchDefaults(); g.Strength = 0 }, false},
	}
	for name, c := range cases {
		g := quiet()
		c.set(&g)
		if g.Moves() != c.moves {
			t.Errorf("%s: moves %v", name, g.Moves())
		}
		if s := g.Stilled(); s.On() || s.Moves() || s.Wobble != g.Wobble {
			t.Errorf("%s: stilled %+v", name, s)
		}
	}
}
