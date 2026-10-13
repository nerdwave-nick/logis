package render_test

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
)

// Ported from Rust tests/faces.rs a_caret_stands_between_the_characters_in_its_shape.

func caretLine(shape render.CaretShape, caret bool) *render.Scene {
	line := render.NewText("rite", [2]float32{8, 8}, 16, render.Hex(0xf1e6cc))
	if caret {
		line.Caret = &render.Caret{At: 2, Shape: shape, Width: 2, Color: render.Hex(0xf2c14e)}
	}
	return &render.Scene{Clear: render.Hex(0), Texts: []render.Text{line}}
}

// difference is the columns and rows where two renders differ.
func difference(a, b *render.Image) (columns, rows []int) {
	differs := func(x, y int) bool { return a.Pixel(x, y) != b.Pixel(x, y) }
	for x := range a.Width {
		for y := range a.Height {
			if differs(x, y) {
				columns = append(columns, x)
				break
			}
		}
	}
	for y := range a.Height {
		for x := range a.Width {
			if differs(x, y) {
				rows = append(rows, y)
				break
			}
		}
	}
	return columns, rows
}

func TestACaretStandsBetweenTheCharactersInItsShape(t *testing.T) {
	h := withForgeFaces(t)
	draw := func(sc *render.Scene) *render.Image { return offscreen(t, h, sc, 80, 36, at(120)) }
	without := draw(caretLine(render.CaretBar, false))
	// "ri" is two monospace advances of about 9.6 px from x = 8.
	columns, rows := difference(without, draw(caretLine(render.CaretBar, true)))
	if len(columns) == 0 || len(columns) > 3 {
		t.Fatalf("a bar: %v", columns)
	}
	if columns[0] < 26 || columns[0] > 30 {
		t.Fatalf("after 'ri': %v", columns)
	}
	if len(rows) < 14 {
		t.Fatalf("a bar as tall as the line: %v", rows)
	}
	if columns, _ := difference(without, draw(caretLine(render.CaretBlock, true))); len(columns) < 8 || len(columns) > 12 {
		t.Fatalf("a block a character wide: %v", columns)
	}
	if _, rows := difference(without, draw(caretLine(render.CaretUnderscore, true))); len(rows) == 0 || len(rows) > 3 || rows[0] < 22 {
		t.Fatalf("an underscore at the baseline: %v", rows)
	}
}

func TestACaretAtTheEndStandsAfterTheLastCharacterAndOneOutsideItsColumnIsNotDrawn(t *testing.T) {
	h := withForgeFaces(t)
	draw := func(sc *render.Scene) *render.Image { return offscreen(t, h, sc, 120, 36, at(120)) }
	without := draw(caretLine(render.CaretBar, false))
	end := caretLine(render.CaretBar, true)
	end.Texts[0].Caret.At = 4
	columns, _ := difference(without, draw(end))
	// Four advances of about 9.6 px from x = 8.
	if len(columns) == 0 || columns[0] < 44 || columns[0] > 49 {
		t.Fatalf("after 'rite': %v", columns)
	}
	clipped := caretLine(render.CaretBar, true)
	clipped.Texts[0] = clipped.Texts[0].InColumn(12, render.Clip)
	bare := caretLine(render.CaretBar, false)
	bare.Texts[0] = bare.Texts[0].InColumn(12, render.Clip)
	if columns, _ := difference(draw(bare), draw(clipped)); len(columns) != 0 {
		t.Fatalf("a caret outside its column: %v", columns)
	}
}

// An empty query still shows where typing begins.
func TestAnEmptyLineShowsItsCaretAtItsStart(t *testing.T) {
	h := withForgeFaces(t)
	draw := func(sc *render.Scene) *render.Image { return offscreen(t, h, sc, 40, 36, at(120)) }
	empty := caretLine(render.CaretBar, true)
	empty.Texts[0].Text, empty.Texts[0].Caret.At = "", 0
	bare := caretLine(render.CaretBar, false)
	bare.Texts[0].Text = ""
	columns, rows := difference(draw(bare), draw(empty))
	if len(columns) == 0 || columns[0] < 6 || columns[0] > 9 || len(rows) < 14 {
		t.Fatalf("columns %v rows %v", columns, rows)
	}
}
