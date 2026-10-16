package fuzzy

import (
	"strings"
	"testing"
)

// How matches rank: where a match starts (a word's start beats a hump
// beats the middle of a word), how close its characters stand, and that
// a character the match never touches changes nothing.

// ranked checks that query scores the texts strictly best first.
func ranked(t *testing.T, query string, texts ...string) {
	t.Helper()
	var prev uint32
	for i, text := range texts {
		s, ok := score(query, text)
		if !ok {
			t.Errorf("%q does not match %q", query, text)
			return
		}
		if i > 0 && s >= prev {
			t.Errorf("%q: %q scores %d, not below %q's %d", query, text, s, texts[i-1], prev)
		}
		prev = s
	}
}

// tied checks that query scores the texts alike.
func tied(t *testing.T, query string, texts ...string) {
	t.Helper()
	first, ok := score(query, texts[0])
	if !ok {
		t.Fatalf("%q does not match %q", query, texts[0])
	}
	for _, text := range texts[1:] {
		if s, ok := score(query, text); !ok || s != first {
			t.Errorf("%q: %q scores %d (%v), %q %d", query, text, s, ok, texts[0], first)
		}
	}
}

// A word's start ranks first (at the text's start or after whitespace),
// then after a delimiter (/ , : ; |), then after other punctuation, then
// a camelCase hump or the first digit, then the middle of a word.
func TestAMatchAtAWordsStartRanksAboveOneInsideAWord(t *testing.T) {
	for _, query := range []string{"r", "'r", "rite", "rt"} {
		tied(t, query, "rite", "x rite", "x\trite")
		ranked(t, query, "x rite", "x/rite", "x-rite", "xRite", "xrite")
	}
	tied(t, "r", "x/rite", "x,rite", "x:rite", "x;rite", "x|rite")
	tied(t, "r", "x-rite", "x_rite", "x.rite", "x@rite")
	ranked(t, "7", "x 7", "x/7", "x-7", "x7")
	// The same in a text that is not ASCII.
	for _, query := range []string{"r", "'r", "rite", "rt"} {
		ranked(t, query, "é rite", "é/rite", "é-rite", "éRite", "érite")
	}
	ranked(t, "ж", "é ж", "é/ж", "é-ж", "éж")
}

func TestConsecutiveCharactersRankAboveScatteredOnes(t *testing.T) {
	ranked(t, "rite", "xritex", "xrxitex", "xrxixtxex")
	ranked(t, "rite", "érite", "érxite", "érxixtxe")
}

func TestAShorterGapRanksAboveALongerOne(t *testing.T) {
	ranked(t, "ab", "xaxbx", "xaxxbx", "xaxxxxxxbx")
	ranked(t, "ab", "éaxb", "éaxxb", "éaxxxxxxb")
}

// The best place counts, not the first: a later word start outranks an
// earlier match inside a word.
func TestTheBestPlaceOfAMatchCountsNotTheFirst(t *testing.T) {
	tied(t, "of", "of", "proof of")
	tied(t, "'of", "of", "proof of")
	tied(t, "o", "o", "proof of")
	tied(t, "'σ", "σ", "ασ σ")
}

// Scores are logis's own scale (score.go): a matched character scores
// scoreMatch, plus its bonus, the first character's bonus counted
// bonusFirstCharMul times; a gap costs penaltyGapStart, each further
// character penaltyGapExtend. These values are the design's constants,
// asserted through their names.
func TestScoresAreSumsOfTheDesignsConstants(t *testing.T) {
	first := uint32(scoreMatch + bonusWhite*bonusFirstCharMul)
	for _, tc := range []struct {
		query, text string
		want        uint32
	}{
		{"r", "rite", first},
		{"r", "x rite", first},
		{"r", "x/rite", scoreMatch + bonusDelimiter*bonusFirstCharMul},
		{"r", "x-rite", scoreMatch + bonusBoundary*bonusFirstCharMul},
		{"r", "xRite", scoreMatch + bonusCamel123*bonusFirstCharMul},
		{"r", "xrite", scoreMatch},
		// A run inside a word carries its first character's bonus on.
		{"ri", "rite", first + scoreMatch + bonusWhite},
		{"it", "xrite", 2*scoreMatch + bonusConsecutive},
		// A gap of one, then of two.
		{"rt", "rite", first - penaltyGapStart + scoreMatch},
		{"re", "rite", first - penaltyGapStart - penaltyGapExtend + scoreMatch},
		{"^rite$", "rite", first + 3*(scoreMatch+bonusWhite)},
		{"é", "é", first},
		// A run that crosses into a word takes that word start's bonus on.
		// and carries it to the characters after.
		{"x-r", "ax-rite r", 3*scoreMatch + 2*bonusBoundary},
		{"x-ri", "ax-ri", 4*scoreMatch + 3*bonusBoundary},
		{"x-ri", "ax-rite ri", 4*scoreMatch + 3*bonusBoundary},
		{"x-ri", "éx-rite ri", 4*scoreMatch + 3*bonusBoundary},
	} {
		if got, ok := score(tc.query, tc.text); !ok || got != tc.want {
			t.Errorf("%q in %q: %d (%v), want %d", tc.query, tc.text, got, ok, tc.want)
		}
	}
	if bonusWhite <= bonusDelimiter || bonusDelimiter <= bonusBoundary || bonusBoundary <= bonusCamel123 {
		t.Error("word-start bonuses are not ordered white > delimiter > boundary > camel")
	}
}

// A character the match never touches, ASCII or not, leaves the score as
// it is: a text with an accented letter or a symbol elsewhere scores as
// its ASCII twin.
func TestACharacterOutsideTheMatchChangesNothing(t *testing.T) {
	for _, tc := range []struct{ query, ascii, other string }{
		{"o", "power-profile\t-\tThe hunger of the machine", "power-profile\t·\tThe hunger of the machine"},
		{"a", "strasse STRASSE X", "straße STRASSE ẞ"},
		{"n", "cafe x canon nero", "café x canon nero"},
		{"l", "fle ligature", "ﬁle ligature"},
		{"r", "E_-2Forgeso|riteSe", "É_-2Forgesø|riteΣé"},
		{"6", "_u\t#fooBarx\t\tx86_64/:", "_ü\t😀fooBarx\t\tx86_64/:"},
		{"e", "loilForge1rmachine-e9/rt", "λoilForge1rmachine-e9/rt"},
		{"ñ mc", ":BCogitator.n^^  nmachineCogitator", ":BCogitator.ñ^^  ñmachineCogitator"},
		{"'c", "eForgen#ncafe:$Cogitator", "eForgeñ👍🏽ñcafe:$Cogitator"},
		{"a a a", "strasse X", "straße ẞ"},
	} {
		q := tc.query
		if strings.ContainsRune(q, 'ñ') {
			q = "n mc" // the ASCII twin of the query too
		}
		want, ok := score(q, tc.ascii)
		if !ok {
			t.Fatalf("%q does not match %q", q, tc.ascii)
		}
		if got, ok := score(tc.query, tc.other); !ok || got != want {
			t.Errorf("%q in %q: %d (%v), want %d as in %q", tc.query, tc.other, got, ok, want, tc.ascii)
		}
	}
}

// Rows keep their order among equals; the empty query ties every text.
func TestTheEmptyQueryAndEqualMatchesTie(t *testing.T) {
	tied(t, "", "a", "bbbb", "é")
	tied(t, "ab", "ab 2", "ab 1", "AB 3")
}

// A text too long for the optimal alignment still matches, and ranks by
// the match it finds.
func TestATextTooLongForTheOptimalAlignmentStillMatches(t *testing.T) {
	long := strings.Repeat("x", 60000)
	for _, text := range []string{"r" + long + "ite", "é r" + strings.Repeat("ж", 12000) + "ite"} {
		if _, ok := score("rite", text); !ok {
			t.Errorf("rite does not match a text of %d bytes", len(text))
		}
		if _, ok := score("rite", text+"!"); !ok {
			t.Errorf("rite does not match a text of %d bytes", len(text))
		}
	}
	if _, ok := score("ritz", "r"+long+"ite"); ok {
		t.Error("ritz matches")
	}
	if _, ok := score("ritz", "é r"+strings.Repeat("ж", 12000)+"ite"); ok {
		t.Error("ritz matches")
	}
}
