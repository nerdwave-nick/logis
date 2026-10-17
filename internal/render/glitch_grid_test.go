package render_test

import (
	"testing"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// The glitch's hashed grids (the noise band's cells, the slices that
// flash or tear, the blocks' cells) lie on whole device pixels, counted
// from the glass's top left corner rounded to a device pixel, and are
// whole device pixels in size. At a fractional scale a grid of scaled
// logical lengths would put cell edges exactly on pixel centres (at 1.3
// the band's 1.3 px cells from the glass at 42.9 px, every 13th row), and
// which cell such a pixel takes would depend on how the driver rounds the
// last bit: llvmpipe and Intel drew different noise rows (ad8913d4).
// Whole-pixel cells never meet a pixel centre, on any driver.

// gridScale is 1.3, where the glass's corner (33 logical px in) lies at
// 42.9 device px and is taken as 43.
var gridScale = scale.From120ths(156)

const gridOrigin = 43

// glitchOnly draws a flat opaque frame, its body's glass under the glitch
// g alone at the peak of a burst, at gridScale.
func glitchOnly(t *testing.T, g effects.Glitch) *render.Image {
	t.Helper()
	h := rendertest.Headless(t)
	logical := scale.Size{W: 200, H: 200}
	sc := &render.Scene{
		Clear: render.RGBA(0.3, 0.3, 0.3, 1),
		Post:  render.Post{Rect: [4]float32{32, 32, 136, 136}, Inset: 1, Glitch: g, Moment: effects.Moment{Time: 1, Event: 1}},
	}
	img, err := h.Offscreen(sc, logical, gridScale)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// onePart is the glitch at full strength with every part off.
func onePart() effects.Glitch {
	var g effects.Glitch
	g.Strength = 1
	g.Slices.Px, g.Slices.Group, g.NoiseBand.CellPx, g.LinePulse.PitchPx = 9, 1, 1, 2.3
	return g
}

// The band's cells of 3 logical px are 4 device px at 1.3 (3.9 rounded):
// along every row, the noise holds still within each 4 px span from the
// glass's rounded corner and changes only between spans.
func TestTheNoiseBandsCellsAreWholeDevicePixels(t *testing.T) {
	g := onePart()
	g.NoiseBand = effects.NoiseBand{Episode: 1, CellPx: 3, Min: 0.5, Max: 0.5}
	img := glitchOnly(t, g)
	const cell, right = 4, 216 // the glass ends at 167 × 1.3 = 217.1 px
	changes := 0
	for y := gridOrigin; y < right; y++ {
		for x := gridOrigin; x+cell <= right; x += cell {
			first := img.Pixel(x, y)
			for i := 1; i < cell; i++ {
				if got := img.Pixel(x+i, y); got != first {
					t.Fatalf("row %d: the cell from x %d is %v at its start but %v at x %d", y, x, first, got, x+i)
				}
			}
			if x > gridOrigin && img.Pixel(x-1, y) != first {
				changes++
			}
		}
	}
	if changes == 0 {
		t.Fatal("no noise drawn")
	}
}

// The slices of 9 logical px are 12 device px at 1.3 (11.7 rounded): a
// slice lit by a flash is lit from its first row to its last, each slice
// starting a whole number of slices below the glass's rounded corner.
func TestTheSlicesAreWholeDevicePixels(t *testing.T) {
	g := onePart()
	g.Flashes = effects.Flashes{Episode: 0.5, Gain: 1}
	img := glitchOnly(t, g)
	const slice, bottom, x = 12, 216, 100
	lit, dark := 0, 0
	for y := gridOrigin; y+slice <= bottom; y += slice {
		first := img.Pixel(x, y)
		for i := 1; i < slice; i++ {
			if got := img.Pixel(x, y+i); got != first {
				t.Fatalf("the slice from row %d is %v at its top but %v at row %d", y, first, got, y+i)
			}
		}
		if first[0] > 100 {
			lit++
		} else {
			dark++
		}
	}
	if lit == 0 || dark == 0 {
		t.Fatalf("%d slices lit, %d dark: the flashes are not drawn per slice", lit, dark)
	}
}
