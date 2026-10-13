package provider

import (
	"bytes"
	"encoding/json"

	"github.com/nerdwave-nick/logis/internal/list"
)

// The provider protocol on the wire (Rust src/provider/wire.rs;
// docs/provider-protocol.md): JSON lines over a provider process's stdin
// (logis → provider) and stdout (provider → logis), one object per line,
// tagged by "type". Unknown fields are ignored, so later versions can add
// optional ones; a line of an unknown type is skipped.

// Protocol is the version logis speaks; both hello lines name it.
const Protocol uint32 = 1

// Message is a line from the provider: Hello, Stage, Items, Remove, Toast,
// Ready, Close, or Unknown (a later version's type, skipped).
type Message interface{ fromProvider() }

// Hello is the provider's first line: the protocol version it speaks.
type Hello struct{ Protocol uint32 }

// Stage is one list of a multi-stage flow; the first shows the window.
type Stage struct {
	// ID is the provider's name for the stage, echoed in activate/submit.
	ID string `json:"id"`
	// Prompt is shown before the query; "> " when absent.
	Prompt *string `json:"prompt"`
	// Placeholder is shown in place of an empty query.
	Placeholder string `json:"placeholder"`
	// Query is the query the stage opens with (a decreed default).
	Query string `json:"query"`
	// Input is what Return does when no item matches.
	Input Input `json:"input"`
	// Select is the id of the item to select once it is there and settled.
	Select *string `json:"select"`
	// Lines are the rows the window has room for (first stage only).
	Lines *uint32 `json:"lines"`
	// Width is the window's width in characters (first stage only).
	Width *uint32 `json:"width"`
	// Items are the stage's first items.
	Items []Item `json:"items"`
}

// Items adds items to the current stage, or replaces items of the same id
// in their place; Select names the item to select once it is there and
// settled (unless the user has moved).
type Items struct {
	Items  []Item
	Select *string
}

// Remove: items of the current stage that are gone.
type Remove struct{ IDs []string }

// Toast is a short notice over the list for Seconds (default 3, 1 to 60).
type Toast struct {
	Text    string
	Level   list.Level
	Seconds *uint32
}

// Ready: after an activate or submit, the current stage takes Return again.
type Ready struct{}

// Close: the flow is over; the window closes (after a showing toast's
// time).
type Close struct{}

// Unknown is a line of a type this version does not know; it is skipped.
type Unknown struct{ Type string }

func (Hello) fromProvider()   {}
func (Stage) fromProvider()   {}
func (Items) fromProvider()   {}
func (Remove) fromProvider()  {}
func (Toast) fromProvider()   {}
func (Ready) fromProvider()   {}
func (Close) fromProvider()   {}
func (Unknown) fromProvider() {}

// Input is what Return does when no item matches the query.
type Input int

const (
	// InputNone: nothing; only items can be chosen.
	InputNone Input = iota
	// InputOptional: the typed text is submitted, even when empty.
	InputOptional
	// InputRequired: the typed text is submitted once it is not empty.
	InputRequired
)

// Item is one row.
type Item struct {
	// ID is stable within its stage: updates, removals and activate name it.
	ID string `json:"id"`
	// Columns are shown left to right, lined up with the other rows'.
	Columns []string `json:"columns"`
	// Match is matched instead of the columns.
	Match *string `json:"match"`
	// Icon is a freedesktop icon name or an absolute path.
	Icon *string `json:"icon"`
	// Glyph is a short text for the icon column (used without an icon).
	Glyph *string `json:"glyph"`
	// Badges are short labels at the row's end.
	Badges []string `json:"badges"`
	// Pending: not settled yet; shown as pending, never activated or
	// preselected.
	Pending bool `json:"pending"`
}

// ToProvider is a line to the provider: Hello (Greeting), Activate, Submit
// or Cancel.
type ToProvider interface{ toProvider() }

// Activate: the user chose item ID of stage Stage.
type Activate struct {
	Stage string `json:"stage"`
	ID    string `json:"id"`
}

// Submit: the user submitted typed Text in stage Stage.
type Submit struct {
	Stage string `json:"stage"`
	Text  string `json:"text"`
}

// Cancel: the window is gone (dismissed, replaced, or its caller left);
// the provider should finish what it started and exit.
type Cancel struct{}

// Greeting is logis's first line: the protocol version it speaks.
func Greeting() ToProvider { return Hello{Protocol: Protocol} }

func (Hello) toProvider()    {}
func (Activate) toProvider() {}
func (Submit) toProvider()   {}
func (Cancel) toProvider()   {}

// Encode is one line to a provider, newline included, its strings as they
// are (no HTML escapes, as serde writes them).
func Encode(m ToProvider) []byte {
	var w any
	switch m := m.(type) {
	case Hello:
		w = struct {
			Type     string `json:"type"`
			Protocol uint32 `json:"protocol"`
		}{"hello", m.Protocol}
	case Activate:
		w = struct {
			Type string `json:"type"`
			Activate
		}{"activate", m}
	case Submit:
		w = struct {
			Type string `json:"type"`
			Submit
		}{"submit", m}
	case Cancel:
		w = struct {
			Type string `json:"type"`
		}{"cancel"}
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(w); err != nil {
		panic(err) // strings and numbers always encode
	}
	return b.Bytes()
}
