package main

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ports of Rust src/window/debug/mod.rs's shape tests (its text rows and
// query line come with the text and keyboard slices).

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
