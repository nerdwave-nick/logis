package ot

// cmap is the character map HarfBuzz would pick, plus the variation
// sequences subtable.
type cmap struct {
	sub      table
	format   int
	symbol   bool // Windows symbol encoding: U+F0xx stands for 0x00–0xFF
	macRoman bool
	vs       table // format 14
}

// cmapPreference is HarfBuzz's order of (platform, encoding) subtables.
var cmapPreference = [...][2]uint16{
	{3, 0}, {3, 10}, {0, 6}, {0, 4}, {3, 1}, {0, 3}, {0, 2}, {0, 1}, {0, 0}, {1, 0},
}

func parseCmap(t table) cmap {
	n := int(t.u16(2))
	var c cmap
	best := len(cmapPreference)
	for i := range n {
		rec := 4 + 8*i
		platform, encoding := t.u16(rec), t.u16(rec+2)
		sub := t.at(int(t.u32(rec + 4)))
		if platform == 0 && encoding == 5 && sub.u16(0) == 14 {
			c.vs = sub
			continue
		}
		for rank, p := range cmapPreference[:best] {
			if p == [2]uint16{platform, encoding} && supportedCmap(sub.u16(0)) {
				best = rank
				c.sub, c.format = sub, int(sub.u16(0))
				c.symbol = platform == 3 && encoding == 0
				c.macRoman = platform == 1
			}
		}
	}
	return c
}

func supportedCmap(format uint16) bool {
	switch format {
	case 0, 4, 6, 10, 12, 13:
		return true
	}
	return false
}

// Glyph is the glyph the cmap maps r to, 0 (and false) for none.
func (f *Face) Glyph(r rune) (uint16, bool) {
	g := f.cmap.lookup(r)
	return g, g != 0
}

// VariantGlyph is the glyph of r followed by variation selector vs: the
// font's variant, its default glyph for r when the sequence asks for the
// default, false when the font has no such sequence.
func (f *Face) VariantGlyph(r, vs rune) (uint16, bool) {
	t := f.cmap.vs
	n := int(t.u32(6))
	lo, hi := 0, min(n, (len(t)-10)/11)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		rec := 10 + 11*m
		v := rune(uint32(t.u8(rec))<<16 | uint32(t.u16(rec+1)))
		switch {
		case v < vs:
			lo = m + 1
		case v > vs:
			hi = m
		default:
			if inDefaultUVS(t.at(int(t.u32(rec+3))), r) {
				return f.Glyph(r)
			}
			if g, ok := nonDefaultUVS(t.at(int(t.u32(rec+7))), r); ok {
				return g, g != 0
			}
			return 0, false
		}
	}
	return 0, false
}

func inDefaultUVS(t table, r rune) bool {
	n := int(t.u32(0))
	lo, hi := 0, min(n, (len(t)-4)/4)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		start := rune(uint32(t.u8(4+4*m))<<16 | uint32(t.u16(5+4*m)))
		end := start + rune(t.u8(7+4*m))
		switch {
		case end < r:
			lo = m + 1
		case start > r:
			hi = m
		default:
			return true
		}
	}
	return false
}

func nonDefaultUVS(t table, r rune) (uint16, bool) {
	n := int(t.u32(0))
	lo, hi := 0, min(n, (len(t)-4)/5)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		v := rune(uint32(t.u8(4+5*m))<<16 | uint32(t.u16(5+5*m)))
		switch {
		case v < r:
			lo = m + 1
		case v > r:
			hi = m
		default:
			return t.u16(7 + 5*m), true
		}
	}
	return 0, false
}

func (c *cmap) lookup(r rune) uint16 {
	if r < 0 || r > 0x10FFFF {
		return 0
	}
	cp := r
	if c.macRoman && cp > 0x7f {
		cp = toMacRoman(cp)
	}
	g := c.lookupFormat(cp)
	if g == 0 && c.symbol && r <= 0xff {
		return c.lookupFormat(0xf000 + r)
	}
	return g
}

func (c *cmap) lookupFormat(r rune) uint16 {
	t := c.sub
	switch c.format {
	case 0:
		if r < 256 {
			return uint16(t.u8(6 + int(r)))
		}
	case 6:
		first, n := rune(t.u16(6)), rune(t.u16(8))
		if r >= first && r < first+n {
			return t.u16(10 + 2*int(r-first))
		}
	case 10:
		first, n := rune(t.u32(12)), rune(t.u32(16))
		if r >= first && r-first < n {
			return t.u16(20 + 2*int(r-first))
		}
	case 4:
		return cmap4(t, r)
	case 12, 13:
		return cmap12(t, r, c.format == 13)
	}
	return 0
}

func cmap4(t table, r rune) uint16 {
	if r > 0xffff {
		return 0
	}
	segs := int(t.u16(6)) / 2
	ends, starts := 14, 16+2*segs
	deltas, ranges := starts+2*segs, starts+4*segs
	lo, hi := 0, segs
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		end, start := rune(t.u16(ends+2*m)), rune(t.u16(starts+2*m))
		switch {
		case end < r:
			lo = m + 1
		case start > r:
			hi = m
		default:
			delta := t.u16(deltas + 2*m)
			ro := int(t.u16(ranges + 2*m))
			if ro == 0 {
				return uint16(r) + delta
			}
			g := t.u16(ranges + 2*m + ro + 2*int(r-start))
			if g == 0 {
				return 0
			}
			return g + delta
		}
	}
	return 0
}

func cmap12(t table, r rune, manyToOne bool) uint16 {
	n := int(t.u32(12))
	lo, hi := 0, min(n, (len(t)-16)/12)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		g := 16 + 12*m
		start, end := rune(t.u32(g)), rune(t.u32(g+4))
		switch {
		case end < r:
			lo = m + 1
		case start > r:
			hi = m
		default:
			id := t.u32(g + 8)
			if !manyToOne {
				id += uint32(r - start)
			}
			if id > 0xffff {
				return 0
			}
			return uint16(id)
		}
	}
	return 0
}

// macRoman is Mac OS Roman's upper half in Unicode.
var macRoman = [128]uint16{
	0x00C4, 0x00C5, 0x00C7, 0x00C9, 0x00D1, 0x00D6, 0x00DC, 0x00E1, 0x00E0, 0x00E2, 0x00E4, 0x00E3, 0x00E5, 0x00E7, 0x00E9, 0x00E8,
	0x00EA, 0x00EB, 0x00ED, 0x00EC, 0x00EE, 0x00EF, 0x00F1, 0x00F3, 0x00F2, 0x00F4, 0x00F6, 0x00F5, 0x00FA, 0x00F9, 0x00FB, 0x00FC,
	0x2020, 0x00B0, 0x00A2, 0x00A3, 0x00A7, 0x2022, 0x00B6, 0x00DF, 0x00AE, 0x00A9, 0x2122, 0x00B4, 0x00A8, 0x2260, 0x00C6, 0x00D8,
	0x221E, 0x00B1, 0x2264, 0x2265, 0x00A5, 0x00B5, 0x2202, 0x2211, 0x220F, 0x03C0, 0x222B, 0x00AA, 0x00BA, 0x03A9, 0x00E6, 0x00F8,
	0x00BF, 0x00A1, 0x00AC, 0x221A, 0x0192, 0x2248, 0x2206, 0x00AB, 0x00BB, 0x2026, 0x00A0, 0x00C0, 0x00C3, 0x00D5, 0x0152, 0x0153,
	0x2013, 0x2014, 0x201C, 0x201D, 0x2018, 0x2019, 0x00F7, 0x25CA, 0x00FF, 0x0178, 0x2044, 0x20AC, 0x2039, 0x203A, 0xFB01, 0xFB02,
	0x2021, 0x00B7, 0x201A, 0x201E, 0x2030, 0x00C2, 0x00CA, 0x00C1, 0x00CB, 0x00C8, 0x00CD, 0x00CE, 0x00CF, 0x00CC, 0x00D3, 0x00D4,
	0xF8FF, 0x00D2, 0x00DA, 0x00DB, 0x00D9, 0x0131, 0x02C6, 0x02DC, 0x00AF, 0x02D8, 0x02D9, 0x02DA, 0x00B8, 0x02DD, 0x02DB, 0x02C7,
}

func toMacRoman(r rune) rune {
	for i, u := range macRoman {
		if rune(u) == r {
			return rune(0x80 + i)
		}
	}
	return 0
}
