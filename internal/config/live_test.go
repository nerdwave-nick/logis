package config

import (
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// The config while the daemon runs (ported from Rust src/config/live.rs;
// the theme half arrives with the theme engine, G18).

func withIdle(d time.Duration) Config {
	c := Default()
	c.IdleTimeout = d
	return c
}

func heresy() error {
	return &Error{Path: "/cfg/logis.jsonc", Problem: Invalid,
		Err: &jsonc.Invalid{Line: 2, Column: 8, Message: "unknown field `thme`"}}
}

func TestWithoutTheFlagTheConfigSetsTheIdleTimeout(t *testing.T) {
	if got := NewLive(withIdle(10 * time.Second)).IdleTimeout(); got != 10*time.Second {
		t.Error(got)
	}
	if got := NewLive(Default()).IdleTimeout(); got != 0 {
		t.Error(got)
	}
}

func TestTheFlagOutranksTheConfig(t *testing.T) {
	if got := NewLive(withIdle(10 * time.Second)).WithIdleFlag(5 * time.Second).IdleTimeout(); got != 5*time.Second {
		t.Error(got)
	}
	if got := NewLive(Default()).WithIdleFlag(5 * time.Second).IdleTimeout(); got != 5*time.Second {
		t.Error(got)
	}
}

func TestAFlagOfZeroKeepsTheDaemonResidentWhateverTheConfigSays(t *testing.T) {
	live := NewLive(withIdle(10 * time.Second)).WithIdleFlag(0)
	if got := live.IdleTimeout(); got != 0 {
		t.Error(got)
	}
	live.Reload(withIdle(3*time.Second), nil)
	if got := live.IdleTimeout(); got != 0 {
		t.Error(got)
	}
}

func TestASoundEditIsApplied(t *testing.T) {
	live := NewLive(Default())
	if got := live.Reload(withIdle(3*time.Second), nil); got != Applied {
		t.Error(got)
	}
	if !live.Config().Equal(withIdle(3*time.Second)) || live.IdleTimeout() != 3*time.Second {
		t.Errorf("%+v", live.Config())
	}
}

func TestTheSameConfigAgainChangesNothing(t *testing.T) {
	live := NewLive(withIdle(3 * time.Second))
	if got := live.Reload(withIdle(3*time.Second), nil); got != Unchanged {
		t.Error(got)
	}
}

func TestAnInvalidEditIsRejectedAndTheLastSoundConfigStays(t *testing.T) {
	live := NewLive(withIdle(3 * time.Second))
	if got := live.Reload(Default(), heresy()); got != Rejected {
		t.Error(got)
	}
	if !strings.Contains(heresy().Error(), "/cfg/logis.jsonc:2:8: unknown field `thme`") {
		t.Error(heresy())
	}
	if !live.Config().Equal(withIdle(3*time.Second)) || live.IdleTimeout() != 3*time.Second {
		t.Errorf("%+v", live.Config())
	}
}

func TestAfterARejectionTheOldConfigIsRestoredAndANewOneApplied(t *testing.T) {
	live := NewLive(withIdle(3 * time.Second))
	live.Reload(Default(), heresy())
	if got := live.Reload(withIdle(3*time.Second), nil); got != Restored {
		t.Error(got)
	}
	if got := live.Reload(withIdle(3*time.Second), nil); got != Unchanged {
		t.Error(got)
	}
	live.Reload(Default(), heresy())
	keep := withIdle(3 * time.Second)
	keep.FocusLoss = FocusKeep
	if got := live.Reload(keep, nil); got != Applied || !live.Config().Equal(keep) {
		t.Errorf("%v %+v", got, live.Config())
	}
}

func TestReloadsNameThemselves(t *testing.T) {
	for r, name := range map[Reload]string{Applied: "applied", Restored: "restored",
		Unchanged: "unchanged", Rejected: "rejected"} {
		if r.String() != name {
			t.Errorf("%d: %s", r, r)
		}
	}
}
