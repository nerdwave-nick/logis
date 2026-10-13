package daemon

import (
	"encoding/json"

	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Session is what a resolver shows: one client's session as it becomes
// visible, with the items that arrived so far.
type Session struct {
	ID   SessionID
	Mode protocol.Mode
	// Options are the list's options exactly as the client sent them (nil:
	// the defaults); the daemon does not read them.
	Options json.RawMessage
	Items   []protocol.Item
}

// Resolver is the seam between the daemon's session bookkeeping and
// whatever lets the user answer a session: the cogitator window, or the
// Scripted resolver in tests and measurements.
//
// The daemon calls it only from its own goroutine, and in order for one
// session: Show, any number of Appends, at most one End (a dmenu client
// sent all its items), then Hide once the session is no longer visible
// (answered, replaced, cancelled or disconnected). A resolver must not block
// in these calls: a window hands them to its own goroutine. It answers the
// visible session at any time through its Host, from any goroutine.
type Resolver interface {
	// Accepts says whether sessions of this mode can be answered at all; the
	// error ends the opening client's connection.
	Accepts(mode protocol.Mode) error
	// Show makes the session the visible one.
	Show(session Session)
	// Append: more items for the visible session, in arrival order.
	Append(id SessionID, items []protocol.Item)
	// End: the visible dmenu session's client has sent all its items.
	End(id SessionID)
	// Hide: the session is no longer visible.
	Hide(id SessionID)
}

// AnswerKind is how the user answered a session.
type AnswerKind int

const (
	// AnswerItem: the item at Index (in arrival order) was chosen.
	AnswerItem AnswerKind = iota
	// AnswerTyped: the typed Text was accepted.
	AnswerTyped
	// AnswerDismissed: the session was dismissed.
	AnswerDismissed
)

// Answer is the user's answer to the visible session.
type Answer struct {
	Kind  AnswerKind
	Index int
	Text  string
}

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
