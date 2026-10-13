package fuzzy

import (
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// class is what a character is to word-boundary bonuses; the order
// matters (classes above classDelimiter are word characters).
type class uint8

const (
	classWhitespace class = iota
	classNonWord
	classDelimiter
	classLower
	classUpper
	classLetter
	classNumber
)

// asciiClass is a byte's class: the delimiters are nucleo's default
// "/,:;|", whitespace is Rust's is_ascii_whitespace (no \v).
var asciiClass = func() (t [128]class) {
	for c := range t {
		switch {
		case c >= 'a' && c <= 'z':
			t[c] = classLower
		case c >= 'A' && c <= 'Z':
			t[c] = classUpper
		case c >= '0' && c <= '9':
			t[c] = classNumber
		case c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r':
			t[c] = classWhitespace
		case c == '/' || c == ',' || c == ':' || c == ';' || c == '|':
			t[c] = classDelimiter
		default:
			t[c] = classNonWord
		}
	}
	return t
}()

// classOf is r's class, from Rust's Unicode properties as nucleo reads
// them: Lowercase, then "changes under case folding", Numeric,
// Alphabetic, White_Space.
func classOf(r rune) class {
	if r < utf8.RuneSelf {
		return asciiClass[r]
	}
	if r < cachedRunes {
		return cache[r].class
	}
	return slowClass(r)
}

func slowClass(r rune) class {
	switch {
	case unicode.IsLower(r) || unicode.Is(unicode.Other_Lowercase, r):
		return classLower
	case fold(r) != r:
		return classUpper
	case unicode.IsNumber(r):
		return classNumber
	case unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Other_Alphabetic, r):
		return classLetter
	case unicode.Is(unicode.White_Space, r):
		return classWhitespace
	}
	return classNonWord
}

// fold is Unicode's simple case folding (CaseFolding.txt, statuses C and
// S), which Go does not export: the lower case of the upper case, but
// Cherokee folds to upper case and the Turkish dotted/dotless i stay.
func fold(r rune) rune {
	if r < utf8.RuneSelf {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}
	switch {
	case r == 'İ' || r == 'ı' || (r >= 0x13A0 && r <= 0x13F5):
		return r
	case (r >= 0x13F8 && r <= 0x13FD) || (r >= 0xAB70 && r <= 0xABBF):
		return unicode.ToUpper(r)
	}
	return unicode.ToLower(unicode.ToUpper(r))
}

// normalizeFolded folds a case-folded Latin letter to the ASCII letter it
// is a variant of ("accent folding"): its canonical decomposition when that
// is an ASCII letter and nonspacing marks (x/text/unicode/norm), else the
// residue table for letters that do not decompose (strokes, hooks, ß).
// Other scripts stay as they are.
//
// This is nucleo's normalize, applied after case folding instead of
// before, so upper-case letters fold too. nucleo 0.3.1's own tables for
// U+1D00 and above are misgenerated (they take the values of its first
// table), so Latin Extended Additional folded to the wrong letters there.
func normalizeFolded(r rune) rune {
	if r < 0xC0 {
		return r
	}
	if r < cachedRunes {
		return cache[r].normalized
	}
	return slowNormalize(r)
}

func slowNormalize(r rune) rune {
	if to, ok := residue[r]; ok {
		return to
	}
	if !unicode.Is(unicode.Latin, r) {
		return r
	}
	var buf [utf8.UTFMax]byte
	d := norm.NFD.Properties(utf8.AppendRune(buf[:0], r)).Decomposition()
	if len(d) < 2 || d[0] >= utf8.RuneSelf || d[0] == ' ' {
		return r
	}
	for _, m := range string(d[1:]) {
		if !unicode.Is(unicode.Mn, m) {
			return r
		}
	}
	return rune(d[0])
}

// cachedRunes: below this, class, fold and normalized fold come from a
// table (the Latin, Greek and Cyrillic blocks a list mostly holds).
const cachedRunes = 0x2000

type runeInfo struct {
	class      class
	folded     rune
	normalized rune
}

var cache = func() []runeInfo {
	t := make([]runeInfo, cachedRunes)
	for r := range rune(cachedRunes) {
		t[r] = runeInfo{slowClass(r), fold(r), slowNormalize(r)}
	}
	return t
}()

// residue maps case-folded letters that do not decompose canonically to
// the ASCII letter nucleo's table gives them.
var residue = map[rune]rune{
	0x00DF: 's', 0x00F8: 'o', 0x0111: 'd', 0x0127: 'h', 0x0130: 'i',
	0x0131: 'i', 0x0140: 'l', 0x0142: 'l', 0x0167: 't', 0x0180: 'b',
	0x0183: 'b', 0x0188: 'c', 0x018C: 'd', 0x0192: 'f', 0x0199: 'k',
	0x019A: 'l', 0x019E: 'n', 0x01A5: 'p', 0x01AB: 't', 0x01AD: 't',
	0x01B4: 'y', 0x01B6: 'z', 0x01DD: 'e', 0x01E5: 'g', 0x0221: 'd',
	0x0225: 'z', 0x0234: 'l', 0x0235: 'n', 0x0236: 't', 0x0237: 'j',
	0x023C: 'c', 0x023F: 's', 0x0240: 'z', 0x0247: 'e', 0x0249: 'j',
	0x024B: 'q', 0x024D: 'r', 0x024F: 'y', 0x0250: 'a', 0x0251: 'a',
	0x0253: 'b', 0x0254: 'o', 0x0255: 'c', 0x0256: 'd', 0x0257: 'd',
	0x0258: 'e', 0x025B: 'e', 0x025C: 'e', 0x025D: 'e', 0x025E: 'e',
	0x025F: 'j', 0x0260: 'g', 0x0261: 'g', 0x0262: 'g', 0x0265: 'h',
	0x0266: 'h', 0x0268: 'i', 0x026A: 'i', 0x026B: 'l', 0x026C: 'l',
	0x026D: 'l', 0x026F: 'm', 0x0270: 'm', 0x0271: 'm', 0x0272: 'n',
	0x0273: 'n', 0x0274: 'n', 0x0275: 'o', 0x0279: 'r', 0x027A: 'r',
	0x027B: 'r', 0x027C: 'r', 0x027D: 'r', 0x027E: 'r', 0x027F: 'r',
	0x0280: 'r', 0x0281: 'r', 0x0282: 's', 0x0287: 't', 0x0288: 't',
	0x0289: 'u', 0x028B: 'v', 0x028C: 'v', 0x028D: 'w', 0x028E: 'y',
	0x028F: 'y', 0x0290: 'z', 0x0291: 'z', 0x0297: 'c', 0x0299: 'b',
	0x029A: 'e', 0x029B: 'g', 0x029C: 'h', 0x029D: 'j', 0x029E: 'k',
	0x029F: 'l', 0x02A0: 'q', 0x02AE: 'h', 0x0363: 'a', 0x0364: 'e',
	0x0365: 'i', 0x0366: 'o', 0x0367: 'u', 0x0368: 'c', 0x0369: 'd',
	0x036A: 'h', 0x036B: 'm', 0x036C: 'r', 0x036D: 't', 0x036E: 'v',
	0x036F: 'x', 0x2C65: 'a', 0x2C66: 't',
}
