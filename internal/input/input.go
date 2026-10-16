// Package input is keyboard input and query editing (spec feat/logis-go
// D3, feat/logis §4.5; Rust src/input), a pipeline of small parts, each
// testable without Wayland:
//
//  1. Keyboard is fed the wl_keyboard events (keymap, enter, leave, key,
//     modifiers, repeat info) and turns key presses into Actions. It owns
//     a Layout (the compositor's keymap and the state from its modifier
//     masks: a key resolves to its keysym, its text and the modifiers it
//     did not consume), a Compose table (libxkbcommon's lookup; modifier
//     keysyms never cancel a sequence), client-side Repeat (a deadline the
//     window arms its timerfd for; nothing wakes while no key is held) and
//     the focus-loss policy from the config.
//  2. Binding maps a resolved key to an Action with consumed-modifier
//     semantics: Shift+Tab is ISO_Left_Tab with Shift consumed, Ctrl+[ on
//     a layout where [ needs AltGr is still Ctrl+[, and Caps Lock or Num
//     Lock never change a shortcut.
//  3. Query applies the Edits: insert at the cursor, delete a grapheme or
//     a word, clear, move the cursor.
//
// # The text-input-v3 seam (IME preedit, later)
//
// An input method talks to the client through zwp_text_input_v3, not
// through wl_keyboard: while one is active, the compositor routes the keys
// it consumes to it, and the client receives only preedit_string,
// commit_string and delete_surrounding_text, applied together on done.
// The model already separates what that needs:
//
//   - Committed text reaches the query as an Insert edit, exactly as typed
//     or composed text does; delete_surrounding_text becomes a bounded
//     delete around Query.Cursor (a new EditKind). Keys the IME lets
//     through keep flowing through Keyboard unchanged.
//   - Preedit is display state, never part of Query.Text and never
//     matched: a later Query field (text plus cursor range) drawn at the
//     cursor and replaced on every done.
//   - Surrounding text for set_surrounding_text is Query.Text with
//     Query.Cursor as both cursor and anchor (byte offsets, as the
//     protocol wants); the cursor rectangle comes from the renderer.
//   - Enabling text-input on keyboard enter and disabling it on leave sits
//     beside Keyboard.Enter and Keyboard.Leave in the window's seat.
//
// Nothing here assumes keys are the only source of edits, so adding the
// protocol adds a feeder, not a rewrite. Paste is such a feeder: the
// window reads the selection and hands its text back as an Insert edit.
package input

// Kind is what an Action asks of the visible session.
type Kind int

const (
	// Dismiss ends the session without a choice (Escape, Ctrl+G, Ctrl+[,
	// or focus loss under the default policy).
	Dismiss Kind = iota + 1
	// Accept chooses the selected item, or the typed text (Return).
	Accept
	// Descend chooses the selected item for its next stage, such as an
	// app's desktop actions (Shift+Return); otherwise like Accept.
	Descend
	// Select moves the selection by Step (Up/Down, Tab/Shift+Tab,
	// Ctrl+N/P).
	Select
	// EditQuery changes the query by Edit.
	EditQuery
	// Paste inserts the text of Action.Selection at the cursor (Ctrl+V,
	// Shift+Insert, the middle button). The window reads it and hands it
	// back as an Insert edit; sessions never see this action.
	Paste
	// PointAt: the pointer moved, clicked or scrolled over the window
	// (Action.Point).
	PointAt
)

// Step is a direction for the selection.
type Step int

const (
	// Previous is towards the first item.
	Previous Step = iota + 1
	// Next is towards the last item.
	Next
)

// Action is what a key, the pointer (or focus loss) asks of the visible
// session.
type Action struct {
	Kind      Kind
	Step      Step      // for Select
	Edit      Edit      // for EditQuery
	Selection Selection // for Paste
	Point     Point     // for PointAt
	// ByTab: a Select that Tab or Shift+Tab asked for. Plain logis's
	// tabbed launcher switches tabs with it instead (feat/logis-go
	// 8cba2fc9, a divergence from Rust v1); everywhere else it moves
	// the selection as Down and Up do.
	ByTab bool
}

// Actions as values, for bindings and tests.
var (
	ActDismiss  = Action{Kind: Dismiss}
	ActAccept   = Action{Kind: Accept}
	ActDescend  = Action{Kind: Descend}
	ActPrevious = Action{Kind: Select, Step: Previous}
	ActNext     = Action{Kind: Select, Step: Next}
	// ActTabNext and ActTabPrevious are Tab's and Shift+Tab's.
	ActTabNext     = Action{Kind: Select, Step: Next, ByTab: true}
	ActTabPrevious = Action{Kind: Select, Step: Previous, ByTab: true}
)

// EditAction is the action applying e.
func EditAction(e Edit) Action { return Action{Kind: EditQuery, Edit: e} }

// Insert is the action inserting text at the cursor.
func Insert(text string) Action { return EditAction(Edit{Kind: InsertText, Text: text}) }
