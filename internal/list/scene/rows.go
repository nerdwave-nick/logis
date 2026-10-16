package scene

import (
	"math"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The rows: each a single line whose tab-separated columns line up with the
// other rows' (at the widths the Look carries, else estimated) and take the
// theme's column colours, matched characters set apart in the match colour,
// face and underline; an emblem column when some item has one; badges and
// the pending mark at the end. The selected row lies on the band.

// selectionBand draws the band on the row whose top is top, pitch tall,
// with its edge bar.
func selectionBand(sc *render.Scene, body [4]float32, top, pitch float32, t *theme.Theme) {
	s := t.Selection
	x, w := body[0], body[2]
	left, width := x+s.Inset, max(w-2*s.Inset, 0)
	band := render.RoundedRect{
		Rect: [4]float32{left, top + 1, width, pitch - 2}, Radius: 2,
		Fill: render.Ramp([2]float32{x, 0}, [2]float32{x + w, 0}, s.Band...),
	}
	if s.Outline.A > 0 {
		band.Border = render.Border{Width: 1, Paint: render.Solid(s.Outline)}
	}
	if s.Glow.A > 0 {
		band.Glow = render.Glow{Radius: 14, Color: s.Glow}
	}
	sc.Shapes = append(sc.Shapes, band)
	if s.Edge.A > 0 {
		sc.Shapes = append(sc.Shapes, render.RoundedRect{
			Rect: [4]float32{left + 6, top + 7, 3, max(pitch-14, 1)}, Radius: 1.5, Fill: render.Solid(s.Edge),
			Glow: render.Glow{Radius: 8, Color: s.Edge.WithAlpha(s.Edge.A * 0.9)},
		})
	}
}

// rows draws every row in view that fits, icons from icons, its columns
// lined up at the stops all rows share.
func rows(sc *render.Scene, l *list.Look, g Grid, s Style, icons Icons) {
	t := s.Theme
	font := t.Row.Font
	side := IconSize(font)
	emblem := float32(side) + font.Size*0.4
	left := g.Left + t.Row.Padding
	width := g.Right - t.Row.Padding - left
	if l.Glyphs {
		left += emblem
		width -= emblem
	}
	shown := l.Rows[:min(len(l.Rows), g.Rows)]
	stops := newStops(widths(l, shown, t), width, t)
	for i, row := range shown {
		centre := g.Top(i) + g.Pitch/2
		fade := float32(1)
		if row.Pending {
			fade = t.Pending.Opacity
		}
		if l.Glyphs {
			emblemColumn(sc, row, left-emblem, centre, emblem, side, fade, t, icons)
		}
		ends := marks(row, left+width, centre, s)
		sc.Texts = append(sc.Texts, cells(row, left, centre, width-ends.width, stops, t)...)
		sc.Shapes = append(sc.Shapes, ends.shapes...)
		sc.Texts = append(sc.Texts, ends.texts...)
	}
}

// emblemColumn draws a row's icon, or else its glyph, at at.
func emblemColumn(sc *render.Scene, row list.Row, at, centre, emblem float32, side uint32, fade float32, t *theme.Theme, icons Icons) {
	if row.Icon != "" && icons != nil {
		if id, ok := icons.Image(row.Icon, side); ok {
			sc.Images = append(sc.Images, render.ImageQuad{Image: id, Origin: [2]float32{at, centre - float32(side)/2}, Opacity: fade})
			return
		}
	}
	if row.Glyph == "" {
		return
	}
	c := t.Row.Glyph
	if row.Selected {
		c = t.Selection.Columns[0]
	}
	sc.Texts = append(sc.Texts, column(text(row.Glyph, at, centre, t.Row.Font, faded(c, fade)), emblem-4, render.Clip))
}

// widths are the columns' widths: as the Look carries them, or else
// estimated from shown.
func widths(l *list.Look, shown []list.Row, t *theme.Theme) []float32 {
	if len(l.Columns) > 0 {
		return l.Columns
	}
	texts := make([]string, len(shown))
	for i, r := range shown {
		texts[i] = r.Text
	}
	w, _ := widest(texts, t.Row.Font, NoMeasure)
	return w
}

// stops are where the columns start, from the left of the row's text:
// each after the one before, at its width (at most the theme's share of
// the row) and the gap.
type stops struct {
	starts []float32
	gap    float32
}

func newStops(widths []float32, width float32, t *theme.Theme) stops {
	limit := float32(math.Floor(float64(t.Row.ColumnMax * width)))
	s := stops{starts: []float32{0}, gap: t.Row.ColumnGap}
	at := float32(0)
	for _, w := range widths {
		at += min(ceil(w), limit) + s.gap
		s.starts = append(s.starts, at)
	}
	return s
}

// column is column i's start and, unless it is the row's last, its width:
// up to half the gap beyond its text, so a slightly wider line (a bolder
// match) still fits.
func (s stops) column(i int, last bool) (float32, float32, bool) {
	start := s.starts[len(s.starts)-1]
	if i < len(s.starts) {
		start = s.starts[i]
	}
	if last || i+1 >= len(s.starts) {
		return start, 0, false
	}
	return start, s.starts[i+1] - start - s.gap/2, true
}

// cells are a row's cells: each column of its text in its colour at its
// stop, its matches set apart (none while it is pending); the last runs to
// room.
func cells(row list.Row, left, centre, room float32, s stops, t *theme.Theme) []render.Text {
	font := t.Row.Font
	columns, matched := t.Row.Columns, t.Match.Color
	highlights := row.Highlights
	switch {
	case row.Pending:
		c := faded(t.Pending.Color, t.Pending.Opacity)
		columns, matched, highlights = []render.Color{c}, c, nil
	case row.Selected:
		columns, matched = t.Selection.Columns, t.Match.Selected
	}
	ranges := cellRanges(row.Text)
	out := make([]render.Text, 0, len(ranges))
	for i, r := range ranges {
		start, width, bounded := s.column(i, i+1 == len(ranges))
		if !bounded || width > room-start {
			width = room - start
		}
		if width < 1 {
			break
		}
		colour := matched
		switch {
		case i < len(columns):
			colour = columns[i]
		case len(columns) > 0:
			colour = columns[len(columns)-1]
		}
		cell := column(text(row.Text[r.Start:r.End], left+start, centre, font, colour), width, render.Ellipsis)
		face := t.Match.Face
		for _, m := range within(highlights, r) {
			cell.Spans = append(cell.Spans, render.Span{Start: m.Start, End: m.End, Color: matched})
			cell.Emphasis = append(cell.Emphasis, render.Range{Start: m.Start, End: m.End})
		}
		cell.EmphasisFace, cell.UnderlineEmphasis = &face, t.Match.Underline
		out = append(out, cell)
	}
	return out
}

// cellRanges are the byte ranges of text's tab-separated columns.
func cellRanges(text string) []list.Range {
	var out []list.Range
	at := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\t' {
			out = append(out, list.Range{Start: at, End: i})
			at = i + 1
		}
	}
	return append(out, list.Range{Start: at, End: len(text)})
}

// within are the parts of highlights inside cell, counted from its start.
func within(highlights []list.Range, cell list.Range) []list.Range {
	var out []list.Range
	for _, h := range highlights {
		start := max(h.Start, cell.Start) - cell.Start
		end := max(min(h.End, cell.End), cell.Start) - cell.Start
		if end > start {
			out = append(out, list.Range{Start: start, End: end})
		}
	}
	return out
}
