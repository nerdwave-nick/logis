// Package ft is logis's thin binding to libfreetype (spec §5, D2): faces
// opened from mapped font files, hinted glyph loading and rasterisation
// (TrueType bytecode, CFF hinting, the autohinter for unhinted fonts) and
// colour bitmap strikes (CBDT) through FT_LOAD_COLOR.
//
// Every C object is owned by one Go value and released by its Close,
// never by a finalizer. An FT_Library and its faces are not thread-safe:
// they have one owner at a time (the goroutine resolving fonts, then the
// renderer's locked goroutine), which alone calls into them.
package ft

/*
#cgo pkg-config: freetype2
#include <ft2build.h>
#include FT_FREETYPE_H
#include FT_MODULE_H
#include FT_TRUETYPE_TABLES_H
#include FT_DRIVER_H
#include <stdlib.h>

static int logis_ft_has_color(FT_Face f) { return FT_HAS_COLOR(f) ? 1 : 0; }

static FT_Error logis_ft_set_uint(FT_Library l, const char *module, const char *prop, FT_UInt v) {
	return FT_Property_Set(l, module, prop, &v);
}
static FT_Error logis_ft_set_bool(FT_Library l, const char *module, const char *prop, FT_Bool v) {
	return FT_Property_Set(l, module, prop, &v);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Library is an FT_Library with its drivers configured as logis needs.
type Library struct{ l C.FT_Library }

// New initialises FreeType and pins the hinting engines, so that a user's
// FREETYPE_PROPERTIES cannot change logis's glyphs: the TrueType
// interpreter v40 (vertical-only "subpixel" hinting, what skrifa's Smooth
// mode emulates) and no stem darkening for CFF and the autohinter.
func New() (*Library, error) {
	var l C.FT_Library
	if e := C.FT_Init_FreeType(&l); e != 0 {
		return nil, fmt.Errorf("freetype would not wake: error %d", int(e))
	}
	set := func(module, prop string, apply func(m, p *C.char) C.FT_Error) error {
		m, p := C.CString(module), C.CString(prop)
		defer C.free(unsafe.Pointer(m))
		defer C.free(unsafe.Pointer(p))
		if e := apply(m, p); e != 0 {
			return fmt.Errorf("freetype refused %s %s: error %d", module, prop, int(e))
		}
		return nil
	}
	err := errors.Join(
		set("truetype", "interpreter-version", func(m, p *C.char) C.FT_Error {
			return C.logis_ft_set_uint(l, m, p, C.TT_INTERPRETER_VERSION_40)
		}),
		set("cff", "no-stem-darkening", func(m, p *C.char) C.FT_Error { return C.logis_ft_set_bool(l, m, p, 1) }),
		set("autofitter", "no-stem-darkening", func(m, p *C.char) C.FT_Error { return C.logis_ft_set_bool(l, m, p, 1) }),
	)
	if err != nil {
		C.FT_Done_FreeType(l)
		return nil, err
	}
	return &Library{l}, nil
}

// Close releases the library. Its faces must be closed first.
func (l *Library) Close() {
	if l.l != nil {
		C.FT_Done_FreeType(l.l)
		l.l = nil
	}
}

// Mapped is a font file mapped read-only into memory outside the Go heap,
// so FreeType may keep pointing into it and Go may read it (through
// golang.org/x/image/font/sfnt) without a copy.
type Mapped struct{ data []byte }

// MapFile maps the file at path.
func MapFile(path string) (*Mapped, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() <= 0 || st.Size() > 1<<31 {
		return nil, fmt.Errorf("%s: %d bytes cannot be a font", path, st.Size())
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Mapped{data}, nil
}

// Bytes is the file's contents, valid until Close.
func (m *Mapped) Bytes() []byte { return m.data }

// Close unmaps the file. Faces opened from it must be closed first.
func (m *Mapped) Close() {
	if m.data != nil {
		_ = syscall.Munmap(m.data)
		m.data = nil
	}
}

// Face is an FT_Face over a Mapped file.
type Face struct {
	f    C.FT_Face
	size float32 // the size last set, 0 for none
}

// Open opens face index of m. m must outlive the face.
func (l *Library) Open(m *Mapped, index int) (*Face, error) {
	var f C.FT_Face
	e := C.FT_New_Memory_Face(l.l, (*C.FT_Byte)(unsafe.Pointer(&m.data[0])), C.FT_Long(len(m.data)), C.FT_Long(index), &f)
	if e != 0 {
		return nil, fmt.Errorf("freetype cannot read face %d: error %d", index, int(e))
	}
	return &Face{f: f}, nil
}

// Close releases the face.
func (f *Face) Close() {
	if f.f != nil {
		C.FT_Done_Face(f.f)
		f.f = nil
	}
}

// CharIndex is the glyph the face's cmap gives r, 0 for none.
func (f *Face) CharIndex(r rune) uint32 {
	return uint32(C.FT_Get_Char_Index(f.f, C.FT_ULong(r)))
}

// HasColor reports whether the face has colour glyphs (CBDT here).
func (f *Face) HasColor() bool { return C.logis_ft_has_color(f.f) == 1 }

// Metrics are a face's line metrics in font units and its weight.
type Metrics struct {
	UnitsPerEm int
	// Ascent and Descent are positive distances from the baseline: hhea's,
	// or OS/2's typographic ones when the font asks for them
	// (USE_TYPO_METRICS) or hhea has none.
	Ascent, Descent, LineGap int
	// Weight is OS/2's usWeightClass (400 without OS/2).
	Weight int
}

// Metrics reads the face's hhea and OS/2 tables.
func (f *Face) Metrics() Metrics {
	m := Metrics{UnitsPerEm: int(f.f.units_per_EM), Weight: 400}
	if hhea := (*C.TT_HoriHeader)(C.FT_Get_Sfnt_Table(f.f, C.FT_SFNT_HHEA)); hhea != nil {
		m.Ascent, m.Descent, m.LineGap = int(hhea.Ascender), -int(hhea.Descender), int(hhea.Line_Gap)
	}
	if os2 := (*C.TT_OS2)(C.FT_Get_Sfnt_Table(f.f, C.FT_SFNT_OS2)); os2 != nil && os2.version != 0xffff {
		m.Weight = int(os2.usWeightClass)
		const useTypoMetrics = 1 << 7
		if os2.fsSelection&useTypoMetrics != 0 || m.Ascent == 0 && m.Descent == 0 {
			m.Ascent, m.Descent, m.LineGap = int(os2.sTypoAscender), -int(os2.sTypoDescender), int(os2.sTypoLineGap)
		}
	}
	return m
}
