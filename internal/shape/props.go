package shape

import (
	"github.com/nerdwave-nick/logis/internal/ot"
	"github.com/nerdwave-nick/logis/internal/ucd"
)

// isDefaultIgnorable is HarfBuzz's Default_Ignorable_Code_Point: the
// derived property less a few characters that fonts draw.
func isDefaultIgnorable(r rune) bool {
	switch r >> 16 {
	case 0:
		switch r >> 8 {
		case 0x00:
			return r == 0x00AD
		case 0x03:
			return r == 0x034F
		case 0x06:
			return r == 0x061C
		case 0x17:
			return r >= 0x17B4 && r <= 0x17B5
		case 0x18:
			return r >= 0x180B && r <= 0x180E
		case 0x20:
			return r >= 0x200B && r <= 0x200F || r >= 0x202A && r <= 0x202E || r >= 0x2060 && r <= 0x206F
		case 0xFE:
			return r >= 0xFE00 && r <= 0xFE0F || r == 0xFEFF
		case 0xFF:
			return r >= 0xFFF0 && r <= 0xFFF8
		}
	case 1:
		return r >= 0x1D173 && r <= 0x1D17A
	case 0xE:
		return r >= 0xE0000 && r <= 0xE0FFF
	}
	return false
}

func isVariationSelector(r rune) bool {
	return r >= 0xFE00 && r <= 0xFE0F || r >= 0xE0100 && r <= 0xE01EF
}

// modifiedCombiningClass reorders the Hebrew, Arabic, Syriac and a few
// other fixed-position classes the way HarfBuzz does, so marks reach the
// font in the order its lookups expect.
func modifiedCombiningClass(r rune) uint8 {
	switch r {
	case 0x1A60, 0x0FC6:
		return 254
	case 0x0F39:
		return 127
	}
	c := ucd.CombiningClass(r)
	if c >= 10 && c <= 36 {
		return hebrewArabicCCC[c-10]
	}
	switch c {
	case 84, 91:
		return 0
	case 103:
		return 3
	case 130:
		return 132
	case 132:
		return 131
	}
	return c
}

// hebrewArabicCCC maps classes 10–36.
var hebrewArabicCCC = [...]uint8{
	22, 15, 16, 17, 23, 18, 19, 20, 21, 14, 24, 12, 25, 13, 10, 11, 26, // Hebrew 10–26
	28, 29, 30, 31, 32, 33, 27, 34, 35, // Arabic 27–35
	36, // Syriac
}

// initProps sets a character's Unicode properties (HarfBuzz's
// init_unicode_props), recording what the buffer contains.
func initProps(info *ot.Info, b *ot.Buffer) {
	r := rune(info.Glyph)
	gc := ucd.GeneralCategory(r)
	props := uint16(gc)
	if r >= 0x80 {
		if isDefaultIgnorable(r) {
			props |= ot.UPropsIgnorable
			b.ScratchFlags |= ot.ScratchHasDefaultIgnorables
			switch {
			case r == 0x200C:
				props |= ot.UPropsZWNJ
			case r == 0x200D:
				props |= ot.UPropsZWJ
			case r >= 0x180B && r <= 0x180D || r == 0x180F, r >= 0xE0020 && r <= 0xE007F:
				props |= ot.UPropsHidden
			case r == 0x034F:
				props |= ot.UPropsHidden
				b.ScratchFlags |= ot.ScratchHasCGJ
			}
		}
		if ot.GeneralCategory(gc).IsMark() {
			b.ScratchFlags |= ot.ScratchHasContinuations
			props |= ot.UPropsContinuation | uint16(modifiedCombiningClass(r))<<8
		}
	}
	info.UProps = props
}

// setUnicodeProps gives every character its properties and marks the
// grapheme continuations HarfBuzz recognises beyond marks: emoji
// modifiers, regional indicator pairs, ZWJ sequences, halfwidth kana
// voicing and tags.
func setUnicodeProps(b *ot.Buffer) {
	infos := b.Info
	for i := 0; i < len(infos); i++ {
		info := &infos[i]
		initProps(info, b)
		r := rune(info.Glyph)
		if r < 0x80 {
			continue
		}
		switch ot.GeneralCategory(info.GenCat()) {
		case ot.LowercaseLetter, ot.UppercaseLetter, ot.TitlecaseLetter, ot.OtherLetter, ot.SpaceSeparator:
			continue
		}
		switch {
		case info.GenCat() == ot.ModifierSymbol && r >= 0x1F3FB && r <= 0x1F3FF:
			info.SetContinuation()
			b.ScratchFlags |= ot.ScratchHasContinuations
		case i > 0 && r >= 0x1F1E6 && r <= 0x1F1FF:
			prev := &infos[i-1]
			if rune(prev.Glyph) >= 0x1F1E6 && rune(prev.Glyph) <= 0x1F1FF && !prev.IsContinuation() {
				info.SetContinuation()
				b.ScratchFlags |= ot.ScratchHasContinuations
			}
		case info.IsZWJ():
			info.SetContinuation()
			b.ScratchFlags |= ot.ScratchHasContinuations
			if i+1 < len(infos) && ucd.ExtendedPictographic(rune(infos[i+1].Glyph)) {
				i++
				initProps(&infos[i], b)
				infos[i].SetContinuation()
			}
		case r >= 0xFF9E && r <= 0xFF9F, r >= 0xE0020 && r <= 0xE007F:
			info.SetContinuation()
			b.ScratchFlags |= ot.ScratchHasContinuations
		case r == 0x2044:
			b.ScratchFlags |= ot.ScratchHasFractionSlash
		}
	}
}

// guessScript is the script of the first character that has one.
func guessScript(infos []ot.Info) ucd.Script {
	for _, info := range infos {
		switch s := ucd.ScriptOf(rune(info.Glyph)); s {
		case ucd.Common, ucd.Inherited, ucd.Unknown:
		default:
			return s
		}
	}
	return 0
}

// nativeDirection is a script's horizontal direction: rtl, and false for
// scripts written either way (and no script).
func nativeDirection(s ucd.Script) (rtl, known bool) {
	switch s.String() {
	case "Arab", "Hebr", "Syrc", "Thaa", "Cprt", "Khar", "Phnx", "Nkoo", "Lydi", "Avst", "Armi",
		"Phli", "Prti", "Sarb", "Orkh", "Samr", "Mand", "Merc", "Mero", "Mani", "Mend", "Nbat",
		"Narb", "Palm", "Phlp", "Hatr", "Adlm", "Rohg", "Sogo", "Sogd", "Elym", "Chrs", "Yezi",
		"Ougr", "Gara", "Sidt":
		return true, true
	case "Hung", "Ital", "Runr", "Tfng", "\x00\x00\x00\x00":
		return false, false
	}
	return false, true
}

// scriptTags are the OpenType script tags to try for s, best first.
func scriptTags(s ucd.Script) []ot.Tag {
	if s == 0 {
		return nil
	}
	var tags []ot.Tag
	if t, ok := indic2[s.String()]; ok {
		if t != "mym2" {
			tags = append(tags, ot.MakeTag(t[:3]+"3"))
		}
		tags = append(tags, ot.MakeTag(t))
	}
	old := ot.Tag(uint32(s) | 0x20000000) // lowercase the first letter
	switch s.String() {
	case "Zmth":
		old = ot.MakeTag("math")
	case "Hira":
		old = ot.MakeTag("kana")
	case "Laoo":
		old = ot.MakeTag("lao ")
	case "Yiii":
		old = ot.MakeTag("yi  ")
	case "Nkoo":
		old = ot.MakeTag("nko ")
	case "Vaii":
		old = ot.MakeTag("vai ")
	}
	return append(tags, old)
}

var indic2 = map[string]string{
	"Beng": "bng2", "Deva": "dev2", "Gujr": "gjr2", "Guru": "gur2", "Knda": "knd2",
	"Mlym": "mlm2", "Orya": "ory2", "Taml": "tml2", "Telu": "tel2", "Mymr": "mym2",
}

// spaceType is HarfBuzz's fallback width class of a space character.
func spaceType(r rune) uint8 {
	switch r {
	case 0x0020, 0x00A0:
		return spaceSpace
	case 0x2000, 0x2002:
		return 2
	case 0x2001, 0x2003, 0x3000:
		return 1
	case 0x2004:
		return 3
	case 0x2005:
		return 4
	case 0x2006:
		return 6
	case 0x2007:
		return spaceFigure
	case 0x2008:
		return spacePunctuation
	case 0x2009:
		return 5
	case 0x200A:
		return 16
	case 0x202F:
		return spaceNarrow
	case 0x205F:
		return space4Em18
	}
	return 0
}

// Space classes beyond the "1/n em" ones (1–6, 16).
const (
	space4Em18       = 17
	spaceSpace       = 18
	spaceFigure      = 19
	spacePunctuation = 20
	spaceNarrow      = 21
)
