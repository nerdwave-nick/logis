package theme

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// The theme's effects (cd816303): each effect a group of the CRT pass's
// controls; a bare number is the group's strength, and the older keys
// (bloom, curvature, vignette) set their new places, so an older theme
// stays valid.

// ownerLook is a look as the owner chose it in the effects lab (99fe9bbb).
func ownerLook(scan effects.Scanlines, glow effects.Glow, ab effects.Aberration, mask effects.Mask,
	round effects.Roundness, grain effects.Grain, flicker effects.Flicker) effects.CRT {
	return effects.CRT{Scanlines: scan, Glow: glow, Aberration: ab, Mask: mask, Roundness: round, Grain: grain, Flicker: flicker}
}

func TestTheBundledThemesWearTheOwnersLooks(t *testing.T) {
	want := map[string]effects.CRT{
		"forge-crimson-brass": ownerLook(effects.Scanlines{Strength: 0.38, Period: 2, Sharpness: 0.79, Roll: 0.5},
			effects.Glow{Strength: 0.26, Threshold: 0.25, Reach: 1.5, Bleed: 0.65, DimBright: 0.55},
			effects.Aberration{Amount: 0.06, EdgesOnly: 0.13, Wobble: 0.33, WobbleRate: 1.65, Twitch: 0.29},
			effects.Mask{Strength: 0.04, Size: 2, Kind: effects.MaskDots},
			effects.Roundness{Curvature: 0.02, Corners: 0, Vignette: 0.14},
			effects.Grain{Strength: 0.12, Rate: 24}, effects.Flicker{Strength: 0.01, Frequency: 7.48}),
		"cogitator-phosphor": ownerLook(effects.Scanlines{Strength: 0.8, Period: 3, Sharpness: 0.47, Roll: -2},
			effects.Glow{Strength: 0.63, Threshold: 0.54, Reach: 1.5, Bleed: 0.9, DimBright: 0.15},
			effects.Aberration{Amount: 0.62, EdgesOnly: 0, Wobble: 0.08, WobbleRate: 1.9, Twitch: 0},
			effects.Mask{Strength: 0.04, Size: 2, Kind: effects.MaskDots},
			effects.Roundness{Curvature: 0.06, Corners: 0, Vignette: 0.21},
			effects.Grain{Strength: 0.55, Rate: 24}, effects.Flicker{Strength: 0.06, Frequency: 3.49}),
	}
	for name, w := range want {
		if got := load(t, Themes{}, name).Effects; got != w {
			t.Errorf("%s:\n got %+v\nwant %+v", name, got, w)
		}
	}
}

// An older theme's strengths land on their groups' strengths; the group's
// other controls come from the base, as any section's keys do.
func TestAnOlderThemesStrengthsStayValid(t *testing.T) {
	th, _ := themesDir(t, map[string]string{"older": `{"effects": {"scanlines": 0.2, "grain": 0.1, "flicker": 0.02,
		"vignette": 0.3, "bloom": 0.4, "curvature": 0.1, "aberration": 0.5}}`})
	got := load(t, th, "older").Effects
	want := Default().Effects
	want.Scanlines.Strength, want.Grain.Strength, want.Flicker.Strength = 0.2, 0.1, 0.02
	want.Roundness.Vignette, want.Glow.Strength, want.Roundness.Curvature, want.Aberration.Amount = 0.3, 0.4, 0.1, 0.5
	if got != want {
		t.Errorf("\n got %+v\nwant %+v", got, want)
	}
}

func TestAGroupSetsItsControlsAndKeepsTheRestOfTheBase(t *testing.T) {
	th, _ := themesDir(t, map[string]string{
		"grille": `{"effects": {"mask": {"kind": "grille", "size": 4}, "roundness": {"corners": 0.5}}}`,
		"slot":   `{"extends": "grille", "effects": {"mask": {"kind": "slot"}, "glow": 0.9}}`,
	})
	forge := Default().Effects
	got := load(t, th, "slot").Effects
	want := forge
	want.Mask.Kind, want.Mask.Size, want.Roundness.Corners, want.Glow.Strength = effects.MaskSlot, 4, 0.5, 0.9
	if got != want {
		t.Errorf("\n got %+v\nwant %+v", got, want)
	}
}

func TestEveryControlIsHeldToItsRangeInLogissWords(t *testing.T) {
	cases := map[string]string{
		`{"effects": {"bloom": 2}}`:                           "effects.bloom: 2 is out of range (0 to 1)",
		`{"effects": {"scanlines": 2}}`:                       "effects.scanlines: 2 is out of range (0 to 1)",
		`{"effects": {"aberration": -1}}`:                     "effects.aberration: -1 is out of range (0 to 1)",
		`{"effects": {"scanlines": {"period": 40}}}`:          "effects.scanlines.period: 40 is out of range (1 to 16)",
		`{"effects": {"scanlines": {"roll": -61}}}`:           "effects.scanlines.roll: -61 is out of range (-60 to 60)",
		`{"effects": {"glow": {"bleed": 3.5}}}`:               "effects.glow.bleed: 3.5 is out of range (0 to 3)",
		`{"effects": {"glow": {"reach": 0.5}}}`:               "effects.glow.reach: 0.5 is out of range (1 to 32)",
		`{"effects": {"aberration": {"wobble-rate": 0}}}`:     "effects.aberration.wobble-rate: 0 is out of range (0.05 to 20)",
		`{"effects": {"mask": {"size": 17}}}`:                 "effects.mask.size: 17 is out of range (1 to 16)",
		`{"effects": {"roundness": {"curvature": 1.5}}}`:      "effects.roundness.curvature: 1.5 is out of range (0 to 1)",
		`{"effects": {"grain": {"rate": 61}}}`:                "effects.grain.rate: 61 is out of range (0 to 60)",
		`{"effects": {"flicker": {"frequency": 0.1}}}`:        "effects.flicker.frequency: 0.1 is out of range (0.5 to 30)",
		`{"effects": {"vignette": 1e39}}`:                     "effects.vignette: inf is out of range (0 to 1)",
		`{"effects": {"glow": {"dim-bright": -0.5}}}`:         "effects.glow.dim-bright: -0.5 is out of range (0 to 1)",
		`{"effects": {"aberration": {"edges-only": 1.01}}}`:   "effects.aberration.edges-only: 1.01 is out of range (0 to 1)",
		`{"effects": {"scanlines": {"sharpness": -0.01}}}`:    "effects.scanlines.sharpness: -0.01 is out of range (0 to 1)",
		`{"effects": {"glow": {"threshold": 2, "reach": 0}}}`: "effects.glow.threshold: 2 is out of range (0 to 1)",
	}
	for text, want := range cases {
		th, dir := themesDir(t, map[string]string{"t": text})
		e := refusal(t, th, "t")
		if e.Source != filepath.Join(dir, "t.jsonc") || e.Problem != want {
			t.Errorf("%s:\n got %v\nwant %s", text, e, want)
		}
	}
}

func TestEffectsAreRefusedWithTheirPlace(t *testing.T) {
	for text, want := range map[string]string{
		`{"effects": {"mask": {"kind": "hex"}}}`:                       "1:35: unknown variant `hex`, expected one of `dots`, `grille`, `slot`",
		`{"effects": {"roundness": 0.5}}`:                              "1:29: invalid type: floating point `0.5`, expected struct RoundnessDoc",
		`{"effects": {"glow": "x"}}`:                                   "1:24: invalid type: string \"x\", expected " + groupWants,
		`{"effects": {"grain": {"rte": 3}}}`:                           "1:28: unknown field `rte`, expected `strength` or `rate`",
		`{"effects": {"vignette": 0.3, "roundness": {"vignette": 0}}}`: "1:41: `vignette` is the older name of `roundness.vignette`: give it once",
		`{"effects": {"roundness": {"curvature": 0}, "curvature": 0}}`: "1:55: `curvature` is the older name of `roundness.curvature`: give it once",
		`{"effects": {"glow": {"strength": 0}, "bloom": 0.2}}`:         "1:45: `bloom` is the older name of `glow.strength`: give it once",
		`{"effects": {"bloom": 0.2, "glow": 0.2}}`:                     "1:33: `bloom` is the older name of `glow.strength`: give it once",
		`{"effects": {"glitter": 1}}`: "1:22: unknown field `glitter`, expected one of `scanlines`, `glow`, `aberration`, " +
			"`mask`, `roundness`, `grain`, `flicker`, `glitch`, `custom-shader`, `vignette`, `bloom`, `curvature`",
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

// The forge moves on its own through its effects too (rolling scanlines,
// its aberration's wobble, grain, flicker); reduce-motion stills them and
// keeps the still look.
func TestTheEffectsMoveUntilStilled(t *testing.T) {
	forge := Default()
	forge.Glow.Pulse, forge.Border.HasHighlight = 0, false
	if !forge.IsContinuous() {
		t.Fatal("the forge's effects stand still")
	}
	s := forge.Stilled()
	if s.IsContinuous() || s.Effects != forge.Effects.Stilled() {
		t.Errorf("%+v", s.Effects)
	}
	if s.Effects.Grain.Strength != forge.Effects.Grain.Strength || s.Effects.Mask != forge.Effects.Mask {
		t.Error("stilling took the still look")
	}
}

// The schema holds each control to the range the resolver does, and names
// the mask's kinds as the parser reads them.
func TestTheSchemaHoldsEachControlToItsRange(t *testing.T) {
	root := themeSchema(t)
	for _, k := range effects.Controls {
		c := follow(root, objectAt(root, []string{"effects", k.Group})["properties"].(object)[k.Key])
		if k.Words != nil {
			var enum []string
			for _, w := range c["enum"].([]any) {
				enum = append(enum, w.(string))
			}
			if strings.Join(enum, " ") != strings.Join(k.Words, " ") {
				t.Errorf("%s.%s: schema %v, parser %v", k.Group, k.Key, enum, k.Words)
			}
			continue
		}
		lo, hi := c["minimum"].(float64), c["maximum"].(float64)
		if float32(lo) != k.Min || float32(hi) != k.Max {
			t.Errorf("%s.%s: schema %v to %v, resolver %v to %v", k.Group, k.Key, lo, hi, k.Min, k.Max)
		}
	}
}
