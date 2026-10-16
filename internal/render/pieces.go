package render

import (
	"slices"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// piece is a run of a line with one colour and one face.
type piece struct {
	start, end int
	color      *Color
	emphasised bool
}

// pieces cuts line wherever a colour span or an emphasis begins or ends;
// ranges out of order, overlapping or off a character boundary are skipped.
func (t *Text) pieces(line string) []piece {
	colours := validSpans(line, t.Spans)
	marks := validRanges(line, t.Emphasis)
	cuts := []int{0, len(line)}
	for _, s := range colours {
		cuts = append(cuts, s.Start, s.End)
	}
	for _, r := range marks {
		cuts = append(cuts, r.Start, r.End)
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	var out []piece
	for i := 1; i < len(cuts); i++ {
		p := piece{start: cuts[i-1], end: cuts[i]}
		for j := range colours {
			if colours[j].Start <= p.start && p.start < colours[j].End {
				p.color = &colours[j].Color
			}
		}
		for _, r := range marks {
			p.emphasised = p.emphasised || r.Start <= p.start && p.start < r.End
		}
		out = append(out, p)
	}
	return out
}

func fits(line string, at, start, end int) bool {
	return at <= start && start < end && end <= len(line) &&
		(start == len(line) || utf8.RuneStart(line[start])) && (end == len(line) || utf8.RuneStart(line[end]))
}

func validSpans(line string, spans []Span) []Span {
	var out []Span
	at := 0
	for _, s := range spans {
		if fits(line, at, s.Start, s.End) {
			out = append(out, s)
			at = s.End
		}
	}
	return out
}

func validRanges(line string, ranges []Range) []Range {
	var out []Range
	at := 0
	for _, r := range ranges {
		if fits(line, at, r.Start, r.End) {
			out = append(out, r)
			at = r.End
		}
	}
	return out
}

// options are the text's line options at s.
func (t *Text) options(line string, s scale.Scale) text.LineOptions {
	opt := text.LineOptions{Face: t.Face, Tracking: t.Tracking, AlignEnd: t.AlignEnd}
	if l, r, ok := t.column(s); ok {
		opt.Column, opt.Width, opt.Ellipsis = true, float32(r-l), t.Overflow == Ellipsis
	}
	if t.EmphasisFace != nil {
		for _, p := range t.pieces(line) {
			if p.emphasised {
				opt.Faces = append(opt.Faces, text.FaceSpan{Start: p.start, End: p.end, Face: *t.EmphasisFace})
			}
		}
	}
	return opt
}

// colorAt is the colour of byte i: its span's, else the text's.
func (t *Text) colorAt(line string, i int) Color {
	for _, s := range validSpans(line, t.Spans) {
		if s.Start <= i && i < s.End {
			return s.Color
		}
	}
	return t.Color
}
