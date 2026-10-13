package input

import (
	"strings"
	"unicode"

	"github.com/nerdwave-nick/logis/internal/grapheme"
)

// EditKind is what an Edit does.
type EditKind int

const (
	// InsertText inserts Edit.Text at the cursor and moves the cursor
	// after it.
	InsertText EditKind = iota + 1
	// DeleteBack deletes the grapheme before the cursor (Backspace).
	DeleteBack
	// DeleteForward deletes the grapheme after the cursor (Delete).
	DeleteForward
	// DeleteWordBack deletes the word before the cursor (Ctrl+W): first
	// the white space right before it, then the run of other characters
	// before that, as dmenu and readline's unix-word-rubout do.
	DeleteWordBack
	// Clear deletes everything (Ctrl+U).
	Clear
	// Left moves the cursor one grapheme left.
	Left
	// Right moves the cursor one grapheme right.
	Right
	// Home moves the cursor to the start.
	Home
	// End moves the cursor to the end.
	End
)

// Edit is one change to a Query, whatever produced it (a key, a repeat,
// later a paste or an input method).
type Edit struct {
	Kind EditKind
	Text string // for InsertText
}

// Query is the query being typed: its text and the cursor, a byte offset
// that always sits on a grapheme boundary (UAX #29 extended grapheme
// clusters).
type Query struct {
	text   string
	cursor int
}

// Text is the committed text.
func (q *Query) Text() string { return q.text }

// Cursor is the cursor as a byte offset into Text.
func (q *Query) Cursor() int { return q.cursor }

// Apply applies e and reports whether the text or the cursor changed.
func (q *Query) Apply(e Edit) bool {
	switch e.Kind {
	case InsertText:
		q.text = q.text[:q.cursor] + e.Text + q.text[q.cursor:]
		q.cursor += len(e.Text)
		return e.Text != ""
	case DeleteBack:
		return q.deleteTo(q.previousBoundary())
	case DeleteForward:
		return q.deleteTo(q.nextBoundary())
	case DeleteWordBack:
		return q.deleteTo(q.wordStart())
	case Clear:
		had := q.text != ""
		q.text, q.cursor = "", 0
		return had
	case Left:
		return q.moveTo(q.previousBoundary())
	case Right:
		return q.moveTo(q.nextBoundary())
	case Home:
		return q.moveTo(0)
	case End:
		return q.moveTo(len(q.text))
	}
	return false
}

// deleteTo deletes between the cursor and to (either side), leaving the
// cursor at the start of the deleted range.
func (q *Query) deleteTo(to int) bool {
	start, end := min(to, q.cursor), max(to, q.cursor)
	q.text = q.text[:start] + q.text[end:]
	q.cursor = start
	return start != end
}

func (q *Query) moveTo(to int) bool {
	moved := to != q.cursor
	q.cursor = to
	return moved
}

// previousBoundary is the grapheme boundary before the cursor (the cursor
// at the start).
func (q *Query) previousBoundary() int {
	at := 0
	for i := 0; i < q.cursor; {
		at = i
		i += max(grapheme.Next(q.text[i:q.cursor]), 1)
	}
	return at
}

// nextBoundary is the grapheme boundary after the cursor (the cursor at
// the end).
func (q *Query) nextBoundary() int {
	return q.cursor + grapheme.Next(q.text[q.cursor:])
}

// wordStart is where Ctrl+W stops: skip white space back, then the rest.
func (q *Query) wordStart() int {
	head := strings.TrimRightFunc(q.text[:q.cursor], unicode.IsSpace)
	return len(strings.TrimRightFunc(head, func(r rune) bool { return !unicode.IsSpace(r) }))
}
