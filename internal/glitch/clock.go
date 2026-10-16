// Package glitch keeps the glitch's time for one panel (feat/logis-go
// 33cfcf44, new beside Rust v1): the ambient clock counted from the
// panel's opening, and the bursts that events (the opening, a switch of
// tab, a key press) and chance fire, with the envelope the glitch pass reads as its
// burst intensity. It also says when the panel must keep drawing for the
// glitch and when it must wake for the next episode or random burst.
// The scheduling is the effects lab's, which the owner approved.
//
// Times are CLOCK_MONOTONIC nanoseconds, as the window's frames have them;
// a Clock is used on the window's goroutine only.
package glitch

import (
	"math"

	"github.com/nerdwave-nick/logis/internal/effects"
)

// step is a burst's stutter step: each is kept or damped as a whole.
const step = 33_000_000

// damped is a stuttered step's share of the envelope.
const damped = 0.25

// Clock is the glitch's time for one panel.
type Clock struct {
	opened uint64
	open   bool
	// burst is the open, tab or random burst; key the key press's, a
	// small one beside it (8ad70fee): the glitch takes the larger.
	burst, key burst
	// next is when the next random burst comes (valid while scheduled).
	next      uint64
	scheduled bool
	random    func() float64
}

// burst is the burst running (or the last one): its start and length
// (ns), its peak and its steps' gates, drawn as they are reached.
type burst struct {
	start, length uint64
	peak          float32
	gates         []float32
}

// New is a clock that draws its chances from random (numbers in [0, 1),
// math/rand/v2's Float64 say).
func New(random func() float64) *Clock { return &Clock{random: random} }

// Opened reports whether the panel has opened.
func (c *Clock) Opened() bool { return c.open }

// Open starts the clock with the panel's first frame at at: the ambient
// clock counts from it, the open burst fires (if g fires one) and random
// bursts are scheduled from it.
func (c *Clock) Open(at uint64, g effects.Glitch) {
	c.opened, c.open = at, true
	c.scheduled = false
	c.Fire(effects.EventOpen, at, g)
	c.schedule(at, g)
}

// Fire fires the burst of event e at at, if g's bursts fire one on e. An
// open or tab burst replaces the open, tab or random burst running; a
// key burst (effects.EventKey) replaces only a running key burst, and the
// glitch takes the larger of the two. A tab switch calls it with
// effects.EventTab, a key press with effects.EventKey.
func (c *Clock) Fire(e effects.Events, at uint64, g effects.Glitch) {
	l := g.Bursts.Length(e)
	if l <= 0 || !g.On() {
		return
	}
	if e == effects.EventKey {
		c.key = newBurst(at, l, g.Bursts.Peak(e))
		return
	}
	c.start(at, l)
}

func (c *Clock) start(at uint64, lengthMS float32) { c.burst = newBurst(at, lengthMS, 1) }

func newBurst(at uint64, lengthMS, peak float32) burst {
	return burst{start: at, length: uint64(float64(lengthMS) * 1e6), peak: peak}
}

// schedule draws when the next random burst comes after at (none
// without random bursts).
func (c *Clock) schedule(at uint64, g effects.Glitch) {
	per := float64(g.Bursts.RandomPerMinute)
	if per <= 0 || !g.On() {
		c.scheduled = false
		return
	}
	wait := -math.Log(1-c.random()) * 60e9 / per
	c.next, c.scheduled = at+uint64(wait), true
}

// due fires a random burst whose time has come, and schedules the one
// after it; a schedule is drawn when random bursts were turned on.
func (c *Clock) due(at uint64, g effects.Glitch) {
	if !c.scheduled || g.Bursts.RandomPerMinute <= 0 {
		c.schedule(at, g)
		return
	}
	if at >= c.next && g.On() {
		// At its time; a frame that comes later than a whole burst (the
		// panel slept through it) starts it now.
		from := c.next
		if float64(at-from) >= float64(g.Bursts.RandomMS)*1e6 {
			from = at
		}
		c.start(from, g.Bursts.RandomMS)
		c.schedule(from, g)
	}
}

// At is the glitch's moment at at: the time since the panel opened
// (wrapped, see since) and the burst intensity. A random burst whose
// time has come fires first. Before the opening it is zero.
func (c *Clock) At(at uint64, g effects.Glitch) effects.Moment {
	if !c.open {
		return effects.Moment{}
	}
	c.due(at, g)
	return effects.Moment{Time: c.since(at, g), Event: max(c.envelope(&c.burst, at, g), c.envelope(&c.key, at, g))}
}

// since is the seconds since the opening, wrapped after a whole number of
// ambient periods near an hour (or the hour itself), so the pass's float
// time keeps its precision; the clean room asks for the wrap.
func (c *Clock) since(at uint64, g effects.Glitch) float32 {
	s := float64(c.since0(at)) / 1e9
	wrap := 3600.0
	if p := float64(g.Episodes.PeriodS); p > 0 {
		wrap = p * math.Max(math.Floor(3600/p), 1)
	}
	return float32(math.Mod(s, wrap))
}

// envelope is burst b's intensity at at: sin(πt)^0.6 over its length
// times its peak, its stuttered steps damped.
func (c *Clock) envelope(b *burst, at uint64, g effects.Glitch) float32 {
	if !b.running(at) {
		return 0
	}
	t := float64(at-b.start) / float64(b.length)
	i := int((at - b.start) / step)
	for len(b.gates) <= i {
		gate := float32(1)
		if c.random() < float64(g.Bursts.Stutter) {
			gate = damped
		}
		b.gates = append(b.gates, gate)
	}
	return float32(math.Pow(math.Sin(math.Pi*t), 0.6)) * b.peak * b.gates[i]
}

// Moving reports whether the glitch moves at at, so the panel keeps
// drawing: it moves at rest, a burst runs or an ambient episode lasts.
func (c *Clock) Moving(at uint64, g effects.Glitch) bool {
	if !c.open || !g.On() {
		return false
	}
	return g.Moves() || c.bursting(at) || c.inEpisode(at, g)
}

func (c *Clock) bursting(at uint64) bool { return c.burst.running(at) || c.key.running(at) }

func (b *burst) running(at uint64) bool {
	return b.length > 0 && at >= b.start && at-b.start < b.length
}

func (c *Clock) inEpisode(at uint64, g effects.Glitch) bool {
	period, start, ok := episode(g)
	return ok && c.since0(at)%period >= start
}

// since0 is the nanoseconds since the opening.
func (c *Clock) since0(at uint64) uint64 { return at - min(at, c.opened) }

// episode is the ambient period and where an episode starts in each
// (ns); ok is false without episodes.
func episode(g effects.Glitch) (period, start uint64, ok bool) {
	p := float64(g.Episodes.PeriodS)
	length := p * math.Min(math.Max(float64(g.Episodes.Duty), 0), 1)
	if p <= 0 || length <= 0 {
		return 0, 0, false
	}
	// To the microsecond: the controls are float32 (0.1 is not exact).
	us := func(s float64) uint64 { return uint64(math.Round(s*1e6)) * 1000 }
	return us(p), us(p - length), true
}

// Next is when the glitch next starts to move, while it does not move at
// at: the next ambient episode or random burst. The panel sleeps until
// then.
func (c *Clock) Next(at uint64, g effects.Glitch) (uint64, bool) {
	if !c.open || !g.On() || c.Moving(at, g) {
		return 0, false
	}
	var next uint64
	found := false
	if c.scheduled && g.Bursts.RandomPerMinute > 0 {
		next, found = max(c.next, at), true
	}
	if period, start, ok := episode(g); ok {
		// Not in an episode, so before its start in this period.
		ep := at + start - c.since0(at)%period
		if !found || ep < next {
			next, found = ep, true
		}
	}
	return next, found
}
