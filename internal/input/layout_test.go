package input

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nerdwave-nick/logis/internal/xkb"
)

// Tests of the layout: what a key means under a keymap and its state.
// The keymaps are layouts as a compositor serialises them
// (internal/xkb/testdata).

// evdev codes used below.
const (
	keyA       = 30
	keyG       = 34
	keyEight   = 9
	keyTab     = 15
	keyKP1     = 79
	keyShiftL  = 42
	keyEsc     = 1
	keyO       = 24
	keyQuote   = 40
	keyCompose = 127
	keyN       = 49
)

// Real modifier masks, fixed in every keymap.
const (
	mShift   = 1
	mLock    = 1 << 1
	mControl = 1 << 2
	mNum     = 1 << 4
	mLevel3  = 1 << 7
)

// keymapText is a keymap fixture: "us" (with compose:menu, as the E2E
// compositors configure it), "us-plain", "de", "us,ru" or "live".
func keymapText(t testing.TB, name string) string {
	t.Helper()
	file := map[string]string{
		"us":       "xkbcli/us--compose_menu.xkb",
		"us-plain": "xkbcli/us--.xkb",
		"de":       "xkbcli/de--.xkb",
		"us,ru":    "xkbcli/us_ru--.xkb",
		"live":     "live-niri.xkb",
	}[name]
	b, err := os.ReadFile(filepath.Join("..", "xkb", "testdata", "keymaps", file))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func keymap(t testing.TB, name string) *xkb.Keymap {
	t.Helper()
	km, err := xkb.ParseKeymap(keymapText(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return km
}

func layout(t *testing.T, name string) *Layout { return NewLayout(keymap(t, name)) }

func TestShiftIsConsumedByLettersAndTab(t *testing.T) {
	us := layout(t, "us-plain")
	us.SetMask(mShift, 0, 0, 0)
	if a := us.Key(keyA); a.Keysym != 'A' || a.Text != "A" || a.Mods != None {
		t.Errorf("Shift+a: %+v", a)
	}
	if tab := us.Key(keyTab); tab.Keysym != xkb.KeyISOLeftTab || tab.Mods != None {
		t.Errorf("Shift+Tab: %+v", tab)
	}
}

func TestControlStaysSignificantAndTypesAControlCharacter(t *testing.T) {
	us := layout(t, "us-plain")
	us.SetMask(mControl, 0, 0, 0)
	if g := us.Key(keyG); g.Keysym != 'g' || g.Mods != Ctrl || g.Text != "\a" {
		t.Errorf("Ctrl+g: %+v", g)
	}
}

func TestCapsLockChangesTheLetterButIsNeverAModifier(t *testing.T) {
	us := layout(t, "us-plain")
	us.SetMask(mControl, 0, mLock, 0)
	if g := us.Key(keyG); g.Keysym != 'G' || g.Mods != Ctrl {
		t.Errorf("Caps+Ctrl+g: %+v", g)
	}
	us.SetMask(0, 0, mLock, 0)
	if a := us.Key(keyA); a.Text != "A" {
		t.Errorf("Caps a: %+v", a)
	}
}

func TestAltGrIsConsumedSoCtrlBracketWorksOnAGermanLayout(t *testing.T) {
	de := layout(t, "de")
	de.SetMask(mControl|mLevel3, 0, 0, 0)
	if b := de.Key(keyEight); b.Keysym != xkb.KeyBracketLeft || b.Mods != Ctrl {
		t.Errorf("Ctrl+AltGr+8: %+v", b)
	}
	us := layout(t, "us-plain")
	us.SetMask(mControl, 0, 0, 0)
	if b := us.Key(26); b.Keysym != xkb.KeyBracketLeft {
		t.Errorf("Ctrl+[: %+v", b)
	}
}

func TestNumLockTurnsTheKeypadIntoDigits(t *testing.T) {
	us := layout(t, "us-plain")
	if off := us.Key(keyKP1); off.Keysym != xkb.KeyKPEnd || off.Text != "" {
		t.Errorf("KP1: %+v", off)
	}
	us.SetMask(0, 0, mNum, 0)
	if on := us.Key(keyKP1); on.Keysym != xkb.KeysymFromName("KP_1") || on.Text != "1" || on.Mods != None {
		t.Errorf("Num Lock KP1: %+v", on)
	}
}

func TestASecondLayoutKeepsTheFirstLayoutsBaseKeysym(t *testing.T) {
	both := layout(t, "us,ru")
	both.SetMask(mControl, 0, 0, 1)
	if g := both.Key(keyG); !g.OtherLayout || g.Keysym != xkb.KeyCyrillicPe || g.Base != 'g' {
		t.Errorf("Ctrl+g on ru: %+v", g)
	}
}

func TestLettersRepeatAndModifiersDoNot(t *testing.T) {
	us := layout(t, "us-plain")
	if !us.Repeats(keyA) || us.Repeats(keyShiftL) {
		t.Errorf("a repeats %v, Shift repeats %v", us.Repeats(keyA), us.Repeats(keyShiftL))
	}
}
