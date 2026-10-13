package main

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ports of Rust src/window/debug/session.rs's and mod.rs's selection
// tests, at the debug panel's stand-in keyboard (evdev Up, Down and Escape
// until the input model maps keysyms to actions).

const (
	pressed  = 1
	released = 0
)

func TestNextAndPreviousMoveTheSelectionWithinTheRows(t *testing.T) {
	redraws := 0
	k := &debugKeys{redraw: func() { redraws++ }}
	k.Key(0, 0, keyUp, pressed)
	if k.row != 0 || redraws != 0 {
		t.Fatalf("Up at the top: row %d, %d redraws", k.row, redraws)
	}
	k.Key(0, 0, keyDown, pressed)
	k.Key(0, 0, keyDown, released)
	if k.row != 1 || redraws != 1 {
		t.Fatalf("one Down: row %d, %d redraws", k.row, redraws)
	}
	for range debugSelectable {
		k.Key(0, 0, keyDown, pressed)
	}
	if k.row != debugSelectable-1 || redraws != debugSelectable-1 {
		t.Fatalf("Down past the end: row %d, %d redraws", k.row, redraws)
	}
	k.Key(0, 0, keyUp, pressed)
	if k.row != debugSelectable-2 || redraws != debugSelectable {
		t.Errorf("Up: row %d, %d redraws", k.row, redraws)
	}
	if k.done() {
		t.Error("moving the selection ended the panel")
	}
}

func TestEscapeDismissesThePanel(t *testing.T) {
	k := &debugKeys{redraw: func() {}}
	k.Key(0, 0, keyEscape, released)
	if k.done() {
		t.Fatal("a release dismissed the panel")
	}
	k.Key(0, 0, keyEscape, pressed)
	if !k.done() {
		t.Error("Escape did not dismiss the panel")
	}
}

func TestTheSelectionAndItsMarkerMoveByOneRowPitchPerRow(t *testing.T) {
	top, third := debugSceneAt(debugSize, 0), debugSceneAt(debugSize, 2)
	plain := debugScene(debugSize)
	for i := range plain.Shapes {
		if top.Shapes[i] != plain.Shapes[i] {
			t.Errorf("row 0 changed shape %d", i)
		}
		moved := float32(0)
		if i == debugSelected || i == debugMarker {
			moved = 2 * debugRowPitch
		}
		if d := third.Shapes[i].Rect[1] - top.Shapes[i].Rect[1]; d != moved {
			t.Errorf("shape %d moved %v, want %v", i, d, moved)
		}
	}
}

func TestTheSelectionNeverLeavesASmallerBody(t *testing.T) {
	sc := debugSceneAt(scale.Size{W: 466, H: 300}, debugSelectable-1)
	body, band := sc.Shapes[0].Rect, sc.Shapes[debugSelected].Rect
	if band[1]+band[3] > body[1]+body[3]-queryBlock {
		t.Errorf("band %v reaches into the query block of %v", band, body)
	}
}
