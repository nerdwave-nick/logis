// Package ot is an OpenType layout engine: the tables a shaper reads from
// a face (cmap with variation sequences, metrics, GDEF, GSUB, GPOS, kern)
// and the application of their lookups to a glyph buffer — GSUB lookup
// types 1–8 and GPOS 1–9 with lookup flags, mark filtering sets, device
// tables and HarfBuzz's matching rules (default ignorables, ligature
// components, attachment chains). It follows HarfBuzz's algorithm (as
// ported by harfrust, which Rust logis v1 shapes with) so that glyphs and
// positions equal HarfBuzz's; the Unicode side of shaping (normalisation,
// scripts, joining) is internal/shape's.
//
// Fonts are external data: every read is bounds-checked and a malformed
// table gives empty answers, never a panic. Variable fonts, AAT tables and
// vertical layout are out of scope.
package ot

import "errors"

// Tag is an OpenType tag.
type Tag uint32

// MakeTag is the tag of four ASCII characters.
func MakeTag(s string) Tag {
	var t Tag
	for i := range 4 {
		c := byte(' ')
		if i < len(s) {
			c = s[i]
		}
		t = t<<8 | Tag(c)
	}
	return t
}

func (t Tag) String() string {
	return string([]byte{byte(t >> 24), byte(t >> 16), byte(t >> 8), byte(t)})
}

// ErrNotFont reports data that is not an sfnt font or collection.
var ErrNotFont = errors.New("ot: not an OpenType font")

// Face is the layout data of one font face. It only reads the font's
// bytes, which must stay unchanged while it is used; a Face is safe for
// concurrent use.
type Face struct {
	upem        int
	numGlyphs   int
	numHMetrics int
	hmtx        table
	cmap        cmap
	gdef        gdef
	gsub, gpos  *Layout
	kern        table
	loca, glyf  table
	longLoca    bool
}

// Parse reads face index of data, a font file or a collection.
func Parse(data []byte, index int) (*Face, error) {
	t := table(data)
	dir := 0
	switch t.u32(0) {
	case 0x74746366: // 'ttcf'
		if index < 0 || index >= int(t.u32(8)) {
			return nil, ErrNotFont
		}
		dir = int(t.u32(12 + 4*index))
	case 0x00010000, 0x4f54544f, 0x74727565: // TrueType, 'OTTO', 'true'
	default:
		return nil, ErrNotFont
	}
	tables := map[Tag]table{}
	n := int(t.u16(dir + 4))
	for i := range n {
		rec := dir + 12 + 16*i
		off, length := int(t.u32(rec+8)), int(t.u32(rec+12))
		if off <= 0 || length <= 0 || off+length > len(data) || off+length < off {
			continue
		}
		tables[Tag(t.u32(rec))] = t[off : off+length]
	}
	head, maxp, hhea := tables[MakeTag("head")], tables[MakeTag("maxp")], tables[MakeTag("hhea")]
	if head == nil || maxp == nil {
		return nil, ErrNotFont
	}
	f := &Face{
		upem:        int(head.u16(18)),
		numGlyphs:   int(maxp.u16(4)),
		numHMetrics: int(hhea.u16(34)),
		hmtx:        tables[MakeTag("hmtx")],
		cmap:        parseCmap(tables[MakeTag("cmap")]),
		gdef:        parseGDEF(tables[MakeTag("GDEF")]),
		gsub:        parseLayout(tables[MakeTag("GSUB")], true),
		gpos:        parseLayout(tables[MakeTag("GPOS")], false),
		kern:        tables[MakeTag("kern")],
		loca:        tables[MakeTag("loca")],
		glyf:        tables[MakeTag("glyf")],
		longLoca:    head.u16(50) == 1,
	}
	if f.upem < 16 || f.upem > 16384 {
		f.upem = 1000
	}
	return f, nil
}

// UnitsPerEm is the face's design units per em.
func (f *Face) UnitsPerEm() int { return f.upem }

// NumGlyphs is the number of glyphs in the face.
func (f *Face) NumGlyphs() int { return f.numGlyphs }

// Advance is glyph g's horizontal advance in font units.
func (f *Face) Advance(g uint16) int {
	if int(g) >= f.numGlyphs && f.numGlyphs > 0 {
		return 0
	}
	n := f.numHMetrics
	if n <= 0 {
		return 0
	}
	if int(g) >= n {
		g = uint16(n - 1)
	}
	return int(f.hmtx.u16(4 * int(g)))
}

// GSUB and GPOS are the face's layout tables, nil when absent.
func (f *Face) GSUB() *Layout { return f.gsub }

// GPOS: see GSUB.
func (f *Face) GPOS() *Layout { return f.gpos }

// HasGlyphClasses reports a GDEF glyph class definition.
func (f *Face) HasGlyphClasses() bool { return f.gdef.classes != nil }

// Glyph property bits (HarfBuzz's GlyphPropsFlags).
const (
	PropBaseGlyph   = 0x02
	PropLigature    = 0x04
	PropMark        = 0x08
	PropClassMask   = PropBaseGlyph | PropLigature | PropMark
	PropSubstituted = 0x10
	PropLigated     = 0x20
	PropMultiplied  = 0x40
	PropPreserve    = PropSubstituted | PropLigated | PropMultiplied
)

// GlyphProps is g's class from GDEF as property bits; a mark carries its
// mark attachment class in the high byte.
func (f *Face) GlyphProps(g uint16) uint16 {
	switch f.gdef.classes.classOf(g) {
	case 1:
		return PropBaseGlyph
	case 2:
		return PropLigature
	case 3:
		return uint16(f.gdef.markAttach.classOf(g))<<8 | PropMark
	}
	return 0
}

// IsInMarkSet reports whether g is in GDEF mark glyph set i.
func (f *Face) IsInMarkSet(i int, g uint16) bool {
	s := f.gdef.markSets
	if i < 0 || i >= int(s.u16(2)) {
		return false
	}
	return s.sub32(4+4*i).coverage(g) >= 0
}

// gdef is the parts of GDEF the shaper uses.
type gdef struct {
	classes, markAttach, markSets table
}

func parseGDEF(t table) gdef {
	if t.u16(0) != 1 {
		return gdef{}
	}
	g := gdef{classes: t.sub(4), markAttach: t.sub(10)}
	if t.u16(2) >= 2 {
		g.markSets = t.sub(12)
	}
	return g
}

// Extents is glyph g's ink box in font units, as HarfBuzz reports it for
// TrueType outlines: the x bearing is hmtx's left side bearing, y bearing
// the top, height negative. False without glyf outlines.
func (f *Face) Extents(g uint16) (xBearing, yBearing, width, height int32, ok bool) {
	if f.glyf == nil || int(g) >= f.numGlyphs {
		return 0, 0, 0, 0, false
	}
	var start, end int
	if f.longLoca {
		start, end = int(f.loca.u32(4*int(g))), int(f.loca.u32(4*int(g)+4))
	} else {
		start, end = 2*int(f.loca.u16(2*int(g))), 2*int(f.loca.u16(2*int(g)+2))
	}
	if end <= start || start >= len(f.glyf) {
		return 0, 0, 0, 0, true
	}
	h := f.glyf[start:] // offset 0 is a glyph here, not a null offset
	xMin, yMin, xMax, yMax := int32(h.i16(2)), int32(h.i16(4)), int32(h.i16(6)), int32(h.i16(8))
	return int32(f.lsb(g)), yMax, xMax - xMin, yMin - yMax, true
}

// lsb is glyph g's left side bearing from hmtx.
func (f *Face) lsb(g uint16) int16 {
	n := f.numHMetrics
	if int(g) < n {
		return f.hmtx.i16(4*int(g) + 2)
	}
	return f.hmtx.i16(4*n + 2*(int(g)-n))
}
