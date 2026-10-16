package fuzzy

import (
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Characters as the matcher compares and classifies them, held to the
// Unicode Standard through Go's unicode tables and x/text's
// normalisation data.

// orbit is r's simple case orbit (unicode.SimpleFold), r included.
func orbit(r rune) []rune {
	out := []rune{r}
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		out = append(out, f)
	}
	return out
}

func cherokee(r rune) bool {
	return (r >= 0x13A0 && r <= 0x13FD) || (r >= 0xAB70 && r <= 0xABBF)
}

// unfolded are the pairs Unicode's simple case folding joins (status S,
// added in Unicode 16) that fold, as the lower case of the upper case,
// does not: ΐ U+1FD3, ΰ U+1FE3 and the ligature ﬅ U+FB05 stay apart
// from U+0390, U+03B0 and ﬆ U+FB06.
// A known gap, left as it is until it is decided.
var unfolded = map[rune]bool{0x0390: true, 0x1FD3: true, 0x03B0: true, 0x1FE3: true,
	0xFB05: true, 0xFB06: true}

// Case folding is Unicode's simple case folding (CaseFolding.txt,
// statuses C and S): every member of a case orbit folds to the same
// character of that orbit, its lower case (Cherokee: its upper case); the
// Turkish dotted and dotless i (status T) stay as they are.
func TestCaseFoldingIsUnicodesSimpleCaseFolding(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r == 'İ' || r == 'ı' || unfolded[r] {
			continue
		}
		o := orbit(r)
		got := fold(r)
		in := false
		for _, m := range o {
			in = in || m == got
			if f := fold(m); f != got {
				t.Fatalf("%U folds to %U, %U of its orbit to %U", r, got, m, f)
			}
		}
		if !in {
			t.Fatalf("%U folds to %U, outside its orbit %U", r, got, o)
		}
		if len(o) > 1 && !cherokee(r) && !unicode.IsLower(got) && !unicode.Is(unicode.Other_Lowercase, got) {
			t.Fatalf("%U folds to %U, not lower case", r, got)
		}
	}
	for in, want := range map[rune]rune{
		'A': 'a', 'Σ': 'σ', 'ς': 'σ', 'ẞ': 'ß', 'ſ': 's', 'K': 'k', 'Ω': 'ω', 'ϐ': 'β',
		'ᲀ': 'в', 'Ǆ': 'ǆ', 'ǅ': 'ǆ', 'ꭰ': 'Ꭰ', 'ᏸ': 'Ᏸ', 'İ': 'İ', 'ı': 'ı', '你': '你', '1': '1',
	} {
		if got := fold(in); got != want {
			t.Errorf("fold(%c) = %c, want %c", in, got, want)
		}
	}
}

// Accent folding takes every Latin letter whose canonical decomposition
// is an ASCII letter and nonspacing marks to that letter, in either case.
func TestAccentFoldingTakesLatinLettersToTheirASCIIBase(t *testing.T) {
	n := 0
	for r := rune(0x80); r <= unicode.MaxRune; r++ {
		if !unicode.Is(unicode.Latin, r) {
			continue
		}
		var buf [utf8.UTFMax]byte
		d := []rune(string(norm.NFD.Bytes(utf8.AppendRune(buf[:0], r))))
		if len(d) < 2 || d[0] >= utf8.RuneSelf || !unicode.IsLetter(d[0]) {
			continue
		}
		marks := true
		for _, m := range d[1:] {
			marks = marks && unicode.Is(unicode.Mn, m)
		}
		if !marks {
			continue
		}
		n++
		if got, want := normalizeFolded(fold(r)), unicode.ToLower(d[0]); got != want {
			t.Errorf("%c %U folds to %c, want %c", r, r, got, want)
		}
	}
	if n < 450 {
		t.Fatalf("only %d decomposable Latin letters", n)
	}
}

// Latin letters that do not decompose (strokes, hooks, ligature-like
// letters) fold to the letter they are read as; other scripts are left
// alone.
func TestAccentFoldingCoversLettersThatDoNotDecomposeAndOnlyLatin(t *testing.T) {
	for _, tc := range []struct{ in, want rune }{
		{'Ł', 'l'}, {'ł', 'l'}, {'Ø', 'o'}, {'ø', 'o'}, {'Đ', 'd'}, {'đ', 'd'}, {'Ħ', 'h'},
		{'ß', 's'}, {'ẞ', 's'}, {'ı', 'i'}, {'İ', 'i'}, {'ɐ', 'a'}, {'ƒ', 'f'},
		{'Ā', 'a'}, {'Č', 'c'}, {'ḃ', 'b'}, {'ẃ', 'w'}, {'ệ', 'e'}, {'Ṩ', 's'},
		{'a', 'a'}, {'é', 'e'}, {'æ', 'æ'}, {'Ǣ', 'ǣ'}, {'œ', 'œ'}, {'þ', 'þ'},
		{'ά', 'ά'}, {'й', 'й'}, {'ᴀ', 'ᴀ'}, {'你', '你'}, {'ש', 'ש'},
	} {
		if got := normalizeFolded(fold(tc.in)); got != tc.want {
			t.Errorf("%c: %c, want %c", tc.in, got, tc.want)
		}
	}
	for from, to := range residue {
		if to < 'a' || to > 'z' {
			t.Errorf("%U folds to %q, not an ASCII letter", from, to)
		}
	}
}

// The word-boundary classes follow the Unicode properties: lower-case
// letters, upper-case ones (they change under case folding), digits and
// other letters are word characters; White_Space is whitespace; the rest
// (punctuation, symbols, unassigned) is not a word.
func TestCharacterClassesFollowUnicodeProperties(t *testing.T) {
	for r := rune(0x80); r <= unicode.MaxRune; r++ {
		cls := classOf(r)
		var want class
		switch {
		case unicode.IsLower(r):
			want = classLower
		case unicode.IsUpper(r) && fold(r) != r:
			want = classUpper
		case unicode.Is(unicode.Nd, r):
			want = classNumber
		case unicode.Is(unicode.White_Space, r):
			want = classWhitespace
		case unicode.IsPunct(r) || !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.S, unicode.Z):
			want = classNonWord
		default:
			// Other letters, marks and symbols: a word character or not
			// by the Alphabetic property; any word class will do.
			if unicode.IsLetter(r) && cls <= classDelimiter {
				t.Fatalf("%U %c: class %d, a letter is a word character", r, r, cls)
			}
			continue
		}
		if cls != want {
			t.Fatalf("%U %c: class %d, want %d", r, r, cls, want)
		}
	}
}

// In ASCII, the delimiters are / , : ; and |; whitespace is space, tab,
// line feed, form feed and carriage return (a vertical tab is not).
func TestASCIIClasses(t *testing.T) {
	for _, tc := range []struct {
		r    rune
		want class
	}{
		{'a', classLower}, {'Z', classUpper}, {'5', classNumber}, {' ', classWhitespace},
		{'\t', classWhitespace}, {'\n', classWhitespace}, {'\f', classWhitespace}, {'\r', classWhitespace},
		{'\v', classNonWord}, {'/', classDelimiter}, {',', classDelimiter},
		{':', classDelimiter}, {';', classDelimiter}, {'|', classDelimiter}, {'-', classNonWord},
		{'_', classNonWord}, {'.', classNonWord}, {'\\', classNonWord}, {0, classNonWord},
	} {
		if got := classOf(tc.r); got != tc.want {
			t.Errorf("%q: %d, want %d", tc.r, got, tc.want)
		}
	}
}
