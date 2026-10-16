package fuzzy

import (
	"slices"
	"strings"
	"testing"
)

// What a match highlights: the characters of its best alignment, counted
// in graphemes, ascending, each once.

func highlights(t *testing.T, query, text string, want ...uint32) {
	t.Helper()
	if want == nil {
		want = []uint32{}
	}
	got := positions(query, text)
	if got == nil {
		got = []uint32{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("%q in %q highlights %v, want %v", query, text, got, want)
	}
}

func TestTheBestAlignmentIsHighlightedNotTheFirst(t *testing.T) {
	highlights(t, "of", "proof of", 6, 7)
	highlights(t, "rite", "pyrite rite", 7, 8, 9, 10)
	highlights(t, "rt", "xrxt rite", 5, 7)
	highlights(t, "o", "proof of", 6)
	highlights(t, "'of", "proof of", 6, 7)
	highlights(t, "of", "prööf öf", 6, 7)
	highlights(t, "'öf", "prööf öf", 6, 7)
	highlights(t, "ö", "prööf öf", 6)
	highlights(t, "a", "straße STRASSE ẞ", 3)
	highlights(t, "rite", "Rite", 0, 1, 2, 3)
	highlights(t, "rite", "xRxixtxe", 1, 3, 5, 7)
	// A run that crosses into a word takes that word start's bonus on.
	highlights(t, "x-r", "ax-rite r", 1, 2, 3)
	// A word start outweighs a short gap.
	highlights(t, "ab", "a xab", 0, 4)
	highlights(t, "ab", "é a xab", 2, 6)
}

func TestEachKindOfWordHighlightsWhatItMatched(t *testing.T) {
	highlights(t, "^rite", "  rite of oil", 2, 3, 4, 5)
	highlights(t, "oil$", "rite of oil  ", 8, 9, 10)
	highlights(t, "^oil$", " oil ", 1, 2, 3)
	highlights(t, "'of", "rite of oil", 5, 6)
	highlights(t, "rite oil", "rite of oil", 0, 1, 2, 3, 8, 9, 10)
	highlights(t, "rite rit", "rite", 0, 1, 2, 3) // each position once
}

func TestANegatedWordOrAFailedMatchHighlightsNothing(t *testing.T) {
	highlights(t, "rite !fire", "rite of oil", 0, 1, 2, 3)
	highlights(t, "!fire", "rite of oil")
	highlights(t, "rite fire", "rite of oil")
	highlights(t, "zzz", "rite")
	highlights(t, "", "rite")
}

// Positions count grapheme clusters, so a highlight never splits what a
// reader sees as one character.
func TestPositionsCountGraphemes(t *testing.T) {
	highlights(t, "tud", "étude", 1, 2, 3)
	highlights(t, "tud", "étude", 1, 2, 3)
	highlights(t, "x", "👍🏽x", 1)
	highlights(t, "x", "🧑‍💻 x", 2)
	highlights(t, "精霊", "機械の精霊", 3, 4)
}

// A substring is found wherever it fits, up to the text's last place.
func TestASubstringIsFoundUpToTheLastPlaceItFits(t *testing.T) {
	highlights(t, "'ab", "éab", 1, 2)
	highlights(t, "'ab", "éxab", 2, 3)
	highlights(t, "'ÉTé", "øéTé", 1, 2, 3)
	highlights(t, "'ς👍🏽", "fooBar\\Σς👍🏽", 8, 9)
	matches(t, "!ab", map[string]bool{"éab": false, "éba": true})
	matches(t, "!👍🏽x", map[string]bool{"ς👍🏽x": false})
}

// Past the optimal alignment's scratch limits the match is the first one
// found from the left, shortened from its end: still the query's
// characters in order.
func TestATextTooLongForTheOptimalAlignmentHighlightsAMatchInOrder(t *testing.T) {
	long := strings.Repeat("x", 60000)
	highlights(t, "rite", "r"+long+"ite", 0, 60001, 60002, 60003)
	highlights(t, "rite", "r"+long+"r it e", 60001, 60003, 60004, 60006)
	wide := strings.Repeat("ж", 12000)
	highlights(t, "rite", "é r"+wide+"ite", 2, 12003, 12004, 12005)
	highlights(t, "rite", "é r"+wide+"r it e", 12003, 12005, 12006, 12008)
	highlights(t, "rite", "é r"+wide+"e t i")
	// Just under the limits the same texts take the optimal alignment.
	short := strings.Repeat("x", 100)
	highlights(t, "rite", "r"+short+"r it e", 101, 103, 104, 106)
}

// A matcher reused across texts and queries answers as a fresh one does.
func TestAReusedMatcherAnswersAsAFreshOne(t *testing.T) {
	texts := []string{"rite of oil", strings.Repeat("rite ", 200), "é rite", "ab", "éaxbxc " + strings.Repeat("ж", 300)}
	queries := []string{"rite", "ri oil", "'oil", "abc", "é", "x"}
	m := NewMatcher()
	for _, q := range queries {
		p := Parse(q)
		for _, s := range texts {
			got, gotOK := p.Score(m, NewText(s))
			want, wantOK := score(q, s)
			pos := p.Positions(m, NewText(s), nil)
			if got != want || gotOK != wantOK || !slices.Equal(pos, positions(q, s)) {
				t.Errorf("%q in %.20q: reused %d %v %v, fresh %d %v %v", q, s, got, gotOK, pos, want, wantOK, positions(q, s))
			}
		}
	}
}
