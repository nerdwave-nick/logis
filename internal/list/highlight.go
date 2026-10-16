package list

import "github.com/nerdwave-nick/logis/internal/grapheme"

// Range is a byte range [Start, End) of a row's text.
type Range struct {
	Start, End int
}

// ByteRanges are the byte ranges of text covering the grapheme positions
// (ascending, as Filter.Positions gives them), adjacent graphemes joined
// into one range. Positions past the text's end are ignored. ASCII text is
// counted byte by byte, as the matcher counts it.
func ByteRanges(text string, positions []uint32) []Range {
	var out []Range
	ascii := isASCII(text)
	want := 0
	for at, n := 0, uint32(0); at < len(text) && want < len(positions); n++ {
		size := 1
		if !ascii {
			size = grapheme.Next(text[at:])
		}
		if positions[want] == n {
			want++
			if last := len(out) - 1; last >= 0 && out[last].End == at {
				out[last].End = at + size
			} else {
				out = append(out, Range{at, at + size})
			}
		}
		at += size
	}
	return out
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
