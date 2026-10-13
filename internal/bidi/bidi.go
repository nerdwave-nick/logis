// Package bidi resolves the Unicode Bidirectional Algorithm (UAX #9) for one
// line: embedding levels per character and the visual order, as cosmic-text
// uses them. The algorithm is golang.org/x/text/unicode/bidi's core, forked
// (core.go, bracket.go; BSD licence in LICENSE.x-text) because that package
// exports neither levels nor visual order and its input preparation never
// pairs brackets (rule N0); classes come from its Unicode 17 tables. Drop
// the fork once upstream exposes levels and pairs brackets.
package bidi

import (
	xbidi "golang.org/x/text/unicode/bidi"

	"github.com/nerdwave-nick/logis/internal/ucd"
)

// paragraphOf runs the algorithm over runes at paragraph level dir
// (implicitLevel: from the first strong character, rules P2 and P3).
func paragraphOf(runes []rune, dir level) (*paragraph, error) {
	types := make([]Class, len(runes))
	pairTypes := make([]bracketType, len(runes))
	pairValues := make([]rune, len(runes))
	for i, r := range runes {
		props, _ := xbidi.LookupRune(r)
		types[i] = Class(props.Class())
		if !props.IsBracket() {
			continue
		}
		// A pair is an opener and the closer whose paired bracket it is,
		// canonical equivalents matching (BD16): both carry the opener.
		if props.IsOpeningBracket() {
			pairTypes[i], pairValues[i] = bpOpen, canonicalBracket(r)
		} else if m, ok := ucd.Mirror(r); ok {
			pairTypes[i], pairValues[i] = bpClose, canonicalBracket(m)
		}
	}
	return newParagraph(types, pairTypes, pairValues, dir)
}

// canonicalBracket folds the angle brackets U+2329/U+232A into their
// canonical equivalents U+3008/U+3009.
func canonicalBracket(r rune) rune {
	if a, b, ok := ucd.Decompose(r); ok && b == 0 {
		return a
	}
	return r
}

// removedByX9 reports the classes rule X9 removes.
func removedByX9(c Class) bool {
	switch c {
	case BN, LRE, RLE, LRO, RLO, PDF:
		return true
	}
	return false
}

// Resolve resolves runes as one paragraph (a paragraph separator counts as
// white space), its direction from the first strong character: the level
// of each rune and whether the paragraph is right to left. Levels follow
// cosmic-text's rule L1: segment separators and the white space and
// isolates before them or at the end of the line take the paragraph level;
// explicit embeddings and boundary neutrals neither start nor end such a
// sequence.
func Resolve(runes []rune) (levels []int8, rtl bool) {
	if len(runes) == 0 {
		return nil, false
	}
	line := make([]rune, len(runes))
	for i, r := range runes {
		line[i] = r
		if p, _ := xbidi.LookupRune(r); Class(p.Class()) == B {
			line[i] = ' '
		}
	}
	p, err := paragraphOf(line, implicitLevel)
	if err != nil {
		return make([]int8, len(runes)), false
	}
	para := int8(p.embeddingLevel)
	levels = make([]int8, len(runes))
	for i, l := range p.resultLevels {
		levels[i] = int8(l)
	}
	from := 0 // start of a white space sequence, -1 for none
	reset := func(end int) {
		for j := from; j < end; j++ {
			levels[j] = para
		}
	}
	for i, t := range p.initialTypes {
		switch t {
		case RLE, LRE, RLO, LRO, PDF, BN:
		case B, S:
			if from < 0 {
				from = i
			}
			reset(i + 1)
			from = -1
		case WS, FSI, LRI, RLI, PDI:
			if from < 0 {
				from = i
			}
		default:
			from = -1
		}
	}
	if from >= 0 {
		reset(len(levels))
	}
	return levels, para%2 == 1
}

// Reorder is rule L2: the visual order of elements with levels, each
// element its own unit (as cosmic-text reorders its ranges).
func Reorder(levels []int8) []int {
	if len(levels) == 0 {
		return nil
	}
	order := make([]int, len(levels))
	lo, hi := levels[0], levels[0]
	for i, l := range levels {
		order[i] = i
		lo, hi = min(lo, l), max(hi, l)
	}
	if lo%2 == 0 {
		lo++ // stop at the lowest odd level
	}
	for ; hi >= lo; hi-- {
		for i := 0; i < len(order); {
			if levels[order[i]] < hi {
				i++
				continue
			}
			j := i + 1
			for j < len(order) && levels[order[j]] >= hi {
				j++
			}
			for a, b := i, j-1; a < b; a, b = a+1, b-1 {
				order[a], order[b] = order[b], order[a]
			}
			i = j
		}
	}
	return order
}
