package scene

import (
	"math"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// How a list fits its window, top to bottom: the glow margin, the body —
// the prompt line, the rule, a gap, the rows, a gap — and the margin
// again. The panel's height follows its rows (a divergence from Rust v1,
// whose theme also gave the window a height, so an empty band could stay
// below the last row): the theme's window.rows (or --lines) rows at the
// theme's row height, exactly. On an output too short for them the panel
// takes the output's height and shows the rows that fit, the leftover
// height spread evenly between them, so there is never a gap. Everything
// above the first row is head and everything below the last is foot:
// Fit, FitIn and GridOf all measure through them.

// head is from the surface's top to the first row's top.
func head(t *theme.Theme) float32 { return t.Window.Margin + listTop(t) + t.Row.Gap }

// foot is from the last row's bottom to the surface's bottom.
func foot(t *theme.Theme) float32 { return t.Row.Gap + t.Window.Margin }

// listTop is from the body's top to the space above the first row: the
// prompt line and the rule.
func listTop(t *theme.Theme) float32 { return t.Prompt.Height + t.Rule.Thickness }

// Fit is FitIn on an output of any height.
func Fit(o list.Options, t *theme.Theme) (scale.Size, uint32) { return FitIn(o, t, 0) }

// FitIn is the surface a list with options o asks for on an output room
// logical pixels tall (0: unknown, no limit), and how many rows it shows:
// the theme's window.rows, or o.Lines, at the theme's row height, as wide
// as the theme's window or o.Width average characters. Where they do not
// fit in room, the surface is room tall and shows as many rows as fit (at
// least one), their pitch grown to fill it (GridOf). No rows (--lines 0)
// is the prompt line alone.
func FitIn(o list.Options, t *theme.Theme, room uint32) (scale.Size, uint32) {
	w := t.Window
	width := w.Width
	if o.Width != nil {
		width = float32(max(*o.Width, 1))*advance(t.Row.Font) + 2*t.Panel.Padding
	}
	rows := w.Rows
	if o.Lines != nil {
		rows = *o.Lines
	}
	height := t.Prompt.Height + 2*w.Margin
	if rows > 0 {
		chrome := head(t) + foot(t)
		height = chrome + float32(rows)*t.Row.Height
		if limit := float32(room); room > 0 && height > limit {
			fit := math.Floor(float64((limit - chrome) / t.Row.Height))
			rows = min(rows, uint32(max(fit, 1)))
			height = max(limit, chrome+float32(rows)*t.Row.Height)
		}
	}
	return scale.Size{W: uint32(ceil(width + 2*w.Margin)), H: uint32(ceil(height))}, rows
}

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
// rows, at the theme's row height. When as many rows show as fit, the
// room left below them (less than a row: a panel held to its output's
// height) is spread evenly between them, so the last row ends at the
// foot. Fewer rows than fit (a surface larger than asked for) keep the
// theme's pitch.
func GridOf(logical scale.Size, lines uint32, t *theme.Theme) Grid {
	b, _ := Body(logical, t)
	first := b[1] + listTop(t) + t.Row.Gap
	room := b[1] + b[3] - t.Row.Gap - first
	pitch := t.Row.Height
	fit := max(int(math.Floor(float64(room/pitch))), 0)
	rows := min(fit, int(lines))
	if rows > 0 && rows == fit {
		pitch = room / float32(rows)
	}
	return Grid{Left: b[0], Right: b[0] + b[2], First: first, Pitch: pitch, Rows: rows}
}
