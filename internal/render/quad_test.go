package render

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ports of Rust src/render/images.rs's tests.

func TestPackSnapsTheCornerAndKeepsThePixelSize(t *testing.T) {
	got := packQuad([2]float32{10, 7}, [4]uint32{64, 32, 42, 40}, scale.From120ths(156), 0.5)
	// 10 × 1.3 = 13, 7 × 1.3 = 9.1 → 9; the size stays 42×40 texels.
	if want := [quadFloats]float32{13, 9, 42, 40, 64, 32, 0.5, 0}; got != want {
		t.Errorf("packed %v, want %v", got, want)
	}
}

func TestPackRoundsHalfPixelsAwayFromZero(t *testing.T) {
	got := packQuad([2]float32{2.5, 0}, [4]uint32{0, 0, 4, 4}, scale.One, 1)
	if got[0] != 3 || got[1] != 0 {
		t.Errorf("corner (%v, %v), want (3, 0)", got[0], got[1])
	}
	if got := packQuad([2]float32{0, 0}, [4]uint32{0, 0, 4, 4}, scale.One, 3); got[6] != 1 {
		t.Errorf("opacity %v, want clamped to 1", got[6])
	}
}
