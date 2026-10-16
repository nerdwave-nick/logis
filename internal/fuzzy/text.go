// Package fuzzy is logis's fzf-like matcher: the scoring of nucleo-matcher
// 0.3.1, case- and accent-insensitive, with fzf's query syntax and match
// positions for highlighting.
//
// Equivalence with nucleo is pinned by testdata/nucleo.json, the output of
// the real crate over a corpus, recorded once. The deliberate
// differences, each pinned in testdata/divergences.json, fix nucleo
// defects:
//   - accent folding (chars.go): nucleo's tables beyond U+036F are
//     misgenerated and it never folds upper-case Latin Extended letters;
//     logis folds every Latin letter whose canonical decomposition is an
//     ASCII letter and marks, via golang.org/x/text/unicode/norm;
//   - a substring search ('word, !word) in non-ASCII text skipped the last
//     possible place and scored with the class of the last match instead of
//     the previous character, and one whose first two characters are not
//     letters was case-sensitive and missed places near the end;
//   - an escaped space in a non-ASCII word kept its backslash.
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
