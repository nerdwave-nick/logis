package motion

import (
	"strings"
	"testing"
	"time"
)

// Ported from Rust src/motion/mod.rs.

const ms Nanos = 1_000_000

func TestACurveProgressesFrom0To1OverItsDuration(t *testing.T) {
	c := Millis(100, Linear)
	if c.Progress(0) != 0 || !near(c.Progress(25*ms), 0.25, 1e-6) || c.Progress(100*ms) != 1 || c.Progress(500*ms) != 1 {
		t.Errorf("%v %v %v %v", c.Progress(0), c.Progress(25*ms), c.Progress(100*ms), c.Progress(500*ms))
	}
	if c.Done(99*ms) || !c.Done(100*ms) {
		t.Error("done at the wrong time")
	}
}

func TestAnInstantCurveIsAlwaysDone(t *testing.T) {
	if Instant.Progress(0) != 1 || !Instant.Done(0) {
		t.Error("an instant curve has a course to run")
	}
}

func TestAThemesTimingsKeepThePosesAndCurves(t *testing.T) {
	m := Timed(200*time.Millisecond, 0, 60*time.Millisecond)
	d := Default()
	if m.Open.Duration != 200*time.Millisecond || m.Close.Duration != 0 || m.Selection.Duration != 60*time.Millisecond {
		t.Errorf("%+v", m)
	}
	if m.Open.Easing != d.Open.Easing || m.OpenFrom != d.OpenFrom {
		t.Errorf("%+v", m)
	}
	if Timed(d.Open.Duration, d.Close.Duration, d.Selection.Duration) != d {
		t.Error("the default's own timings make another motion")
	}
}

func TestTheDefaultMovesAndOffDoesNot(t *testing.T) {
	if Default().IsOff() || !Off.IsOff() || !Default().Slowed(0).IsOff() {
		t.Error("wrong")
	}
}

func TestSlowingMultipliesEveryDurationAndKeepsThePoses(t *testing.T) {
	slow := Default().Slowed(4)
	if slow.Open.Duration != 600*time.Millisecond || slow.Close.Duration != 440*time.Millisecond ||
		slow.Selection.Duration != 360*time.Millisecond {
		t.Errorf("%+v", slow)
	}
	if slow.OpenFrom != Default().OpenFrom {
		t.Errorf("%+v", slow.OpenFrom)
	}
}

func TestTheEnvironmentTurnsMotionOffOrSlowsIt(t *testing.T) {
	for value, want := range map[string]Motion{"": Default(), "off": Off, "0": Off, "2": Default().Slowed(2)} {
		got, err := FromEnvValue(value)
		if err != nil || got != want {
			t.Errorf("%q: %+v, %v", value, got, err)
		}
	}
	for _, bad := range []string{"fast", "-1", "inf", "NaN", "1000", "0x2"} {
		_, err := FromEnvValue(bad)
		if err == nil || !strings.Contains(err.Error(), "LOGIS_MOTION") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	_, err := FromEnvValue("fast")
	if want := "LOGIS_MOTION=fast is no rite of motion: use 'off' or a factor such as 4"; err == nil || err.Error() != want {
		t.Errorf("%v, want %q", err, want)
	}
}

func TestTheDefaultOpenStartsVisiblyAndEndsAtRest(t *testing.T) {
	m := Default()
	if m.OpenFrom.Opacity < 0.3 {
		t.Error("the first frame does not show the panel")
	}
	if m.CloseTo.Opacity != 0 {
		t.Error("a closed panel is not gone")
	}
}
