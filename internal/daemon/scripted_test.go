package daemon

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// hostLog records what reaches a Host.
type hostLog struct {
	answers []answered
	tokens  []string
}

type answered struct {
	id     SessionID
	answer Answer
}

func (h *hostLog) Answer(id SessionID, a Answer) { h.answers = append(h.answers, answered{id, a}) }
func (h *hostLog) Fail(SessionID, string)        {}
func (h *hostLog) Exit(int, string)              {}
func (h *hostLog) Activated(id SessionID, token string) {
	h.tokens = append(h.tokens, fmt.Sprintf("%d %s", id, token))
}

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

func entries(texts ...string) []list.Entry {
	var out []list.Entry
	for _, t := range texts {
		out = append(out, list.Entry{Text: t})
	}
	return out
}

func appended(texts ...string) provider.Step {
	return provider.Step{Kind: provider.StepAppend, Entries: entries(texts...)}
}

func wantAnswers(t *testing.T, h *hostLog, want ...answered) {
	t.Helper()
	if got := h.take(); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("answers %+v, want %+v", got, want)
	}
}

// A dmenu session is answered at its end with the first marked item.
func TestADmenuSessionIsAnsweredAtItsEndWithTheFirstMarkedItem(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 4, Mode: protocol.ModeDmenu, Entries: entries("a")})
	r.Step(4, appended("b", ">c", ">d"))
	wantAnswers(t, h) // the client still streams
	r.End(4)
	wantAnswers(t, h, answered{4, Answer{Kind: AnswerItem, Index: 2}})
}

// Ported from without_a_marked_item_the_dmenu_session_is_dismissed.
func TestWithoutAMarkedItemTheDmenuSessionIsDismissed(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 2, Mode: protocol.ModeDmenu, Entries: entries("a", "b")})
	r.End(2)
	wantAnswers(t, h, answered{2, Answer{Kind: AnswerDismissed}})
}

// Ported from a_hidden_session_is_never_answered.
func TestAHiddenSessionIsNeverAnswered(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 1, Mode: protocol.ModeDmenu, Entries: entries(">a")})
	r.Hide(1)
	r.End(1)
	r.Step(1, appended(">b"))
	wantAnswers(t, h)
}

// Ported from servitor_sessions_are_left_open.
func TestServitorSessionsAreLeftOpen(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 2, Mode: protocol.ModeServitor, Entries: entries(">a")})
	r.End(2)
	wantAnswers(t, h)
}

// App sessions are answered as provider stages are (an app whose name
// starts with the mark), and get a stand-in token at once.
func TestAnAppSessionIsAnsweredAndGetsAStandInToken(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 2, Mode: protocol.ModeApps, Options: list.DefaultOptions()})
	wantAnswers(t, h) // the census is still being taken
	o := list.DefaultOptions()
	o.OnlyMatch = true
	r.Step(2, provider.Step{Kind: provider.StepStage, Options: o, Entries: entries("Oil", ">Rite")})
	wantAnswers(t, h, answered{2, Answer{Kind: AnswerItem, Index: 1}})
	r.Activation(2, "forge-rite")
	if !reflect.DeepEqual(h.tokens, []string{"2 scripted-2"}) {
		t.Errorf("tokens %q", h.tokens)
	}
}

// Ported from a_provider_stage_is_answered_once_a_marked_item_settles.
func TestAProviderStageIsAnsweredOnceAMarkedItemSettles(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	pending := list.Entry{Text: ">oil", Decor: &list.Decor{Pending: true}}
	r.Show(Session{ID: 3, Mode: protocol.ModeProvider, Options: list.DefaultOptions(), Entries: []list.Entry{pending}})
	wantAnswers(t, h) // pending is never chosen
	r.Step(3, provider.Step{Kind: provider.StepUpdate, Updates: []provider.Update{{Index: 0, Entry: list.Entry{Text: ">oil"}}}})
	wantAnswers(t, h, answered{3, Answer{Kind: AnswerItem, Index: 0}})
	r.Step(3, appended(">more"))
	wantAnswers(t, h) // it waits for the provider
	typed := list.DefaultOptions()
	typed.Placeholder = ">holy oil"
	r.Step(3, provider.Step{Kind: provider.StepStage, Options: typed})
	wantAnswers(t, h, answered{3, Answer{Kind: AnswerTyped, Text: "holy oil"}})
	r.Step(3, provider.Step{Kind: provider.StepReady})
	wantAnswers(t, h, answered{3, Answer{Kind: AnswerTyped, Text: "holy oil"}})
	r.Step(3, provider.Step{Kind: provider.StepRemove, Removed: []uint32{0}})
	only := list.DefaultOptions()
	only.OnlyMatch, only.Placeholder = true, ">never typed"
	r.Step(3, provider.Step{Kind: provider.StepStage, Options: only, Entries: entries("plain")})
	wantAnswers(t, h)
}

// A session shown after its client already ended is answered too.
func TestASessionShownEndedIsAnsweredAtItsEnd(t *testing.T) {
	h := &hostLog{}
	r := NewScripted(h)
	r.Show(Session{ID: 7, Mode: protocol.ModeDmenu, Entries: entries("x", ">y")})
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
