package main

import (
	"fmt"
	"image"
	"math"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
	"github.com/nerdwave-nick/logis/internal/text"
)

// The panel a compositor shows must be the image the renderer draws: the
// debug scene rendered offscreen at the geometry logis reported,
// composited over the harness's black backdrop, then compared with the
// capture.
//
// The compositor maps the buffer through the viewport onto the output
// itself, and resamples it by under a pixel where that mapping is not 1:1:
// at fractional scales (530 buffer rows over 408 × 1.3 = 530.4 output
// rows) and where the surface's centred position falls between output
// pixels (nested niri at 2.0: 816 rows shown over about 815.2, 0.8 rows
// down). Bilinear resampling by less than a pixel can only produce values
// within the range of a pixel's 3×3 neighbourhood, so each captured pixel
// must lie in that range of the reference, give or take Rust's E2E capture
// tolerance (4 levels, 0.1 % outliers). That still catches a wrong colour,
// a missing shape or geometry off by more than a pixel; the exact pixels
// are the offscreen goldens' business.

// reference renders the debug scene for f at s offscreen, its text with
// the fonts fontconfig gives this process: checkPanel points it at the
// compositor's fonts.conf, the one the live panel resolves.
func reference(f frame, s float64) (*render.Image, error) {
	h, err := render.OpenHeadless()
	if err != nil {
		return nil, err
	}
	defer h.Close()
	fonts, err := text.ResolveSystem().Wait()
	if err != nil {
		return nil, err
	}
	h.SetFonts(fonts)
	sc := scale.From120ths(uint32(math.Round(s * 120)))
	logical := scale.Size{W: uint32(f.logW), H: uint32(f.logH)}
	img, err := h.Offscreen(debugScene(logical), logical, sc)
	if err != nil {
		return nil, err
	}
	if img.Width != f.physW || img.Height != f.physH {
		return nil, fmt.Errorf("the offscreen panel is %dx%d, the buffer %dx%d", img.Width, img.Height, f.physW, f.physH)
	}
	return img, nil
}

// bounds is, per pixel of the reference composited over the black
// backdrop, the lowest and highest value of each channel in its 3×3
// neighbourhood.
type bounds struct {
	w, h   int
	lo, hi [][3]float64
}

func neighbourhoods(want *render.Image) *bounds {
	w, h := want.Width, want.Height
	// Premultiplied over opaque black is the colour channels as they are.
	flat := make([][3]float64, w*h)
	for y := range h {
		for x := range w {
			px := want.Pixel(x, y)
			flat[y*w+x] = [3]float64{float64(px[0]), float64(px[1]), float64(px[2])}
		}
	}
	b := &bounds{w: w, h: h, lo: make([][3]float64, w*h), hi: make([][3]float64, w*h)}
	for y := range h {
		for x := range w {
			lo, hi := flat[y*w+x], flat[y*w+x]
			for ny := max(y-1, 0); ny <= min(y+1, h-1); ny++ {
				for nx := max(x-1, 0); nx <= min(x+1, w-1); nx++ {
					for k := range 3 {
						lo[k] = min(lo[k], flat[ny*w+nx][k])
						hi[k] = max(hi[k], flat[ny*w+nx][k])
					}
				}
			}
			b.lo[y*w+x], b.hi[y*w+x] = lo, hi
		}
	}
	return b
}

// panelProblems finds the reference panel in a capture (the surface is
// centred on the output, within a few pixels of rounding; the output may
// clip it) and lists what is wrong, with how the best match compared.
func panelProblems(capture image.Image, want *render.Image) (string, []string) {
	b := capture.Bounds()
	// Over the harness's backdrop, opaque black at every scale (internal/e2e
	// tests it), never a pixel of the capture: a panel can cover them all
	// (the debug panel at 2.0 fills sway's 640×360 logical output).
	n := neighbourhoods(want)
	cx, cy := (b.Dx()-want.Width)/2, (b.Dy()-want.Height)/2
	best, bestAt := rendertest.Comparison{Outliers: -1}, image.Point{}
	for dy := -3; dy <= 3; dy++ {
		for dx := -3; dx <= 3; dx++ {
			at := image.Pt(b.Min.X+cx+dx, b.Min.Y+cy+dy)
			c := n.compareAt(capture, at)
			if c.Total > 0 && (best.Outliers < 0 || c.Outliers < best.Outliers) {
				best, bestAt = c, at
			}
		}
	}
	if best.Outliers < 0 {
		return "", []string{"no panel on the output"}
	}
	summary := fmt.Sprintf("the panel at %v: %v (beyond its neighbourhood)", bestAt, best)
	if !best.Passes(rendertest.Capture) {
		return summary, []string{"not the renderer's image: " + summary}
	}
	return summary, nil
}

// compareAt compares the capture at offset at with the neighbourhood
// ranges, on the part of the panel the output shows.
func (n *bounds) compareAt(capture image.Image, at image.Point) rendertest.Comparison {
	b := capture.Bounds()
	tol := float64(rendertest.Capture.Channel)
	var c rendertest.Comparison
	for y := range n.h {
		for x := range n.w {
			p := image.Pt(at.X+x, at.Y+y)
			if !p.In(b) {
				continue
			}
			r, g, bl, _ := capture.At(p.X, p.Y).RGBA()
			got := [3]float64{float64(r >> 8), float64(g >> 8), float64(bl >> 8)}
			lo, hi := n.lo[y*n.w+x], n.hi[y*n.w+x]
			var worst float64
			for k := range 3 {
				worst = max(worst, lo[k]-got[k], got[k]-hi[k])
			}
			c.Total++
			c.Worst = max(c.Worst, uint8(min(max(math.Round(worst), 0), 255)))
			if worst > tol {
				c.Outliers++
			}
		}
	}
	return c
}

// translucentFramed is a w×h premultiplied panel: an opaque interior
// inside a one-pixel ring of half-transparent glow.
func translucentFramed(w, h int) *render.Image {
	img := &render.Image{Width: w, Height: h, Pixels: make([]byte, w*h*4)}
	for y := range h {
		for x := range w {
			px := []byte{200, 180, 40, 255}
			if x == 0 || y == 0 || x == w-1 || y == h-1 {
				px = []byte{64, 0, 0, 128}
			}
			copy(img.Pixels[(y*w+x)*4:], px)
		}
	}
	return img
}

// overBlack is img composited over opaque black, cropped to w×h around its
// centre: the capture of a panel larger than its output.
func overBlack(img *render.Image, w, h int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	ox, oy := (img.Width-w)/2, (img.Height-h)/2
	for y := range h {
		for x := range w {
			px := img.Pixel(x+ox, y+oy)
			copy(out.Pix[out.PixOffset(x, y):], []byte{px[0], px[1], px[2], 255})
		}
	}
	return out
}

func TestAPanelCoveringTheWholeOutputIsComparedOverTheBlackBackdrop(t *testing.T) {
	want := translucentFramed(12, 10)
	// The panel fills the output (as the debug panel at 2.0 fills sway's
	// 640×360 logical output): its top-left pixel is the panel's glow, not
	// the backdrop.
	capture := overBlack(want, 12, 10)
	if summary, problems := panelProblems(capture, want); len(problems) > 0 {
		t.Errorf("the renderer's own image was refused: %v (%s)", problems, summary)
	}
	wrong := image.NewRGBA(capture.Bounds())
	copy(wrong.Pix, capture.(*image.RGBA).Pix)
	for y := 3; y < 7; y++ {
		for x := 3; x < 9; x++ {
			copy(wrong.Pix[wrong.PixOffset(x, y):], []byte{0, 0, 255, 255})
		}
	}
	if _, problems := panelProblems(wrong, want); len(problems) == 0 {
		t.Error("a wrong colour inside the panel passed")
	}
}
