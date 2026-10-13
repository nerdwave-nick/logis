package daemon

import (
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/protocol"
	"github.com/nerdwave-nick/logis/internal/provider"
)

// Session is what a resolver shows: one session as it becomes visible,
// with its provider's first stage: the list's options and the entries that
// arrived so far (a dmenu client's lines, an external provider's items).
type Session struct {
	ID      SessionID
	Mode    protocol.Mode
	Options list.Options
	Entries []list.Entry
}

// Resolver is the seam between the daemon's session bookkeeping and
// whatever lets the user answer a session: the cogitator window, or the
// Scripted resolver in tests and measurements.
//
// The daemon calls it only from its own goroutine, and in order for one
// session: Show with its first stage, any number of Steps (more items,
// later stages, toasts …), at most one End (a dmenu client sent all its
// items), then Hide once the session is no longer visible (answered,
// replaced, cancelled or disconnected) — or Linger first, when it is a
// Lingerer. A resolver must not block in these calls: a window hands them
// to its own goroutine. It answers the visible session at any time through
// its Host, from any goroutine: a dmenu answer ends its session, a
// provider's leads to its next step.
type Resolver interface {
	// Accepts says whether sessions of this mode can be answered at all; the
	// error ends the opening client's connection.
	Accepts(mode protocol.Mode) error
	// Show makes the session the visible one.
	Show(session Session)
	// Step is what the visible session's provider asks next (for dmenu,
	// only appended lines).
	Step(id SessionID, step provider.Step)
	// End: the visible dmenu session's client has sent all its items.
	End(id SessionID)
	// Hide: the session is no longer visible.
	Hide(id SessionID)
}

// Lingerer is a Resolver that keeps a session's toast in view after the
// session is over (a provider closed its flow while its toast shows): the
// session lingers, without taking keys, until Hide. Untoast: the visible
// session's toast has had its time. Without it, Linger is a Hide and
// Untoast nothing.
type Lingerer interface {
	Linger(id SessionID)
	Untoast(id SessionID)
}

// AnswerKind is how the user answered a session.
type AnswerKind = provider.AnswerKind

const (
	// AnswerItem: the item at Index (in arrival order) was chosen.
	AnswerItem = provider.AnswerItem
	// AnswerTyped: the typed Text was accepted.
	AnswerTyped = provider.AnswerTyped
	// AnswerDismissed: the session was dismissed.
	AnswerDismissed = provider.AnswerDismissed
	// AnswerDescend: the item at Index was chosen for its next stage
	// (Shift+Return); elsewhere the same as AnswerItem.
	AnswerDescend = provider.AnswerDescend
)

// Answer is the user's answer to the visible session.
type Answer = provider.Answer

// Host is what a resolver, or any other part of the daemon outside its
// goroutine (the Wayland connection, for one), reports to the daemon. Its
// methods may be called from any goroutine; they never block once the
// daemon has stopped.
type Host interface {
	// Answer answers a session; an answer for a session that is no longer
	// visible is ignored.
	Answer(id SessionID, answer Answer)
	// Fail ends the visible session with an error for its client (which then
	// exits 2), and shows the next one.
	Fail(id SessionID, message string)
	// Exit stops the daemon with this exit status; a non-empty message goes
	// to its log first. The compositor's loss is Exit(2, "the noosphere fell
	// silent: …").
	Exit(status int, message string)
}
