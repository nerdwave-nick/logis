package motion

// Values moving over time (Rust src/motion/tween.rs): a Tween between two
// values along a Curve, the Pose a whole panel takes during its open and
// close Transition, and the Glide of the selection band.
//
// A change takes effect one frame interval (lead) before the frame that
// first shows it, since it was asked for between frames: so that frame
// already shows movement, rather than the old state once more.

// Lerper is a value a tween can move between: the value t of the way from
// it to `to` (t in 0..1).
type Lerper[T any] interface{ Lerp(to T, t float32) T }

// Value is a number a tween moves.
type Value float32

// Lerp moves toward to; the ends are exact, so a finished move rests
// exactly on its end.
func (v Value) Lerp(to Value, t float32) Value {
	if t >= 1 {
		return to
	}
	return v + (to-v)*Value(t)
}

// Tween is a move from one value to another along a curve, from a start
// time.
type Tween[T Lerper[T]] struct {
	from, to T
	curve    Curve
	start    Nanos
}

// NewTween moves from `from` to `to` along curve, starting at start.
func NewTween[T Lerper[T]](from, to T, curve Curve, start Nanos) Tween[T] {
	return Tween[T]{from: from, to: to, curve: curve, start: start}
}

// At is the value at time: from before the start, to after the end.
func (tw Tween[T]) At(time Nanos) T {
	return tw.from.Lerp(tw.to, tw.curve.Progress(since(time, tw.start)))
}

// Done reports whether the move is over at time.
func (tw Tween[T]) Done(time Nanos) bool { return tw.curve.Done(since(time, tw.start)) }

// End is where the move ends.
func (tw Tween[T]) End() T { return tw.to }

// EndTime is when the move is over.
func (tw Tween[T]) EndTime() Nanos { return tw.start + uint64(max(tw.curve.Duration, 0)) }

// since is time − start, or 0 before the start.
func since(time, start Nanos) Nanos {
	if time < start {
		return 0
	}
	return time - start
}

// Pose is how a whole panel is shown: its opacity and its offset from
// where it rests, in logical pixels.
type Pose struct {
	// Opacity multiplies every colour's alpha (0..1).
	Opacity float32
	// Offset is added to every position (logical pixels; +y is down).
	Offset [2]float32
}

// Rest is fully opaque, in place.
var Rest = Pose{Opacity: 1}

// IsRest reports whether this is the rest pose (drawing needs no
// transform).
func (p Pose) IsRest() bool { return p == Rest }

// Lerp moves every part of the pose toward to's.
func (p Pose) Lerp(to Pose, t float32) Pose {
	lerp := func(a, b float32) float32 { return float32(Value(a).Lerp(Value(b), t)) }
	return Pose{
		Opacity: lerp(p.Opacity, to.Opacity),
		Offset:  [2]float32{lerp(p.Offset[0], to.Offset[0]), lerp(p.Offset[1], to.Offset[1])},
	}
}

// Place is a rectangle (left, top, width, height) moved by the offset.
func (p Pose) Place(r [4]float32) [4]float32 {
	return [4]float32{r[0] + p.Offset[0], r[1] + p.Offset[1], r[2], r[3]}
}
