package fuzzy

import (
	"reflect"
	"testing"
)

type parsed struct {
	kind     atomKind
	negative bool
	needle   string
}

func atoms(query string) []parsed {
	var out []parsed
	for _, a := range Parse(query).atoms {
		out = append(out, parsed{a.kind, a.negative, string(a.needle)})
	}
	return out
}

// fzf's syntax, as nucleo parses it: ^prefix, suffix$, 'substring,
// ^exact$, !negation, words separated by spaces, backslash escapes.
func TestQueriesParseAsFzfSyntax(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []parsed
	}{
		{"foo", []parsed{{kindFuzzy, false, "foo"}}},
		{"'foo", []parsed{{kindSubstring, false, "foo"}}},
		{"^foo", []parsed{{kindPrefix, false, "foo"}}},
		{"foo$", []parsed{{kindPostfix, false, "foo"}}},
		{"^foo$", []parsed{{kindExact, false, "foo"}}},
		{"'foo$", []parsed{{kindExact, false, "foo"}}},
		{"!foo", []parsed{{kindSubstring, true, "foo"}}},
		{"!^foo", []parsed{{kindPrefix, true, "foo"}}},
		{"!foo$", []parsed{{kindPostfix, true, "foo"}}},
		{"!^foo$", []parsed{{kindExact, true, "foo"}}},
		{"foo\\ bar", []parsed{{kindFuzzy, false, "foo bar"}}},
		{"\\!foo", []parsed{{kindFuzzy, false, "!foo"}}},
		{"\\'foo", []parsed{{kindFuzzy, false, "'foo"}}},
		{"\\^foo", []parsed{{kindFuzzy, false, "^foo"}}},
		{"foo\\$", []parsed{{kindFuzzy, false, "foo$"}}},
		{"^foo\\$", []parsed{{kindPrefix, false, "foo$"}}},
		{"\\^foo\\$", []parsed{{kindFuzzy, false, "^foo$"}}},
		{"\\!^foo\\$", []parsed{{kindFuzzy, false, "!^foo$"}}},
		{"!\\^foo\\$", []parsed{{kindSubstring, true, "^foo$"}}},
		{"rite  oil", []parsed{{kindFuzzy, false, "rite"}, {kindFuzzy, false, "oil"}}},
		{"", nil}, {"   ", nil}, {"!", nil}, {"^", nil}, {"$", nil}, {"^$", nil}, {"'", nil},
		{"Foo", []parsed{{kindFuzzy, false, "foo"}}},
		{"Äxx", []parsed{{kindFuzzy, false, "äxx"}}},
		{"Ⲽxx", []parsed{{kindFuzzy, false, "ⲽxx"}}},
		{"étude", []parsed{{kindFuzzy, false, "etude"}}},
		// nucleo keeps the backslash of an escaped space in a non-ASCII word
		// (and doubles other backslashes); logis unescapes it as in ASCII.
		{"café\\ noir", []parsed{{kindFuzzy, false, "café noir"}}},
		{"é\\x", []parsed{{kindFuzzy, false, "é\\x"}}},
	} {
		if got := atoms(tc.query); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: %+v, want %+v", tc.query, got, tc.want)
		}
	}
}

// Normalization is smart: a query that types an accent means it, so only
// a query without one matches accented letters.
func TestATypedAccentTurnsAccentFoldingOff(t *testing.T) {
	for _, tc := range []struct {
		query     string
		normalize bool
	}{
		{"cafe", true}, {"café", false}, {"CAFÉ", false}, {"ø", false}, {"你", true}, {"ß", false},
	} {
		p := Parse(tc.query)
		if len(p.atoms) != 1 || p.atoms[0].normalize != tc.normalize {
			t.Errorf("%q: %+v, want normalize %v", tc.query, p.atoms, tc.normalize)
		}
	}
}

func TestAnEmptyPatternMatchesEverythingWithScoreZero(t *testing.T) {
	p := Parse("  ")
	if !p.Empty() {
		t.Fatal("not empty")
	}
	if s, ok := p.Score(NewMatcher(), NewText("anything")); !ok || s != 0 {
		t.Errorf("Score = %d, %v", s, ok)
	}
	if pos := p.Positions(NewMatcher(), NewText("anything"), nil); len(pos) != 0 {
		t.Errorf("Positions = %v", pos)
	}
}
