package daemon

import (
	"errors"
	"strings"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Windowless refuses every session: the daemon's resolver until the
// cogitator window exists (feat/logis-go G3).
type Windowless struct{}

// Accepts refuses every mode.
func (Windowless) Accepts(protocol.Mode) error {
	return errors.New("the cogitator has no window to manifest in yet; " +
		"this machine spirit can only listen")
}

func (Windowless) Show(Session)                      {}
func (Windowless) Append(SessionID, []protocol.Item) {}
func (Windowless) End(SessionID)                     {}
func (Windowless) Hide(SessionID)                    {}

// ScriptedMark is the prefix that marks the item Scripted chooses.
const ScriptedMark = ">"

// Scripted answers sessions without a user, for tests and measurements
// (LOGIS_RESOLVER=scripted): a dmenu session once its client has sent every
// item, with the first item whose text starts with ScriptedMark, or
// dismissed when none is marked. Other sessions are never answered; they end
// by replacement, cancel or disconnect.
type Scripted struct {
	host    Host
	visible *scriptedSession
}

type scriptedSession struct {
	id       SessionID
	mode     protocol.Mode
	marked   int // the first marked item, or -1
	count    int
	answered bool
}

// NewScripted answers through host.
func NewScripted(host Host) *Scripted { return &Scripted{host: host} }

// Accepts every mode.
func (s *Scripted) Accepts(protocol.Mode) error { return nil }

// Show remembers the session and where its first marked item is.
func (s *Scripted) Show(session Session) {
	s.visible = &scriptedSession{id: session.ID, mode: session.Mode, marked: -1}
	s.Append(session.ID, session.Items)
}

// Append looks for the first marked item.
func (s *Scripted) Append(id SessionID, items []protocol.Item) {
	v := s.current(id)
	if v == nil {
		return
	}
	for i, item := range items {
		if v.marked < 0 && strings.HasPrefix(item.Text, ScriptedMark) {
			v.marked = v.count + i
		}
	}
	v.count += len(items)
}

// End answers a dmenu session, once.
func (s *Scripted) End(id SessionID) {
	v := s.current(id)
	if v == nil || v.mode != protocol.ModeDmenu || v.answered {
		return
	}
	v.answered = true
	answer := Answer{Kind: AnswerDismissed}
	if v.marked >= 0 {
		answer = Answer{Kind: AnswerItem, Index: v.marked}
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
