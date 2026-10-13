package icons

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render"
)

// Ported from Rust src/icons/raster.rs.

func fixture(t testing.TB, rel string) string { return filepath.Join(fixtures(t), rel) }

func rasterise(t *testing.T, rel string, pixels int) *render.Image {
	t.Helper()
	img, err := Rasterise(fixture(t, rel), pixels)
	if err != nil {
		t.Fatalf("%s at %d: %v", rel, pixels, err)
	}
	if img.Width != pixels || img.Height != pixels || len(img.Pixels) != pixels*pixels*4 {
		t.Fatalf("%s: %dx%d with %d bytes, want %d square", rel, img.Width, img.Height, len(img.Pixels), pixels)
	}
	return img
}

func premultipliedImage(img *render.Image) bool {
	for i := 0; i < len(img.Pixels); i += 4 {
		p := img.Pixels[i : i+4]
		if p[0] > p[3] || p[1] > p[3] || p[2] > p[3] {
			return false
		}
	}
	return true
}

func TestAPNGAtItsOwnSizeKeepsItsPixels(t *testing.T) {
	img := rasterise(t, forgeDir+"/32x32/apps/forge-cog.png", 32)
	if got := img.Pixel(0, 0); got != [4]byte{} {
		t.Errorf("corner %v, want transparent", got)
	}
	if got := img.Pixel(10, 16); got != [4]byte{220, 120, 20, 255} {
		t.Errorf("(10, 16) = %v, want brass", got)
	}
	if !premultipliedImage(img) {
		t.Error("not premultiplied")
	}
}

func TestAPNGIsResampledToTheRequestedSize(t *testing.T) {
	for _, pixels := range []int{42, 20, 100, 16} {
		img := rasterise(t, forgeDir+"/32x32@2/apps/forge-cog.png", pixels)
		if got := img.Pixel(0, 0); got != [4]byte{} {
			t.Errorf("%d: corner %v", pixels, got)
		}
		p := img.Pixel(pixels/2, pixels*3/4)
		if brass := p[3] == 255 && p[0] > 200 && p[1] > 170 && p[2] < 80; !brass {
			t.Errorf("%d: lower middle %v, want opaque brass", pixels, p)
		}
		if !premultipliedImage(img) {
			t.Errorf("%d: not premultiplied", pixels)
		}
	}
}

func TestAnIndexedPNGDecodes(t *testing.T) {
	img := rasterise(t, "pixmaps/pixmap-only.png", 48)
	if a := img.Pixel(0, 0)[3]; a != 0 {
		t.Errorf("corner alpha %d", a)
	}
	if a := img.Pixel(24, 36)[3]; a != 255 {
		t.Errorf("(24, 36) alpha %d", a)
	}
}

func TestAnSVGRendersAtTheRequestedSize(t *testing.T) {
	for _, pixels := range []int{32, 42} {
		img := rasterise(t, forgeDir+"/scalable/apps/forge-cog.svg", pixels)
		// The dark hub at the centre, nothing in the corners.
		if p := img.Pixel(pixels/2, pixels/2); p != [4]byte{0x20, 0x08, 0x08, 255} {
			t.Errorf("%d: hub %v", pixels, p)
		}
		if p := img.Pixel(0, 0); p != [4]byte{} {
			t.Errorf("%d: corner %v", pixels, p)
		}
		if !premultipliedImage(img) {
			t.Errorf("%d: not premultiplied", pixels)
		}
	}
}

func TestAWideSVGIsFittedAndCentred(t *testing.T) {
	img := rasterise(t, forgeDir+"/scalable/apps/forge-banner.svg", 32)
	// 64×32 fits as 32×16 at y 8..24.
	for x := range 32 {
		if a := img.Pixel(x, 6)[3]; a != 0 {
			t.Errorf("above the banner at x %d: alpha %d", x, a)
		}
		if a := img.Pixel(x, 25)[3]; a != 0 {
			t.Errorf("below the banner at x %d: alpha %d", x, a)
		}
	}
	if p := img.Pixel(28, 16); p != [4]byte{0xa0, 0x18, 0x18, 255} {
		t.Errorf("(28, 16) = %v, want the banner's crimson", p)
	}
}

func TestBrokenUnsupportedMissingAndEmptyRequestsFail(t *testing.T) {
	reason := func(rel string, pixels int) string {
		t.Helper()
		_, err := Rasterise(fixture(t, rel), pixels)
		var re *RasterError
		if !errors.As(err, &re) {
			t.Fatalf("%s at %d: %v, want a *RasterError", rel, pixels, err)
		}
		if re.Path != fixture(t, rel) {
			t.Errorf("path %q", re.Path)
		}
		return re.Reason
	}
	for _, rel := range []string{"pixmaps/broken.png", "pixmaps/broken.svg", "pixmaps/missing.png"} {
		if r := reason(rel, 32); r == "" {
			t.Errorf("%s: no reason", rel)
		}
	}
	if r := reason("pixmaps/xpm-only.xpm", 32); r != "only PNG and SVG icons are supported" {
		t.Errorf("xpm: %q", r)
	}
	for _, pixels := range []int{0, -1, 4096} {
		if r := reason("pixmaps/pixmap-only.png", pixels); !strings.Contains(r, "outside") {
			t.Errorf("%d pixels: %q", pixels, r)
		}
	}
	_, err := Rasterise(fixture(t, "pixmaps/broken.png"), 32)
	if !strings.Contains(err.Error(), "broken.png resists the rite of rasterisation: ") {
		t.Errorf("message %q", err)
	}
}
