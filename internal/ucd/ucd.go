// Package ucd holds the Unicode character properties text shaping and
// line breaking need that neither the Go standard library nor
// golang.org/x/text exposes in a usable form: general categories and
// canonical combining classes in one lookup, scripts with their ISO 15924
// codes, Arabic joining types, Bidi_Mirroring_Glyph, one-step canonical
// decompositions and primary compositions, Line_Break, East_Asian_Width
// and Extended_Pictographic. The tables are generated from UCD 17.0.0, the
// version of Go 1.27's unicode package (gen.go).
package ucd

import "sort"

//go:generate go run gen.go

// ascii caches the ASCII values of the most used properties.
var ascii struct {
	gc, lb, script [0x80]uint8
}

func init() {
	for r := range rune(0x80) {
		ascii.gc[r] = find(generalCategories[:], r)
		ascii.lb[r] = find(lineBreaks[:], r)
		ascii.script[r] = find(scripts[:], r)
	}
}

// find is the value of r in runs of (start<<8 | value).
func find(runs []uint32, r rune) uint8 {
	if r < 0 || r > 0x10FFFF {
		return 0
	}
	i := sort.Search(len(runs), func(i int) bool { return rune(runs[i]>>8) > r }) - 1
	if i < 0 {
		return 0
	}
	return uint8(runs[i])
}

// General categories in HarfBuzz's order.
const (
	Cc = iota
	Cf
	Cn
	Co
	Cs
	Ll
	Lm
	Lo
	Lt
	Lu
	Mc
	Me
	Mn
	Nd
	Nl
	No
	Pc
	Pd
	Pe
	Pf
	Pi
	Po
	Ps
	Sc
	Sk
	Sm
	So
	Zl
	Zp
	Zs
)

// GeneralCategory is r's general category (Cc … Zs).
func GeneralCategory(r rune) uint8 {
	if r < 0 || r > 0x10FFFF {
		return Cn
	}
	if r < 0x80 {
		return ascii.gc[r]
	}
	return find(generalCategories[:], r)
}

// CombiningClass is r's canonical combining class.
func CombiningClass(r rune) uint8 { return find(combiningClasses[:], r) }

// Script is a script's ISO 15924 code as a big-endian tag ('Latn').
type Script uint32

// Scripts that shaping treats specially.
const (
	Common    Script = 'Z'<<24 | 'y'<<16 | 'y'<<8 | 'y'
	Inherited Script = 'Z'<<24 | 'i'<<16 | 'n'<<8 | 'h'
	Unknown   Script = 'Z'<<24 | 'z'<<16 | 'z'<<8 | 'z'
	Arabic    Script = 'A'<<24 | 'r'<<16 | 'a'<<8 | 'b'
	Hebrew    Script = 'H'<<24 | 'e'<<16 | 'b'<<8 | 'r'
	Syriac    Script = 'S'<<24 | 'y'<<16 | 'r'<<8 | 'c'
	Latin     Script = 'L'<<24 | 'a'<<16 | 't'<<8 | 'n'
	Hiragana  Script = 'H'<<24 | 'i'<<16 | 'r'<<8 | 'a'
	Mongolian Script = 'M'<<24 | 'o'<<16 | 'n'<<8 | 'g'
)

// ScriptOf is r's Script property.
func ScriptOf(r rune) Script {
	var i uint8
	if r >= 0 && r < 0x80 {
		i = ascii.script[r]
	} else {
		i = find(scripts[:], r)
	}
	c := scriptCodes[i]
	return Script(uint32(c[0])<<24 | uint32(c[1])<<16 | uint32(c[2])<<8 | uint32(c[3]))
}

func (s Script) String() string {
	return string([]byte{byte(s >> 24), byte(s >> 16), byte(s >> 8), byte(s)})
}

// JoiningType is an Arabic joining type, with the Syriac groups HarfBuzz
// distinguishes; X means "not listed".
type JoiningType uint8

// Joining types.
const (
	JoinU JoiningType = iota
	JoinL
	JoinR
	JoinD
	JoinAlaph
	JoinDalathRish
	JoinT
	JoinX
)

// Joining is r's joining type as ArabicShaping.txt lists it.
func Joining(r rune) JoiningType { return JoiningType(find(joiningTypes[:], r)) }

// Mirror is r's Bidi_Mirroring_Glyph.
func Mirror(r rune) (rune, bool) {
	i := sort.Search(len(mirrors), func(i int) bool { return rune(mirrors[i]>>32) >= r })
	if i < len(mirrors) && rune(mirrors[i]>>32) == r {
		return rune(uint32(mirrors[i])), true
	}
	return 0, false
}

// Hangul syllable composition constants.
const (
	sBase, lBase, vBase, tBase = 0xAC00, 0x1100, 0x1161, 0x11A7
	lCount, vCount, tCount     = 19, 21, 28
	nCount                     = vCount * tCount
	sCount                     = lCount * nCount
)

// Decompose is r's canonical decomposition, one step: a then b, or a alone
// (b 0) for a singleton.
func Decompose(r rune) (a, b rune, ok bool) {
	if si := r - sBase; si >= 0 && si < sCount {
		if si%tCount != 0 {
			return sBase + si/tCount*tCount, tBase + si%tCount, true
		}
		return lBase + si/nCount, vBase + si%nCount/tCount, true
	}
	i := sort.Search(len(decompositions), func(i int) bool { return decompositions[i][0] >= r })
	if i < len(decompositions) && decompositions[i][0] == r {
		return decompositions[i][1], decompositions[i][2], true
	}
	return 0, 0, false
}

// Compose is the primary composite of a and b.
func Compose(a, b rune) (rune, bool) {
	switch {
	case a >= lBase && a < lBase+lCount && b >= vBase && b < vBase+vCount:
		return sBase + (a-lBase)*nCount + (b-vBase)*tCount, true
	case a >= sBase && a <= sBase+sCount-tCount && b > tBase && b < tBase+tCount && (a-sBase)%tCount == 0:
		return a + b - tBase, true
	}
	i := sort.Search(len(compositions), func(i int) bool {
		c := compositions[i]
		return c[0] > a || c[0] == a && c[1] >= b
	})
	if i < len(compositions) && compositions[i][0] == a && compositions[i][1] == b {
		return compositions[i][2], true
	}
	return 0, false
}

// LineBreak is r's Line_Break class, an index into linebreak's classes.
func LineBreak(r rune) uint8 {
	if r >= 0 && r < 0x80 {
		return ascii.lb[r]
	}
	return find(lineBreaks[:], r)
}

// East_Asian_Width values.
const (
	EAWNeutral = iota
	EAWAmbiguous
	EAWFullwidth
	EAWHalfwidth
	EAWNarrow
	EAWWide
)

// EastAsianWidth is r's East_Asian_Width.
func EastAsianWidth(r rune) uint8 { return find(eastAsianWidths[:], r) }

// ExtendedPictographic reports the Extended_Pictographic property.
func ExtendedPictographic(r rune) bool { return find(extPict[:], r) == 1 }
