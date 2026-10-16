package fuzzy

import "testing"

// What a query matches: every word of it, each by its kind, ignoring case
// and (unless the word types one) accents.

func score(query, text string) (uint32, bool) {
	return Parse(query).Score(NewMatcher(), NewText(text))
}

func positions(query, text string) []uint32 {
	return Parse(query).Positions(NewMatcher(), NewText(text), nil)
}

// matches checks, for each text, whether query matches it.
func matches(t *testing.T, query string, want map[string]bool) {
	t.Helper()
	for text, w := range want {
		if _, ok := score(query, text); ok != w {
			t.Errorf("%q in %q: matched %v, want %v", query, text, ok, w)
		}
	}
}

func TestAFuzzyWordMatchesItsCharactersInOrderWithAnythingBetween(t *testing.T) {
	matches(t, "rto", map[string]bool{
		"rite of oil": true, "r-t-o": true, "rto": true, "proto": true,
		"tor": false, "rt": false, "": false, "ro t": false,
	})
	matches(t, "é", map[string]bool{"é": true, "café": true, "e": false})
	matches(t, "機精", map[string]bool{"機械の精霊": true, "精霊の機械": false})
	matches(t, "abc", map[string]bool{"éaxcbx": false, "éaxcxcbx": false, "éaxbxc": true})
	matches(t, "a1", map[string]bool{"xa b1 x": true, "x1 a": false})
	matches(t, "z", map[string]bool{"éa": false, "éz": true})
	// An ASCII text never holds a word that is not ASCII.
	matches(t, "é", map[string]bool{"cafe": false})
	matches(t, "'é", map[string]bool{"cafe": false})
}

func TestEachKindOfWordMatchesWhereItsSyntaxSays(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  map[string]bool
	}{
		{"'rit", map[string]bool{"a rite": true, "r-i-t": false, "RIT": true, "ri": false, "ri t": false}},
		{"'r", map[string]bool{"a rite": true, "ite": false, "ж": false, "жx": false, "жr": true}},
		{"'rж", map[string]bool{"ärж": true, "äжr": false, "ärxж": false}},
		{"^rite", map[string]bool{"rite of oil": true, "  rite": true, "the rite": false, "rit": false}},
		{"oil$", map[string]bool{"rite of oil": true, "oil  ": true, "oily": false, "oi": false}},
		{"^rite$", map[string]bool{"rite": true, " Rite ": true, "rites": false, "a rite": false, "   ": false, "": false}},
		{"'rite$", map[string]bool{"rite": true, "rite!": false}},
		{"!oil", map[string]bool{"rite of oil": false, "boil": false, "o-i-l": true, "rite": true}},
		{"!^rite", map[string]bool{"rite of oil": false, "the rite": true}},
		{"!oil$", map[string]bool{"rite of oil": false, "oily": true}},
		{"!^rite$", map[string]bool{"rite": false, "rites": true}},
		{"!'rite", map[string]bool{"a rite": false, "r-i-t-e": true}},
	} {
		matches(t, tc.query, tc.want)
	}
}

// A word that asks for whitespace at the start or end gets it; one that
// does not is matched past the text's own.
func TestAnchoredWordsSkipOnlyTheWhitespaceTheyDoNotAskFor(t *testing.T) {
	matches(t, `^\ rite`, map[string]bool{" rite": true, "rite": false})
	matches(t, `rite\ $`, map[string]bool{"rite ": true, "rite": false})
	matches(t, `^\ rite\ $`, map[string]bool{" rite ": true, "rite": false})
	matches(t, "^ש", map[string]bool{"　שלום": true, "aש": false})
	matches(t, "^ש$", map[string]bool{"　ש　": true, "　　": false})
	matches(t, "ם$", map[string]bool{"שלום　": true, "ם": true, "םa": false})
}

func TestEveryWordOfAQueryMustMatch(t *testing.T) {
	matches(t, "rite oil", map[string]bool{
		"rite of oil": true, "oil rite": true, "rite": false, "oil": false,
	})
	matches(t, "rite !oil", map[string]bool{"rite of oil": false, "rite of fire": true})
	// A word's score is its own: the query scores their sum.
	a, _ := score("rite", "rite of oil")
	b, _ := score("oil", "rite of oil")
	if both, ok := score("rite oil", "rite of oil"); !ok || both != a+b {
		t.Errorf("rite oil scores %d, rite %d and oil %d", both, a, b)
	}
}

func TestAnEmptyQueryMatchesEverythingWithScoreZero(t *testing.T) {
	for _, query := range []string{"", "  ", "!", "^", "$", "'"} {
		p := Parse(query)
		if !p.Empty() {
			t.Errorf("%q is not empty", query)
		}
		if s, ok := p.Score(NewMatcher(), NewText("anything")); !ok || s != 0 {
			t.Errorf("%q: Score = %d, %v", query, s, ok)
		}
		if pos := p.Positions(NewMatcher(), NewText("anything"), nil); len(pos) != 0 {
			t.Errorf("%q: Positions = %v", query, pos)
		}
	}
}

func TestCaseIsIgnoredOnBothSides(t *testing.T) {
	for _, query := range []string{"rite", "RITE", "Rite", "'RiTe", "^RITE", "rite$", "^rItE$"} {
		matches(t, query, map[string]bool{"rite": true, "RITE": true, "Rite": true})
	}
	matches(t, "straße", map[string]bool{"STRAẞE": true, "Straße": true})
	matches(t, "σοφία", map[string]bool{"ΣΟΦΊΑ": true})
	matches(t, "ΣΟΦΊΑ", map[string]bool{"σοφία": true})
	matches(t, "'--foo", map[string]bool{"--FOO --foo": true, "--FOO": true})
}

// An unaccented word matches accented letters; a word that types an
// accent means it.
func TestAccentsAreIgnoredUnlessTheQueryTypesOne(t *testing.T) {
	matches(t, "cafe", map[string]bool{"Café Noir": true, "CAFÉ": true, "cafe": true})
	matches(t, "café", map[string]bool{"Café Noir": true, "CAFÉ": true, "cafe": false})
	matches(t, "lodz", map[string]bool{"Łódź": true, "ŁÓDŹ": true})
	matches(t, "dorde", map[string]bool{"Đorđe": true})
	matches(t, "tieng viet", map[string]bool{"Tiếng Việt": true})
	// ß folds to one s.
	matches(t, "strase", map[string]bool{"straße": true})
	matches(t, "'lodz", map[string]bool{"x ŁÓDŹ": true})
	matches(t, "^lodz$", map[string]bool{"Łódź": true})
	// Other scripts keep their letters apart.
	matches(t, "αθηνα", map[string]bool{"Αθήνα": false})
	matches(t, "иван", map[string]bool{"Иван": true, "йван": false})
}
