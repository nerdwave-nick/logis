package render

import (
	"math"
	"strings"

	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// Text is one line of text in a scene, painted after the shapes and
// images (Rust render::Text, the simple-run part). Lengths are logical
// pixels. Line breaks are drawn as spaces: it stays one line.
type Text struct {
	Text string
	// Origin is the left and top of the line box.
	Origin [2]float32
	// Size is the em; LineHeight the line box's height, the glyphs centred
	// in it.
	Size, LineHeight float32
	// Color is ordinary glyphs' colour; colour emoji keep their own.
	Color Color
	// Face indexes the renderer's faces (SetFaces); 0, or one out of
	// range, is the first, and without faces the default family.
	Face int
	// Tracking is letter spacing in ems.
	Tracking float32
}

// NewText is a line at origin with a line box of 1.3 × size.
func NewText(s string, origin [2]float32, size float32, c Color) Text {
	return Text{Text: s, Origin: origin, Size: size, LineHeight: size * 1.3, Color: c}
}

// shapedLine is a line shaped at a scale, reused while both stay equal.
type shapedLine struct {
	text  Text
	scale scale.Scale
	line  text.Line
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

// shape is texts shaped at s, reusing unchanged lines (Rust TextPass
// prepare): in buffer pixels, so advances round to the real pixel grid.
func (p *glyphPass) shape(texts []Text, s scale.Scale) {
	p.shaped = p.shaped[:min(len(p.shaped), len(texts))]
	f := s.Factor()
	for i, t := range texts {
		if i < len(p.shaped) && p.shaped[i].scale == s && p.shaped[i].text == t {
			continue
		}
		// One byte for one: the clusters' byte offsets still hold.
		line := strings.NewReplacer("\n", " ", "\r", " ").Replace(t.Text)
		shaped := shapedLine{text: t, scale: s, line: p.fonts.Shape(line, t.Face, max(t.Size*f, 1), t.Tracking)}
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
