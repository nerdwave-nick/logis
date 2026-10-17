package list

import (
	"time"
	"unicode/utf8"
)

// Options are how a session's list looks and behaves, as the client asked
// (fuzzel-compatible). DefaultOptions has every default, so a client names
// only what it changes.
type Options struct {
	// Prompt is shown before the query.
	Prompt string
	// Placeholder is shown in place of an empty query.
	Placeholder string
	// Password, when not 0, is shown in place of every typed character.
	Password rune
	// Lines is the most rows shown at once (0: only the prompt line); nil
	// for the theme's window, its height and rows.
	Lines *uint32
	// Width is the window's width in characters (an estimate for
	// proportional fonts); nil for the theme's window width.
	Width *uint32
	// Counter shows how many items match, of how many.
	Counter bool
	// Search is the query the list opens with.
	Search string
	// Select is the item selected when it arrives, until the user moves.
	Select *Preselect
	// OnlyMatch: Return accepts only an item, never the typed text.
	OnlyMatch bool
	// Required: Return accepts typed text only when it is not empty (a
	// provider's required inscription).
	Required bool
}

// DefaultRows is how many rows a list without a theme to ask shows when
// no Lines were given: the bundled default theme's.
const DefaultRows = 11

// DefaultOptions are the options a client that names none gets.
func DefaultOptions() Options { return Options{Prompt: "> "} }

// Rows is the most rows shown at once.
func (o Options) Rows() uint32 {
	if o.Lines != nil {
		return *o.Lines
	}
	return DefaultRows
}

// Preselect is which item starts selected: the first whose text equals
// Text ignoring case, or, with ByIndex, the one at Index (0-based, in the
// order the items arrived).
type Preselect struct {
	Text    string
	Index   uint64
	ByIndex bool
}

// Notice is a short themed notice over the list (a toast, vox-cast): an
// action's outcome or an error.
type Notice struct {
	Text  string
	Level Level
}

// Reading a notice: a second to notice it, then fifteen characters a
// second (a slow reading pace, for words read in passing), ten seconds at
// most, so that a long error does not stand over the list for long.
const (
	readNotice = time.Second
	readPace   = 15 // characters a second
	readAtMost = 10 * time.Second
)

// ReadFor is how long the notice shows when asked to show for asked: that
// long, or longer if its words take longer to read (at most readAtMost).
func (n Notice) ReadFor(asked time.Duration) time.Duration {
	reading := min(readNotice+time.Duration(utf8.RuneCountInString(n.Text))*time.Second/readPace, readAtMost)
	return max(asked, reading)
}

// Level is what kind of news a notice brings.
type Level uint8

const (
	// Info is plain word.
	Info Level = iota
	// Success: something succeeded.
	Success
	// Error: something failed.
	Error
)
