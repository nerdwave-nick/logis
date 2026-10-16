package effects

// GlitchControl is one control of the glitch pass: where a theme writes it
// (effects.glitch.<Part>.<Key>, or effects.glitch.strength with no part),
// the shader's uniform, the range a theme may set and its default
// (testdata/glitch-controls.json, the clean room's table).
type GlitchControl struct {
	Part, Key, Uniform string
	Min, Max, Default  float32
	field              func(*Glitch) *float32
}

// Value is the control's value in g.
func (k GlitchControl) Value(g Glitch) float32 { return *k.field(&g) }

// Set sets the control in g.
func (k GlitchControl) Set(g *Glitch, v float32) { *k.field(g) = v }

// GlitchControls are the glitch pass's 43 controls in the clean room's
// order: the master strength, the episodes, then the parts from signal to
// surface.
var GlitchControls = []GlitchControl{
	{"", "strength", "u_strength", 0, 1, 1, func(g *Glitch) *float32 { return &g.Strength }},
	{"episodes", "period-s", "u_episode_period_s", 0, 60, 9, func(g *Glitch) *float32 { return &g.Episodes.PeriodS }},
	{"episodes", "duty", "u_episode_duty", 0, 1, 0.1, func(g *Glitch) *float32 { return &g.Episodes.Duty }},
	{"wobble", "rest", "u_wobble_rest", 0, 4, 0.7, func(g *Glitch) *float32 { return &g.Wobble.Rest }},
	{"wobble", "episode", "u_wobble_episode", 0, 80, 24, func(g *Glitch) *float32 { return &g.Wobble.Episode }},
	{"wobble", "band-px", "u_wobble_band_px", 2, 80, 26, func(g *Glitch) *float32 { return &g.Wobble.BandPx }},
	{"wobble", "speed-hz", "u_wobble_speed_hz", 0, 60, 24, func(g *Glitch) *float32 { return &g.Wobble.SpeedHz }},
	{"tracking", "px", "u_tracking_px", 0, 20, 3, func(g *Glitch) *float32 { return &g.Tracking.Px }},
	{"tracking", "height-px", "u_tracking_height_px", 0.5, 12, 3, func(g *Glitch) *float32 { return &g.Tracking.HeightPx }},
	{"tracking", "a-hz", "u_tracking_a_hz", 0, 3, 0.43, func(g *Glitch) *float32 { return &g.Tracking.AHz }},
	{"tracking", "b-hz", "u_tracking_b_hz", 0, 3, 0.29, func(g *Glitch) *float32 { return &g.Tracking.BHz }},
	{"fringe", "px", "u_fringe_px", 0, 4, 0.5, func(g *Glitch) *float32 { return &g.Fringe.Px }},
	{"fringe", "growth", "u_fringe_growth", 0, 0.5, 0.12, func(g *Glitch) *float32 { return &g.Fringe.Growth }},
	{"slices", "rest", "u_slice_rest", 0, 1, 0, func(g *Glitch) *float32 { return &g.Slices.Rest }},
	{"slices", "episode", "u_slice_episode", 0, 1, 0.12, func(g *Glitch) *float32 { return &g.Slices.Episode }},
	{"slices", "px", "u_slice_px", 1, 60, 9, func(g *Glitch) *float32 { return &g.Slices.Px }},
	{"slices", "shift-px", "u_slice_shift_px", 0, 120, 40, func(g *Glitch) *float32 { return &g.Slices.ShiftPx }},
	{"slices", "group", "u_slice_group", 1, 16, 5, func(g *Glitch) *float32 { return &g.Slices.Group }},
	{"slices", "rate-hz", "u_slice_rate_hz", 0, 60, 30, func(g *Glitch) *float32 { return &g.Slices.RateHz }},
	{"blocks", "rest", "u_block_rest", 0, 1, 0, func(g *Glitch) *float32 { return &g.Blocks.Rest }},
	{"blocks", "episode", "u_block_episode", 0, 1, 0.45, func(g *Glitch) *float32 { return &g.Blocks.Episode }},
	{"blocks", "size", "u_block_size", 0.2, 4, 1, func(g *Glitch) *float32 { return &g.Blocks.Size }},
	{"blocks", "shift-px", "u_block_shift_px", 0, 120, 26, func(g *Glitch) *float32 { return &g.Blocks.ShiftPx }},
	{"blocks", "split-px", "u_block_split_px", 0, 12, 3, func(g *Glitch) *float32 { return &g.Blocks.SplitPx }},
	{"blocks", "brightness", "u_block_brightness", 0, 1, 0.4, func(g *Glitch) *float32 { return &g.Blocks.Brightness }},
	{"blocks", "rate-hz", "u_block_rate_hz", 0, 60, 30, func(g *Glitch) *float32 { return &g.Blocks.RateHz }},
	{"flashes", "rest", "u_flash_rest", 0, 1, 0, func(g *Glitch) *float32 { return &g.Flashes.Rest }},
	{"flashes", "episode", "u_flash_episode", 0, 1, 0.06, func(g *Glitch) *float32 { return &g.Flashes.Episode }},
	{"flashes", "gain", "u_flash_gain", 0, 2, 0.33, func(g *Glitch) *float32 { return &g.Flashes.Gain }},
	{"flashes", "rate-hz", "u_flash_rate_hz", 0, 60, 30, func(g *Glitch) *float32 { return &g.Flashes.RateHz }},
	{"noise-band", "rest", "u_band_rest", 0, 1, 0, func(g *Glitch) *float32 { return &g.NoiseBand.Rest }},
	{"noise-band", "episode", "u_band_episode", 0, 1, 0.3, func(g *Glitch) *float32 { return &g.NoiseBand.Episode }},
	{"noise-band", "cell-px", "u_band_cell_px", 0.5, 8, 1, func(g *Glitch) *float32 { return &g.NoiseBand.CellPx }},
	{"noise-band", "min", "u_band_min", 0, 0.5, 0.03, func(g *Glitch) *float32 { return &g.NoiseBand.Min }},
	{"noise-band", "max", "u_band_max", 0, 0.5, 0.1, func(g *Glitch) *float32 { return &g.NoiseBand.Max }},
	{"noise-band", "rate-hz", "u_band_rate_hz", 0, 60, 30, func(g *Glitch) *float32 { return &g.NoiseBand.RateHz }},
	{"static", "rest", "u_static_rest", 0, 1, 0.025, func(g *Glitch) *float32 { return &g.Static.Rest }},
	{"static", "episode", "u_static_episode", 0, 1, 0.07, func(g *Glitch) *float32 { return &g.Static.Episode }},
	{"static", "rate-hz", "u_static_rate_hz", 0, 60, 30, func(g *Glitch) *float32 { return &g.Static.RateHz }},
	{"line-pulse", "rest", "u_pulse_rest", 0, 1, 0.03, func(g *Glitch) *float32 { return &g.LinePulse.Rest }},
	{"line-pulse", "episode", "u_pulse_episode", 0, 1, 0.12, func(g *Glitch) *float32 { return &g.LinePulse.Episode }},
	{"line-pulse", "pitch-px", "u_pulse_pitch_px", 1, 8, 2.3, func(g *Glitch) *float32 { return &g.LinePulse.PitchPx }},
	{"line-pulse", "drift-px-s", "u_pulse_drift_px_s", -60, 60, 6, func(g *Glitch) *float32 { return &g.LinePulse.DriftPxS }},
}

// GlitchParts are the controls' parts in order ("" first: the strength).
func GlitchParts() []string {
	var out []string
	for i, k := range GlitchControls {
		if i == 0 || out[len(out)-1] != k.Part {
			out = append(out, k.Part)
		}
	}
	return out
}
