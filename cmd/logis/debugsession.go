package main

import (
	"fmt"
	"io"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/input"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/trace"
)

// outcome is what an action did to the debug session.
type outcome int

const (
	unchanged outcome = iota // nothing changed
	selected                 // the selection moved
	edited                   // the query (text or cursor) changed
	dismissed                // the session ended without a choice
	accepted                 // the selected row was chosen
)

// debugSession is the debug panel as a session: the keyboard's actions
// move its selection, edit its query, accept or dismiss it. With the test
// trace on it reports `select <row>`, `query <JSON string>`, `accept <row>`
// and `dismiss`, so harnesses can check what each key did.
type debugSession struct {
	row    int
	query  input.Query
	done   bool
	stdout io.Writer // the accepted row goes here
	redraw func()    // asks the window for the next frame
}

// apply applies a.
func (s *debugSession) apply(a input.Action) outcome {
	switch {
	case a.Kind == input.Dismiss:
		return dismissed
	case a.Kind == input.Accept || a.Kind == input.Descend:
		return accepted
	case a.Kind == input.Select && a.Step == input.Previous && s.row > 0:
		s.row--
		return selected
	case a.Kind == input.Select && a.Step == input.Next && s.row+1 < debugSelectable:
		s.row++
		return selected
	case a.Kind == input.EditQuery && s.query.Apply(a.Edit):
		return edited
	}
	return unchanged
}

// handle applies a as the panel does: redraw on a change, print the
// accepted row, end on accept or dismiss. It is the keyboard's sink.
func (s *debugSession) handle(a input.Action) {
	if s.done {
		return
	}
	switch s.apply(a) {
	case selected:
		trace.Emit("select %d", s.row)
		s.redraw()
	case edited:
		trace.Emit("query %s", trace.Quote(s.query.Text()))
		s.redraw()
	case dismissed:
		trace.Emit("dismiss")
		s.done = true
	case accepted:
		trace.Emit("accept %d", s.row)
		// A closed stdout must not turn the choice into a failure.
		_, _ = fmt.Fprintln(s.stdout, debugRows[s.row])
		s.done = true
	}
}

// debugKeyboard is the panel's keyboard: the locale's Compose table
// (loaded now, at start, not on the first key), the config's focus-loss
// policy, refused keymaps reported on stderr, and every action to s.
func debugKeyboard(cfg config.Config, s *debugSession, stderr io.Writer) *input.Keyboard {
	k := input.NewKeyboard(input.ComposeFromEnv())
	k.SetFocusLoss(cfg.FocusLoss)
	k.SetLog(stderr)
	k.SetSink(s.handle)
	return k
}

// debugSceneWithQuery is debugSceneAt with the query in the query line:
// the prompt, the text before the cursor, the cursor mark, the rest.
func debugSceneWithQuery(logical scale.Size, row int, q *input.Query) *render.Scene {
	sc := debugSceneAt(logical, row)
	text := q.Text()
	line := &sc.Texts[len(sc.Texts)-1]
	line.Text = debugPrompt + text[:q.Cursor()] + debugCursor + text[q.Cursor():]
	return sc
}

// The query line: the prompt, then the query with the cursor marked.
const (
	debugPrompt = "> "
	debugCursor = "|"
)
