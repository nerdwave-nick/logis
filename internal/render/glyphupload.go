package render

import (
	"cmp"
	"slices"

	"github.com/nerdwave-nick/logis/internal/text"
)

// pendingGlyph is a glyph image placed in the atlas, not yet uploaded.
type pendingGlyph struct {
	x, y uint32
	img  *text.GlyphImage
}

// flush uploads the pending glyph images, one upload per shelf they lie on:
// each upload is a driver call of tens of microseconds, and a first frame
// of a panel full of text rasterises a hundred glyphs or more. The shelf
// packer fills a shelf left to right and never frees, so a frame's new
// glyphs on one shelf form a strip holding only them and free padding,
// written as zeros.
func (p *glyphPass) flush() {
	if len(p.pending) == 0 {
		return
	}
	slices.SortFunc(p.pending, func(a, b pendingGlyph) int {
		return cmp.Or(cmp.Compare(a.y, b.y), cmp.Compare(a.x, b.x))
	})
	for start := 0; start < len(p.pending); {
		end := start + 1
		for end < len(p.pending) && p.pending[end].y == p.pending[start].y {
			end++
		}
		p.uploadStrip(p.pending[start:end])
		start = end
	}
	clear(p.pending) // let the images go
	p.pending = p.pending[:0]
}

// uploadStrip uploads glyphs of one shelf, ordered left to right, as one
// rectangle.
func (p *glyphPass) uploadStrip(glyphs []pendingGlyph) {
	x0, y := glyphs[0].x, glyphs[0].y
	var x1, h uint32
	for _, g := range glyphs {
		x1, h = max(x1, g.x+uint32(g.img.Width)), max(h, uint32(g.img.Height))
	}
	w := int(x1 - x0)
	need := w * int(h) * 4
	if cap(p.staging) < need {
		p.staging = make([]byte, need)
	}
	buf := p.staging[:need]
	clear(buf)
	for _, g := range glyphs {
		img := g.img
		for r := range img.Height {
			row := buf[(r*w+int(g.x-x0))*4:]
			if img.Color {
				copy(row[:img.Width*4], img.Pixels[r*img.Width*4:])
				continue
			}
			// Coverage in alpha.
			for i, c := range img.Pixels[r*img.Width : (r+1)*img.Width] {
				row[4*i], row[4*i+1], row[4*i+2], row[4*i+3] = 255, 255, 255, c
			}
		}
	}
	p.tex.Upload(int(x0), int(y), w, int(h), buf)
	p.uploads++
}
