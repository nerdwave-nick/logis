package daemon

import (
	"slices"
	"testing"
)

const (
	dmenu       = false
	interactive = true
)

func effects(e ...effect) []effect { return e }

func wantEffects(t *testing.T, what string, got, want []effect) {
	t.Helper()
	if !slices.Equal(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("%s: %v, want %v", what, got, want)
	}
}

// Ported from Rust src/daemon/sessions.rs the_first_session_is_shown_at_once.
func TestTheFirstSessionIsShownAtOnce(t *testing.T) {
	var s sessions
	wantEffects(t, "open 1", s.open(1, dmenu), effects(effect{show, 1}))
	if v, ok := s.visible(); !ok || v != 1 {
		t.Errorf("visible %d %v", v, ok)
	}
}

// Ported from dmenu_sessions_queue_and_are_shown_first_in_first_out.
func TestDmenuSessionsQueueAndAreShownFirstInFirstOut(t *testing.T) {
	var s sessions
	s.open(1, dmenu)
	wantEffects(t, "open 2", s.open(2, dmenu), effects(effect{queue, 2}))
	wantEffects(t, "open 3", s.open(3, dmenu), effects(effect{queue, 3}))
	wantEffects(t, "close 1", s.close(1), effects(effect{hide, 1}, effect{show, 2}))
	wantEffects(t, "close 2", s.close(2), effects(effect{hide, 2}, effect{show, 3}))
	wantEffects(t, "close 3", s.close(3), effects(effect{hide, 3}))
	if _, ok := s.visible(); ok {
		t.Error("a session is still visible")
	}
}

// Ported from a_dmenu_session_queues_behind_an_interactive_one.
func TestADmenuSessionQueuesBehindAnInteractiveOne(t *testing.T) {
	var s sessions
	s.open(1, interactive)
	wantEffects(t, "open 2", s.open(2, dmenu), effects(effect{queue, 2}))
	wantEffects(t, "close 1", s.close(1), effects(effect{hide, 1}, effect{show, 2}))
}

// Ported from an_interactive_session_replaces_the_visible_one_and_keeps_the_queue.
func TestAnInteractiveSessionReplacesTheVisibleOneAndKeepsTheQueue(t *testing.T) {
	var s sessions
	s.open(1, dmenu)
	s.open(2, dmenu)
	wantEffects(t, "open 3", s.open(3, interactive),
		effects(effect{hide, 1}, effect{replaced, 1}, effect{show, 3}))
	wantEffects(t, "open 4", s.open(4, interactive),
		effects(effect{hide, 3}, effect{replaced, 3}, effect{show, 4}))
	wantEffects(t, "close 4", s.close(4), effects(effect{hide, 4}, effect{show, 2}))
}

// Ported from closing_a_queued_session_drops_it_silently.
func TestClosingAQueuedSessionDropsItSilently(t *testing.T) {
	var s sessions
	s.open(1, dmenu)
	s.open(2, dmenu)
	s.open(3, dmenu)
	wantEffects(t, "close 2", s.close(2), nil)
	wantEffects(t, "close 1", s.close(1), effects(effect{hide, 1}, effect{show, 3}))
}

// Ported from closing_an_unknown_or_replaced_session_does_nothing.
func TestClosingAnUnknownOrReplacedSessionDoesNothing(t *testing.T) {
	var s sessions
	wantEffects(t, "close 9", s.close(9), nil)
	s.open(1, interactive)
	s.open(2, interactive)
	wantEffects(t, "close 1", s.close(1), nil)
	if v, ok := s.visible(); !ok || v != 2 {
		t.Errorf("visible %d %v, want 2", v, ok)
	}
}
