package effects

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

// The control table is the clean room's crt-controls.json (testdata, as
// delivered): the same groups, controls, uniforms, ranges and defaults, in
// its order. Theme keys are its names with spaces and slashes as dashes.
type delivered struct {
	Group, Name, Uniform string
	Min, Max, Default    float64
	Options              []string
}

func readDelivered(t *testing.T) []delivered {
	t.Helper()
	b, err := os.ReadFile("testdata/crt-controls.json")
	if err != nil {
		t.Fatal(err)
	}
	var out []delivered
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheControlsAreTheDeliveredOnes(t *testing.T) {
	want := readDelivered(t)
	if len(Controls) != len(want) {
		t.Fatalf("%d controls, delivered %d", len(Controls), len(want))
	}
	key := strings.NewReplacer(" ", "-", "/", "-")
	for i, w := range want {
		c := Controls[i]
		if c.Group != w.Group || c.Key != key.Replace(w.Name) || c.Uniform != w.Uniform {
			t.Errorf("%d: %s.%s (%s), delivered %s.%s (%s)", i, c.Group, c.Key, c.Uniform, w.Group, w.Name, w.Uniform)
		}
		if c.Min != float32(w.Min) || c.Max != float32(w.Max) {
			t.Errorf("%s.%s: %v to %v, delivered %v to %v", c.Group, c.Key, c.Min, c.Max, w.Min, w.Max)
		}
		if got := c.Value(Defaults()); got != float32(w.Default) {
			t.Errorf("%s.%s: default %v, delivered %v", c.Group, c.Key, got, w.Default)
		}
		if strings.Join(c.Words, ",") != strings.Join(w.Options, ",") {
			t.Errorf("%s.%s: words %v, delivered %v", c.Group, c.Key, c.Words, w.Options)
		}
	}
}

// Every control reads and writes its own field: setting each in turn to a
// distinct value changes only that one.
func TestEachControlIsItsOwnField(t *testing.T) {
	var c CRT
	for i, k := range Controls {
		k.Set(&c, float32(i+1))
	}
	for i, k := range Controls {
		if got := k.Value(c); got != float32(i+1) {
			t.Errorf("%s.%s: %v, want %v", k.Group, k.Key, got, i+1)
		}
	}
}

// The default flicker frequency is today's 47 rad/s exactly in float32
// (the shader's max(f·2π, 0) then gives 47.0).
func TestTheDefaultFlickerIsTodays47RadiansASecond(t *testing.T) {
	if got, want := Defaults().Flicker.Frequency, float32(47/(2*math.Pi)); got != want {
		t.Errorf("%v, want %v", got, want)
	}
}

func TestDefaultsAreOff(t *testing.T) {
	if Defaults().Any() || (CRT{}).Any() {
		t.Error("the defaults ask for the pass")
	}
	for _, on := range []func(*CRT){
		func(c *CRT) { c.Scanlines.Strength = 0.1 },
		func(c *CRT) { c.Glow.Strength = 0.1 },
		func(c *CRT) { c.Aberration.Amount = 0.1 },
		func(c *CRT) { c.Mask.Strength = 0.1 },
		func(c *CRT) { c.Roundness.Curvature = 0.1 },
		func(c *CRT) { c.Roundness.Corners = 0.1 },
		func(c *CRT) { c.Roundness.Vignette = 0.1 },
		func(c *CRT) { c.Grain.Strength = 0.1 },
		func(c *CRT) { c.Flicker.Strength = 0.1 },
	} {
		c := Defaults()
		on(&c)
		if !c.Any() {
			t.Errorf("%+v asks for no pass", c)
		}
	}
}

// What moves on its own: rolling scanlines, a wobbling aberration,
// flicker and changing grain, each only while its effect is on.
func TestWhatMovesAndWhatStillingLeaves(t *testing.T) {
	cases := map[string]struct {
		set   func(*CRT)
		moves bool
	}{
		"rolling scanlines":   {func(c *CRT) { c.Scanlines.Strength, c.Scanlines.Roll = 0.5, -2 }, true},
		"still scanlines":     {func(c *CRT) { c.Scanlines.Strength = 0.5 }, false},
		"roll without lines":  {func(c *CRT) { c.Scanlines.Roll = 3 }, false},
		"wobbling aberration": {func(c *CRT) { c.Aberration.Amount, c.Aberration.Wobble = 0.5, 0.3 }, true},
		"steady aberration":   {func(c *CRT) { c.Aberration.Amount = 0.5 }, false},
		"wobble without it":   {func(c *CRT) { c.Aberration.Wobble = 0.5 }, false},
		"flicker":             {func(c *CRT) { c.Flicker.Strength = 0.01 }, true},
		"changing grain":      {func(c *CRT) { c.Grain.Strength = 0.1 }, true},
		"frozen grain":        {func(c *CRT) { c.Grain.Strength, c.Grain.Rate = 0.1, 0 }, false},
		"glow and mask":       {func(c *CRT) { c.Glow.Strength, c.Mask.Strength, c.Roundness.Vignette = 0.5, 0.5, 0.5 }, false},
	}
	for name, tc := range cases {
		c := Defaults()
		tc.set(&c)
		if c.Moves() != tc.moves {
			t.Errorf("%s: moves %v", name, c.Moves())
		}
		s := c.Stilled()
		if s.Moves() {
			t.Errorf("%s: still moves once stilled: %+v", name, s)
		}
		if s.Scanlines.Strength != c.Scanlines.Strength || s.Grain.Strength != c.Grain.Strength ||
			s.Aberration.Amount != c.Aberration.Amount {
			t.Errorf("%s: stilling turned a static effect off: %+v", name, s)
		}
	}
}
