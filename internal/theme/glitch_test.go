package theme

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// The theme's glitch (33cfcf44): effects.glitch is a
// group of the glitch pass's controls in parts, with the bursts; a bare
// number is its strength. The bundled themes wear the owner's looks.

// ownerGlitch is the owner's glitch look shared by both bundled themes,
// before each theme's own values.
func ownerGlitch() effects.Glitch {
	return effects.Glitch{
		Strength:  0.45,
		Episodes:  effects.Episodes{PeriodS: 9, Duty: 0.1},
		Wobble:    effects.Wobble{Rest: 0.2, Episode: 24, BandPx: 26, SpeedHz: 24},
		Tracking:  effects.Tracking{Px: 3, HeightPx: 3, AHz: 0.43, BHz: 0.29},
		Fringe:    effects.Fringe{Px: 0.5, Growth: 0.12},
		Slices:    effects.Slices{Rest: 0, Episode: 0.12, Px: 9, ShiftPx: 40, Group: 5, RateHz: 30},
		Blocks:    effects.Blocks{Rest: 0, Episode: 0.45, Size: 1, ShiftPx: 26, SplitPx: 3, Brightness: 0.4, RateHz: 30},
		Flashes:   effects.Flashes{Rest: 0, Episode: 0.06, Gain: 0.33, RateHz: 30},
		NoiseBand: effects.NoiseBand{Rest: 0.02, Episode: 0.3, CellPx: 1, Min: 0.03, Max: 0.1, RateHz: 30},
		Static:    effects.Static{Rest: 0.03, Episode: 0.07, RateHz: 30},
		LinePulse: effects.LinePulse{Rest: 0.03, Episode: 0.12, PitchPx: 2.3, DriftPxS: 6},
		Bursts: effects.Bursts{OpenMS: 420, TabMS: 240, KeyMS: 120, KeyStrength: 0.3, RandomPerMinute: 6, RandomMS: 300,
			Stutter: 0.28, On: effects.EventOpen | effects.EventTab | effects.EventKey},
	}
}

func TestTheBundledThemesWearTheOwnersGlitch(t *testing.T) {
	forge := ownerGlitch()
	phosphor := ownerGlitch()
	phosphor.Strength, phosphor.Episodes.Duty = 0.73, 0.05
	phosphor.Slices.Episode = 0.24
	phosphor.Flashes.Rest, phosphor.Flashes.Episode = 0.05, 0.52
	phosphor.NoiseBand.Episode = 0.43
	phosphor.LinePulse.Rest = 0.09
	phosphor.Bursts.OpenMS, phosphor.Bursts.TabMS, phosphor.Bursts.RandomPerMinute = 500, 280, 3.5
	for name, want := range map[string]effects.Glitch{"forge-crimson-brass": forge, "cogitator-phosphor": phosphor} {
		if got := load(t, Themes{}, name).Glitch; got != want {
			t.Errorf("%s:\n got %+v\nwant %+v", name, got, want)
		}
	}
}

// A bare number is the strength; a part sets its controls and keeps the
// rest of the base, as any section does; the events replace the base's.
func TestTheGlitchLiesOverItsBase(t *testing.T) {
	th, _ := themesDir(t, map[string]string{
		"bare":  `{"effects": {"glitch": 0.2}}`,
		"parts": `{"extends": "bare", "effects": {"glitch": {"wobble": {"rest": 1}, "bursts": {"on": ["tab"], "tab-ms": 100}}}}`,
		"keys":  `{"extends": "bare", "effects": {"glitch": {"bursts": {"on": ["key"], "key-ms": 200, "key-strength": 0.6}}}}`,
		"off":   `{"effects": {"glitch": 0}}`,
		"none":  `{"effects": {"glitch": {"bursts": {"on": []}}}}`,
	})
	want := Default().Glitch
	want.Strength = 0.2
	if got := load(t, th, "bare").Glitch; got != want {
		t.Errorf("bare:\n got %+v\nwant %+v", got, want)
	}
	want.Wobble.Rest, want.Bursts.On, want.Bursts.TabMS = 1, effects.EventTab, 100
	if got := load(t, th, "parts").Glitch; got != want {
		t.Errorf("parts:\n got %+v\nwant %+v", got, want)
	}
	keys := Default().Glitch
	keys.Strength, keys.Bursts.On, keys.Bursts.KeyMS, keys.Bursts.KeyStrength = 0.2, effects.EventKey, 200, 0.6
	if got := load(t, th, "keys").Glitch; got != keys {
		t.Errorf("keys:\n got %+v\nwant %+v", got, keys)
	}
	if load(t, th, "off").Glitch.On() {
		t.Error("a glitch of 0 is on")
	}
	if got := load(t, th, "none").Glitch.Bursts.On; got != 0 {
		t.Errorf("no events: %v", got)
	}
}

func TestEveryGlitchControlIsHeldToItsRangeInLogissWords(t *testing.T) {
	cases := map[string]string{
		`{"effects": {"glitch": 2}}`:                                     "effects.glitch: 2 is out of range (0 to 1)",
		`{"effects": {"glitch": {"strength": -0.1}}}`:                    "effects.glitch.strength: -0.1 is out of range (0 to 1)",
		`{"effects": {"glitch": {"wobble": {"episode": 81}}}}`:           "effects.glitch.wobble.episode: 81 is out of range (0 to 80)",
		`{"effects": {"glitch": {"episodes": {"period-s": 61}}}}`:        "effects.glitch.episodes.period-s: 61 is out of range (0 to 60)",
		`{"effects": {"glitch": {"line-pulse": {"drift-px-s": -61}}}}`:   "effects.glitch.line-pulse.drift-px-s: -61 is out of range (-60 to 60)",
		`{"effects": {"glitch": {"noise-band": {"cell-px": 0.25}}}}`:     "effects.glitch.noise-band.cell-px: 0.25 is out of range (0.5 to 8)",
		`{"effects": {"glitch": {"bursts": {"random-ms": 10}}}}`:         "effects.glitch.bursts.random-ms: 10 is out of range (50 to 1500)",
		`{"effects": {"glitch": {"bursts": {"random-per-minute": 31}}}}`: "effects.glitch.bursts.random-per-minute: 31 is out of range (0 to 30)",
		`{"effects": {"glitch": {"bursts": {"open-ms": 2000}}}}`:         "effects.glitch.bursts.open-ms: 2000 is out of range (0 to 1500)",
		`{"effects": {"glitch": {"bursts": {"stutter": 1.5}}}}`:          "effects.glitch.bursts.stutter: 1.5 is out of range (0 to 1)",
		`{"effects": {"glitch": {"bursts": {"key-ms": 1501}}}}`:          "effects.glitch.bursts.key-ms: 1501 is out of range (0 to 1500)",
		`{"effects": {"glitch": {"bursts": {"key-strength": 1.2}}}}`:     "effects.glitch.bursts.key-strength: 1.2 is out of range (0 to 1)",
		`{"effects": {"glitch": {"slices": {"group": 0, "px": 100}}}}`:   "effects.glitch.slices.px: 100 is out of range (1 to 60)",
		`{"effects": {"glitch": {"blocks": {"size": 0.1}}}}`:             "effects.glitch.blocks.size: 0.1 is out of range (0.2 to 4)",
		`{"effects": {"glitch": {"tracking": {"height-px": 0}}}}`:        "effects.glitch.tracking.height-px: 0 is out of range (0.5 to 12)",
		`{"effects": {"glitch": {"fringe": {"growth": 0.6}}}}`:           "effects.glitch.fringe.growth: 0.6 is out of range (0 to 0.5)",
		`{"effects": {"glitch": {"flashes": {"gain": 3}}}}`:              "effects.glitch.flashes.gain: 3 is out of range (0 to 2)",
		`{"effects": {"glitch": {"static": {"rate-hz": 61}}}}`:           "effects.glitch.static.rate-hz: 61 is out of range (0 to 60)",
	}
	for text, want := range cases {
		th, dir := themesDir(t, map[string]string{"t": text})
		e := refusal(t, th, "t")
		if e.Source != filepath.Join(dir, "t.jsonc") || e.Problem != want {
			t.Errorf("%s:\n got %v\nwant %s", text, e, want)
		}
	}
}

func TestTheGlitchIsRefusedWithItsPlace(t *testing.T) {
	for text, want := range map[string]string{
		`{"effects": {"glitch": {"bursts": {"on": ["close"]}}}}`: "1:49: unknown variant `close`, expected one of `open`, `tab`, `key`",
		`{"effects": {"glitch": {"bursts": {"on": "open"}}}}`:    "1:47: invalid type: string \"open\", expected a sequence",
		`{"effects": {"glitch": {"bursts": {"on": [1]}}}}`:       "1:43: invalid type: integer `1`, expected a string",
		`{"effects": {"glitch": {"wobbel": {}}}}`: "1:32: unknown field `wobbel`, expected one of `strength`, `episodes`, " +
			"`wobble`, `tracking`, `fringe`, `slices`, `blocks`, `flashes`, `noise-band`, `static`, `line-pulse`, `bursts`",
		`{"effects": {"glitch": "x"}}`: "1:26: invalid type: string \"x\", expected " + groupWants,
	} {
		_, err := parseDoc([]byte(text))
		var invalid *jsonc.Invalid
		if !errors.As(err, &invalid) {
			t.Errorf("%s: want a refusal, got %v", text, err)
			continue
		}
		if got := invalid.Error(); got != want {
			t.Errorf("%s:\n got %s\nwant %s", text, got, want)
		}
	}
}

// The glitch moves at rest in the bundled looks, so the theme keeps the
// panel drawing; reduce-motion turns it off.
func TestReduceMotionTurnsTheGlitchOff(t *testing.T) {
	forge := Default()
	forge.Glow.Pulse, forge.Border.HasHighlight = 0, false
	forge.Effects = forge.Effects.Stilled()
	if !forge.IsContinuous() {
		t.Fatal("the forge's glitch stands still")
	}
	s := forge.Stilled()
	if s.Glitch.On() || s.IsContinuous() {
		t.Errorf("stilled: %+v", s.Glitch)
	}
	if s.Glitch.Wobble != forge.Glitch.Wobble {
		t.Error("stilling changed the glitch's controls")
	}
}

// The schema holds each glitch control and burst number to the range the
// resolver does, and names the events as the parser reads them.
func TestTheSchemaHoldsEachGlitchControlToItsRange(t *testing.T) {
	root := themeSchema(t)
	at := func(path ...string) object {
		return objectAt(root, append([]string{"effects", "glitch"}, path...))
	}
	check := func(name string, o object, key string, lo, hi float32) {
		c := follow(root, o["properties"].(object)[key])
		min, _ := c["minimum"].(float64)
		max, _ := c["maximum"].(float64)
		if float32(min) != lo || float32(max) != hi {
			t.Errorf("%s: schema %v to %v, resolver %v to %v", name, c["minimum"], c["maximum"], lo, hi)
		}
	}
	for _, k := range effects.GlitchControls {
		if k.Part == "" {
			check(k.Key, at(), k.Key, k.Min, k.Max)
			continue
		}
		check(k.Part+"."+k.Key, at(k.Part), k.Key, k.Min, k.Max)
	}
	for _, k := range effects.BurstControls {
		check("bursts."+k.Key, at("bursts"), k.Key, k.Min, k.Max)
	}
	on := at("bursts")["properties"].(object)["on"].(object)
	var words []string
	for _, w := range on["items"].(object)["enum"].([]any) {
		words = append(words, w.(string))
	}
	if strings.Join(words, " ") != strings.Join(effects.EventWords, " ") {
		t.Errorf("bursts.on: schema %v, parser %v", words, effects.EventWords)
	}
}
