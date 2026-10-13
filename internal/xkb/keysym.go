package xkb

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

//go:generate go run gen.go

// Keysym is an X keysym.
type Keysym uint32

// Keysyms used by name in this package and by its users.
const (
	NoSymbol        Keysym = 0
	KeyBackSpace    Keysym = 0xff08
	KeyTab          Keysym = 0xff09
	KeyClear        Keysym = 0xff0b
	KeyReturn       Keysym = 0xff0d
	KeyEscape       Keysym = 0xff1b
	KeyMultiKey     Keysym = 0xff20
	KeyHome         Keysym = 0xff50
	KeyLeft         Keysym = 0xff51
	KeyUp           Keysym = 0xff52
	KeyRight        Keysym = 0xff53
	KeyDown         Keysym = 0xff54
	KeyEnd          Keysym = 0xff57
	KeyInsert       Keysym = 0xff63
	KeyModeSwitch   Keysym = 0xff7e
	KeyNumLock      Keysym = 0xff7f
	KeyKPSpace      Keysym = 0xff80
	KeyKPTab        Keysym = 0xff89
	KeyKPEnter      Keysym = 0xff8d
	KeyKPHome       Keysym = 0xff95
	KeyKPLeft       Keysym = 0xff96
	KeyKPUp         Keysym = 0xff97
	KeyKPRight      Keysym = 0xff98
	KeyKPDown       Keysym = 0xff99
	KeyKPEnd        Keysym = 0xff9c
	KeyKPInsert     Keysym = 0xff9e
	KeyKPDelete     Keysym = 0xff9f
	KeyKPMultiply   Keysym = 0xffaa
	KeyKP9          Keysym = 0xffb9
	KeyKPEqual      Keysym = 0xffbd
	KeyShiftL       Keysym = 0xffe1
	KeyHyperR       Keysym = 0xffee
	KeyDelete       Keysym = 0xffff
	KeyISOLock      Keysym = 0xfe01
	KeyISOLevel5L   Keysym = 0xfe13
	KeyISOLeftTab   Keysym = 0xfe20
	KeyBracketLeft  Keysym = 0x5b
	KeyDeadAcute    Keysym = 0xfe51
	KeyCyrillicPe   Keysym = 0x6d0
	xf86Numeric0    Keysym = 0x10081200
	xf86Numeric9    Keysym = 0x10081209
	xf86NumericStar Keysym = 0x1008120a
	xf86NumericHash Keysym = 0x1008120b
	unicodeOffset   Keysym = 0x01000000
	unicodeMin      Keysym = 0x01000100
	unicodeMax      Keysym = 0x0110ffff
	keysymMax       Keysym = 0x1fffffff
)

// KeysymFromName parses a keysym name as xkb_keysym_from_name does
// without flags: a name from the header, "Uxxxx", or "0xNNNN".
func KeysymFromName(name string) Keysym {
	i := sort.Search(len(keysymByName), func(i int) bool { return keysymByName[i].name >= name })
	if i < len(keysymByName) && keysymByName[i].name == name {
		return keysymByName[i].sym
	}
	switch {
	case strings.HasPrefix(name, "U"):
		if !isHex(name[1:]) {
			return NoSymbol
		}
		v, err := strconv.ParseUint(name[1:], 16, 32)
		if err != nil {
			return NoSymbol
		}
		if v > 0xff && v <= 0x10ffff {
			return unicodeOffset + Keysym(v)
		}
		return UTF32ToKeysym(rune(v))
	case strings.HasPrefix(name, "0x"):
		if !isHex(name[2:]) {
			return NoSymbol
		}
		v, err := strconv.ParseUint(name[2:], 16, 32)
		if err != nil || Keysym(v) > keysymMax {
			return NoSymbol
		}
		return Keysym(v)
	case strings.HasPrefix(name, "XF86_"):
		return KeysymFromName("XF86" + name[5:])
	}
	return NoSymbol
}

func isHex(s string) bool {
	if s == "" || len(s) > 8 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// Name is the keysym's canonical name, or its Uxxxx or 0x form.
func (ks Keysym) Name() string {
	i := sort.Search(len(keysymNameOf), func(i int) bool { return keysymNameOf[i].sym >= ks })
	if i < len(keysymNameOf) && keysymNameOf[i].sym == ks {
		return keysymNameOf[i].name
	}
	if ks >= unicodeMin && ks <= unicodeMax {
		return "U" + strings.ToUpper(strconv.FormatUint(uint64(ks-unicodeOffset), 16))
	}
	return "0x" + strconv.FormatUint(uint64(ks), 16)
}

// String is the keysym's name.
func (ks Keysym) String() string { return ks.Name() }

// UTF32 is the character the keysym types, or -1 for none
// (xkb_keysym_to_utf32 returns 0 there).
func (ks Keysym) UTF32() rune {
	switch {
	case ks >= 0x20 && ks <= 0x7e, ks >= 0xa0 && ks <= 0xff:
		return rune(ks)
	case ks == KeyKPSpace:
		return ' '
	case ks >= KeyBackSpace && ks <= KeyClear,
		ks >= KeyKPMultiply && ks <= KeyKP9,
		ks == KeyReturn, ks == KeyEscape, ks == KeyDelete,
		ks == KeyKPTab, ks == KeyKPEnter, ks == KeyKPEqual:
		return rune(ks & 0x7f)
	case ks >= 0x0100d800 && ks <= 0x0100dfff:
		return -1
	case ks >= unicodeOffset && ks <= unicodeMax:
		return rune(ks - unicodeOffset)
	case ks >= xf86Numeric0 && ks <= xf86Numeric9:
		return rune(ks-xf86Numeric0) + '0'
	case ks == xf86NumericStar:
		return '*'
	case ks == xf86NumericHash:
		return '#'
	}
	i := sort.Search(len(legacyUCS), func(i int) bool { return legacyUCS[i].sym >= ks })
	if i < len(legacyUCS) && legacyUCS[i].sym == ks {
		return legacyUCS[i].ucs
	}
	return -1
}

// UTF32ToKeysym is xkb_utf32_to_keysym.
func UTF32ToKeysym(u rune) Keysym {
	switch {
	case u >= 0x20 && u <= 0x7e, u >= 0xa0 && u <= 0xff:
		return Keysym(u)
	case u >= rune(KeyBackSpace&0x7f) && u <= rune(KeyClear&0x7f), u == rune(KeyReturn&0x7f), u == rune(KeyEscape&0x7f):
		return Keysym(u) | 0xff00
	case u == rune(KeyDelete&0x7f):
		return KeyDelete
	case u <= 0, u >= 0xd800 && u <= 0xdfff, u > 0x10ffff:
		return NoSymbol
	}
	i := sort.Search(len(ucsLegacy), func(i int) bool { return ucsLegacy[i].ucs >= u })
	if i < len(ucsLegacy) && ucsLegacy[i].ucs == u {
		return ucsLegacy[i].sym
	}
	return Keysym(u) | unicodeOffset
}

// IsKeypad is xkb_keysym_is_keypad.
func (ks Keysym) IsKeypad() bool { return ks >= KeyKPSpace && ks <= KeyKPEqual }

// IsModifier is xkb_keysym_is_modifier: Shift, Control, Caps Lock, Alt,
// Super, the ISO level and group keys, Mode_switch and Num Lock.
func (ks Keysym) IsModifier() bool {
	return (ks >= KeyShiftL && ks <= KeyHyperR) ||
		(ks >= KeyISOLock && ks <= KeyISOLevel5L) ||
		ks == KeyModeSwitch || ks == KeyNumLock
}

// caseChar is the keysym's character, if it is a keysym libxkbcommon's
// case tables cover (legacy keysyms up to 0x13be, and Unicode keysyms).
func (ks Keysym) caseChar() (rune, bool) {
	if ks <= 0x13be || (ks >= unicodeMin && ks <= unicodeMax) {
		u := ks.UTF32()
		return u, u >= 0
	}
	return 0, false
}

// mapCase applies a simple case mapping and maps the result back to a
// keysym of the same kind: legacy keysyms stay legacy, Unicode keysyms
// stay Unicode (except where the result is Latin-1).
func (ks Keysym) mapCase(f func(rune) rune) Keysym {
	u, ok := ks.caseChar()
	if !ok {
		return ks
	}
	m := f(u)
	if m == u {
		return ks
	}
	if ks <= 0x13be {
		if r := UTF32ToKeysym(m); r != NoSymbol {
			return r
		}
		return ks
	}
	if m < 0x100 {
		return Keysym(m)
	}
	return unicodeOffset + Keysym(m)
}

// ToUpper is xkb_keysym_to_upper. Unicode gives ß no simple upper case;
// libxkbcommon maps it to ẞ (U+1E9E).
func (ks Keysym) ToUpper() Keysym {
	return ks.mapCase(func(r rune) rune {
		if r == 'ß' {
			return 'ẞ'
		}
		return unicode.ToUpper(r)
	})
}

// ToLower is xkb_keysym_to_lower.
func (ks Keysym) ToLower() Keysym { return ks.mapCase(unicode.ToLower) }

// IsLower is xkb_keysym_is_lower: it has an upper case and no lower case.
func (ks Keysym) IsLower() bool { return ks.ToUpper() != ks && ks.ToLower() == ks }

// IsUpperOrTitle is xkb_keysym_is_upper_or_title: it has a lower case.
func (ks Keysym) IsUpperOrTitle() bool { return ks.ToLower() != ks }
