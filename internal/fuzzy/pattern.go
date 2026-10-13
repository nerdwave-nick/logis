package fuzzy

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/grapheme"
)

type atomKind uint8

const (
	kindFuzzy     atomKind = iota // word
	kindSubstring                 // 'word, or a negated word
	kindPrefix                    // ^word
	kindPostfix                   // word$
	kindExact                     // ^word$ or 'word$
)

// atom is one word of a query.
type atom struct {
	kind     atomKind
	negative bool
	// needle is case-folded; ascii says whether the word was ASCII (nucleo
	// matches an ASCII needle in an ASCII haystack its own fast way).
	needle []rune
	ascii  bool
	// normalize: accents are folded, unless the word types one itself.
	normalize bool
}

// Pattern is a parsed query: its words must all match.
type Pattern struct {
	atoms []atom
}

// Parse reads a query with fzf's syntax, as nucleo's Pattern::parse does
// (CaseMatching::Ignore, Normalization::Smart): words separated by spaces
// (an escaped space "\ " is part of a word), each optionally ^prefix,
// suffix$, 'substring, ^exact$ or !negated; a backslash before ^ ' ! or $
// takes it literally.
func Parse(query string) Pattern {
	var p Pattern
	for _, word := range words(query) {
		if a := parseAtom(word); len(a.needle) > 0 {
			p.atoms = append(p.atoms, a)
		}
	}
	return p
}

// Empty reports whether the pattern has no words: it then matches
// everything with score 0.
func (p Pattern) Empty() bool { return len(p.atoms) == 0 }

// words splits at spaces not escaped by a backslash.
func words(query string) []string {
	var out []string
	from, escaped := 0, false
	for i := 0; i < len(query); i++ {
		switch c := query[i]; {
		case c == ' ' && !escaped:
			out = append(out, query[from:i])
			from = i + 1
		default:
			escaped = c == '\\'
		}
	}
	return append(out, query[from:])
}

func parseAtom(word string) atom {
	negative := false
	switch {
	case strings.HasPrefix(word, "!"):
		word, negative = word[1:], true
	case strings.HasPrefix(word, "\\!"):
		word = word[1:]
	}
	kind := kindFuzzy
	switch {
	case strings.HasPrefix(word, "^"):
		word, kind = word[1:], kindPrefix
	case strings.HasPrefix(word, "'"):
		word, kind = word[1:], kindSubstring
	case strings.HasPrefix(word, "\\^"), strings.HasPrefix(word, "\\'"):
		word = word[1:]
	}
	dollar := false
	switch {
	case strings.HasSuffix(word, "\\$"):
		word, dollar = word[:len(word)-2], true
	case strings.HasSuffix(word, "$"):
		word = word[:len(word)-1]
		if kind == kindFuzzy {
			kind = kindPostfix
		} else {
			kind = kindExact
		}
	}
	if negative && kind == kindFuzzy {
		kind = kindSubstring
	}
	a := atom{kind: kind, negative: negative, normalize: true, ascii: isASCII(word)}
	word = strings.ReplaceAll(word, "\\ ", " ")
	if a.ascii {
		for i := 0; i < len(word); i++ {
			a.needle = append(a.needle, rune(lowerASCII(word[i])))
		}
	} else {
		for s := word; s != ""; {
			n := grapheme.Next(s)
			r, _ := utf8.DecodeRuneInString(s)
			c := fold(r)
			a.normalize = a.normalize && normalizeFolded(c) == c
			a.needle = append(a.needle, c)
			s = s[n:]
		}
	}
	if dollar {
		a.needle = append(a.needle, '$')
	}
	return a
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func isSpace(r rune) bool { return unicode.Is(unicode.White_Space, r) }

// Score is how well t matches (the sum of its words' scores, each at most
// 65535); ok is false when it does not match.
func (p Pattern) Score(m *Matcher, t Text) (score uint32, ok bool) {
	for i := range p.atoms {
		s, ok := m.score(&p.atoms[i], t, false)
		if !ok {
			return 0, false
		}
		score += uint32(s)
	}
	return score, true
}

// Positions appends to dst the positions of t the query matched, ascending
// and each once, as graphemes counted from 0; nothing when t does not match
// or the query is empty.
func (p Pattern) Positions(m *Matcher, t Text, dst []uint32) []uint32 {
	start := len(dst)
	m.indices = m.indices[:0]
	for i := range p.atoms {
		if _, ok := m.score(&p.atoms[i], t, true); !ok {
			return dst[:start]
		}
	}
	return appendSortedUnique(dst, m.indices)
}

func appendSortedUnique(dst, idx []uint32) []uint32 {
	slices.Sort(idx)
	for i, v := range idx {
		if i == 0 || v != idx[i-1] {
			dst = append(dst, v)
		}
	}
	return dst
}
