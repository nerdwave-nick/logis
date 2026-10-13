package render

import "math"

// decorations are what the prepared lines draw with the shapes: the
// underlines under their emphasis (Rust TextPass::decorations), in logical
// pixels.
func (p *glyphPass) decorations() []RoundedRect {
	var out []RoundedRect
	for i := range p.shaped {
		l := &p.shaped[i]
		if l.text.UnderlineEmphasis && len(l.text.Emphasis) > 0 {
			out = append(out, underlines(l)...)
		}
	}
	return out
}

// underlines are a line's underlines: one bar per run of consecutive
// glyphs of one emphasised piece, just below the baseline, clipped to the
// line's column.
func underlines(l *shapedLine) []RoundedRect {
	type mark struct {
		start, end int
		color      Color
	}
	var marked []mark
	for _, pc := range l.text.pieces(l.str) {
		if pc.emphasised {
			c := l.text.Color
			if pc.color != nil {
				c = *pc.color
			}
			marked = append(marked, mark{pc.start, pc.end, c})
		}
	}
	f := l.scale.Factor()
	at := place(l)
	clip0, clip1 := float32(-math.MaxFloat32), float32(math.MaxFloat32)
	if left, right, ok := l.text.column(l.scale); ok {
		clip0, clip1 = float32(left), float32(right)
	}
	thickness := max(at.size*0.06, 1)
	lineY := (max(l.text.LineHeight*f, 1)-(l.line.Ascent+l.line.Descent))/2 + l.line.Ascent
	y := at.top + lineY + max(at.size*0.12, 1)
	var bars []RoundedRect
	open, x0, x1 := -1, float32(0), float32(0)
	closeBar := func() {
		if open >= 0 {
			a, b := max(x0, clip0), min(x1, clip1)
			if b > a {
				bars = append(bars, bar([4]float32{a, y, b - a, thickness}, f, marked[open].color))
			}
			open = -1
		}
	}
	for _, g := range l.line.Glyphs {
		piece := -1
		for i, m := range marked {
			if m.start <= g.Cluster && g.Cluster < m.end {
				piece = i
				break
			}
		}
		gx0, gx1 := at.left+g.X, at.left+g.X+g.Advance
		switch {
		case piece >= 0 && piece == open:
			x0, x1 = min(x0, gx0), max(x1, gx1)
		case piece >= 0:
			closeBar()
			open, x0, x1 = piece, gx0, gx1
		default:
			closeBar()
		}
	}
	closeBar()
	return bars
}

// bar is a rectangle in buffer pixels as a shape in logical ones.
func bar(rect [4]float32, f float32, c Color) RoundedRect {
	for i := range rect {
		rect[i] /= f
	}
	return RoundedRect{Rect: rect, Fill: Solid(c)}
}
