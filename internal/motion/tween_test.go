package motion

import "testing"

// Ported from Rust src/motion/tween/tests.rs.

const t0 Nanos = 1_000 * ms

var linear100 = Millis(100, Linear)

func TestATweenMovesFromItsStartToItsEnd(t *testing.T) {
	tw := NewTween(Value(10), 20, linear100, t0)
	if tw.At(t0-5*ms) != 10 || tw.At(t0) != 10 || !near(float32(tw.At(t0+50*ms)), 15, 1e-4) ||
		tw.At(t0+100*ms) != 20 || tw.At(t0+900*ms) != 20 {
		t.Errorf("%v %v %v %v", tw.At(t0-5*ms), tw.At(t0+50*ms), tw.At(t0+100*ms), tw.At(t0+900*ms))
	}
	if tw.Done(t0+99*ms) || !tw.Done(t0+100*ms) || tw.EndTime() != t0+100*ms {
		t.Error("done at the wrong time")
	}
}

func TestAFinishedTweenRestsExactlyOnItsEnd(t *testing.T) {
	from := Pose{Opacity: 0.4, Offset: [2]float32{0, 10}}
	tw := NewTween(from, Rest, Millis(150, EaseOut), t0)
	if !tw.At(t0 + 150*ms).IsRest() {
		t.Errorf("%+v", tw.At(t0+150*ms))
	}
}

func TestOpeningStartsWithTheFirstFrameOneIntervalIn(t *testing.T) {
	tr := Opening(Default().OpenFrom, linear100)
	if !tr.Moving(0) {
		t.Fatal("an unstarted open will not move")
	}
	// The first frame is shown at T0; the open began one 10 ms frame earlier.
	first := tr.Pose(t0, 10*ms)
	tr.Presented(t0, 10*ms)
	if !near(first.Opacity, 0.4+0.6*0.1, 1e-4) || !near(first.Offset[1], 9, 1e-4) {
		t.Fatalf("%+v", first)
	}
	if !tr.Moving(t0) {
		t.Fatal("not moving after the first frame")
	}
	later := tr.Pose(t0+50*ms, 10*ms)
	if later.Opacity <= first.Opacity || later.Offset[1] >= first.Offset[1] {
		t.Fatalf("%+v after %+v", later, first)
	}
	if got := tr.Pose(t0+90*ms, 10*ms); got != Rest {
		t.Fatalf("%+v", got)
	}
	if tr.Moving(t0 + 90*ms) {
		t.Error("at rest, yet more to draw")
	}
	if tr.Finished(t0 + 90*ms) {
		t.Error("an open panel went")
	}
}

func TestASlowFirstFrameDoesNotEatIntoTheOpen(t *testing.T) {
	tr := Opening(Pose{}, linear100)
	if first := tr.Pose(t0, 10*ms); !near(first.Opacity, 0.1, 1e-4) {
		t.Fatalf("%+v", first)
	}
	// The first frame took 80 ms to reach the screen: the open starts there.
	tr.Presented(t0+80*ms, 10*ms)
	if second := tr.Pose(t0+90*ms, 10*ms); !near(second.Opacity, 0.2, 1e-4) {
		t.Fatalf("%+v", second)
	}
	if !tr.Moving(t0 + 150*ms) {
		t.Error("stopped early")
	}
	if got := tr.Pose(t0+170*ms, 10*ms); got != Rest {
		t.Errorf("%+v", got)
	}
}

func TestUntilItIsPresentedEveryFrameShowsTheOpenOneIntervalIn(t *testing.T) {
	tr := Opening(Pose{}, linear100)
	if a, b := tr.Pose(t0, 10*ms), tr.Pose(t0+50*ms, 10*ms); a != b {
		t.Errorf("%+v, %+v", a, b)
	}
	if !tr.Moving(t0 + 500*ms) {
		t.Error("an unpresented open stopped")
	}
}

func TestARestingPanelNeverMoves(t *testing.T) {
	tr := Resting()
	if tr.Moving(t0) || tr.Pose(t0, 10*ms) != Rest {
		t.Error("a resting panel moved")
	}
}

func TestClosingRunsFromTheLastPoseToTheClosedPoseAndFinishes(t *testing.T) {
	tr := Resting()
	tr.Pose(t0, 0)
	to := Pose{Opacity: 0, Offset: [2]float32{0, 6}}
	tr.Close(to, linear100, t0+10*ms)
	if !tr.IsClosing() || !tr.Moving(t0+10*ms) {
		t.Fatal("not closing")
	}
	if mid := tr.Pose(t0+60*ms, 0); !near(mid.Opacity, 0.5, 1e-4) || !near(mid.Offset[1], 3, 1e-4) {
		t.Fatalf("%+v", mid)
	}
	if tr.Finished(t0 + 60*ms) {
		t.Fatal("finished halfway")
	}
	if end, ok := tr.FinishTime(); !ok || end != t0+110*ms {
		t.Fatalf("%v %v", end, ok)
	}
	if !tr.Finished(t0+110*ms) || tr.Pose(t0+110*ms, 0) != to {
		t.Error("the close did not end in its pose")
	}
}

func TestClosingWhileOpeningStartsFromWhereThePanelIs(t *testing.T) {
	tr := Opening(Pose{Opacity: 0.4, Offset: [2]float32{0, 10}}, linear100)
	tr.Pose(t0, 0)
	tr.Presented(t0, 0)
	half := tr.Pose(t0+50*ms, 0)
	if !tr.Moving(t0 + 50*ms) {
		t.Fatal("not moving")
	}
	tr.Close(Rest, linear100, t0+50*ms)
	if got := tr.Pose(t0+50*ms, 0); got != half {
		t.Errorf("jumped from %+v to %+v", half, got)
	}
}

func TestAnInstantCloseFinishesAtOnce(t *testing.T) {
	tr := Resting()
	tr.Close(Rest, Instant, t0)
	if !tr.Finished(t0) {
		t.Error("an instant close lingers")
	}
}

type glided struct {
	at     float32
	ok     bool
	moving bool
}

func toward(g *Glide, target float32, time, lead Nanos) glided {
	at, ok, moving := g.Toward(target, true, time, lead)
	return glided{at, ok, moving}
}

func TestTheFirstTargetOfAGlideIsTakenAtOnce(t *testing.T) {
	g := NewGlide(linear100)
	if got := toward(&g, 40, t0, 10*ms); got != (glided{40, true, false}) {
		t.Errorf("%+v", got)
	}
	if got := toward(&g, 40, t0+ms, 10*ms); got != (glided{40, true, false}) {
		t.Errorf("%+v", got)
	}
}

func TestANewTargetGlidesThereStartingOneIntervalBack(t *testing.T) {
	g := NewGlide(linear100)
	toward(&g, 0, t0, 10*ms)
	got := toward(&g, 100, t0+200*ms, 10*ms)
	if !got.moving || !near(got.at, 10, 1e-3) {
		t.Fatalf("%+v: not already moving", got)
	}
	if got := toward(&g, 100, t0+240*ms, 10*ms); !near(got.at, 50, 1e-3) {
		t.Fatalf("%+v", got)
	}
	if got := toward(&g, 100, t0+290*ms, 10*ms); got != (glided{100, true, false}) {
		t.Fatalf("%+v", got)
	}
	if got := toward(&g, 100, t0+400*ms, 10*ms); got != (glided{100, true, false}) {
		t.Fatalf("%+v", got)
	}
}

func TestRetargetingMidGlideContinuesFromWhereTheBandIs(t *testing.T) {
	g := NewGlide(linear100)
	toward(&g, 0, t0, 0)
	toward(&g, 100, t0, 0)
	if got := toward(&g, 100, t0+50*ms, 0); !near(got.at, 50, 1e-3) {
		t.Fatalf("%+v", got)
	}
	if got := toward(&g, 0, t0+50*ms, 0); !got.moving || !near(got.at, 50, 1e-3) {
		t.Fatalf("%+v: jumped", got)
	}
}

func TestNoTargetResetsTheGlide(t *testing.T) {
	g := NewGlide(linear100)
	toward(&g, 0, t0, 0)
	if at, ok, moving := g.Toward(0, false, t0+ms, 0); ok || moving || at != 0 {
		t.Fatalf("%v %v %v", at, ok, moving)
	}
	if got := toward(&g, 80, t0+2*ms, 0); got != (glided{80, true, false}) {
		t.Fatalf("%+v", got)
	}
}

func TestAnInstantGlideJumps(t *testing.T) {
	g := NewGlide(Instant)
	toward(&g, 0, t0, 0)
	if got := toward(&g, 52, t0+ms, 0); got != (glided{52, true, false}) {
		t.Fatalf("%+v", got)
	}
}

func TestAResetGlideTakesItsNextTargetAtOnce(t *testing.T) {
	g := NewGlide(linear100)
	toward(&g, 0, t0, 0)
	toward(&g, 100, t0, 0)
	g.Reset()
	if got := toward(&g, 52, t0+10*ms, 0); got != (glided{52, true, false}) {
		t.Fatalf("%+v", got)
	}
}
