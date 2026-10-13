package main

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ports of Rust src/window/debug/mod.rs's tests (the query itself comes
// with the keyboard slice, columns and the ellipsis with the shaping
// slice).

func TestAtTheRequestedSizeTheBodyIsTheFullPanel(t *testing.T) {
	sc := debugScene(debugSize)
	if got, want := sc.Shapes[0].Rect, [4]float32{debugMargin, debugMargin, 640, 360}; got != want {
		t.Errorf("body %v, want %v", got, want)
	}
	if len(sc.Shapes) != 6 {
		t.Errorf("%d shapes, want body, rune, rule, selection, marker, query rule", len(sc.Shapes))
	}
}

func TestASmallerConfigureShrinksTheBodyAndKeepsItsGlowInside(t *testing.T) {
	sc := debugScene(scale.Size{W: 466, H: 300})
	body := sc.Shapes[0].Rect
	if want := [4]float32{debugMargin, debugMargin, 466 - 2*debugMargin, 300 - 2*debugMargin}; body != want {
		t.Errorf("body %v, want %v", body, want)
	}
	for i, s := range sc.Shapes {
		if s.Rect[1]+s.Rect[3] > body[1]+body[3] {
			t.Errorf("shape %d %v leaves the body %v", i, s.Rect, body)
		}
	}
	if glow := sc.Shapes[0].Glow.Radius; body[0] < glow {
		t.Errorf("glow %v reaches past the surface edge at %v", glow, body[0])
	}
}

func TestTheBodyIsSootUnderABrassToCrimsonBorderWithACrimsonGlow(t *testing.T) {
	body := debugScene(debugSize).Shapes[0]
	top, bottom := body.Border.Paint.Stops()[0], body.Border.Paint.Stops()[1]
	if top != brass || bottom != crimson || body.Border.Width != 2 {
		t.Errorf("border %+v, want 2 px brass to crimson", body.Border)
	}
	if body.Fill.Stops()[0] != sootTop || body.Glow.Color != crimson.WithAlpha(0.55) {
		t.Errorf("fill %+v glow %+v", body.Fill, body.Glow)
	}
	if body.Radius != 14 {
		t.Errorf("radius %v, want 14", body.Radius)
	}
}

func TestTheTitleRowsAndQueryLineAreTheRustPanels(t *testing.T) {
	sc := debugScene(debugSize)
	if len(sc.Texts) != 1+len(debugRows)+1 || len(debugRows) != 9 {
		t.Fatalf("%d texts for %d rows, want a title, every row and the query line", len(sc.Texts), len(debugRows))
	}
	title := sc.Texts[0]
	if title.Text != "COGITATOR // RITE OF TESTING" || title.Origin != [2]float32{debugMargin + 46, debugMargin + 12} || title.Size != 18 || title.Color != brass {
		t.Errorf("title %+v", title)
	}
	rows := sc.Texts[1 : len(sc.Texts)-1]
	for i, row := range rows {
		want := [2]float32{debugMargin + 22, debugMargin + 68 + 26*float32(i)}
		if row.Text != debugRows[i] || row.Origin != want || row.Size != 15 {
			t.Errorf("row %d %+v", i, row)
		}
	}
	if rows[0].Color != bone || rows[1].Color != ash.WithAlpha(0.9) {
		t.Errorf("row colours %v %v", rows[0].Color, rows[1].Color)
	}
}

func TestTheQueryLineShowsItsCursorBelowEveryRow(t *testing.T) {
	sc := debugScene(debugSize)
	line := sc.Texts[len(sc.Texts)-1]
	if line.Text != "> |" || line.Color != brass {
		t.Errorf("query line %+v", line)
	}
	rule := sc.Shapes[len(sc.Shapes)-1].Rect
	for _, row := range sc.Texts[1 : len(sc.Texts)-1] {
		if row.Origin[1]+row.LineHeight > rule[1] {
			t.Errorf("row %q reaches below the query rule at %v", row.Text, rule[1])
		}
	}
	if line.Origin[1] <= rule[1] {
		t.Errorf("the query line at %v is above its rule at %v", line.Origin[1], rule[1])
	}
}

func TestASmallerBodyShowsOnlyTheRowsThatFit(t *testing.T) {
	sc := debugScene(scale.Size{W: 466, H: 300})
	rule := sc.Shapes[len(sc.Shapes)-1].Rect
	rows := sc.Texts[1 : len(sc.Texts)-1]
	if len(rows) == 0 || len(rows) >= len(debugRows) {
		t.Fatalf("%d rows in a 300 px surface", len(rows))
	}
	for _, row := range rows {
		if row.Origin[1]+22 > rule[1] {
			t.Errorf("row %q at %v crosses the query rule at %v", row.Text, row.Origin[1], rule[1])
		}
	}
}
