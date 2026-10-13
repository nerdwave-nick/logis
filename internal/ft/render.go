package ft

/*
#include <ft2build.h>
#include FT_FREETYPE_H
#include FT_OUTLINE_H

enum {
	logis_ft_normal = FT_LOAD_TARGET_NORMAL,
	logis_ft_light = FT_LOAD_TARGET_LIGHT,
	logis_ft_lcd = FT_LOAD_TARGET_LCD,
};

static int logis_ft_bitmap_only(FT_Face f) { return !FT_IS_SCALABLE(f) && FT_HAS_FIXED_SIZES(f); }
static int logis_ft_strike_ppem(FT_Face f, int i) { return (int)((f->available_sizes[i].y_ppem + 32) >> 6); }
*/
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

// Target is how outlines are hinted: FreeType's load target. Every target
// is rasterised as an 8-bit coverage mask.
type Target int

const (
	// Light is FT_LOAD_TARGET_LIGHT: with interpreter v40 the TrueType
	// bytecode and the CFF hinter both hint (snap) vertically only; the
	// autohinter, for fonts without hints, does the same.
	Light Target = iota
	// Normal is FT_LOAD_TARGET_NORMAL: as Light for TrueType v40 and CFF,
	// but the autohinter also hints horizontally.
	Normal
	// LCD is FT_LOAD_TARGET_LCD (the bytecode is told it renders for a
	// horizontal LCD, as skrifa's Smooth mode with lcd_subpixel does),
	// rasterised as grey coverage.
	LCD
	// Unhinted loads outlines as they are (FT_LOAD_NO_HINTING).
	Unhinted
)

func (t Target) String() string {
	switch t {
	case Light:
		return "light"
	case Normal:
		return "normal"
	case LCD:
		return "lcd"
	case Unhinted:
		return "unhinted"
	}
	return fmt.Sprintf("Target(%d)", int(t))
}

func (t Target) flags() C.FT_Int32 {
	switch t {
	case Normal:
		return C.logis_ft_normal
	case LCD:
		return C.logis_ft_lcd
	case Unhinted:
		return C.FT_LOAD_NO_HINTING
	}
	return C.logis_ft_light
}

// Bitmap is a rendered glyph.
type Bitmap struct {
	// Width and Height in pixels; 0 for a glyph without ink.
	Width, Height int
	// Left and Top are the bitmap's top-left corner relative to the glyph
	// origin on the baseline, x right and y up (FreeType's bitmap_left and
	// bitmap_top).
	Left, Top int
	// Color marks premultiplied RGBA8 pixels from a colour strike, drawn
	// at PPEM pixels per em (the caller scales them to its size);
	// otherwise Pixels is coverage, one byte per pixel, at the size asked.
	Color bool
	PPEM  int
	// Pixels are rows top to bottom without padding.
	Pixels []byte
}

// Render loads glyph id at size pixels per em, offset right by xoff
// pixels (a subpixel position), hinted for target, and rasterises it.
// Faces with only colour strikes give the strike that fits size best
// (swash's StrikeWith::BestFit: the smallest at least size, else the
// largest), unscaled.
func (f *Face) Render(id uint32, size, xoff float32, target Target) (*Bitmap, error) {
	if C.logis_ft_bitmap_only(f.f) == 1 {
		return f.strike(id, size)
	}
	if size != f.size {
		px := C.FT_F26Dot6(math.Round(float64(size) * 64))
		if e := C.FT_Set_Char_Size(f.f, 0, px, 72, 72); e != 0 {
			return nil, fmt.Errorf("freetype cannot size a face at %g px: error %d", size, int(e))
		}
		f.size = size
	}
	// Only outlines: embedded monochrome or grey strikes are never used
	// (swash's sources are colour outlines, colour bitmaps, outlines).
	if e := C.FT_Load_Glyph(f.f, C.FT_UInt(id), target.flags()|C.FT_LOAD_NO_BITMAP); e != 0 {
		return nil, fmt.Errorf("freetype cannot load glyph %d: error %d", id, int(e))
	}
	slot := f.f.glyph
	if dx := C.FT_Pos(math.Round(float64(xoff) * 64)); dx != 0 && slot.format == C.FT_GLYPH_FORMAT_OUTLINE {
		C.FT_Outline_Translate(&slot.outline, dx, 0)
	}
	if e := C.FT_Render_Glyph(slot, C.FT_RENDER_MODE_NORMAL); e != 0 {
		return nil, fmt.Errorf("freetype cannot rasterise glyph %d: error %d", id, int(e))
	}
	return copyBitmap(slot, 1), nil
}

// strike loads glyph id from the colour strike that fits size best.
func (f *Face) strike(id uint32, size float32) (*Bitmap, error) {
	best, ppem := -1, 0
	for i := range int(f.f.num_fixed_sizes) {
		p := int(C.logis_ft_strike_ppem(f.f, C.int(i)))
		fits, bestFits := float32(p) >= size, float32(ppem) >= size
		if best < 0 || fits && (!bestFits || p < ppem) || !fits && !bestFits && p > ppem {
			best, ppem = i, p
		}
	}
	if best < 0 {
		return nil, fmt.Errorf("the face has no strike for glyph %d", id)
	}
	if e := C.FT_Select_Size(f.f, C.FT_Int(best)); e != 0 {
		return nil, fmt.Errorf("freetype cannot select the %d ppem strike: error %d", ppem, int(e))
	}
	f.size = 0
	if e := C.FT_Load_Glyph(f.f, C.FT_UInt(id), C.FT_LOAD_COLOR); e != 0 {
		return nil, fmt.Errorf("freetype cannot load glyph %d: error %d", id, int(e))
	}
	slot := f.f.glyph
	if slot.bitmap.pixel_mode != C.FT_PIXEL_MODE_BGRA {
		return nil, fmt.Errorf("glyph %d is not a colour bitmap", id)
	}
	b := copyBitmap(slot, 4)
	b.Color, b.PPEM = true, ppem
	return b, nil
}

// copyBitmap copies the slot's bitmap, bpp bytes per pixel (grey, or BGRA
// turned into RGBA), into Go memory without row padding.
func copyBitmap(slot C.FT_GlyphSlot, bpp int) *Bitmap {
	bm := slot.bitmap
	w, h := int(bm.width), int(bm.rows)
	b := &Bitmap{Width: w, Height: h, Left: int(slot.bitmap_left), Top: int(slot.bitmap_top)}
	if w == 0 || h == 0 || bm.buffer == nil {
		b.Width, b.Height = 0, 0
		return b
	}
	pitch := int(bm.pitch)
	src := unsafe.Slice((*byte)(unsafe.Pointer(bm.buffer)), max(pitch, -pitch)*h)
	b.Pixels = make([]byte, w*h*bpp)
	for y := range h {
		row := y * pitch
		if pitch < 0 { // rows bottom to top
			row = (h - 1 - y) * -pitch
		}
		dst := b.Pixels[y*w*bpp : (y+1)*w*bpp]
		copy(dst, src[row:row+w*bpp])
		if bpp == 4 {
			for x := 0; x < len(dst); x += 4 {
				dst[x], dst[x+2] = dst[x+2], dst[x]
			}
		}
	}
	return b
}
