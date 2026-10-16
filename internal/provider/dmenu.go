package provider

import (
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Dmenu is the built-in dmenu provider: its items are the lines a client
// streams from stdin, its one stage the list the client asked for, and an
// answer ends the session with the chosen line's original bytes (or the
// typed text). It speaks the same Steps and Answers as an external
// provider, without a process or ids: an item's index is its line number.
type Dmenu struct {
	options list.Options
	// shown: the stage went out; later lines are appended.
	shown bool
	// lines are every line so far, as the client sent them.
	lines []protocol.Item
}

// NewDmenu is the provider of a dmenu session with these list options.
func NewDmenu(o list.Options) *Dmenu { return &Dmenu{options: o} }

// Show: the session is shown; its stage with the lines so far.
func (d *Dmenu) Show() Step {
	d.shown = true
	return Step{Kind: StepStage, Options: d.options, Entries: entries(d.lines)}
}

// Push takes more lines; once the list is shown, the step that appends
// them.
func (d *Dmenu) Push(items []protocol.Item) (Step, bool) {
	d.lines = append(d.lines, items...)
	if !d.shown || len(items) == 0 {
		return Step{}, false
	}
	return Step{Kind: StepAppend, Entries: entries(items)}, true
}

// Outcome is how the session ends for this answer.
func (d *Dmenu) Outcome(a Answer) protocol.Outcome {
	switch a.Kind {
	case AnswerItem, AnswerDescend:
		if a.Index >= 0 && a.Index < len(d.lines) {
			return protocol.Outcome{Kind: protocol.Selected, Index: uint64(a.Index), Item: d.lines[a.Index]}
		}
	case AnswerTyped:
		return protocol.Outcome{Kind: protocol.Typed, Text: a.Text}
	}
	return protocol.Outcome{Kind: protocol.Dismissed}
}

// entries are lines as the list takes them: shown text and match text.
func entries(lines []protocol.Item) []list.Entry {
	out := make([]list.Entry, len(lines))
	for i, l := range lines {
		out[i].Text = l.Text
		if l.Match != nil {
			out[i].MatchText, out[i].HasMatchText = *l.Match, true
		}
	}
	return out
}
