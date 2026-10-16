package motion

// Transition is a panel's open and close transition: opening into its
// rest pose (from its first frame on screen), resting, or closing out of
// it.
type Transition struct {
	// opening: until the first frame is on screen, which starts the move
	// to rest from openFrom along openCurve.
	opening   bool
	openFrom  Pose
	openCurve Curve
	// move is the move under way (opening or closing), if any.
	move    *Tween[Pose]
	closing bool
	last    Pose
}

// Opening is a panel that opens from `from` to rest along curve, starting
// with its first frame on screen.
func Opening(from Pose, curve Curve) Transition {
	return Transition{opening: true, openFrom: from, openCurve: curve, last: from}
}

// Resting is a panel already at rest (no open transition).
func Resting() Transition { return Transition{last: Rest} }

// Close starts closing at now, from the pose last drawn to `to`.
func (t *Transition) Close(to Pose, curve Curve, now Nanos) {
	t.closing, t.opening = true, false
	move := NewTween(t.last, to, curve, now)
	t.move = &move
}

// Presented: the last frame drawn is on screen from at. The open's clock
// starts here, one interval (lead) back, so however long the first frame
// took to reach the screen, the whole open is seen after it.
func (t *Transition) Presented(at, lead Nanos) {
	if t.opening {
		t.opening = false
		move := NewTween(t.openFrom, Rest, t.openCurve, since(at, lead))
		t.move = &move
	}
}

// IsClosing reports whether Close was called.
func (t *Transition) IsClosing() bool { return t.closing }

// Pose is the pose of a frame shown at time. Until its first frame is on
// screen (Presented), an opening panel is shown one interval (lead) into
// its open.
func (t *Transition) Pose(time, lead Nanos) Pose {
	if t.opening {
		t.last = NewTween(t.openFrom, Rest, t.openCurve, since(time, lead)).At(time)
	}
	if t.move != nil {
		t.last = t.move.At(time)
		if t.move.Done(time) && !t.closing {
			t.move = nil
		}
	}
	return t.last
}

// Moving reports whether frames after time still move.
func (t *Transition) Moving(time Nanos) bool {
	return t.opening || t.move != nil && !t.move.Done(time)
}

// Finished reports whether a closing panel has finished at time (it can
// go).
func (t *Transition) Finished(time Nanos) bool { return t.closing && !t.Moving(time) }

// FinishTime is when a closing panel finishes; false if it is not closing.
func (t *Transition) FinishTime() (Nanos, bool) {
	if !t.closing || t.move == nil {
		return 0, false
	}
	return t.move.EndTime(), true
}

// Glide is a value that glides to each new target instead of jumping: the
// selection band's top. The first target, and the first after none, is
// taken at once.
type Glide struct {
	curve  Curve
	target *float32
	move   *Tween[Value]
}

// NewGlide glides along curve, with no target yet.
func NewGlide(curve Curve) Glide { return Glide{curve: curve} }

// Reset forgets the target (a new list): the next one is taken at once.
func (g *Glide) Reset() { *g = NewGlide(g.curve) }

// Toward is the value for a frame shown at time while the target is
// target (none without has: then neither ok nor moving), and whether later
// frames still move. A new target starts a glide from the value at
// time − lead.
func (g *Glide) Toward(target float32, has bool, time, lead Nanos) (at float32, ok, moving bool) {
	if !has {
		g.Reset()
		return 0, false, false
	}
	if g.target == nil || *g.target != target {
		start := since(time, lead)
		from := target
		switch {
		case g.move != nil:
			from = float32(g.move.At(start))
		case g.target != nil:
			from = *g.target
		}
		g.target = &target
		g.move = nil
		if from != target {
			move := NewTween(Value(from), Value(target), g.curve, start)
			g.move = &move
		}
	}
	switch {
	case g.move == nil:
		return target, true, false
	case g.move.Done(time):
		g.move = nil
		return target, true, false
	}
	return float32(g.move.At(time)), true, true
}
