package list

import (
	"math"
	"testing"

	"github.com/nerdwave-nick/logis/internal/input"
)

// The pointer over the list: hover, clicks and scrolling.

const (
	pointerLines = 3
	pitch        = 26.0
)

// testGrid lays rows out as scene.Grid does (the list cannot import the
// scene, which draws it).
type testGrid struct{ left, right, first float64 }

func (g testGrid) RowAt(x, y float64) (int, bool) {
	i := math.Floor((y - g.first) / pitch)
	return int(i), x >= g.left && x < g.right && i >= 0 && i < pointerLines
}

func (testGrid) RowPitch() float32 { return pitch }

// grid is three rows below a prompt line, as a scene lays them out.
var grid = testGrid{left: 24, right: 400, first: 72}

func pointerList(n int) *View {
	v := NewView(Options{Lines: lines(pointerLines)})
	entries := make([]Entry, n)
	for i := range entries {
		entries[i] = Entry{Text: "item " + itoa(i)}
	}
	v.Push(entries)
	return v
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// overRow is the middle of row row in view.
func overRow(row int) (float64, float64) { return 100, grid.first + float64(row)*pitch + pitch/2 }

func at(kind input.PointKind, row int) input.Point {
	x, y := overRow(row)
	return input.Point{Kind: kind, X: x, Y: y}
}

func hover(v *View, row int) Reaction { return v.Point(at(input.PointMoved, row), grid) }

func click(v *View, down, up int) Reaction {
	v.Point(at(input.PointPressed, down), grid)
	return v.Point(at(input.PointReleased, up), grid)
}

func scrolled(v *View, s input.Scroll, row int) Reaction {
	p := at(input.PointScrolled, row)
	p.Scroll = s
	return v.Point(p, grid)
}

func notches(v120 int32) input.Scroll { return input.Scroll{Kind: input.ScrollWheel, Value120: v120} }
func px(p float64) input.Scroll       { return input.Scroll{Kind: input.ScrollPixels, Pixels: p} }

func TestHoveringARowSelectsIt(t *testing.T) {
	v := pointerList(5)
	eq(t, hover(v, 2), changed)
	selected(t, v, 2)
	eq(t, hover(v, 2), unchanged) // already selected
	eq(t, v.Point(input.Point{Kind: input.PointMoved, X: 100, Y: 5}, grid), unchanged)
	selected(t, v, 2) // the prompt line selects nothing
	eq(t, v.Point(input.Point{Kind: input.PointLeft}, grid), unchanged)
}

func TestHoveringAnEmptyRowSelectsNothing(t *testing.T) {
	v := pointerList(2)
	eq(t, hover(v, 2), unchanged)
	selected(t, v, 0)
}

func TestAClickOnARowAcceptsItsItem(t *testing.T) {
	eq(t, click(pointerList(5), 1, 1), accept(Choice{Kind: ChoseItem, Index: 1}))
}

func TestAPressDraggedToAnotherRowAcceptsNothing(t *testing.T) {
	v := pointerList(5)
	eq(t, click(v, 0, 2), unchanged)
	selected(t, v, 0) // the press selected row 0
	eq(t, v.Point(at(input.PointReleased, 1), grid), unchanged)
}

func TestAClickAfterTypingAcceptsTheFilteredRow(t *testing.T) {
	v := pointerList(12)
	typed(v, "1")
	eq(t, rowTexts(v.Look())[1], "item 10")
	eq(t, click(v, 1, 1), accept(Choice{Kind: ChoseItem, Index: 10}))
}

func TestAWheelNotchScrollsThreeRowsAndTheRowUnderThePointerIsSelected(t *testing.T) {
	v := pointerList(10)
	eq(t, scrolled(v, notches(120), 0), changed)
	eq(t, rowTexts(v.Look()), []string{"item 3", "item 4", "item 5"})
	selected(t, v, 3)
	scrolled(v, notches(-120), 2)
	eq(t, rowTexts(v.Look()), []string{"item 0", "item 1", "item 2"})
	selected(t, v, 2)
}

func TestHighResolutionWheelFractionsAddUpToRows(t *testing.T) {
	v := pointerList(10)
	eq(t, scrolled(v, notches(30), 0), unchanged)
	eq(t, scrolled(v, notches(30), 0), changed)
	eq(t, rowTexts(v.Look())[0], "item 1") // 60/120 of a notch: one row
	scrolled(v, notches(-20), 0)
	eq(t, rowTexts(v.Look())[0], "item 1") // the remainder carried
}

func TestScrollingStopsAtBothEnds(t *testing.T) {
	v := pointerList(5)
	eq(t, scrolled(v, notches(-120), 0), unchanged)
	scrolled(v, notches(1200), 0)
	eq(t, rowTexts(v.Look()), []string{"item 2", "item 3", "item 4"})
	eq(t, scrolled(v, notches(120), 0), unchanged)
	eq(t, scrolled(pointerList(2), notches(120), 0), unchanged)
}

func TestAFingerScrollsARowPerRowPitchAndItsStopDropsTheRest(t *testing.T) {
	v := pointerList(10)
	scrolled(v, px(pitch*0.75), 0)
	eq(t, rowTexts(v.Look())[0], "item 0")
	scrolled(v, px(pitch*0.5), 0)
	eq(t, rowTexts(v.Look())[0], "item 1")
	scrolled(v, input.Scroll{Kind: input.ScrollStop}, 0)
	scrolled(v, px(pitch*0.75), 0)
	eq(t, rowTexts(v.Look())[0], "item 1") // the quarter row was dropped
}

func TestScrollingAwayFromThePointerKeepsTheSelectionInView(t *testing.T) {
	v := pointerList(10)
	// The pointer is over the prompt line: no row under it.
	p := input.Point{Kind: input.PointScrolled, X: 100, Y: 5, Scroll: notches(240)}
	eq(t, v.Point(p, grid), changed)
	eq(t, rowTexts(v.Look()), []string{"item 6", "item 7", "item 8"})
	selected(t, v, 6)
}

func TestAClickNeverAcceptsAPendingItem(t *testing.T) {
	v := NewView(Options{Lines: lines(pointerLines)})
	v.Push([]Entry{{Text: "awaited", Decor: &Decor{Pending: true}}})
	eq(t, click(v, 0, 0), unchanged)
}

func TestHoveringEndsALatePreselection(t *testing.T) {
	v := pointerList(3)
	hover(v, 1)
	if v.Preselect(2) {
		t.Error("the user moved with the pointer")
	}
	selected(t, v, 1)
}

func TestTheHoveredItemStaysSelectedWhileItemsStreamIn(t *testing.T) {
	v := pointerList(3)
	hover(v, 2)
	v.Push(plain("a newcomer"))
	selected(t, v, 2)
}

func TestLeavingForgetsAPress(t *testing.T) {
	v := pointerList(5)
	v.Point(at(input.PointPressed, 1), grid)
	v.Point(input.Point{Kind: input.PointLeft}, grid)
	eq(t, v.Point(at(input.PointReleased, 1), grid), unchanged)
}

func TestKeysAndPastesThroughApplyLeaveTheListAlone(t *testing.T) {
	v := pointerList(3)
	eq(t, v.Apply(input.Action{Kind: input.Paste, Selection: input.Clipboard}), unchanged)
	eq(t, v.Apply(input.Action{Kind: input.PointAt, Point: at(input.PointMoved, 2)}), unchanged)
}
