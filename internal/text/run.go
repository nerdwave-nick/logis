package text

import (
	"math"
	"slices"
	"strings"

	"github.com/nerdwave-nick/logis/internal/shape"
)

// shapedGlyph is cosmic-text's ShapeGlyph: a glyph of a word with its
// byte range, and its advance (letter spacing included), offsets, and its
// font's ascent and descent, in ems.
type shapedGlyph struct {
	start, end       int
	xAdvance         float32
	xOffset, yOffset float32
	ascent, descent  float32
	font             FontID
	id               uint16
}

// width is the glyph's advance at size pixels per em as it is drawn: a
// whole pixel. A line is laid out and measured in these.
func (g *shapedGlyph) width(size float32) float32 {
	return float32(math.Round(float64(size * g.xAdvance)))
}

// shapeWith shapes line[start:end] with one font (cosmic-text's
// shape_fallback): tabs as spaces, the glyphs in visual order, and the
// byte offsets of the clusters the font has no glyph for.
func (f *Fonts) shapeWith(id FontID, line string, start, end int, rtl bool, tracking float32) ([]shapedGlyph, []int) {
	font := f.fonts[id]
	run := strings.ReplaceAll(line[start:end], "\t", " ")
	upem := float32(font.Metrics.UnitsPerEm)
	ascent, descent := float32(font.Metrics.Ascent)/upem, float32(font.Metrics.Descent)/upem
	var glyphs []shapedGlyph
	var missing []int
	for _, g := range font.shaper.Shape(run, shape.Options{RTL: rtl}) {
		s := start + g.Cluster
		if g.ID == 0 {
			missing = append(missing, s)
		}
		glyphs = append(glyphs, shapedGlyph{
			start: s, end: end,
			xAdvance: float32(float32(g.XAdvance)/upem) + tracking,
			xOffset:  float32(g.XOffset) / upem,
			yOffset:  float32(g.YOffset) / upem,
			ascent:   ascent, descent: descent,
			font: id, id: g.ID,
		})
	}
	// A glyph ends where the next cluster in logical order begins.
	if rtl {
		for i := 1; i < len(glyphs); i++ {
			next := glyphs[i-1]
			if glyphs[i].start == next.start {
				glyphs[i].end = next.end
			} else {
				glyphs[i].end = next.start
			}
		}
	} else {
		for i := len(glyphs) - 1; i > 0; i-- {
			next := glyphs[i]
			if glyphs[i-1].start == next.start {
				glyphs[i-1].end = next.end
			} else {
				glyphs[i-1].end = next.start
			}
		}
	}
	return glyphs, missing
}

// shapeRun shapes line[start:end] in face (cosmic-text's shape_run): with
// the face's font, then cluster by cluster with fallback fonts for what
// it lacks.
func (f *Fonts) shapeRun(line string, start, end int, rtl bool, face FontID, tracking float32) []shapedGlyph {
	if start >= end {
		return nil
	}
	glyphs, missing := f.shapeWith(face, line, start, end, rtl, tracking)
	for _, id := range f.fallbackOrder(face) {
		if len(missing) == 0 {
			break
		}
		fb, fbMissing := f.shapeWith(id, line, start, end, rtl, tracking)
		for i := 0; i < len(fb); {
			s, e := fb[i].start, fb[i].end
			if !slices.Contains(missing, s) || slices.Contains(fbMissing, s) {
				i++
				continue
			}
			missing = slices.DeleteFunc(missing, func(m int) bool { return m >= s && m < e })
			inCluster := func(g shapedGlyph) bool { return g.start >= s && g.end <= e }
			j := slices.IndexFunc(glyphs, inCluster)
			if j < 0 {
				j = len(glyphs)
			}
			k := j
			for k < len(glyphs) && inCluster(glyphs[k]) {
				k++
			}
			n := i
			for n < len(fb) && inCluster(fb[n]) {
				n++
			}
			glyphs = slices.Replace(glyphs, j, k, fb[i:n]...)
			fb = slices.Delete(fb, i, n)
		}
	}
	return glyphs
}

// fallbackOrder is the order cosmic-text tries fonts in after the face's
// own: emoji fonts first, then by closeness to the face's weight, stretch
// and upright style, then weight, stretch and load order.
func (f *Fonts) fallbackOrder(face FontID) []FontID {
	weight := f.fonts[face].Weight
	ids := make([]FontID, 0, len(f.fonts))
	for i := range f.fonts {
		if FontID(i) != face {
			ids = append(ids, FontID(i))
		}
	}
	key := func(id FontID) [6]int {
		font := f.fonts[id]
		notEmoji, style := 1, 0
		if font.emoji {
			notEmoji = 0
		}
		if font.slanted {
			style = 2
		}
		return [6]int{notEmoji, abs(font.Weight - weight), abs(font.stretch - 100), style, font.Weight, font.stretch}
	}
	slices.SortStableFunc(ids, func(a, b FontID) int {
		ka, kb := key(a), key(b)
		for i := range ka {
			if ka[i] != kb[i] {
				return ka[i] - kb[i]
			}
		}
		return int(a - b)
	})
	return ids
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
