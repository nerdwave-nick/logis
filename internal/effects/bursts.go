package effects

import "math"

// Events are what fire a burst, as a set.
type Events uint8

const (
	// EventOpen is the panel opening.
	EventOpen Events = 1 << iota
	// EventTab is a switch of tab (the tab strip of feat/logis-go 8cba2fc9).
	EventTab
	// EventKey is a key press in the panel (8ad70fee).
	EventKey
)

// EventWords are the events' names in a theme (bursts.on), in bit order.
var EventWords = []string{"open", "tab", "key"}

// Bursts are short glitch episodes that logis fires: on events (the panel
// opening, a switch of tab, a key press) and at random. A burst's
// intensity rises and falls as sin(πt)^0.6 over its length, and Stutter
// is the share of its 33 ms steps damped to a quarter; the glitch pass
// takes the larger of it and the ambient cycle (the effects lab's
// scheduling, owner-approved).
type Bursts struct {
	// OpenMS, TabMS and KeyMS are the bursts' lengths on those events (0:
	// none); RandomPerMinute how many random bursts come a minute on
	// average (a Poisson process; 0: none), each RandomMS long.
	// KeyStrength is a key burst's peak (the others peak at 1): a key
	// press fires a small burst of its own (8ad70fee).
	OpenMS, TabMS, KeyMS, KeyStrength, RandomPerMinute, RandomMS, Stutter float32
	// On are the events that fire a burst.
	On Events
}

// BurstControl is one number of the bursts: its key in
// effects.glitch.bursts, range and default (the effects lab's).
type BurstControl struct {
	Key               string
	Min, Max, Default float32
	field             func(*Bursts) *float32
}

// Value is the control's value in b.
func (k BurstControl) Value(b Bursts) float32 { return *k.field(&b) }

// Set sets the control in b.
func (k BurstControl) Set(b *Bursts, v float32) { *k.field(b) = v }

// BurstControls are the bursts' numbers, in the lab's order.
var BurstControls = []BurstControl{
	{"open-ms", 0, 1500, 420, func(b *Bursts) *float32 { return &b.OpenMS }},
	{"tab-ms", 0, 1500, 240, func(b *Bursts) *float32 { return &b.TabMS }},
	{"key-ms", 0, 1500, 120, func(b *Bursts) *float32 { return &b.KeyMS }},
	{"key-strength", 0, 1, 0.3, func(b *Bursts) *float32 { return &b.KeyStrength }},
	{"random-per-minute", 0, 30, 0, func(b *Bursts) *float32 { return &b.RandomPerMinute }},
	{"random-ms", 50, 1500, 300, func(b *Bursts) *float32 { return &b.RandomMS }},
	{"stutter", 0, 1, 0.28, func(b *Bursts) *float32 { return &b.Stutter }},
}

// Length is how long a burst on e lasts, in ms (0: e fires none, as
// a key burst of strength 0 does).
func (b Bursts) Length(e Events) float32 {
	if b.On&e == 0 || b.Peak(e) <= 0 {
		return 0
	}
	switch e {
	case EventOpen:
		return b.OpenMS
	case EventTab:
		return b.TabMS
	case EventKey:
		return b.KeyMS
	}
	return 0
}

// Peak is the highest intensity of a burst on e: KeyStrength for a key
// press, 1 for any other.
func (b Bursts) Peak(e Events) float32 {
	if e == EventKey {
		return b.KeyStrength
	}
	return 1
}

// Moment is where the glitch stands in a frame: Time is the seconds since
// the panel opened (the ambient clock and the pass's time), Event the
// burst intensity, 0 to 1.
type Moment struct{ Time, Event float32 }

// Ambient is the ambient cycle's intensity at t seconds since the panel
// opened: a smooth bell, (4x(1-x))², over the last Duty share of each
// period, so a period begins calm; 0 without a period or duty. As the
// glitch pass computes it.
func (e Episodes) Ambient(t float32) float32 {
	start, length, ok := e.window()
	if !ok {
		return 0
	}
	x := (float32(math.Mod(float64(t), float64(e.PeriodS))) - start) / length
	if x <= 0 || x >= 1 {
		return 0
	}
	b := 4 * x * (1 - x)
	return b * b
}

// window is where an episode starts in each period and how long it lasts
// (seconds); ok is false without episodes.
func (e Episodes) window() (start, length float32, ok bool) {
	length = e.PeriodS * min(max(e.Duty, 0), 1)
	if e.PeriodS <= 0 || length <= 0 {
		return 0, 0, false
	}
	return e.PeriodS - length, length, true
}

// Intensity is the episode intensity E the glitch pass draws at m:
// the larger of the ambient cycle and the burst, times the strength.
// A later pass that wants the glitch's intensity (a custom pass's
// iGlitch) takes this or m.Event, the burst alone.
func (g Glitch) Intensity(m Moment) float32 {
	return min(max(max(g.Episodes.Ambient(m.Time), min(max(m.Event, 0), 1))*g.Strength, 0), 1)
}
