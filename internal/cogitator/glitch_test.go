package cogitator

import (
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/motion"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The glitch as the window wears it (33cfcf44): each shown panel keeps its
// glitch clock from its first frame, which fires the open burst; a tab
// switch fires the tab burst (the hook 8cba2fc9 calls); the panel draws
// while the glitch moves and sleeps until its next episode or random
// burst; reduce-motion leaves no glitch.

// glitchRig wears a stilled theme with a glitch that does not move at
// rest: only its bursts (and episodes, if set) move.
func glitchRig(set func(*effects.Glitch)) *movingRig {
	r := newMovingRig()
	th := theme.Default().Stilled()
	g := theme.Default().Glitch
	g.Wobble.Rest, g.Tracking.Px, g.Static.Rest, g.LinePulse.Rest, g.NoiseBand.Rest = 0, 0, 0, 0, 0
	g.Episodes.PeriodS, g.Bursts.RandomPerMinute, g.Bursts.Stutter = 0, 0, 0
	set(&g)
	th.Glitch = g
	r.ui.theme = &th
	r.ui.random = func() float64 { return 0.5 }
	return r
}

func TestThePanelOpensWithAGlitchBurst(t *testing.T) {
	r := glitchRig(func(g *effects.Glitch) { g.Bursts.OpenMS = 400 })
	r.show(1, list.DefaultOptions(), "alpha")
	sc, d := r.frame(1000, motion.Rest)
	if !sc.Post.Glitch.On() || sc.Post.Moment != (effects.Moment{}) || !d.Moving {
		t.Fatalf("the first frame: %+v moving %v", sc.Post.Moment, d.Moving)
	}
	sc, d = r.frame(1200, motion.Rest)
	if sc.Post.Moment.Event != 1 || sc.Post.Moment.Time != 0.2 || !d.Moving {
		t.Errorf("mid burst: %+v moving %v", sc.Post.Moment, d.Moving)
	}
	sc, d = r.frame(1400, motion.Rest)
	if sc.Post.Moment.Event != 0 || d.Moving {
		t.Errorf("after the burst: %+v moving %v", sc.Post.Moment, d.Moving)
	}
	// The next panel opens with a burst of its own.
	r.show(2, list.DefaultOptions(), "beta")
	r.frame(5000, motion.Rest)
	if sc, _ := r.frame(5200, motion.Rest); sc.Post.Moment.Event != 1 || sc.Post.Moment.Time != 0.2 {
		t.Errorf("the next panel: %+v", sc.Post.Moment)
	}
}

func TestATabSwitchFiresTheTabBurst(t *testing.T) {
	r := glitchRig(func(g *effects.Glitch) { g.Bursts.OpenMS, g.Bursts.TabMS = 0, 240 })
	now := uint64(0)
	r.ui.clock = func() uint64 { return now }
	r.show(1, list.DefaultOptions(), "alpha")
	if _, d := r.frame(1000, motion.Rest); d.Moving {
		t.Fatal("moving without an open burst")
	}
	redraws := r.surf.redraws
	now = 2000 * msNS
	r.ui.glitchBurst(effects.EventTab)
	if r.surf.redraws != redraws+1 {
		t.Error("the tab burst asked for no frame")
	}
	if sc, d := r.frame(2120, motion.Rest); sc.Post.Moment.Event != 1 || !d.Moving {
		t.Errorf("mid tab burst: %+v moving %v", sc.Post.Moment, d.Moving)
	}
	r.ui.hide(1)
	r.ui.glitchBurst(effects.EventTab) // no panel: nothing to fire
}

// Between ambient episodes nothing moves: the window sleeps until the
// next one and then asks for one frame, which keeps drawing through the
// episode.
func TestTheWindowWakesForTheNextEpisode(t *testing.T) {
	r := glitchRig(func(g *effects.Glitch) {
		g.Bursts.On = 0
		g.Episodes = effects.Episodes{PeriodS: 9, Duty: 0.1}
	})
	now := uint64(1000 * msNS)
	wall := time.Unix(100, 0)
	r.ui.clock = func() uint64 { return now }
	r.ui.now = func() time.Time { return wall }
	r.show(1, list.DefaultOptions(), "alpha")
	if _, d := r.frame(1000, motion.Rest); d.Moving {
		t.Fatal("moving between episodes")
	}
	at, ok := r.ui.deadline()
	if want := wall.Add(8100 * time.Millisecond); !ok || !at.Equal(want) {
		t.Fatalf("deadline %v %v, want %v", at, ok, want)
	}
	redraws := r.surf.redraws
	r.ui.wake()
	if r.surf.redraws != redraws {
		t.Fatal("woken early")
	}
	now, wall = 9100*msNS, wall.Add(8100*time.Millisecond)
	r.ui.wake()
	if r.surf.redraws != redraws+1 {
		t.Fatal("not woken for the episode")
	}
	if _, ok := r.ui.deadline(); ok {
		t.Error("the same wake is due again before its frame")
	}
	if _, d := r.frame(9500, motion.Rest); !d.Moving {
		t.Error("the episode does not keep the window drawing")
	}
	now = 10_100 * msNS
	if _, d := r.frame(10_100, motion.Rest); d.Moving {
		t.Error("still drawing after the episode")
	}
	if at, ok := r.ui.deadline(); !ok || !at.Equal(wall.Add(8*time.Second)) {
		t.Errorf("the next episode: %v %v", at, ok)
	}
}

// Reduce-motion (a stilled theme) leaves no glitch: no pass, no burst, no
// wake.
func TestReduceMotionLeavesNoGlitch(t *testing.T) {
	r := newMovingRig()
	r.ui.clock = func() uint64 { return 1000 * msNS }
	r.show(1, list.DefaultOptions(), "alpha")
	sc, d := r.frame(1000, motion.Rest)
	if sc.Post.Glitch.On() || sc.Post.Moment != (effects.Moment{}) || d.Moving {
		t.Errorf("%+v moving %v", sc.Post, d.Moving)
	}
	if _, ok := r.ui.deadline(); ok {
		t.Error("a wake for a glitch that is off")
	}
}
