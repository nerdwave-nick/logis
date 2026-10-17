// Package scene draws a list as a cogitator panel in a theme (feat/logis
// §4.9): the body with its glow, border and ornaments; the prompt line with
// its sigil, label, query, caret and counter; a rule; the rows below it,
// the selected one on a band, matched characters set apart; badges, pending
// rows and a toast. Rows are single lines whose tab-separated columns line
// up (at measured widths when the Look carries them, else estimated from
// the rows in view) and take the theme's column colours.
//
// Every length is the theme's, in logical pixels. A scene is drawn at a
// moment (Style.At) so the theme's continuous effects (glow pulse,
// travelling highlight, shimmer) move with time; at a frozen moment it is
// a still image.
package scene

import (
	"math"
	"slices"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// Style is a theme at a moment: what a frame is drawn in.
type Style struct {
	// Theme is the theme, already stilled when motion is reduced.
	Theme *theme.Theme
	// At is the frame's time (CLOCK_MONOTONIC ns): the phase of the
	// continuous effects.
	At uint64
	// Glitch is where the theme's glitch stands in this frame: the time
	// since the panel opened and the burst (internal/glitch's clock); a
	// custom pass takes its time too.
	Glitch effects.Moment
	// Frame is how many frames the panel drew before this one (a custom
	// pass's iFrame).
	Frame int
}

// phase is where a cycle of period stands at this moment, 0 to 1 (always
// 0 for a zero period).
func (s Style) phase(period int64) float32 {
	if period <= 0 {
		return 0
	}
	return float32(s.At%uint64(period)) / float32(period)
}

// wave is a slow wave of period between 0 and 1, 1 at the cycle's start.
func (s Style) wave(period int64) float32 {
	return 0.5 + 0.5*float32(math.Cos(float64(s.phase(period))*2*math.Pi))
}

// Icons finds the images rows and the sigil name, at a side in logical
// pixels; nil finds none.
type Icons interface {
	Image(name string, side uint32) (render.ImageID, bool)
}

// BandTop is where the selection band rests: the top of the selected row
// in view; false when no row in view is selected.
func BandTop(l *list.Look, logical scale.Size, t *theme.Theme) (float32, bool) {
	g := GridOf(logical, l.Lines, t)
	shown := l.Rows[:min(len(l.Rows), g.Rows)]
	if i := slices.IndexFunc(shown, func(r list.Row) bool { return r.Selected }); i >= 0 {
		return g.Top(i), true
	}
	return 0, false
}

// Draw is the scene for l on a surface of logical size, the band on the
// selected row, without icons (glyphs only).
func Draw(l *list.Look, logical scale.Size, s Style) *render.Scene {
	return DrawWith(l, logical, s, nil)
}

// DrawWith is Draw with rows' icons from icons (a row whose icon is not
// found shows its glyph, if it has one).
func DrawWith(l *list.Look, logical scale.Size, s Style, icons Icons) *render.Scene {
	band, banded := BandTop(l, logical, s.Theme)
	return DrawAt(l, logical, band, banded, s, icons)
}

// DrawAt is DrawWith with the selection band at band (its glide toward the
// selected row) instead of on the selected row; without banded, no band.
func DrawAt(l *list.Look, logical scale.Size, band float32, banded bool, s Style, icons Icons) *render.Scene {
	t := s.Theme
	body, _ := Body(logical, t)
	g := GridOf(logical, l.Lines, t)
	sc := &render.Scene{}
	panel(sc, body, s)
	promptLine(sc, l, body, s, icons)
	if l.Lines > 0 {
		rule(sc, body, t)
	}
	if banded {
		selectionBand(sc, body, band, g.Pitch, t)
	}
	var toastLines []string
	if l.Toast != nil {
		// The row fit leaves no room below the rows: a row whose line the
		// toast would cover is not drawn, so nothing shows through it.
		toastLines = toastLinesOf(l, body, t)
		g.Rows = rowsAbove(g, t.Row.Font.Size, toastTop(body, t, len(toastLines)))
	}
	rows(sc, l, g, s, icons)
	if l.Toast != nil {
		toast(sc, *l.Toast, toastLines, body, t)
	}
	sc.Post = post(body, s)
	return sc
}

// Moving reports whether the scene moves on its own in theme t: a
// continuous effect, or a shimmering pending row in view. A window drawing
// it keeps drawing.
func Moving(l *list.Look, t *theme.Theme) bool {
	return t.IsContinuous() || t.Shimmers() && slices.ContainsFunc(l.Rows, func(r list.Row) bool { return r.Pending })
}

// IconSize is an icon's side in the emblem column for rows in f, in
// logical pixels.
func IconSize(f theme.Font) uint32 { return uint32(math.Round(float64(f.Size * 1.25))) }

// text is a line of words in f, its box centred on centre.
func text(words string, left, centre float32, f theme.Font, c render.Color) render.Text {
	t := render.NewText(words, [2]float32{left, 0}, f.Size, c)
	t.Origin[1] = centre - t.LineHeight/2
	t.Face, t.Tracking = f.Face, f.Tracking
	return t
}

// column is t confined to a column of width.
func column(t render.Text, width float32, o render.Overflow) render.Text {
	return t.InColumn(max(width, 1), o)
}

func ceil(v float32) float32 { return float32(math.Ceil(float64(v))) }

// faded is c at fade times its alpha.
func faded(c render.Color, fade float32) render.Color { return c.WithAlpha(c.A * fade) }
