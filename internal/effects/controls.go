package effects

import "math"

// Control is one control of the CRT pass: where a theme writes it
// (effects.<Group>.<Key>), the shader's uniform, the range a theme may set
// and its default (testdata/crt-controls.json, the clean room's table).
type Control struct {
	Group, Key, Uniform string
	Min, Max, Default   float32
	// Words are an enumeration's names (the mask's kind), its value their
	// index; nil for a number.
	Words []string
	field func(*CRT) *float32
}

// Value is the control's value in c (an enumeration's as its index).
func (k Control) Value(c CRT) float32 {
	if k.field == nil {
		return float32(c.Mask.Kind)
	}
	return *k.field(&c)
}

// Set sets the control in c (an enumeration's by its index).
func (k Control) Set(c *CRT, v float32) {
	if k.field == nil {
		c.Mask.Kind = MaskKind(v)
		return
	}
	*k.field(c) = v
}

// Controls are the CRT pass's controls, group by group in the clean room's
// order; the first of a group is its strength (the aberration's amount),
// which a bare number in a theme sets.
var Controls = []Control{
	{"scanlines", "strength", "u_scan_strength", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Scanlines.Strength }},
	{"scanlines", "period", "u_scan_period", 1, 16, 3, nil, func(c *CRT) *float32 { return &c.Scanlines.Period }},
	{"scanlines", "sharpness", "u_scan_sharpness", 0, 1, 0.25, nil, func(c *CRT) *float32 { return &c.Scanlines.Sharpness }},
	{"scanlines", "roll", "u_scan_roll", -60, 60, 0, nil, func(c *CRT) *float32 { return &c.Scanlines.Roll }},
	{"glow", "strength", "u_glow_strength", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Glow.Strength }},
	{"glow", "threshold", "u_glow_threshold", 0, 1, 0.35, nil, func(c *CRT) *float32 { return &c.Glow.Threshold }},
	{"glow", "reach", "u_glow_reach", 1, 32, 7, nil, func(c *CRT) *float32 { return &c.Glow.Reach }},
	{"glow", "bleed", "u_glow_bleed", 0, 3, 1, nil, func(c *CRT) *float32 { return &c.Glow.Bleed }},
	{"glow", "dim-bright", "u_glow_adapt", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Glow.DimBright }},
	{"aberration", "amount", "u_ab_amount", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Aberration.Amount }},
	{"aberration", "edges-only", "u_ab_edges", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Aberration.EdgesOnly }},
	{"aberration", "wobble", "u_ab_wobble", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Aberration.Wobble }},
	{"aberration", "wobble-rate", "u_ab_rate", 0.05, 20, 1, nil, func(c *CRT) *float32 { return &c.Aberration.WobbleRate }},
	{"aberration", "twitch", "u_ab_twitch", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Aberration.Twitch }},
	{"mask", "strength", "u_mask_strength", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Mask.Strength }},
	{"mask", "size", "u_mask_size", 1, 16, 3, nil, func(c *CRT) *float32 { return &c.Mask.Size }},
	{"mask", "kind", "u_mask_kind", 0, 2, 0, MaskWords, nil},
	{"roundness", "curvature", "u_round_curvature", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Roundness.Curvature }},
	{"roundness", "corners", "u_round_corners", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Roundness.Corners }},
	{"roundness", "vignette", "u_round_vignette", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Roundness.Vignette }},
	{"grain", "strength", "u_grain_strength", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Grain.Strength }},
	{"grain", "rate", "u_grain_rate", 0, 60, 24, nil, func(c *CRT) *float32 { return &c.Grain.Rate }},
	{"flicker", "strength", "u_flicker_strength", 0, 1, 0, nil, func(c *CRT) *float32 { return &c.Flicker.Strength }},
	// 47 rad/s, the default tremble, exactly in float32.
	{"flicker", "frequency", "u_flicker_freq", 0.5, 30, 47 / (2 * math.Pi), nil, func(c *CRT) *float32 { return &c.Flicker.Frequency }},
}

// Groups are the controls' groups in order.
func Groups() []string {
	var out []string
	for _, k := range Controls {
		if len(out) == 0 || out[len(out)-1] != k.Group {
			out = append(out, k.Group)
		}
	}
	return out
}
