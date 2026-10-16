package render

import (
	"math"
	"slices"
	"strings"

	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// Overflow is what a line wider than its column does.
type Overflow int

const (
	// Ellipsis ends the line with "…" so that it fits, then cuts it at
	// the column's edges.
	Ellipsis Overflow = iota
	// Clip cuts the line at the column's edges.
	Clip
)

// Span paints bytes Start..End of a line in another colour.
type Span struct {
	Start, End int
	Color      Color
}

// Range is a byte range of a line.
type Range struct{ Start, End int }

// Text is one line of text in a scene, painted after the shapes and images.
// Lengths are logical pixels. Line breaks are drawn as spaces: it stays one
// line.
type Text struct {
	Text string
	// Origin is the left and top of the line box.
	Origin [2]float32
	// Size is the em; LineHeight the line box's height, the glyphs centred
	// in it.
	Size, LineHeight float32
	// Color is ordinary glyphs' colour; colour emoji keep their own.
	Color Color
	// Column confines the line to Width from Origin[0]; Overflow says
	// what a line too wide for it does.
	Column   bool
	Width    float32
	Overflow Overflow
	// Spans paint byte ranges in their own colour (match highlights),
	// ascending and not overlapping.
	Spans []Span
	// AlignEnd ends the line at its column's end edge.
	AlignEnd bool
	// Face indexes the renderer's faces (SetFaces); 0, or one out of
	// range, is the first, and without faces the default family.
	Face int
	// Tracking is letter spacing in ems.
	Tracking float32
	// Emphasis sets byte ranges apart (matched characters), ascending and
	// not overlapping: in EmphasisFace if set, underlined if
	// UnderlineEmphasis, in whatever colour they have otherwise.
	Emphasis          []Range
	EmphasisFace      *int
	UnderlineEmphasis bool
	// Caret is where the next character goes, if shown.
	Caret *Caret
}

// NewText is a line at origin with a line box of 1.3 × size.
func NewText(s string, origin [2]float32, size float32, c Color) Text {
	return Text{Text: s, Origin: origin, Size: size, LineHeight: size * 1.3, Color: c}
}

// InColumn is the same line confined to a column of width.
func (t Text) InColumn(width float32, o Overflow) Text {
	t.Column, t.Width, t.Overflow = true, width, o
	return t
}

// equal reports texts that shape and draw alike.
func (t *Text) equal(u *Text) bool {
	return t.Text == u.Text && t.Origin == u.Origin && t.Size == u.Size && t.LineHeight == u.LineHeight &&
		t.Color == u.Color && t.Column == u.Column && t.Width == u.Width && t.Overflow == u.Overflow &&
		slices.Equal(t.Spans, u.Spans) && t.AlignEnd == u.AlignEnd && t.Face == u.Face &&
		t.Tracking == u.Tracking && slices.Equal(t.Emphasis, u.Emphasis) &&
		(t.EmphasisFace == nil) == (u.EmphasisFace == nil) &&
		(t.EmphasisFace == nil || *t.EmphasisFace == *u.EmphasisFace) &&
		t.UnderlineEmphasis == u.UnderlineEmphasis &&
		(t.Caret == nil) == (u.Caret == nil) && (t.Caret == nil || *t.Caret == *u.Caret)
}

// column is the column's left and right edges in buffer pixels.
func (t *Text) column(s scale.Scale) (int, int, bool) {
	if !t.Column {
		return 0, 0, false
	}
	f := s.Factor()
	left := int(round32(t.Origin[0] * f))
	return left, left + int(round32(max(t.Width, 0)*f)), true
}

// line is the text as one line: line breaks as spaces, one byte for one,
// so byte ranges still hold.
func (t *Text) line() string { return strings.NewReplacer("\n", " ", "\r", " ").Replace(t.Text) }

// shapedLine is a line shaped at a scale, reused while both stay equal.
type shapedLine struct {
	text  Text
	scale scale.Scale
	line  text.Line
	str   string
}

// SetFonts gives the renderer the fonts it draws text with; it owns them
// from now on and releases them in Close. Without fonts, texts are not
// drawn (the cogitator stays mute).
func (r *Renderer) SetFonts(f *text.Fonts) {
	r.glyphs.setFonts(f)
}

// SetFaces resolves the faces texts name by index (a theme's); every line
// is shaped anew. See text.Fonts.SetFaces for faces that do not resolve.
func (r *Renderer) SetFaces(faces []text.Face) error {
	if r.glyphs.fonts == nil {
		return nil
	}
	r.glyphs.shaped = r.glyphs.shaped[:0]
	return r.glyphs.fonts.SetFaces(faces)
}

// Fonts are the renderer's fonts, nil before SetFonts.
func (r *Renderer) Fonts() *text.Fonts { return r.glyphs.fonts }

// GlyphCount is how many glyph images the glyph atlas holds.
func (r *Renderer) GlyphCount() int { return len(r.glyphs.cache) }

// MeasureText is the width t takes on one line at s in logical pixels,
// ignoring its column, colours and emphasis; false without fonts.
func (r *Renderer) MeasureText(t Text, s scale.Scale) (float32, bool) {
	if r.glyphs.fonts == nil {
		return 0, false
	}
	f := s.Factor()
	l := r.glyphs.fonts.Layout(t.line(), max(t.Size*f, 1), text.LineOptions{Face: t.Face, Tracking: t.Tracking})
	return l.Width / f, true
}

// shape is texts shaped at s, reusing unchanged lines: in buffer pixels, so
// advances round to the real pixel grid.
func (p *glyphPass) shape(texts []Text, s scale.Scale) {
	p.shaped = p.shaped[:min(len(p.shaped), len(texts))]
	f := s.Factor()
	for i := range texts {
		t := &texts[i]
		if i < len(p.shaped) && p.shaped[i].scale == s && p.shaped[i].text.equal(t) {
			continue
		}
		str := t.line()
		shaped := shapedLine{text: *t, scale: s, str: str,
			line: p.fonts.Layout(str, max(t.Size*f, 1), t.options(str, s))}
		if i < len(p.shaped) {
			p.shaped[i] = shaped
		} else {
			p.shaped = append(p.shaped, shaped)
		}
	}
}

// placed is where a shaped line's glyphs go, as glyphon places them: the
// line's left edge and top snapped to whole pixels, the baseline at the
// line box's centre less half the glyphs' height plus the ascent.
type placed struct {
	left, top, size float32
	baseline        int
}

func place(l *shapedLine) placed {
	f := l.scale.Factor()
	size := max(l.text.Size*f, 1)
	height := max(l.text.LineHeight*f, 1)
	lineY := (height-(l.line.Ascent+l.line.Descent))/2 + l.line.Ascent
	return placed{
		left:     round32(l.text.Origin[0] * f),
		top:      round32(l.text.Origin[1] * f),
		size:     size,
		baseline: int(round32(lineY)),
	}
}

func round32(v float32) float32 { return float32(math.Round(float64(v))) }

// subpixelBin is cosmic-text's SubpixelBin::new: a position as a whole
// pixel and a quarter-pixel bin.
func subpixelBin(pos float32) (int, uint8) {
	trunc := int(pos)
	fract := pos - float32(trunc)
	if pos < 0 {
		switch {
		case fract > -0.125:
			return trunc, 0
		case fract > -0.375:
			return trunc - 1, 3
		case fract > -0.625:
			return trunc - 1, 2
		case fract > -0.875:
			return trunc - 1, 1
		}
		return trunc - 1, 0
	}
	switch {
	case fract < 0.125:
		return trunc, 0
	case fract < 0.375:
		return trunc, 1
	case fract < 0.625:
		return trunc, 2
	case fract < 0.875:
		return trunc, 3
	}
	return trunc + 1, 0
}

// Caret is a caret drawn at a character of a line.
type Caret struct {
	// At is the byte offset in the line's text it stands before.
	At    int
	Shape CaretShape
	// Width is a bar's width in logical pixels (a block or an underscore
	// is as wide as the character it stands at, or an average one at the
	// end).
	Width float32
	Color Color
	// Glow is its glow (transparent for none).
	Glow Color
}

// CaretShape is a caret's form.
type CaretShape int

const (
	// CaretBar is a thin upright bar as tall as the glyphs.
	CaretBar CaretShape = iota
	// CaretBlock is a block over the character.
	CaretBlock
	// CaretUnderscore is a low bar under the character.
	CaretUnderscore
)
