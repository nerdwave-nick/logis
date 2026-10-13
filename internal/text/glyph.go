package text

import (
	"fmt"

	"github.com/nerdwave-nick/logis/internal/ft"
)

// Hinting is how outlines are hinted: FreeType's load target.
type Hinting = ft.Target

// The hinting targets (see ft.Target).
const (
	Light    = ft.Light
	Normal   = ft.Normal
	LCD      = ft.LCD
	Unhinted = ft.Unhinted
)

// DefaultHinting is the load target chosen by comparing the Go renders
// with the Rust text goldens (tests/goldens/text) at scales 1.0, 1.3 and
// 2.0 (G6 handoff): FT_LOAD_TARGET_LCD with interpreter v40 is what
// swash's skrifa runs (HintingMode::Smooth with a horizontal LCD layout:
// the bytecode hints vertically and is told it renders ClearType; CFF and
// the autohinter hint lightly). On the pinned Noto fonts it renders like
// Normal and matches the goldens at 1.0 within their tolerance; Light is
// 3-6 % of pixels off, unhinted 10-18 %.
const DefaultHinting = LCD

// SetHinting changes how glyphs are hinted from now on (for comparisons;
// the renderer keeps DefaultHinting). Images already cached elsewhere are
// not redrawn.
func (f *Fonts) SetHinting(h Hinting) { f.hinting = h }

// GlyphKey names one glyph image: a font's glyph at a size in pixels per
// em and a quarter-pixel horizontal bin (cosmic-text's SubpixelBin, 0–3).
type GlyphKey struct {
	Font FontID
	ID   uint16
	Size float32
	XBin uint8
}

// GlyphImage is a rendered glyph, its top-left corner Left pixels right of
// and Top pixels above the glyph's origin on the baseline. Without Color,
// Pixels is coverage, one byte per pixel; with Color, straight-alpha RGBA8
// (a colour emoji scaled to the em).
type GlyphImage struct {
	Width, Height int
	Left, Top     int
	Color         bool
	Pixels        []byte
}

// Glyph renders k: an outline hinted and rasterised by FreeType, or a
// colour strike scaled to the size with swash's filter.
func (f *Fonts) Glyph(k GlyphKey) (*GlyphImage, error) {
	if k.Font < 0 || int(k.Font) >= len(f.fonts) {
		return nil, fmt.Errorf("no font %d is loaded", k.Font)
	}
	b, err := f.fonts[k.Font].face.Render(uint32(k.ID), k.Size, float32(k.XBin)/4, f.hinting)
	if err != nil {
		return nil, err
	}
	img := &GlyphImage{Width: b.Width, Height: b.Height, Left: b.Left, Top: b.Top, Pixels: b.Pixels}
	if b.Color {
		img = scaleStrike(b, k.Size)
	}
	return img, nil
}

// scaleStrike scales a colour strike's premultiplied bitmap to size as
// swash does: size and placement truncated, Mitchell resampling.
func scaleStrike(b *ft.Bitmap, size float32) *GlyphImage {
	s := size / float32(b.PPEM)
	img := &GlyphImage{Left: b.Left, Top: b.Top, Width: b.Width, Height: b.Height, Color: true}
	if size == 0 || s == 1 {
		img.Pixels = unpremultiply(b.Pixels)
		return img
	}
	img.Width, img.Height = int(float32(b.Width)*s), int(float32(b.Height)*s)
	img.Left, img.Top = int(float32(b.Left)*s), int(float32(b.Top)*s)
	if img.Width <= 0 || img.Height <= 0 {
		img.Width, img.Height = 0, 0
		return img
	}
	img.Pixels = resize(b.Pixels, b.Width, b.Height, img.Width, img.Height)
	return img
}

func unpremultiply(p []byte) []byte {
	out := make([]byte, len(p))
	for i := 0; i < len(p); i += 4 {
		a := uint32(p[i+3])
		out[i+3] = byte(a)
		if a == 0 {
			continue
		}
		for c := range 3 {
			out[i+c] = byte(min(255, (uint32(p[i+c])*255+a/2)/a))
		}
	}
	return out
}
