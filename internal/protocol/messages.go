package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Request types, from a client to the daemon.
const (
	TypeHello  = "hello"
	TypeOpen   = "open"
	TypeItems  = "items"
	TypeEnd    = "end"
	TypeCancel = "cancel"
)

// Request is a message from a client to the daemon. Type says which; only
// that type's fields are set.
type Request struct {
	Type string `json:"type"`
	// Version is hello's protocol version.
	Version uint32 `json:"version,omitempty"`
	// Mode is the session open asks for.
	Mode Mode `json:"mode,omitempty"`
	// Options are the list's options (prompt, placeholder …) for open. The
	// daemon hands them on to the resolver unread.
	Options json.RawMessage `json:"options,omitempty"`
	// Provider is what a provider session runs.
	Provider *Launch `json:"provider,omitempty"`
	// Env is an app session's client environment, NAME=VALUE each.
	Env [][]byte `json:"env,omitempty"`
	// Rites is what plain logis's app session runs as its rites tab
	// (servitor mode's provider): the session is then the tabbed
	// launcher (feat/logis-go 8cba2fc9).
	Rites *Launch `json:"rites,omitempty"`
	// Items is one batch of a dmenu session's rows.
	Items []Item `json:"items,omitempty"`
}

// Hello greets the daemon in this protocol's version.
func Hello() Request { return Request{Type: TypeHello, Version: Version} }

// Open asks for a session of mode with the list's options (nil: defaults).
func Open(mode Mode, options json.RawMessage) Request {
	return Request{Type: TypeOpen, Mode: mode, Options: options}
}

// Items streams a batch of a dmenu session's rows.
func Items(items []Item) Request { return Request{Type: TypeItems, Items: items} }

// End says a dmenu client has sent all its rows.
func End() Request { return Request{Type: TypeEnd} }

// Cancel ends the client's session.
func Cancel() Request { return Request{Type: TypeCancel} }

// DecodeRequest parses one request, refusing an unknown type or a missing
// field that type needs, as a malformed message.
func DecodeRequest(payload []byte, request *Request) error {
	var r Request
	if err := json.Unmarshal(payload, &r); err != nil {
		return &MalformedError{err}
	}
	var missing string
	switch r.Type {
	case TypeHello:
		if r.Version == 0 {
			missing = "version"
		}
	case TypeOpen:
		if r.Mode == "" {
			missing = "mode"
		}
	case TypeItems:
		if r.Items == nil {
			missing = "items"
		}
	case TypeEnd, TypeCancel:
	case "":
		missing = "type"
	default:
		return &MalformedError{fmt.Errorf("unknown type '%s'", r.Type)}
	}
	if missing != "" {
		return &MalformedError{fmt.Errorf("missing field '%s'", missing)}
	}
	*request = r
	return nil
}

// Reply types, from the daemon to a client.
const (
	TypeWelcome = "welcome"
	TypeQueued  = "queued"
	TypeVisible = "visible"
	TypeResult  = "result"
	TypeError   = "error"
)

// Reply is a message from the daemon to a client.
type Reply struct {
	Type    string   `json:"type"`
	Version uint32   `json:"version,omitempty"`
	PID     int      `json:"pid,omitempty"`
	Outcome *Outcome `json:"outcome,omitempty"`
	Message string   `json:"message,omitempty"`
}

// Welcome answers a hello of this version; pid is the daemon's.
func Welcome(pid int) Reply { return Reply{Type: TypeWelcome, Version: Version, PID: pid} }

// Queued: another session is visible; this one waits its turn.
func Queued() Reply { return Reply{Type: TypeQueued} }

// Visible: the session is now the one shown.
func Visible() Reply { return Reply{Type: TypeVisible} }

// Result ends a session with its outcome.
func Result(outcome Outcome) Reply { return Reply{Type: TypeResult, Outcome: &outcome} }

// Error ends a connection with the reason.
func Error(message string) Reply { return Reply{Type: TypeError, Message: message} }

// OutcomeKind is how a session ended.
type OutcomeKind string

const (
	// Selected: the user chose the item at Index (in the order the items
	// arrived).
	Selected OutcomeKind = "selected"
	// Typed: the user accepted the typed Text, which matched no item.
	Typed OutcomeKind = "typed"
	// Dismissed: the user dismissed the session.
	Dismissed OutcomeKind = "dismissed"
	// Replaced: an interactive session took its place.
	Replaced OutcomeKind = "replaced"
	// Cancelled: the client asked to cancel.
	Cancelled OutcomeKind = "cancelled"
	// Closed: the provider ended its flow.
	Closed OutcomeKind = "closed"
)

// Outcome is how a session ended; Index and Item belong to Selected, Text to
// Typed.
type Outcome struct {
	Kind  OutcomeKind
	Index uint64
	Item  Item
	Text  string
}

type selectedWire struct {
	Kind  OutcomeKind `json:"kind"`
	Index uint64      `json:"index"`
	Item  Item        `json:"item"`
}

type typedWire struct {
	Kind OutcomeKind `json:"kind"`
	Text string      `json:"text"`
}

type kindWire struct {
	Kind OutcomeKind `json:"kind"`
}

// MarshalJSON writes only the fields of the outcome's kind.
func (o Outcome) MarshalJSON() ([]byte, error) {
	switch o.Kind {
	case Selected:
		return json.Marshal(selectedWire{o.Kind, o.Index, o.Item})
	case Typed:
		return json.Marshal(typedWire{o.Kind, o.Text})
	default:
		return json.Marshal(kindWire{o.Kind})
	}
}

// UnmarshalJSON reads any outcome; an unknown kind is refused.
func (o *Outcome) UnmarshalJSON(b []byte) error {
	var w struct {
		Kind  OutcomeKind `json:"kind"`
		Index uint64      `json:"index"`
		Item  Item        `json:"item"`
		Text  string      `json:"text"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	switch w.Kind {
	case Selected, Typed, Dismissed, Replaced, Cancelled, Closed:
	default:
		return errors.New("unknown outcome '" + string(w.Kind) + "'")
	}
	*o = Outcome{Kind: w.Kind, Index: w.Index, Item: w.Item, Text: w.Text}
	return nil
}
