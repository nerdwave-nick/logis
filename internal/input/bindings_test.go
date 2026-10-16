package input

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/xkb"
)

// Tests ported from Rust src/input/bindings.rs.

var alt = Mods{Alt: true}

func sym(name string) xkb.Keysym { return xkb.KeysymFromName(name) }

func bound(t *testing.T, name string, m Mods) (Action, bool) {
	t.Helper()
	ks := sym(name)
	if ks == xkb.NoSymbol {
		t.Fatalf("no keysym %q", name)
	}
	return Binding(ks, m)
}

func wantBinding(t *testing.T, name string, m Mods, want Action) {
	t.Helper()
	if got, ok := bound(t, name, m); !ok || got != want {
		t.Errorf("%s %+v: %+v %v, want %+v", name, m, got, ok, want)
	}
}

func wantNone(t *testing.T, name string, m Mods) {
	t.Helper()
	if got, ok := bound(t, name, m); ok {
		t.Errorf("%s %+v is bound to %+v", name, m, got)
	}
}

func TestEscapeCtrlGAndCtrlBracketDismiss(t *testing.T) {
	wantBinding(t, "Escape", None, ActDismiss)
	for _, s := range []string{"g", "G", "bracketleft"} {
		wantBinding(t, s, Ctrl, ActDismiss)
	}
	wantNone(t, "g", None)
	wantNone(t, "Escape", Ctrl)
}

func TestReturnAndKeypadEnterAcceptShiftReturnDescends(t *testing.T) {
	for _, s := range []string{"Return", "KP_Enter"} {
		wantBinding(t, s, None, ActAccept)
		wantBinding(t, s, Shift, ActDescend)
	}
}

func TestUpDownTabAndCtrlNPMove(t *testing.T) {
	wantBinding(t, "Up", None, ActPrevious)
	wantBinding(t, "KP_Up", None, ActPrevious)
	wantBinding(t, "Down", None, ActNext)
	wantBinding(t, "KP_Down", None, ActNext)
	// Tab and Shift+Tab move too, marked as Tab's: plain logis's tabbed
	// launcher switches tabs with them instead (8cba2fc9).
	wantBinding(t, "Tab", None, ActTabNext)
	wantBinding(t, "ISO_Left_Tab", None, ActTabPrevious)
	wantBinding(t, "Tab", Shift, ActTabPrevious)
	wantBinding(t, "ISO_Left_Tab", Shift, ActTabPrevious)
	wantBinding(t, "n", Ctrl, ActNext)
	wantBinding(t, "p", Ctrl, ActPrevious)
	wantBinding(t, "N", Ctrl, ActNext) // Caps Lock on
	wantBinding(t, "P", Ctrl, ActPrevious)
	wantNone(t, "Tab", alt) // Alt+Tab is not ours
}

func TestTabsMovesAreMovesMarkedAsTabs(t *testing.T) {
	if ActTabNext.Kind != Select || ActTabNext.Step != Next || !ActTabNext.ByTab {
		t.Errorf("Tab: %+v", ActTabNext)
	}
	if ActTabPrevious.Kind != Select || ActTabPrevious.Step != Previous || !ActTabPrevious.ByTab {
		t.Errorf("Shift+Tab: %+v", ActTabPrevious)
	}
	if ActNext.ByTab || ActPrevious.ByTab {
		t.Error("Down and Up are marked as Tab's")
	}
}

func TestEditingKeysEdit(t *testing.T) {
	edit := func(k EditKind) Action { return EditAction(Edit{Kind: k}) }
	wantBinding(t, "BackSpace", None, edit(DeleteBack))
	wantBinding(t, "BackSpace", Shift, edit(DeleteBack))
	wantBinding(t, "u", Ctrl, edit(Clear))
	wantBinding(t, "U", Ctrl, edit(Clear))
	wantBinding(t, "w", Ctrl, edit(DeleteWordBack))
	wantBinding(t, "W", Ctrl, edit(DeleteWordBack))
	wantBinding(t, "Delete", None, edit(DeleteForward))
	wantBinding(t, "KP_Delete", None, edit(DeleteForward))
	for _, c := range []struct {
		names []string
		kind  EditKind
	}{
		{[]string{"Left", "KP_Left"}, Left},
		{[]string{"Right", "KP_Right"}, Right},
		{[]string{"Home", "KP_Home"}, Home},
		{[]string{"End", "KP_End"}, End},
	} {
		for _, n := range c.names {
			wantBinding(t, n, None, edit(c.kind))
			wantNone(t, n, Ctrl)
		}
	}
}

func TestTextIsTypedOnlyPrintableAndWithoutCommandModifiers(t *testing.T) {
	for _, c := range []struct {
		text string
		m    Mods
		ok   bool
	}{
		{"ö", None, true},
		{"A", Shift, true},
		{"u", Ctrl, false},
		{"x", alt, false},
		{"x", Mods{Logo: true}, false},
		{"\u0015", None, false}, // a control character
		{"\r", None, false},
		{"", None, false},
	} {
		got, ok := Typed(c.text, c.m)
		if ok != c.ok || ok && got != Insert(c.text) {
			t.Errorf("%q %+v: %+v %v", c.text, c.m, got, ok)
		}
	}
}

func TestCtrlVAndShiftInsertPasteTheClipboard(t *testing.T) {
	paste := Action{Kind: Paste, Selection: Clipboard}
	wantBinding(t, "v", Ctrl, paste)
	wantBinding(t, "V", Ctrl, paste) // Caps Lock on
	wantBinding(t, "Insert", Shift, paste)
	wantBinding(t, "KP_Insert", Shift, paste)
	wantNone(t, "v", None) // v types
	wantNone(t, "Insert", None)
	wantNone(t, "Insert", Ctrl)
}
