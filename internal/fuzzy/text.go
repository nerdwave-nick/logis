// Package fuzzy is logis's fzf-like matcher: fzf's query syntax, case-
// and accent-insensitive matching, scores that rank a match at a word's
// start above one inside a word and close characters above scattered
// ones, and match positions (in graphemes) for highlighting. Its scoring
// follows nucleo-matcher 0.3.1's design (score.go).
//
// Deliberate behaviours, each held by its test:
//   - accent folding (chars.go) folds every Latin letter whose canonical
//     decomposition is an ASCII letter and marks, in either case, via
//     golang.org/x/text/unicode/norm, plus the Latin letters that do not
//     decompose; other scripts are left alone;
//   - a character the match never touches leaves the score as it is, in
//     any text: a substring ('word, !word) is scored by the character
//     before each place and found up to the last place it fits, ignoring
//     case whatever its first characters are;
//   - an escaped space in a word is a space, in any script.
package fuzzy

import (
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/grapheme"
)

// Text is a haystack prepared for matching: an ASCII text as its bytes,
// any other as the first code point of each grapheme cluster (nucleo's way
// of matching graphemes as units), so positions count graphemes.
type Text struct {
	ascii []byte
	runes []rune
}

// NewText prepares s for matching.
func NewText(s string) Text {
	if isASCII(s) {
		return Text{ascii: []byte(s)}
	}
	runes := make([]rune, 0, len(s))
	for s != "" {
		n := grapheme.Next(s)
		r, _ := utf8.DecodeRuneInString(s)
		runes = append(runes, r)
		s = s[n:]
	}
	return Text{runes: runes}
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// Len is the number of positions in t.
func (t Text) Len() int {
	if t.runes != nil {
		return len(t.runes)
	}
	return len(t.ascii)
}

func (t Text) isASCII() bool { return t.runes == nil }
