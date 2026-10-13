// Package scene draws a list as a cogitator panel in a theme (feat/logis
// §4.9; Rust src/list/scene): the body with its glow, border and
// ornaments; the prompt line with its sigil, label, query, caret and
// counter; a rule; the rows below it, the selected one on a band, matched
// characters set apart; badges, pending rows and a toast. Rows are single
// lines whose tab-separated columns line up (at measured widths when the
// Look carries them, else estimated from the rows in view) and take the
// theme's column colours.
//
// Every length is the theme's, in logical pixels. A scene is drawn at a
// moment (Style.At) so the theme's continuous effects (glow pulse,
// travelling highlight, shimmer) move with time; at a frozen moment it is
// a still image.
package scene

import (
	"math"
	"slices"

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

// Fit is the surface a list with options o asks for, and the most rows it
// shows: the theme's window when o names no size, else the prompt line and
// Lines rows, Width average characters wide.
func Fit(o list.Options, t *theme.Theme) (scale.Size, uint32) {
	w := t.Window
	width, height := w.Width, w.Height
	if o.Width != nil {
		width = float32(max(*o.Width, 1))*advance(t.Row.Font) + 2*t.Panel.Padding
	}
	rows := w.Rows
	if o.Lines != nil {
		rows = *o.Lines
		if rows == 0 {
			height = t.Prompt.Height
		} else {
			height = listTop(t) + 2*t.Row.Gap + float32(rows)*t.Row.Height
		}
	}
	m := 2 * w.Margin
	return scale.Size{W: uint32(ceil(width + m)), H: uint32(ceil(height + m))}, rows
}

// listTop is from the body's top to the space above the first row: the
// prompt line and the rule.
func listTop(t *theme.Theme) float32 { return t.Prompt.Height + t.Rule.Thickness }

// advance is an average character's advance in f (for --width in
// characters).
func advance(f theme.Font) float32 { return f.Size * (0.6 + f.Tracking) }

// Grid is where the rows lie on a surface: the pointer's view of the list.
type Grid struct {
	// Left and Right are the body's edges; First the first row's top;
	// Pitch from one row's top to the next.
	Left, Right, First, Pitch float32
	// Rows is how many rows fit and are asked for.
	Rows int
}

// RowAt is the row in view (0 is the top one) under the surface-local
// point (x, y), whether or not an item fills it: the band across the
// body's width.
func (g Grid) RowAt(x, y float64) (int, bool) {
	i := math.Floor((y - float64(g.First)) / float64(g.Pitch))
	inside := x >= float64(g.Left) && x < float64(g.Right) && i >= 0 && i < float64(g.Rows)
	return int(i), inside
}

// RowPitch is the distance from one row's top to the next (list.Rows).
func (g Grid) RowPitch() float32 { return g.Pitch }

var _ list.Rows = Grid{}

// Top is the top of row i in view.
func (g Grid) Top(i int) float32 { return g.First + float32(i)*g.Pitch }

// Body is the panel's body (left, top, width, height) on a surface of
// logical size, and its corner radius: it fills the surface less the glow
// margin, so a smaller configure still shows a whole panel (with fewer
// rows).
func Body(logical scale.Size, t *theme.Theme) ([4]float32, float32) {
	m := t.Window.Margin
	w := max(float32(logical.W)-2*m, 2*t.Panel.Padding+1)
	h := max(float32(logical.H)-2*m, t.Prompt.Height)
	return [4]float32{m, m, w, h}, t.Panel.Radius
}

// GridOf is the rows of a surface of logical size showing at most lines
// rows.
func GridOf(logical scale.Size, lines uint32, t *theme.Theme) Grid {
	b, _ := Body(logical, t)
	first := b[1] + listTop(t) + t.Row.Gap
	pitch := t.Row.Height
	fit := max(int(math.Floor(float64((b[1]+b[3]-t.Row.Gap-first)/pitch))), 0)
	return Grid{Left: b[0], Right: b[0] + b[2], First: first, Pitch: pitch, Rows: min(fit, int(lines))}
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
		selectionBand(sc, body, band, t)
	}
	rows(sc, l, g, s, icons)
	if l.Toast != nil {
		toast(sc, *l.Toast, body, t)
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
