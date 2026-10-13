// Package grapheme splits text into extended grapheme clusters (UAX #29,
// rules GB3–GB13 including GB9c), which neither the Go standard library nor
// golang.org/x/text provides. The properties are generated from UCD 17.0.0,
// the version of Go 1.27's unicode package (gen.go); the breaker passes the
// whole of that version's GraphemeBreakTest.txt.
package grapheme

import (
	"sort"
	"unicode/utf8"
)

//go:generate go run gen.go

// Grapheme_Cluster_Break values.
const (
	gcbOther byte = iota
	gcbCR
	gcbLF
	gcbControl
	gcbExtend
	gcbZWJ
	gcbRegionalIndicator
	gcbPrepend
	gcbSpacingMark
	gcbL
	gcbV
	gcbT
	gcbLV
	gcbLVT
)

// Indic_Conjunct_Break values.
const (
	inCBNone byte = iota
	inCBLinker
	inCBConsonant
	inCBExtend
)

type rangeVal struct {
	lo, hi rune
	val    byte
}

func find(t []rangeVal, r rune) (byte, bool) {
	i := sort.Search(len(t), func(i int) bool { return t[i].hi >= r })
	if i < len(t) && t[i].lo <= r {
		return t[i].val, true
	}
	return 0, false
}

func breakProperty(r rune) byte {
	if r < 0x7F && r >= 0x20 {
		return gcbOther
	}
	v, _ := find(graphemeBreak, r)
	return v
}

func pictographic(r rune) bool {
	if r < 0xA9 {
		return false
	}
	_, ok := find(extPict, r)
	return ok
}

func conjunct(r rune) byte {
	if r < 0x300 {
		return inCBNone
	}
	v, _ := find(inCB, r)
	return v
}

// Next returns the byte length of the first grapheme cluster of s (0 for
// an empty s). An invalid UTF-8 byte counts as U+FFFD.
func Next(s string) int {
	if s == "" {
		return 0
	}
	r, at := utf8.DecodeRuneInString(s)
	if at == len(s) || (r < 0x80 && s[at] < 0x80 && r != '\r') {
		return at // the common case: ASCII followed by ASCII
	}
	prev := breakProperty(r)
	st := state{emoji: pictographic(r)}
	if prev == gcbRegionalIndicator {
		st.ri = 1
	}
	st.consonant = conjunct(r) == inCBConsonant
	for at < len(s) {
		r, size := utf8.DecodeRuneInString(s[at:])
		cur := breakProperty(r)
		pict := pictographic(r)
		incb := conjunct(r)
		if breaks(prev, cur, st.emojiZWJ && pict, st.ri, st.consonant && st.linker && incb == inCBConsonant) {
			return at
		}
		st.advance(cur, pict, incb)
		prev = cur
		at += size
	}
	return at
}

// state is what the rules that look further back than one character need.
type state struct {
	// GB11: Extended_Pictographic Extend*, then whether a ZWJ followed.
	emoji, emojiZWJ bool
	// GB12/13: regional indicators in a row so far.
	ri int
	// GB9c: a consonant seen, then a linker after it.
	consonant, linker bool
}

func (st *state) advance(cur byte, pict bool, incb byte) {
	switch {
	case pict:
		st.emoji, st.emojiZWJ = true, false
	case cur == gcbExtend && st.emoji && !st.emojiZWJ:
	case cur == gcbZWJ && st.emoji && !st.emojiZWJ:
		st.emojiZWJ = true
	default:
		st.emoji, st.emojiZWJ = false, false
	}
	if cur == gcbRegionalIndicator {
		st.ri++
	} else {
		st.ri = 0
	}
	switch incb {
	case inCBConsonant:
		st.consonant, st.linker = true, false
	case inCBLinker:
		st.linker = st.linker || st.consonant
	case inCBExtend:
	default:
		st.consonant, st.linker = false, false
	}
}

func breaks(prev, cur byte, gb11 bool, ri int, gb9c bool) bool {
	switch {
	case prev == gcbCR && cur == gcbLF: // GB3
		return false
	case prev == gcbControl || prev == gcbCR || prev == gcbLF: // GB4
		return true
	case cur == gcbControl || cur == gcbCR || cur == gcbLF: // GB5
		return true
	case prev == gcbL && (cur == gcbL || cur == gcbV || cur == gcbLV || cur == gcbLVT): // GB6
		return false
	case (prev == gcbLV || prev == gcbV) && (cur == gcbV || cur == gcbT): // GB7
		return false
	case (prev == gcbLVT || prev == gcbT) && cur == gcbT: // GB8
		return false
	case cur == gcbExtend || cur == gcbZWJ || cur == gcbSpacingMark: // GB9, GB9a
		return false
	case prev == gcbPrepend: // GB9b
		return false
	case gb9c: // GB9c
		return false
	case prev == gcbZWJ && gb11: // GB11
		return false
	case prev == gcbRegionalIndicator && cur == gcbRegionalIndicator: // GB12, GB13
		return ri%2 == 0
	}
	return true // GB999
}

// Clusters splits s into grapheme clusters.
func Clusters(s string) []string {
	var out []string
	for s != "" {
		n := Next(s)
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}
