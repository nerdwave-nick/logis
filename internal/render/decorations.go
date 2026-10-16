package render

import "math"

// decorations are what the prepared lines draw with the shapes: the
// underlines under their emphasis and their carets, in logical pixels.
func (p *glyphPass) decorations() []RoundedRect {
	var out []RoundedRect
	for i := range p.shaped {
		l := &p.shaped[i]
		if l.text.UnderlineEmphasis && len(l.text.Emphasis) > 0 {
			out = append(out, underlines(l)...)
		}
		if c, ok := caret(l); ok {
			out = append(out, c)
		}
	}
	return out
}

// caret is a line's caret: before the glyph its byte offset starts, or
// after the last one, as tall as the glyphs (a bar or a block) or low at
// the baseline (an underscore), with its glow; none outside the column.
func caret(l *shapedLine) (RoundedRect, bool) {
	c := l.text.Caret
	if c == nil {
		return RoundedRect{}, false
	}
	f := l.scale.Factor()
	at := place(l)
	baseline := (max(l.text.LineHeight*f, 1)-(l.line.Ascent+l.line.Descent))/2 + l.line.Ascent
	ascent, descent := at.size*0.8, at.size*0.2
	x, advance, found := float32(0), at.size*0.6, false
	for _, g := range l.line.Glyphs {
		if !g.Ellipsis && g.Cluster >= c.At {
			x, advance, found = g.X, g.Advance, true
			break
		}
	}
	if !found {
		for i := len(l.line.Glyphs) - 1; i >= 0; i-- {
			if g := l.line.Glyphs[i]; !g.Ellipsis {
				x = g.X + g.Advance
				break
			}
		}
	}
	x += at.left
	var rect [4]float32
	switch c.Shape {
	case CaretBlock:
		rect = [4]float32{x, at.top + baseline - ascent, advance, ascent + descent}
	case CaretUnderscore:
		rect = [4]float32{x, at.top + baseline + max(at.size*0.08, 1), advance, max(at.size*0.1, 1)}
	default:
		w := max(c.Width*f, 1)
		rect = [4]float32{x - w/2, at.top + baseline - ascent, w, ascent + descent}
	}
	if left, right, ok := l.text.column(l.scale); ok && (rect[0]+rect[2] <= float32(left) || rect[0] >= float32(right)) {
		return RoundedRect{}, false
	}
	shape := bar(rect, f, c.Color)
	if c.Glow.A > 0 {
		shape.Glow = Glow{Radius: 6, Color: c.Glow}
	}
	return shape, true
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
