package fuzzy

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
)

// recorded is testdata/nucleo.json: what nucleo-matcher 0.3.1 made of a
// corpus (nucleo's Pattern::parse with case and normalisation ignored,
// its default config, positions sorted and deduplicated), recorded once
// from the crate itself. It is the reference logis's matcher is held to:
// items and queries, a score and positions for every pair tried, its case
// folding and normalisation of every character they change, and its
// character class of every non-ASCII range.
type recorded struct {
	Items     []string     `json:"items"`
	Queries   []string     `json:"queries"`
	Cases     [][4]any     `json:"cases"`
	Fold      [][2]rune    `json:"fold"`
	Normalize [][2]rune    `json:"normalize"`
	Classes   []classSpan `json:"classes"`
}

type classSpan struct {
	lo, hi rune
	class  string
}

func (s *classSpan) UnmarshalJSON(b []byte) error {
	var v [3]any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	s.lo, s.hi, s.class = rune(v[0].(float64)), rune(v[1].(float64)), v[2].(string)
	return nil
}

var loadRecorded = sync.OnceValues(func() (*recorded, error) {
	b, err := os.ReadFile("testdata/nucleo.json")
	if err != nil {
		return nil, err
	}
	var o recorded
	return &o, json.Unmarshal(b, &o)
})

func nucleo(t *testing.T) *recorded {
	t.Helper()
	o, err := loadRecorded()
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// Case folding is Unicode's simple case folding, as nucleo's table (UCD
// 15.0) has it; Go 1.27's tables are UCD 17.0, so letters added since may
// fold where nucleo's do not.
func TestFoldingIsUnicodeSimpleCaseFolding(t *testing.T) {
	o := nucleo(t)
	want := map[rune]rune{}
	for _, p := range o.Fold {
		want[p[0]] = p[1]
	}
	if len(want) < 1400 {
		t.Fatalf("only %d fold entries", len(want))
	}
	newer := 0
	for r := rune(0); r <= 0x10FFFF; r++ {
		got := fold(r)
		if w, ok := want[r]; ok {
			if got != w {
				t.Errorf("fold(%U) = %U, nucleo %U", r, got, w)
			}
		} else if got != r {
			newer++ // a case pair nucleo's older table lacks
		}
	}
	if newer > 64 {
		t.Errorf("%d folds nucleo does not have; more than letters added since UCD 15", newer)
	}
	t.Logf("%d folds beyond nucleo's UCD 15.0 table", newer)
}

// Accent folding does everything the working part of nucleo's table does
// (U+00C0–U+036F: Latin-1, Latin Extended-A/B, IPA, combining letters).
func TestAccentFoldingCoversNucleosLatinTable(t *testing.T) {
	n := 0
	for _, p := range nucleo(t).Normalize {
		if p[0] >= 0x370 {
			continue // nucleo 0.3.1's later tables are misgenerated; see chars.go
		}
		n++
		if got, want := normalizeFolded(fold(p[0])), fold(p[1]); got != want {
			t.Errorf("%c %U: %c, want %c", p[0], p[0], got, want)
		}
	}
	if n < 250 {
		t.Fatalf("only %d entries", n)
	}
}

// Where nucleo 0.3.1 folds wrongly or not at all, logis folds Latin letters
// to their ASCII base and leaves other scripts alone.
func TestAccentFoldingBeyondNucleo(t *testing.T) {
	for _, tc := range []struct{ in, want rune }{
		{'Ł', 'l'}, {'Ā', 'a'}, {'Č', 'c'}, {'Ø', 'o'}, // nucleo: unchanged uppercase
		{'ḃ', 'b'}, {'ẃ', 'w'}, {'ệ', 'e'}, {'Ṩ', 's'}, // nucleo: garbage
		{'ά', 'ά'}, {'й', 'й'}, {'ᴀ', 'ᴀ'}, {'ß', 's'}, {'ı', 'i'}, {'İ', 'i'},
		{'a', 'a'}, {'é', 'e'}, {'你', '你'},
	} {
		if got := normalizeFolded(fold(tc.in)); got != tc.want {
			t.Errorf("%c: %c, want %c", tc.in, got, tc.want)
		}
	}
}

// Character classes (for word-boundary bonuses) are nucleo's, from the
// Unicode properties of UCD 16.0; Go 1.27 has UCD 17.0, so characters new
// in 17.0 are words here and non-words there, and letters that gained a
// case pair are cased here; nothing else may differ but U+0295, no longer
// Lowercase in 17.0.
func TestClassesAreNucleos(t *testing.T) {
	names := map[class]string{
		classWhitespace: "w", classNonWord: "n", classDelimiter: "n",
		classLower: "l", classUpper: "u", classLetter: "a", classNumber: "d",
	}
	newer := 0
	for _, s := range nucleo(t).Classes {
		for r := s.lo; r <= s.hi; r++ {
			got := names[classOf(r)]
			switch {
			case got == s.class:
			case s.class == "n", s.class == "a" && (got == "u" || got == "l"):
				newer++
			case r == 0x0295 && got == "a":
			default:
				t.Errorf("%U %c: %s, nucleo %s", r, r, got, s.class)
			}
		}
	}
	t.Logf("%d characters assigned or cased since UCD 16.0", newer)
}

func TestASCIIClasses(t *testing.T) {
	for _, tc := range []struct {
		r    rune
		want class
	}{
		{'a', classLower}, {'Z', classUpper}, {'5', classNumber}, {' ', classWhitespace},
		{'\t', classWhitespace}, {'\v', classNonWord}, {'/', classDelimiter}, {',', classDelimiter},
		{':', classDelimiter}, {';', classDelimiter}, {'|', classDelimiter}, {'-', classNonWord},
		{'_', classNonWord}, {'.', classNonWord},
	} {
		if got := classOf(tc.r); got != tc.want {
			t.Errorf("%q: %d, want %d", tc.r, got, tc.want)
		}
	}
}
