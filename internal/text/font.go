package text

import (
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/nerdwave-nick/logis/internal/fc"
	"github.com/nerdwave-nick/logis/internal/ft"
)

// FontID names a loaded font: its index in Fonts.Loaded.
type FontID int

// Font is one loaded face: its file mapped once, read by
// golang.org/x/image/font/sfnt for cmap, advances and kerning (and by the
// shaper for its layout tables) and by FreeType for glyph images.
type Font struct {
	File fc.FontFile
	// Family is the face's family name (typographic if it has one) and
	// Weight its OS/2 weight class, as Rust's identity reads them.
	Family string
	Weight int
	// Metrics are its line metrics in font units.
	Metrics ft.Metrics
	mapped  *ft.Mapped
	face    *ft.Face
	sfnt    *sfnt.Font
	buf     sfnt.Buffer
	upem    fixed.Int26_6
}

// openFont maps file and opens it with lib; any failure is Unreadable.
func openFont(lib *ft.Library, file fc.FontFile) (*Font, error) {
	unreadable := &UnreadableError{File: file}
	m, err := ft.MapFile(file.Path)
	if err != nil {
		return nil, unreadable
	}
	f := &Font{File: file, mapped: m}
	if f.sfnt, err = parse(m.Bytes(), file.Index); err != nil {
		f.close()
		return nil, unreadable
	}
	if f.face, err = lib.Open(m, file.Index); err != nil {
		f.close()
		return nil, unreadable
	}
	f.Metrics = f.face.Metrics()
	// FreeType has no units per em for faces of bitmaps only; head has.
	f.Metrics.UnitsPerEm = int(f.sfnt.UnitsPerEm())
	f.Weight = f.Metrics.Weight
	f.upem = fixed.Int26_6(f.sfnt.UnitsPerEm())
	f.Family, err = f.sfnt.Name(&f.buf, sfnt.NameIDTypographicFamily)
	if err != nil || f.Family == "" {
		f.Family, err = f.sfnt.Name(&f.buf, sfnt.NameIDFamily)
	}
	if err != nil {
		f.close()
		return nil, unreadable
	}
	return f, nil
}

// parse reads face index of data, a font or a collection.
func parse(data []byte, index int) (*sfnt.Font, error) {
	if len(data) >= 4 && string(data[:4]) == "ttcf" {
		c, err := sfnt.ParseCollection(data)
		if err != nil {
			return nil, err
		}
		return c.Font(index)
	}
	return sfnt.Parse(data)
}

// Data is the font file's bytes (mapped, valid while the font is loaded).
func (f *Font) Data() []byte { return f.mapped.Bytes() }

// SFNT is the parsed face, for the shaper's own table reading.
func (f *Font) SFNT() *sfnt.Font { return f.sfnt }

// GlyphIndex is the glyph f's cmap gives r, 0 for none.
func (f *Font) GlyphIndex(r rune) uint16 {
	g, err := f.sfnt.GlyphIndex(&f.buf, r)
	if err != nil {
		return 0
	}
	return uint16(g)
}

// advance is glyph g's advance width in font units.
func (f *Font) advance(g uint16) int {
	a, err := f.sfnt.GlyphAdvance(&f.buf, sfnt.GlyphIndex(g), f.upem, font.HintingNone)
	if err != nil {
		return 0
	}
	return int(a)
}

// kern is the kerning between glyphs a and b in font units: GPOS pair
// adjustments of the kern feature, or the legacy kern table.
func (f *Font) kern(a, b uint16) int {
	k, err := f.sfnt.Kern(&f.buf, sfnt.GlyphIndex(a), sfnt.GlyphIndex(b), f.upem, font.HintingNone)
	if err != nil {
		return 0
	}
	return int(k)
}

// close releases the FreeType face and unmaps the file.
func (f *Font) close() {
	if f.face != nil {
		f.face.Close()
		f.face = nil
	}
	if f.mapped != nil {
		f.mapped.Close()
		f.mapped = nil
	}
}
