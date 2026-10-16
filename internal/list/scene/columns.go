package scene

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/render"
	fonts "github.com/nerdwave-nick/logis/internal/text"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// How wide a list's columns are (Rust src/list/scene/columns.rs): measured
// with the renderer's fonts from the rows the list shows, so tab-separated
// columns line up the way the design canvas draws them.
//
// Policy (stable widths). A column is as wide as the widest text that has
// stood in it in any row shown since the list opened (each column of a row
// but its last, which takes the rest of the row). Widths only grow:
// filtering or scrolling to narrower rows never pulls a column in, so the
// layout moves only when a row wider than any before comes into view, and
// never on a keystroke that only narrows the list. The theme caps each
// column at a share of the row (row.column-max); wider text ends in an
// ellipsis. A new list (a new session or a provider's next stage) starts
// afresh, as does a change of the row's font.
//
// Cost. Only rows in view are measured, each once (remembered by its
// text), so 100 000 items cost no more than the rows shown: shaping
// stays off the paths that see every item (arrival, filtering).

// remembered is how many measured rows are remembered before the memory
// starts again (the widths stay; a row forgotten is only measured again).
const remembered = 4096

// Measure is how wide lines of text are (Rust render::Measure): the width
// t takes on one line in logical pixels, ignoring its column, colours and
// emphasis; false when it cannot be measured (no fonts).
type Measure interface {
	Width(t render.Text) (float32, bool)
}

// MeasureFunc is a function as a Measure.
type MeasureFunc func(render.Text) (float32, bool)

// Width is f(t).
func (f MeasureFunc) Width(t render.Text) (float32, bool) { return f(t) }

// NoMeasure measures nothing: every width is unknown.
var NoMeasure Measure = MeasureFunc(func(render.Text) (float32, bool) { return 0, false })

// Columns are the widths of one list's columns, as they grew. The zero
// value is a list that has measured nothing yet.
type Columns struct {
	// widths are, per column but the last, the widest text measured in it.
	widths []float32
	// seen are the rows already measured, by their text.
	seen map[string]struct{}
	// font and face are the row font (and its face) the widths are in.
	keyed bool
	font  theme.Font
	face  fonts.Face
}

// Fit measures the rows of l not measured before and gives l the columns'
// widths. A row that cannot be measured (no fonts yet) is estimated for
// this frame and measured on a later one.
func (c *Columns) Fit(l *list.Look, t *theme.Theme, m Measure) {
	font := t.Row.Font
	var face fonts.Face
	if font.Face >= 0 && font.Face < len(t.Faces) {
		face = t.Faces[font.Face]
	}
	if !c.keyed || c.font != font || !sameFace(c.face, face) {
		*c = Columns{keyed: true, font: font, face: fonts.Face{Families: slices.Clone(face.Families), Weight: face.Weight}}
	}
	// A row of one column has nothing to measure.
	var fresh []string
	for _, r := range l.Rows {
		if _, seen := c.seen[r.Text]; !seen && strings.Contains(r.Text, "\t") {
			fresh = append(fresh, r.Text)
		}
	}
	widths, measured := widest(fresh, font, m)
	shown := grow(slices.Clone(c.widths), widths)
	if measured {
		c.widths = slices.Clone(shown)
		if c.seen == nil || len(c.seen)+len(fresh) > remembered {
			c.seen = make(map[string]struct{}, len(fresh))
		}
		for _, f := range fresh {
			c.seen[f] = struct{}{}
		}
	}
	l.Columns = shown
}

func sameFace(a, b fonts.Face) bool {
	return a.Weight == b.Weight && slices.Equal(a.Families, b.Families)
}

// widest is, per column but each row's last, the widest text of the rows'
// texts in font, and whether every one was measured (else some were
// estimated).
func widest(texts []string, font theme.Font, m Measure) ([]float32, bool) {
	var widths []float32
	measured := true
	for _, line := range texts {
		cells := strings.Split(line, "\t")
		for i, cell := range cells[:len(cells)-1] {
			width, ok := m.Width(text(cell, 0, 0, font, render.Color{}))
			if !ok {
				measured = false
				width = estimate(cell, font)
			}
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], width)
		}
	}
	return widths, measured
}

// grow makes each of widths at least as wide as the same one of other.
func grow(widths, other []float32) []float32 {
	for len(widths) < len(other) {
		widths = append(widths, 0)
	}
	for i, o := range other {
		widths[i] = max(widths[i], o)
	}
	return widths
}

// estimate is an unmeasured cell's width: its characters at an average
// advance.
func estimate(words string, font theme.Font) float32 {
	return float32(utf8.RuneCountInString(words)) * advance(font)
}
