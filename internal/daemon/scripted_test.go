package daemon

import (
	"reflect"
	"testing"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// hostLog records what reaches a Host.
type hostLog struct {
	answers []answered
}

type answered struct {
	id     SessionID
	answer Answer
}

func (h *hostLog) Answer(id SessionID, a Answer) { h.answers = append(h.answers, answered{id, a}) }
func (h *hostLog) Fail(SessionID, string)        {}
func (h *hostLog) Exit(int, string)              {}

func (h *hostLog) take() []answered {
	a := h.answers
	h.answers = nil
	return a
}

func plain(texts ...string) []protocol.Item {
	var items []protocol.Item
	for _, t := range texts {
		items = append(items, protocol.Item{Text: t, Raw: []byte(t)})
	}
	return items
}

func wantAnswers(t *testing.T, h *hostLog, want ...answered) {
	t.Helper()
	if got := h.take(); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("answers %+v, want %+v", got, want)
	}
}

// Ported from Rust src/daemon/resolver.rs a_dmenu_session_is_answered_at_its_end_with_the_first_marked_item.
func TestADmenuSessionIsAnsweredAtItsEndWithTheFirstMarkedItem(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 4, Mode: protocol.ModeDmenu, Items: plain("a")})
	r.Append(4, plain("b", ">c", ">d"))
	wantAnswers(t, h) // the client still streams
	r.End(4)
	wantAnswers(t, h, answered{4, Answer{Kind: AnswerItem, Index: 2}})
}

// Ported from without_a_marked_item_the_dmenu_session_is_dismissed.
func TestWithoutAMarkedItemTheDmenuSessionIsDismissed(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 2, Mode: protocol.ModeDmenu, Items: plain("a", "b")})
	r.End(2)
	wantAnswers(t, h, answered{2, Answer{Kind: AnswerDismissed}})
}

// Ported from a_hidden_session_is_never_answered.
func TestAHiddenSessionIsNeverAnswered(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 1, Mode: protocol.ModeDmenu, Items: plain(">a")})
	r.Hide(1)
	r.End(1)
	r.Append(1, plain(">b"))
	wantAnswers(t, h)
}

// Ported from servitor_sessions_are_left_open: no interactive session is
// answered by script until its feature brings items to answer.
func TestInteractiveSessionsAreLeftOpen(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	for _, mode := range []protocol.Mode{protocol.ModeServitor, protocol.ModeApps, protocol.ModeProvider} {
		r.Show(Session{ID: 2, Mode: mode, Items: plain(">a")})
		r.End(2)
		wantAnswers(t, h)
	}
}

// A session shown after its client already ended is answered too.
func TestASessionShownEndedIsAnsweredAtItsEnd(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 7, Mode: protocol.ModeDmenu, Items: plain("x", ">y")})
	r.End(7)
	r.End(7)
	wantAnswers(t, h, answered{7, Answer{Kind: AnswerItem, Index: 1}})
}

// Ported from windowless_refuses_every_mode.
func TestWindowlessRefusesEveryMode(t *testing.T) {
	for _, mode := range []protocol.Mode{protocol.ModeDmenu, protocol.ModeApps,
		protocol.ModeServitor, protocol.ModeProvider} {
		if err := (Windowless{}).Accepts(mode); err == nil {
			t.Errorf("%s accepted", mode)
		}
	}
}
