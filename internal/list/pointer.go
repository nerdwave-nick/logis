package list

import (
	"math"

	"github.com/nerdwave-nick/logis/internal/input"
)

// The pointer over a session's list (spec feat/logis §3 story 16; Rust
// src/list/view/pointer.rs): the row under the pointer is selected (hover
// highlights it), the primary button pressed and released on the same
// item accepts it, and scrolling moves the rows in view, the selection
// staying on the row under the pointer.
//
// Scrolling: a wheel notch moves RowsPerNotch rows, a high-resolution
// wheel's fractions add up to that; a finger or a continuous device moves
// one row per row pitch, so the rows follow the finger; remainders carry
// over to the next frame, and a lifted finger drops its remainder.

// RowsPerNotch is how many rows one wheel notch scrolls (the common
// desktop default of three lines).
const RowsPerNotch = 3

// Rows is where the rows lie on the panel, in its logical pixels;
// scene.Grid is one (the list cannot import the scene that draws it).
type Rows interface {
	// RowAt is the row in view (0 is the top one) under (x, y), and
	// whether a row lies there.
	RowAt(x, y float64) (int, bool)
	// RowPitch is the distance from one row's top to the next.
	RowPitch() float32
}

// pointing is what the pointer left in the list: the item a press began
// on, and the scroll not yet worth a whole row.
type pointing struct {
	pressed    uint32
	hasPressed bool
	wheel      int32
	pixels     float64
}

// Point is what p does to the list whose rows lie on rows.
func (v *View) Point(p input.Point, rows Rows) Reaction {
	under := func() (uint32, bool) {
		row, ok := rows.RowAt(p.X, p.Y)
		if m := v.filter.Matches(); ok && v.top+row < len(m) {
			return m[v.top+row].Index, true
		}
		return 0, false
	}
	switch p.Kind {
	case input.PointMoved:
		return v.hover(under())
	case input.PointLeft:
		v.pointing.hasPressed = false
	case input.PointPressed:
		item, ok := under()
		v.pointing.pressed, v.pointing.hasPressed = item, ok
		return v.hover(item, ok)
	case input.PointReleased:
		down, pressed := v.pointing.pressed, v.pointing.hasPressed
		v.pointing.hasPressed = false
		// A pending item is never accepted, by key or by click.
		if up, ok := under(); pressed && ok && up == down && !v.pending(up) {
			return Reaction{Kind: Accept, Choice: Choice{Kind: ChoseItem, Index: up}}
		}
	case input.PointScrolled:
		scrolled := v.scroll(v.pointing.rows(p.Scroll, rows.RowPitch()))
		hovered := v.hover(under())
		if scrolled.Kind == Changed {
			return scrolled
		}
		return hovered
	}
	return Reaction{}
}

// hover selects item (under the pointer) if it is not already.
func (v *View) hover(item uint32, ok bool) Reaction {
	if sel, has := v.Selected(); !ok || has && sel == item {
		return Reaction{}
	}
	v.preselect = nil
	v.selectItem(item)
	v.follow, v.following = item, true
	return Reaction{Kind: Changed}
}

// scroll moves the rows in view by rows (positive: towards later items),
// keeping the selection among them.
func (v *View) scroll(rows int32) Reaction {
	lines := max(int(v.options.Rows()), 1)
	lastTop := max(v.matchedCount()-lines, 0)
	top := int(min(max(int64(v.top)+int64(rows), 0), int64(lastTop)))
	if top == v.top {
		return Reaction{}
	}
	v.top = top
	bottom := min(top+lines, v.matchedCount()) - 1
	v.selected = min(max(v.selected, top), bottom)
	v.preselect = nil
	v.follow, v.following = v.Selected()
	return Reaction{Kind: Changed}
}

// rows is the whole rows s adds up to, keeping the remainder.
func (p *pointing) rows(s input.Scroll, pitch float32) int32 {
	switch s.Kind {
	case input.ScrollWheel:
		p.wheel = sat32(int64(p.wheel) + int64(sat32(int64(s.Value120)*RowsPerNotch)))
		rows := p.wheel / 120
		p.wheel -= rows * 120
		return rows
	case input.ScrollPixels:
		step := float64(max(pitch, 1))
		p.pixels += s.Pixels
		rows := math.Trunc(p.pixels / step)
		p.pixels -= rows * step
		return int32(max(min(rows, math.MaxInt32), math.MinInt32))
	case input.ScrollStop:
		p.pixels = 0
	}
	return 0
}

// sat32 clamps v to int32, as Rust's saturating arithmetic.
func sat32(v int64) int32 { return int32(max(min(v, math.MaxInt32), math.MinInt32)) }
