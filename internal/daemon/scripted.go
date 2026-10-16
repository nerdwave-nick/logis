package daemon

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// Windowless refuses every session: the daemon's resolver until the
// cogitator window exists (feat/logis-go G3).
type Windowless struct{}

// Accepts refuses every mode.
func (Windowless) Accepts(protocol.Mode) error {
	return errors.New("the cogitator has no window to manifest in yet; " +
		"this machine spirit can only listen")
}

func (Windowless) Show(Session)                  {}
func (Windowless) Step(SessionID, provider.Step) {}
func (Windowless) End(SessionID)                 {}
func (Windowless) Hide(SessionID)                {}

// ScriptedMark is the prefix that marks the item Scripted chooses.
const ScriptedMark = ">"

// Scripted answers sessions without a user, for tests and measurements
// (LOGIS_RESOLVER=scripted; Rust src/daemon/resolver.rs Scripted),
// choosing the first item whose text starts with ScriptedMark:
//
//   - a dmenu session once its client has sent every item, or dismissed
//     when none is marked;
//   - a provider's stage as soon as a marked item is there and settled (a
//     pending one never), or, in a stage that takes typed text and has a
//     placeholder starting with the mark, the rest of that placeholder as
//     typed text. After an answer it waits for the provider's next stage
//     or ready, as the window does.
//
// App sessions are answered as provider stages are (an app whose name
// starts with the mark), and get a stand-in activation token,
// scripted-<session>, at once. Servitor sessions are never answered; they
// end by replacement, cancel or disconnect.
type Scripted struct {
	host    Host
	visible *scriptedSession
}

type scriptedSession struct {
	id   SessionID
	mode protocol.Mode
	// typed is what to type into a stage that takes typed text ("" with
	// hasTyped false: nothing).
	typed    string
	hasTyped bool
	entries  []list.Entry
	// waiting: an answer went out; wait for the next stage or ready.
	waiting bool
}

// NewScripted answers through host.
func NewScripted(host Host) *Scripted { return &Scripted{host: host} }

// Accepts every mode.
func (s *Scripted) Accepts(protocol.Mode) error { return nil }

// Show remembers the session and answers a provider's stage if it can.
func (s *Scripted) Show(session Session) {
	v := &scriptedSession{id: session.ID, mode: session.Mode, entries: session.Entries}
	v.typed, v.hasTyped = typedFor(session.Options)
	s.visible = v
	s.tryAnswer()
}

// Step follows the provider's steps.
func (s *Scripted) Step(id SessionID, step provider.Step) {
	v := s.current(id)
	if v == nil {
		return
	}
	switch step.Kind {
	case provider.StepStage:
		v.typed, v.hasTyped = typedFor(step.Options)
		v.entries, v.waiting = step.Entries, false
	case provider.StepAppend:
		v.entries = append(v.entries, step.Entries...)
	case provider.StepUpdate:
		for _, u := range step.Updates {
			if int(u.Index) < len(v.entries) {
				v.entries[u.Index] = u.Entry
			}
		}
	case provider.StepRemove:
		for _, i := range step.Removed {
			if int(i) < len(v.entries) {
				v.entries[i] = list.Entry{}
			}
		}
	case provider.StepReady:
		v.waiting = false
	}
	s.tryAnswer()
}

// End answers a dmenu session, once.
func (s *Scripted) End(id SessionID) {
	v := s.current(id)
	if v == nil || v.mode != protocol.ModeDmenu || v.waiting {
		return
	}
	v.waiting = true
	answer := Answer{Kind: AnswerDismissed}
	if i := marked(v.entries, false); i >= 0 {
		answer = Answer{Kind: AnswerItem, Index: i}
	}
	s.host.Answer(id, answer)
}

// Hide forgets the session.
func (s *Scripted) Hide(id SessionID) {
	if s.current(id) != nil {
		s.visible = nil
	}
}

func (s *Scripted) current(id SessionID) *scriptedSession {
	if s.visible == nil || s.visible.id != id {
		return nil
	}
	return s.visible
}

// tryAnswer answers the visible provider stage if it can be answered now.
func (s *Scripted) tryAnswer() {
	v := s.visible
	if v == nil || v.mode != protocol.ModeProvider && v.mode != protocol.ModeApps || v.waiting {
		return
	}
	var answer Answer
	if i := marked(v.entries, true); i >= 0 {
		answer = Answer{Kind: AnswerItem, Index: i}
	} else if v.hasTyped {
		answer = Answer{Kind: AnswerTyped, Text: v.typed}
	} else {
		return
	}
	v.waiting = true
	s.host.Answer(v.id, answer)
}

// marked is the first marked entry (settled only, if asked), or -1.
func marked(entries []list.Entry, settled bool) int {
	for i, e := range entries {
		if strings.HasPrefix(e.Text, ScriptedMark) && (!settled || !e.IsPending()) {
			return i
		}
	}
	return -1
}

// typedFor is what a stage that takes typed text gets typed into it: the
// rest of a placeholder that starts with the mark.
func typedFor(o list.Options) (string, bool) {
	if o.OnlyMatch {
		return "", false
	}
	return strings.CutPrefix(o.Placeholder, ScriptedMark)
}

// Activation hands out a stand-in token, scripted-<session>, at once.
func (s *Scripted) Activation(id SessionID, _ string) {
	s.host.Activated(id, fmt.Sprintf("scripted-%d", id))
}
