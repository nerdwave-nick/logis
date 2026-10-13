package input

import "github.com/nerdwave-nick/logis/internal/xkb"

// Mods are the modifiers that are active and were not consumed by the key
// (consumed-modifier semantics, XKB mode as in Rust v1). Lock and Num Lock
// are never part of a binding.
type Mods struct {
	Shift, Ctrl, Alt bool
	Logo             bool // Super, the logo key
}

// Modifier combinations bindings use.
var (
	None  = Mods{}
	Shift = Mods{Shift: true}
	Ctrl  = Mods{Ctrl: true}
)

// Commands says whether a modifier that turns a key into a command is
// held: text is typed only without one.
func (m Mods) Commands() bool { return m.Ctrl || m.Alt || m.Logo }

// Binding is the action bound to keysym ks with the unconsumed modifiers
// m (Rust src/input/bindings.rs).
func Binding(ks xkb.Keysym, m Mods) (Action, bool) {
	plain, ctrl, shift := m == None, m == Ctrl, m == Shift
	edit := func(k EditKind) (Action, bool) { return EditAction(Edit{Kind: k}), true }
	switch {
	case ks == xkb.KeyEscape && plain,
		(ks == 'g' || ks == 'G' || ks == xkb.KeyBracketLeft) && ctrl:
		return ActDismiss, true
	case (ks == xkb.KeyReturn || ks == xkb.KeyKPEnter) && plain:
		return ActAccept, true
	case (ks == xkb.KeyReturn || ks == xkb.KeyKPEnter) && shift:
		return ActDescend, true
	case (ks == xkb.KeyUp || ks == xkb.KeyKPUp) && plain,
		// Shift+Tab is ISO_Left_Tab with Shift consumed on most layouts;
		// where Shift is left unconsumed it is Tab or ISO_Left_Tab + Shift.
		ks == xkb.KeyISOLeftTab && (plain || shift),
		ks == xkb.KeyTab && shift,
		(ks == 'p' || ks == 'P') && ctrl:
		return ActPrevious, true
	case (ks == xkb.KeyDown || ks == xkb.KeyKPDown) && plain,
		ks == xkb.KeyTab && plain,
		(ks == 'n' || ks == 'N') && ctrl:
		return ActNext, true
	case ks == xkb.KeyBackSpace && (plain || shift):
		return edit(DeleteBack)
	case (ks == xkb.KeyDelete || ks == xkb.KeyKPDelete) && plain:
		return edit(DeleteForward)
	case (ks == 'w' || ks == 'W') && ctrl:
		return edit(DeleteWordBack)
	case (ks == 'u' || ks == 'U') && ctrl:
		return edit(Clear)
	case (ks == 'v' || ks == 'V') && ctrl,
		(ks == xkb.KeyInsert || ks == xkb.KeyKPInsert) && shift:
		return Action{Kind: Paste, Selection: Clipboard}, true
	case (ks == xkb.KeyLeft || ks == xkb.KeyKPLeft) && plain:
		return edit(Left)
	case (ks == xkb.KeyRight || ks == xkb.KeyKPRight) && plain:
		return edit(Right)
	case (ks == xkb.KeyHome || ks == xkb.KeyKPHome) && plain:
		return edit(Home)
	case (ks == xkb.KeyEnd || ks == xkb.KeyKPEnd) && plain:
		return edit(End)
	}
	return Action{}, false
}

// Typed is the text a key types into the query: printable text, and only
// without a command modifier (Ctrl+U's control character types nothing).
func Typed(text string, m Mods) (Action, bool) {
	if text == "" || m.Commands() {
		return Action{}, false
	}
	for _, r := range text {
		if isControl(r) {
			return Action{}, false
		}
	}
	return Insert(text), true
}

// isControl is Rust's char::is_control: the Cc category.
func isControl(r rune) bool { return r < 0x20 || r >= 0x7f && r < 0xa0 }
