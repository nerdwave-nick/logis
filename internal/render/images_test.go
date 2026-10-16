package render_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// Ports of Rust tests/icons.rs's GPU half: the image atlas and the image
// pass, read back pixel by pixel. (Its sRGB-target case has no
// counterpart: logis's GL targets are plain RGBA8.)

// image is a w×h premultiplied image whose texels come from texel.
func image(w, h int, texel func(x, y int) [4]byte) *render.Image {
	img := &render.Image{Width: w, Height: h}
	for y := range h {
		for x := range w {
			px := texel(x, y)
			img.Pixels = append(img.Pixels, px[:]...)
		}
	}
	return img
}

// pattern gives distinct premultiplied texels, so a misplaced one shows.
func pattern(x, y int) [4]byte {
	a := byte(128 + (x*16+y*8)%128)
	return [4]byte{byte(x*30) % a, byte(y*40) % a, 7 % a, a}
}

func insert(t *testing.T, h *render.Headless, img *render.Image) render.ImageID {
	t.Helper()
	id, err := h.Atlas().Insert(img)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func draw(t *testing.T, h *render.Headless, sc *render.Scene, size scale.Size, s scale.Scale) *render.Image {
	t.Helper()
	img, err := h.Offscreen(sc, size, s)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestAnImageIsDrawnTexelForTexelAtItsSnappedCorner(t *testing.T) {
	source := image(7, 5, pattern)
	// 10.4 logical: 10 at 1.0, 13.52 → 14 at 1.3, 20.8 → 21 at 2.0.
	for _, tc := range []struct {
		n      uint32
		corner int
	}{{120, 10}, {156, 14}, {240, 21}} {
		t.Run(fmt.Sprint(tc.n), func(t *testing.T) {
			h := rendertest.Headless(t)
			id := insert(t, h, source)
			sc := &render.Scene{Images: []render.ImageQuad{{Image: id, Origin: [2]float32{10.4, 10.4}, Opacity: 1}}}
			out := draw(t, h, sc, scale.Size{W: 40, H: 30}, scale.From120ths(tc.n))
			c := tc.corner
			for y := range out.Height {
				for x := range out.Width {
					want := [4]byte{}
					if x >= c && x < c+7 && y >= c && y < c+5 {
						want = pattern(x-c, y-c)
					}
					if got := out.Pixel(x, y); got != want {
						t.Fatalf("pixel (%d, %d) = %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestAnImageIsDrawnAtItsOpacity(t *testing.T) {
	h := rendertest.Headless(t)
	red := insert(t, h, image(4, 4, func(int, int) [4]byte { return [4]byte{200, 0, 0, 200} }))
	sc := &render.Scene{Images: []render.ImageQuad{{Image: red, Opacity: 0.5}}}
	px := draw(t, h, sc, scale.Size{W: 8, H: 8}, scale.One).Pixel(1, 1)
	if px[0] < 99 || px[0] > 101 || px[1] != 0 || px[2] != 0 || px[3] < 99 || px[3] > 101 {
		t.Errorf("pixel %v, want premultiplied red half as opaque (100, 0, 0, 100)", px)
	}
}

func TestImagesPaintOverShapesWithPremultipliedBlending(t *testing.T) {
	h := rendertest.Headless(t)
	redHalf := insert(t, h, image(4, 4, func(int, int) [4]byte { return [4]byte{128, 0, 0, 128} }))
	sc := &render.Scene{
		Shapes: []render.RoundedRect{{Rect: [4]float32{0, 0, 20, 20}, Fill: render.Solid(render.RGBA(0, 0, 1, 1))}},
		Images: []render.ImageQuad{{Image: redHalf, Origin: [2]float32{5, 5}, Opacity: 1}},
	}
	out := draw(t, h, sc, scale.Size{W: 20, H: 20}, scale.One)
	// src + dst × (1 − 128/255).
	px := out.Pixel(6, 6)
	if px[0] != 128 || px[1] != 0 || px[3] != 255 || max(px[2], 127)-min(px[2], 127) > 1 {
		t.Errorf("pixel %v, want (128, 0, 127±1, 255)", px)
	}
	if px := out.Pixel(2, 2); px != [4]byte{0, 0, 255, 255} {
		t.Errorf("beside the image %v, want the shape's blue", px)
	}
}

func TestImagesOfTransparentOrUnknownHandlesAreSkipped(t *testing.T) {
	h := rendertest.Headless(t)
	white := insert(t, h, image(4, 4, func(int, int) [4]byte { return [4]byte{255, 255, 255, 255} }))
	sc := &render.Scene{Images: []render.ImageQuad{
		{Image: white, Opacity: 0},
		{Image: white + 7, Origin: [2]float32{4, 4}, Opacity: 1},
	}}
	out := draw(t, h, sc, scale.Size{W: 8, H: 8}, scale.One)
	for _, p := range [][2]int{{1, 1}, {5, 5}} {
		if px := out.Pixel(p[0], p[1]); px != [4]byte{} {
			t.Errorf("pixel %v = %v, want nothing drawn", p, px)
		}
	}
}

func TestTheAtlasGrowsAndKeepsItsImages(t *testing.T) {
	h := rendertest.HeadlessForStructure(t)
	colours := [][4]byte{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}
	var ids []render.ImageID
	for _, c := range colours {
		ids = append(ids, insert(t, h, image(300, 300, func(int, int) [4]byte { return c })))
	}
	if side := h.Atlas().Side(); side != 1024 {
		t.Errorf("side %d, want 1024", side)
	}
	if n := h.Atlas().Len(); n != 3 {
		t.Errorf("%d images, want 3", n)
	}
	for i, id := range ids {
		if w, hh, ok := h.Atlas().SizeOf(id); !ok || w != 300 || hh != 300 {
			t.Errorf("image %d is %dx%d (%v)", i, w, hh, ok)
		}
	}
	rendertest.SkipPixels(t)
	for i, id := range ids {
		sc := &render.Scene{Images: []render.ImageQuad{{Image: id, Opacity: 1}}}
		out := draw(t, h, sc, scale.Size{W: 300, H: 300}, scale.One)
		for _, p := range [][2]int{{0, 0}, {299, 0}, {150, 150}, {0, 299}, {299, 299}} {
			if got := out.Pixel(p[0], p[1]); got != colours[i] {
				t.Errorf("image %d at %v: %v, want %v", i, p, got, colours[i])
			}
		}
	}
}

func TestTheAtlasRefusesMalformedAndOversizedImages(t *testing.T) {
	h := rendertest.HeadlessForStructure(t)
	atlas := h.Atlas()
	_, err := atlas.Insert(image(0, 3, func(int, int) [4]byte { return [4]byte{} }))
	var malformed *render.MalformedImageError
	if !errors.As(err, &malformed) || malformed.Width != 0 || malformed.Height != 3 ||
		err.Error() != "a malformed 0x3 image was refused by the image atlas" {
		t.Errorf("empty image: %v", err)
	}
	_, err = atlas.Insert(&render.Image{Width: 2, Height: 2, Pixels: make([]byte, 15)})
	if !errors.As(err, &malformed) || malformed.Width != 2 || malformed.Height != 2 {
		t.Errorf("short image: %v", err)
	}
	_, err = atlas.Insert(image(20_000, 1, func(int, int) [4]byte { return [4]byte{} }))
	var large *render.ImageTooLargeError
	if !errors.As(err, &large) || large.Width != 20_000 ||
		err.Error() != fmt.Sprintf("a 20000x1 image cannot fit the image atlas's largest side of %d", large.Max) {
		t.Errorf("wide image: %v", err)
	}
	if atlas.Len() != 0 || atlas.Side() != 512 {
		t.Errorf("atlas holds %d images at side %d, want none at 512", atlas.Len(), atlas.Side())
	}
	if _, _, ok := atlas.SizeOf(0); ok {
		t.Error("an empty atlas knows image 0")
	}
}
