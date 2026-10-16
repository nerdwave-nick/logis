package effects

// The glitch pass's controls (feat/logis-go 33cfcf44, new beside Rust v1):
// the clean-room glitch.frag in internal/render disturbs the glass like a
// signal breaking up, before the CRT pass. A master strength scales
// everything (0 is an exact pass-through); most parts have an amount at
// rest and one in an episode, and the episode intensity comes from an
// ambient cycle (at the end of each period, counted from the panel's
// opening) or from a burst (Bursts, which logis schedules).

// Episodes are the ambient cycle.
type Episodes struct {
	// PeriodS is the seconds from one episode to the next (0: none, bursts
	// only); Duty the share of each period an episode lasts, at its end.
	PeriodS, Duty float32
}

// Wobble is the signal's sideways jitter in bands.
type Wobble struct {
	// Rest and Episode are the most a band moves (px); BandPx the bands'
	// height; SpeedHz how often they are drawn anew.
	Rest, Episode, BandPx, SpeedHz float32
}

// Tracking is two thin kinks sweeping down the glass.
type Tracking struct {
	// Px is how far they kick; HeightPx their half height; AHz and BHz
	// the sweeps per second.
	Px, HeightPx, AHz, BHz float32
}

// Fringe is red and blue split either side of green.
type Fringe struct {
	// Px is the split; Growth the split added per px of wobble.
	Px, Growth float32
}

// Slices are horizontal slices torn sideways.
type Slices struct {
	// Rest and Episode are the share torn; Px a slice's height; ShiftPx
	// the most one moves; Group how many move together; RateHz how often
	// they are picked anew.
	Rest, Episode, Px, ShiftPx, Group, RateHz float32
}

// Blocks are rectangles of the picture shifted, split and brightened.
type Blocks struct {
	Rest, Episode, Size, ShiftPx, SplitPx, Brightness, RateHz float32
}

// Flashes are slices lit up.
type Flashes struct {
	// Rest and Episode are the share lit; Gain the light added.
	Rest, Episode, Gain, RateHz float32
}

// NoiseBand is one band of coarse noise over the glass.
type NoiseBand struct {
	// CellPx is the noise's cell; Min and Max the band's height as a share
	// of the glass.
	Rest, Episode, CellPx, Min, Max, RateHz float32
}

// Static is fine white noise.
type Static struct{ Rest, Episode, RateHz float32 }

// LinePulse is a dense ripple of brightness drifting up the glass.
type LinePulse struct {
	// PitchPx is its wavelength; DriftPxS its drift (up when positive).
	Rest, Episode, PitchPx, DriftPxS float32
}

// Glitch is every control of the glitch pass and the bursts that drive it.
type Glitch struct {
	// Strength is the master strength, 0 to 1: 0 turns the pass off.
	Strength  float32
	Episodes  Episodes
	Wobble    Wobble
	Tracking  Tracking
	Fringe    Fringe
	Slices    Slices
	Blocks    Blocks
	Flashes   Flashes
	NoiseBand NoiseBand
	Static    Static
	LinePulse LinePulse
	Bursts    Bursts
}

// GlitchDefaults is every control at the clean room's default and the
// bursts at the effects lab's.
func GlitchDefaults() Glitch {
	var g Glitch
	for _, k := range GlitchControls {
		k.Set(&g, k.Default)
	}
	for _, k := range BurstControls {
		k.Set(&g.Bursts, k.Default)
	}
	g.Bursts.On = EventOpen | EventTab
	return g
}

// On reports whether the pass runs at all.
func (g Glitch) On() bool { return g.Strength > 0 }

// Moves reports whether the glitch changes over time at rest, between
// episodes and bursts: a part with an amount at rest that is drawn anew
// over time, or the tracking sweeps. Episodes and bursts move while they
// last (the glitch clock in internal/glitch keeps time of them).
func (g Glitch) Moves() bool {
	if !g.On() {
		return false
	}
	return g.Wobble.Rest > 0 && g.Wobble.SpeedHz > 0 ||
		g.Tracking.Px > 0 && (g.Tracking.AHz > 0 || g.Tracking.BHz > 0) ||
		g.Slices.Rest > 0 && g.Slices.ShiftPx > 0 && g.Slices.RateHz > 0 ||
		g.Blocks.Rest > 0 && g.Blocks.RateHz > 0 ||
		g.Flashes.Rest > 0 && g.Flashes.Gain > 0 && g.Flashes.RateHz > 0 ||
		g.NoiseBand.Rest > 0 && g.NoiseBand.RateHz > 0 ||
		g.Static.Rest > 0 && g.Static.RateHz > 0 ||
		g.LinePulse.Rest > 0 && g.LinePulse.DriftPxS != 0
}

// Stilled is g turned off (reduce-motion): a glitch is all motion.
func (g Glitch) Stilled() Glitch {
	g.Strength = 0
	return g
}
