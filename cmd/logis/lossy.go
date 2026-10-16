package main

import (
	"strings"
	"unicode/utf8"
)

// lossy is s as text, the way Rust's to_string_lossy read an argument: each
// maximal ill-formed subsequence (Unicode's "maximal subpart": a lead byte
// and the continuation bytes that still fit it) becomes one U+FFFD.
func lossy(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if r, size := utf8.DecodeRuneInString(s[i:]); r != utf8.RuneError || size > 1 {
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		b.WriteRune(utf8.RuneError)
		i += maximalSubpart(s[i:])
	}
	return b.String()
}

// maximalSubpart is the length of the ill-formed sequence at the start of s:
// its lead byte and the continuation bytes that would still fit it.
func maximalSubpart(s string) int {
	lo, hi, length := byte(0x80), byte(0xBF), 0
	switch c := s[0]; {
	case c >= 0xC2 && c <= 0xDF:
		length = 2
	case c == 0xE0:
		lo, length = 0xA0, 3
	case c == 0xED:
		hi, length = 0x9F, 3
	case c >= 0xE1 && c <= 0xEF:
		length = 3
	case c == 0xF0:
		lo, length = 0x90, 4
	case c == 0xF4:
		hi, length = 0x8F, 4
	case c >= 0xF1 && c <= 0xF3:
		length = 4
	default:
		return 1
	}
	n := 1
	for n < length && n < len(s) && s[n] >= lo && s[n] <= hi {
		n++
		lo, hi = 0x80, 0xBF
	}
	return n
}
