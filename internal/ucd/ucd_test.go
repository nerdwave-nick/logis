package ucd

import (
	"testing"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var gcTables = map[uint8]*unicode.RangeTable{
	Cc: unicode.Cc, Cf: unicode.Cf, Co: unicode.Co, Cs: unicode.Cs,
	Ll: unicode.Ll, Lm: unicode.Lm, Lo: unicode.Lo, Lt: unicode.Lt, Lu: unicode.Lu,
	Mc: unicode.Mc, Me: unicode.Me, Mn: unicode.Mn,
	Nd: unicode.Nd, Nl: unicode.Nl, No: unicode.No,
	Pc: unicode.Pc, Pd: unicode.Pd, Pe: unicode.Pe, Pf: unicode.Pf, Pi: unicode.Pi, Po: unicode.Po, Ps: unicode.Ps,
	Sc: unicode.Sc, Sk: unicode.Sk, Sm: unicode.Sm, So: unicode.So,
	Zl: unicode.Zl, Zp: unicode.Zp, Zs: unicode.Zs,
}

// The generated categories agree with Go's unicode package (also UCD
// 17.0.0) for every code point.
func TestGeneralCategoriesAgreeWithTheStandardLibrary(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		got := GeneralCategory(r)
		if got == Cn {
			for gc, table := range gcTables {
				if unicode.Is(table, r) {
					t.Fatalf("U+%04X: Cn, Go says %d", r, gc)
				}
			}
			continue
		}
		if !unicode.Is(gcTables[got], r) {
			t.Fatalf("U+%04X: category %d disagrees with Go", r, got)
		}
	}
}

// Every Go script table maps to exactly one ISO code, and every code point
// it lists gets that code.
func TestScriptsAgreeWithTheStandardLibrary(t *testing.T) {
	code := map[string]Script{}
	for name, table := range unicode.Scripts {
		for _, r16 := range table.R16 {
			for r := rune(r16.Lo); r <= rune(r16.Hi); r += rune(r16.Stride) {
				check(t, code, name, r)
			}
		}
		for _, r32 := range table.R32 {
			for r := rune(r32.Lo); r <= rune(r32.Hi); r += rune(r32.Stride) {
				check(t, code, name, r)
			}
		}
	}
	for name, want := range map[string]Script{"Arabic": Arabic, "Hebrew": Hebrew, "Common": Common, "Inherited": Inherited, "Latin": Latin, "Syriac": Syriac, "Hiragana": Hiragana} {
		if code[name] != want {
			t.Errorf("%s is %v, want %v", name, code[name], want)
		}
	}
	if ScriptOf(0x0378) != Unknown || ScriptOf(-1) != Unknown {
		t.Error("unassigned code points are Zzzz")
	}
}

func check(t *testing.T, code map[string]Script, name string, r rune) {
	t.Helper()
	got := ScriptOf(r)
	if c, ok := code[name]; ok && c != got {
		t.Fatalf("U+%04X: %s is %v here, %v elsewhere", r, name, got, c)
	}
	code[name] = got
}

// Combining classes, decompositions and compositions agree with
// golang.org/x/text/unicode/norm for characters of its Unicode version.
func TestNormalisationDataAgreesWithXText(t *testing.T) {
	compared := 0
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if GeneralCategory(r) == Cn || GeneralCategory(r) == Cs {
			continue
		}
		p := norm.NFD.PropertiesString(string(r))
		if !unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me) && p.CCC() == 0 && CombiningClass(r) != 0 {
			t.Fatalf("U+%04X: class %d, x/text 0", r, CombiningClass(r))
		}
		if p.CCC() != 0 && p.CCC() != CombiningClass(r) {
			t.Fatalf("U+%04X: class %d, x/text %d", r, CombiningClass(r), p.CCC())
		}
		// Full decomposition by applying ours recursively equals NFD.
		if full := decomposeFully(r); string(full) != norm.NFD.String(string(r)) && len(p.Decomposition()) > 0 {
			t.Fatalf("U+%04X: decomposes to %U, NFD %U", r, full, []rune(norm.NFD.String(string(r))))
		}
		if a, b, ok := Decompose(r); ok && b != 0 {
			if c, ok := Compose(a, b); ok && c != r {
				t.Fatalf("U+%04X: %U+%U composes to %U", r, a, b, c)
			} else if ok != (norm.NFC.String(string([]rune{a, b})) == string(r)) {
				t.Fatalf("U+%04X: composition %v disagrees with NFC", r, ok)
			}
		}
		compared++
	}
	if compared < 100000 {
		t.Fatalf("only %d code points compared", compared)
	}
}

func decomposeFully(r rune) []rune {
	a, b, ok := Decompose(r)
	if !ok {
		return []rune{r}
	}
	out := decomposeFully(a)
	if b != 0 {
		out = append(out, decomposeFully(b)...)
	}
	return out
}

func TestJoiningMirroringAndBreakingSamples(t *testing.T) {
	joins := map[rune]JoiningType{
		'ب': JoinD, 'ا': JoinR, 'د': JoinR, 0x0640: JoinD, 0x200D: JoinD, 0x064E: JoinX, // ZWJ is join-causing; marks are unlisted

		'a': JoinX, 0x0710: JoinAlaph, 0x0715: JoinDalathRish, 0xA872: JoinL,
	}
	for r, want := range joins {
		if got := Joining(r); got != want {
			t.Errorf("Joining(%U) = %d, want %d", r, got, want)
		}
	}
	for r, want := range map[rune]rune{'(': ')', ')': '(', '<': '>', '«': '»', '∈': '∋', '[': ']'} {
		if got, ok := Mirror(r); !ok || got != want {
			t.Errorf("Mirror(%q) = %q %v", r, got, ok)
		}
	}
	if _, ok := Mirror('a'); ok {
		t.Error("'a' does not mirror")
	}
	if c, ok := Compose(0x1100, 0x1161); !ok || c != 0xAC00 {
		t.Errorf("Hangul L+V: %U %v", c, ok)
	}
	if a, b, ok := Decompose(0xAC01); !ok || a != 0xAC00 || b != 0x11A8 {
		t.Errorf("Hangul LVT: %U %U %v", a, b, ok)
	}
	if !ExtendedPictographic(0x1F525) || ExtendedPictographic('a') {
		t.Error("Extended_Pictographic")
	}
	if EastAsianWidth('機') != EAWWide || EastAsianWidth('a') != EAWNarrow || EastAsianWidth(0x20000) != EAWWide {
		t.Error("East_Asian_Width, including @missing ranges")
	}
}
