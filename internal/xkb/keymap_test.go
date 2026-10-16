package xkb

import (
	"os"
	"path/filepath"
	"testing"
)

// evdev codes plus the xkb offset of 8, for the keys used below.
const (
	kcA        = 30 + 8
	kcG        = 34 + 8
	kcEight    = 9 + 8
	kcBracket  = 26 + 8
	kcTab      = 15 + 8
	kcKP1      = 79 + 8
	kcLeftSh   = 42 + 8
	kcBackSp   = 14 + 8
	kcCompose  = 127 + 8
	kcCapsLock = 58 + 8
)

// Real modifier masks, fixed in every keymap.
const (
	mShift   Mask = 1
	mLock    Mask = 1 << 1
	mControl Mask = 1 << 2
	mMod2    Mask = 1 << 4 // NumLock on every xkeyboard-config keymap
	mMod5    Mask = 1 << 7 // LevelThree (AltGr)
)

func load(t testing.TB, name string) *Keymap {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "keymaps", name))
	if err != nil {
		t.Fatal(err)
	}
	km, err := ParseKeymap(string(b))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return km
}

func stateOf(km *Keymap, depressed, locked Mask, group uint32) *State {
	s := NewState(km)
	s.UpdateMask(depressed, 0, locked, 0, 0, group)
	return s
}

// The owner's live niri keymap (us + compose:menu, keysyms in hex) and the
// same layout serialised with keysym names type the same.
func TestTheUSKeymapsTypeWhatTheLayoutSays(t *testing.T) {
	for _, name := range []string{"live-niri.xkb", "xkbcli/us--compose_menu.xkb"} {
		t.Run(name, func(t *testing.T) {
			km := load(t, name)
			plain := stateOf(km, 0, 0, 0)
			if got := plain.OneSym(kcA); got != 'a' || plain.UTF8(kcA) != "a" {
				t.Errorf("a: %v %q", got, plain.UTF8(kcA))
			}
			if got := plain.OneSym(kcCompose); got != KeyMultiKey {
				t.Errorf("Menu is %v, want Multi_key", got)
			}
			shift := stateOf(km, mShift, 0, 0)
			if got := shift.OneSym(kcA); got != 'A' || !shift.ModIsConsumed(kcA, ModShift, ConsumedXKB) {
				t.Errorf("Shift+a: %v, Shift consumed %v", got, shift.ModIsConsumed(kcA, ModShift, ConsumedXKB))
			}
			if got := shift.OneSym(kcTab); got != KeyISOLeftTab || !shift.ModIsConsumed(kcTab, ModShift, ConsumedXKB) {
				t.Errorf("Shift+Tab: %v", got)
			}
			ctrl := stateOf(km, mControl, 0, 0)
			if got := ctrl.UTF8(kcG); got != "\a" || ctrl.ModIsConsumed(kcG, ModControl, ConsumedXKB) {
				t.Errorf("Ctrl+g types %q (want BEL), Control consumed %v", got, ctrl.ModIsConsumed(kcG, ModControl, ConsumedXKB))
			}
			caps := stateOf(km, mControl, mLock, 0)
			if got := caps.OneSym(kcG); got != 'G' {
				t.Errorf("Caps+Ctrl+g: %v", got)
			}
			if got := stateOf(km, 0, mLock, 0).UTF8(kcA); got != "A" {
				t.Errorf("Caps a types %q", got)
			}
			if s := plain.OneSym(kcKP1); s != KeyKPEnd || plain.UTF8(kcKP1) != "" {
				t.Errorf("KP1 without Num Lock: %v %q", s, plain.UTF8(kcKP1))
			}
			num := stateOf(km, 0, mMod2, 0)
			if s := num.OneSym(kcKP1); s != KeysymFromName("KP_1") || num.UTF8(kcKP1) != "1" {
				t.Errorf("KP1 with Num Lock: %v %q", s, num.UTF8(kcKP1))
			}
			if !km.Repeats(kcA) || km.Repeats(kcLeftSh) || km.Repeats(kcCapsLock) {
				t.Errorf("repeat: a %v, Shift %v, Caps Lock %v", km.Repeats(kcA), km.Repeats(kcLeftSh), km.Repeats(kcCapsLock))
			}
		})
	}
}

// Virtual modifiers get their real mapping from the interprets and the
// modifier map, or explicitly (the live keymap's Hyper=0x4000).
func TestVirtualModifiersMapToRealOnesOrTheirOwnBit(t *testing.T) {
	km := load(t, "live-niri.xkb")
	want := map[string]Mask{"NumLock": mMod2, "Alt": 1 << 3, "Super": 1 << 6, "LevelThree": mMod5, "Hyper": 0x4000}
	for name, m := range want {
		i := km.ModIndex(name)
		if i < 0 || km.mods[i].mapping != m {
			t.Errorf("%s: index %d, want mapping %#x", name, i, m)
		}
	}
	s := stateOf(km, 0, Mask(1)<<km.ModIndex("NumLock"), 0)
	if !s.ModIsActive(km.ModIndex("NumLock")) || s.Mods() != mMod2 {
		t.Errorf("NumLock locked by its virtual bit: mods %#x", s.Mods())
	}
}

// AltGr is consumed by the key it shifts, so Ctrl+[ typed with AltGr on a
// German layout is still Ctrl+[.
func TestAltGrIsConsumedOnAGermanLayout(t *testing.T) {
	km := load(t, "xkbcli/de--.xkb")
	s := stateOf(km, mControl|mMod5, 0, 0)
	if got := s.OneSym(kcEight); got != KeyBracketLeft {
		t.Fatalf("AltGr+8 is %v", got)
	}
	level3 := km.ModIndex("LevelThree")
	if !s.ModIsConsumed(kcEight, level3, ConsumedXKB) || s.ModIsConsumed(kcEight, ModControl, ConsumedXKB) {
		t.Errorf("LevelThree consumed %v, Control consumed %v", s.ModIsConsumed(kcEight, level3, ConsumedXKB), s.ModIsConsumed(kcEight, ModControl, ConsumedXKB))
	}
}

// Caps Lock upper-cases a keysym its key type does not shift for Lock
// (the Caps transformation): é on the French layout's 2 key.
func TestCapsLockUpperCasesKeysBeyondTheirType(t *testing.T) {
	km := load(t, "xkbcli/fr--.xkb")
	const kcTwo = 3 + 8
	s := stateOf(km, 0, mLock, 0)
	if got, text := s.OneSym(kcTwo), s.UTF8(kcTwo); got != KeysymFromName("Eacute") || text != "É" {
		t.Errorf("Caps + é: %v %q", got, text)
	}
	if got := stateOf(km, 0, 0, 0).UTF8(kcTwo); got != "é" {
		t.Errorf("é types %q", got)
	}
}

// The second layout of us,ru: its own keysyms, and the first layout's
// base keysym still at hand.
func TestASecondLayoutIsSelectedByTheGroup(t *testing.T) {
	km := load(t, "xkbcli/us_ru--.xkb")
	if km.NumLayouts() != 2 {
		t.Fatalf("%d layouts", km.NumLayouts())
	}
	s := stateOf(km, 0, 0, 1)
	if got := s.OneSym(kcG); got != KeyCyrillicPe || s.Layout(kcG) != 1 {
		t.Errorf("group 2 g: %v in layout %d", got, s.Layout(kcG))
	}
	if base := km.SymsByLevel(kcG, 0, 0); len(base) != 1 || base[0] != 'g' {
		t.Errorf("layout 1 level 1: %v", base)
	}
	if got := stateOf(km, 0, 0, 2).Layout(kcG); got != 0 {
		t.Errorf("group 3 wraps to layout %d", got)
	}
}

// BackSpace is TWO_LEVEL with the same keysym on both levels: XKB mode
// consumes Shift, GTK mode does not.
func TestConsumedModesDifferWhereLevelsAreEqual(t *testing.T) {
	km := load(t, "live-niri.xkb")
	s := stateOf(km, mShift, 0, 0)
	if !s.ModIsConsumed(kcBackSp, ModShift, ConsumedXKB) || s.ModIsConsumed(kcBackSp, ModShift, ConsumedGTK) {
		t.Errorf("Shift+BackSpace consumed: XKB %v, GTK %v", s.ModIsConsumed(kcBackSp, ModShift, ConsumedXKB), s.ModIsConsumed(kcBackSp, ModShift, ConsumedGTK))
	}
}

func TestUnknownKeysAndModifiersAnswerNothing(t *testing.T) {
	km := load(t, "live-niri.xkb")
	s := NewState(km)
	if s.OneSym(5000) != NoSymbol || s.UTF8(5000) != "" || s.Layout(5000) != -1 || s.Level(5000, 0) != -1 {
		t.Error("an undeclared keycode means something")
	}
	if s.ModIsActive(-1) || s.ModIsActive(99) || s.ModIsConsumed(kcA, 99, ConsumedXKB) || km.ModIndex("Nope") != -1 {
		t.Error("an unknown modifier is active or consumed")
	}
	if lo, hi := km.KeyRange(); lo != 8 || hi < 255 {
		t.Errorf("key range %d..%d", lo, hi)
	}
}
