package text

import "math"

// Glyph is one positioned glyph of a line, in buffer pixels: X is its
// origin's distance from the line's start, Y its offset below the
// baseline. Lines are laid out left to right in visual order.
type Glyph struct {
	Font    FontID
	ID      uint16
	X, Y    float32
	Advance float32
	// Cluster is the byte offset in the text of the character it shows.
	Cluster int
}

// Line is a shaped line: its glyphs, its width (the sum of advances) and
// the largest ascent and descent of the fonts it uses, in pixels.
type Line struct {
	Glyphs                 []Glyph
	Width, Ascent, Descent float32
}

// Shape sets s on one line in face (an index as for Face) at size pixels
// per em with tracking ems of letter spacing, as a simple run: each
// character's glyph from the face's cmap, or from the first loaded font
// that covers it (fallback faces are loaded first, see Ensure); advances
// from hmtx plus pair kerning between glyphs of one font, each rounded to
// a whole pixel as cosmic-text's hinted layout does; ignorables hidden.
// No substitutions, marks, bidi or line breaking: the text-shaping slice
// replaces this with its OpenType engine and keeps the Line it returns.
func (f *Fonts) Shape(s string, face int, size, tracking float32) Line {
	f.Ensure(s)
	want := f.face(face)
	var line Line
	var units []int // each glyph's advance in font units, kerning included
	for i, r := range s {
		if Ignorable(r) {
			continue
		}
		id, g := f.cover(want, r)
		font := f.fonts[id]
		if n := len(line.Glyphs); n > 0 && line.Glyphs[n-1].Font == id {
			// A pair's kerning adjusts the first glyph's advance.
			units[n-1] += font.kern(line.Glyphs[n-1].ID, g)
		}
		line.Glyphs = append(line.Glyphs, Glyph{Font: id, ID: g, Cluster: i})
		units = append(units, font.advance(g))
	}
	used := map[FontID]bool{want: len(line.Glyphs) == 0}
	for i := range line.Glyphs {
		g := &line.Glyphs[i]
		g.X, g.Advance = line.Width, px(units[i], f.fonts[g.Font].Metrics.UnitsPerEm, size, tracking)
		line.Width += g.Advance
		used[g.Font] = true
	}
	for id, ok := range used {
		if !ok {
			continue
		}
		m := f.fonts[id].Metrics
		line.Ascent = max(line.Ascent, float32(m.Ascent)*size/float32(m.UnitsPerEm))
		line.Descent = max(line.Descent, float32(m.Descent)*size/float32(m.UnitsPerEm))
	}
	return line
}

// cover is the font and glyph for r: want's if its cmap has r, else the
// first loaded font's that has, else want's missing glyph.
func (f *Fonts) cover(want FontID, r rune) (FontID, uint16) {
	if g := f.fonts[want].GlyphIndex(r); g != 0 {
		return want, g
	}
	for id, font := range f.fonts {
		if g := font.GlyphIndex(r); g != 0 {
			return FontID(id), g
		}
	}
	return want, 0
}

// px is an advance of units (per upem) plus tracking ems at size, rounded
// to a whole pixel half away from zero, in single precision as
// cosmic-text computes it.
func px(units, upem int, size, tracking float32) float32 {
	em := float32(units)/float32(upem) + tracking
	return float32(math.Round(float64(size * em)))
}
