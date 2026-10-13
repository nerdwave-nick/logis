package e2e

import (
	"os"
	"path/filepath"
	"time"

	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// Keymap is the injector's keymap: evdev keycodes, the "complete" types
// and compat, and the US layout with the Menu key as Compose (XKB option
// compose:menu), resolved by the compositor from its xkeyboard-config.
// The test compositors' own keymaps say the same, so a client sees one
// meaning whichever keymap reaches it.
const Keymap = `xkb_keymap {
    xkb_keycodes { include "evdev+aliases(qwerty)" };
    xkb_types { include "complete" };
    xkb_compat { include "complete" };
    xkb_symbols { include "pc+us+inet(evdev)+compose(menu)" };
    xkb_geometry { include "pc(pc105)" };
};
`

// evdev key codes used by tests (linux/input-event-codes.h).
const (
	KeyEsc       = 1
	KeyBackspace = 14
	KeyTab       = 15
	KeyEnter     = 28
	KeyLeftCtrl  = 29
	KeyLeftShift = 42
	KeyLeftAlt   = 56
	KeySpace     = 57
	KeyCapsLock  = 58
	KeyNumLock   = 69
	KeyKP7       = 71
	KeyKP8       = 72
	KeyKP9       = 73
	KeyKP4       = 75
	KeyKP5       = 76
	KeyKP6       = 77
	KeyKP1       = 79
	KeyKP2       = 80
	KeyKP3       = 81
	KeyKP0       = 82
	KeyKPEnter   = 96
	KeyHome      = 102
	KeyUp        = 103
	KeyLeft      = 105
	KeyRight     = 106
	KeyEnd       = 107
	KeyDown      = 108
	KeyInsert    = 110
	KeyDelete    = 111
	KeyLeftMeta  = 125
	KeyCompose   = 127
)

// Modifier is a held modifier: its key and its real-modifier bit in the
// keymap (xkbcommon fixes Shift, Lock, Control, Mod1…Mod5 as bits 0…7).
type Modifier struct{ Code, Mask uint32 }

// The modifiers tests hold.
var (
	Shift   = Modifier{KeyLeftShift, 1}
	Control = Modifier{KeyLeftCtrl, 1 << 2}
	Alt     = Modifier{KeyLeftAlt, 1 << 3}
	Super   = Modifier{KeyLeftMeta, 1 << 6}
)

// Lock is a lock a key press toggles: Caps Lock (Lock, bit 1) or Num Lock
// (Mod2, bit 4).
type Lock struct{ Code, Mask uint32 }

// The locks.
var (
	CapsLock = Lock{KeyCapsLock, 1 << 1}
	NumLock  = Lock{KeyNumLock, 1 << 4}
)

// us is the US layout's printable keys: unshifted, shifted, evdev code.
var us = []struct {
	lower, upper rune
	code         uint32
}{
	{'1', '!', 2}, {'2', '@', 3}, {'3', '#', 4}, {'4', '$', 5}, {'5', '%', 6},
	{'6', '^', 7}, {'7', '&', 8}, {'8', '*', 9}, {'9', '(', 10}, {'0', ')', 11},
	{'-', '_', 12}, {'=', '+', 13}, {'\t', '\t', 15},
	{'q', 'Q', 16}, {'w', 'W', 17}, {'e', 'E', 18}, {'r', 'R', 19}, {'t', 'T', 20},
	{'y', 'Y', 21}, {'u', 'U', 22}, {'i', 'I', 23}, {'o', 'O', 24}, {'p', 'P', 25},
	{'[', '{', 26}, {']', '}', 27}, {'\n', '\n', 28},
	{'a', 'A', 30}, {'s', 'S', 31}, {'d', 'D', 32}, {'f', 'F', 33}, {'g', 'G', 34},
	{'h', 'H', 35}, {'j', 'J', 36}, {'k', 'K', 37}, {'l', 'L', 38}, {';', ':', 39},
	{'\'', '"', 40}, {'`', '~', 41}, {'\\', '|', 43},
	{'z', 'Z', 44}, {'x', 'X', 45}, {'c', 'C', 46}, {'v', 'V', 47}, {'b', 'B', 48},
	{'n', 'N', 49}, {'m', 'M', 50}, {',', '<', 51}, {'.', '>', 52}, {'/', '?', 53},
}

// USKey is the key and Shift state that type c on the US layout.
func USKey(c rune) (code uint32, shift, ok bool) {
	if c == ' ' {
		return KeySpace, false, true
	}
	for _, k := range us {
		switch c {
		case k.lower:
			return k.code, false, true
		case k.upper:
			return k.code, true, true
		}
	}
	return 0, false, false
}

// Focused is proof that the client reported keyboard focus (Traced's
// WaitForFocus gives it): the virtual keyboard types only with one, so no
// key is sent before the client can receive it. Its zero value is no proof.
type Focused struct{ proof *struct{} }

// VirtualKeyboard is a zwp_virtual_keyboard_v1 on the harness connection.
type VirtualKeyboard struct {
	h                 *Harness
	keyboard          *wlproto.ZwpVirtualKeyboardV1
	depressed, locked uint32
	epoch             time.Time
}

// VirtualKeyboard creates a virtual keyboard on the seat and installs
// Keymap. Create it before starting the client: a headless compositor's
// seat gains its keyboard capability only from this device.
func (h *Harness) VirtualKeyboard() *VirtualKeyboard {
	h.t.Helper()
	k := &VirtualKeyboard{h: h, keyboard: h.keyboards.CreateVirtualKeyboard(h.seat), epoch: time.Now()}
	path := filepath.Join(h.runtimeDir, "keymap.xkb")
	data := append([]byte(Keymap), 0)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		h.t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		h.t.Fatal(err)
	}
	k.keyboard.Keymap(wlproto.WlKeyboardKeymapFormatXkbV1, int(f.Fd()), uint32(len(data)))
	h.Roundtrip()
	// libwayland sent a duplicate of the fd with the request.
	_ = f.Close()
	return k
}

func (k *VirtualKeyboard) time() uint32 { return uint32(time.Since(k.epoch).Milliseconds()) }

func (k *VirtualKeyboard) require(f Focused) {
	k.h.t.Helper()
	if f.proof == nil {
		k.h.t.Fatal("typing before the client reported keyboard focus")
	}
}

func (k *VirtualKeyboard) sendKey(code uint32, pressed bool) {
	state := uint32(wlproto.WlKeyboardKeyStateReleased)
	if pressed {
		state = wlproto.WlKeyboardKeyStatePressed
	}
	k.keyboard.Key(k.time(), code, state)
	k.h.Roundtrip()
}

func (k *VirtualKeyboard) sendModifiers() {
	k.keyboard.Modifiers(k.depressed, 0, k.locked, 0)
	k.h.Roundtrip()
}

// Key presses or releases key code.
func (k *VirtualKeyboard) Key(f Focused, code uint32, pressed bool) {
	k.h.t.Helper()
	k.require(f)
	k.sendKey(code, pressed)
}

// Tap presses and releases key code in one go: both requests leave in one
// flush, so a stalled compositor cannot hold the key down long enough to
// repeat it (a tap is never a hold).
func (k *VirtualKeyboard) Tap(f Focused, code uint32) {
	k.h.t.Helper()
	k.require(f)
	t := k.time()
	k.keyboard.Key(t, code, wlproto.WlKeyboardKeyStatePressed)
	k.keyboard.Key(t, code, wlproto.WlKeyboardKeyStateReleased)
	k.h.Roundtrip()
}

// Hold presses a modifier key and then sends its mask.
func (k *VirtualKeyboard) Hold(f Focused, m Modifier) {
	k.h.t.Helper()
	k.require(f)
	k.sendKey(m.Code, true)
	k.depressed |= m.Mask
	k.sendModifiers()
}

// Release releases a modifier key and then clears its mask.
func (k *VirtualKeyboard) Release(f Focused, m Modifier) {
	k.h.t.Helper()
	k.require(f)
	k.sendKey(m.Code, false)
	k.depressed &^= m.Mask
	k.sendModifiers()
}

// Toggle taps a lock key and toggles its locked mask.
func (k *VirtualKeyboard) Toggle(f Focused, l Lock) {
	k.h.t.Helper()
	k.Tap(f, l.Code)
	k.locked ^= l.Mask
	k.sendModifiers()
}

// Type types text on the US layout, holding a real Shift for upper case
// and shifted symbols; a character the layout lacks fails the test.
func (k *VirtualKeyboard) Type(f Focused, text string) {
	k.h.t.Helper()
	for _, c := range text {
		code, shift, ok := USKey(c)
		if !ok {
			k.h.t.Fatalf("%q is not on the US layout", c)
		}
		if shift {
			k.Hold(f, Shift)
		}
		k.Tap(f, code)
		if shift {
			k.Release(f, Shift)
		}
	}
}

// Close destroys the virtual keyboard.
func (k *VirtualKeyboard) Close() {
	k.keyboard.Destroy()
	k.h.Roundtrip()
}
