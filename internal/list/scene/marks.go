package scene

import (
	"strings"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// What ends a row (its badges in their order, then the pending mark,
// shimmering) and the toast across the bottom of the body (Rust
// src/list/scene/marks.rs).

// rowMarks are what ends a row, and the room they take from it, gaps
// included.
type rowMarks struct {
	shapes []render.RoundedRect
	texts  []render.Text
	width  float32
}

type pill struct {
	words           string
	ink, edge, fill render.Color
}

// marks are the badges of row and its pending mark, ending at right,
// centred on centre.
func marks(row list.Row, right, centre float32, s Style) rowMarks {
	t := s.Theme
	b, p := t.Badge, t.Pending
	fade := float32(1)
	if row.Pending {
		fade = p.Opacity
	}
	var pills []pill
	for _, words := range row.Badges {
		pills = append(pills, pill{words, faded(b.Ink, fade), faded(b.Edge, fade), faded(b.Fill, fade)})
	}
	if row.Pending {
		shimmer := 1 - p.Shimmer*(1-s.wave(int64(p.Period)))
		ink := p.Color.WithAlpha(p.Color.A * shimmer)
		pills = append(pills, pill{p.Mark, ink, ink.WithAlpha(ink.A * 0.6), render.Transparent})
	}
	if len(pills) == 0 {
		return rowMarks{}
	}
	pad, gap, height := b.Font.Size*0.55, b.Font.Size*0.55, b.Font.Size*1.55
	if b.Style == theme.BadgeBracket {
		pad = 0
	}
	var out rowMarks
	ws := make([]float32, len(pills))
	for i := range pills {
		pills[i].words = badgeWords(pills[i].words, b)
		ws[i] = float32(utf8.RuneCountInString(pills[i].words))*advance(b.Font) + 2*pad
		out.width += ws[i] + gap
	}
	left := right - out.width + gap
	for i, pl := range pills {
		w := ws[i]
		if b.Style != theme.BadgeBracket {
			shape := render.RoundedRect{Rect: [4]float32{left, centre - height/2, w, height}, Radius: height / 2, Fill: render.Solid(pl.fill)}
			if b.Style == theme.BadgeFill {
				shape.Fill = render.Solid(pl.edge)
			} else {
				shape.Border = render.Border{Width: 1, Paint: render.Solid(pl.edge)}
			}
			out.shapes = append(out.shapes, shape)
		}
		out.texts = append(out.texts, column(text(pl.words, left+pad, centre, b.Font, pl.ink), w-2*pad+2, render.Clip))
		left += w + gap
	}
	return out
}

// badgeWords is a badge's words in the theme's style.
func badgeWords(words string, b theme.Badge) string {
	if b.Uppercase {
		words = strings.ToUpper(words)
	}
	if b.Style == theme.BadgeBracket {
		words = "[" + words + "]"
	}
	return words
}

// toast draws a toast's strip across the bottom of body, and its words:
// its edge in the colour of its level.
func toast(sc *render.Scene, n list.Notice, body [4]float32, t *theme.Theme) {
	font := t.Row.Font
	height := font.Size*1.3 + 12
	const inset = 8
	edge := t.Prompt.Color
	switch n.Level {
	case list.Success:
		edge = t.Match.Color
	case list.Error:
		edge = t.Error
	}
	rect := [4]float32{body[0] + inset, body[1] + body[3] - height - inset, body[2] - 2*inset, height}
	base := render.Hex(0)
	if len(t.Panel.Fill) > 0 {
		base = t.Panel.Fill[len(t.Panel.Fill)-1]
	}
	sc.Shapes = append(sc.Shapes, render.RoundedRect{
		Rect: rect, Radius: 6, Fill: render.Solid(base.WithAlpha(0.97)),
		Border: render.Border{Width: 1.5, Paint: render.Solid(edge)},
		Glow:   render.Glow{Radius: 8, Color: edge.WithAlpha(0.35)},
	})
	words := text(n.Text, rect[0]+12, rect[1]+height/2, font, t.Row.Columns[0])
	sc.Texts = append(sc.Texts, column(words, rect[2]-24, render.Ellipsis))
}
