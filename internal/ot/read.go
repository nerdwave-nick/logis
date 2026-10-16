package ot

import "encoding/binary"

// table is a view of font bytes; every read past its end is zero, so a
// malformed font yields empty answers rather than a panic.
type table []byte

func (t table) u8(off int) uint8 {
	if off < 0 || off >= len(t) {
		return 0
	}
	return t[off]
}

func (t table) u16(off int) uint16 {
	if off < 0 || off+2 > len(t) {
		return 0
	}
	return binary.BigEndian.Uint16(t[off:])
}

func (t table) i16(off int) int16 { return int16(t.u16(off)) }

func (t table) u32(off int) uint32 {
	if off < 0 || off+4 > len(t) {
		return 0
	}
	return binary.BigEndian.Uint32(t[off:])
}

// at is the table starting at byte off; nil for null or out of range.
func (t table) at(off int) table {
	if off <= 0 || off >= len(t) {
		return nil
	}
	return t[off:]
}

// sub is the table at the 16-bit offset stored at off.
func (t table) sub(off int) table { return t.at(int(t.u16(off))) }

// sub32 is the table at the 32-bit offset stored at off.
func (t table) sub32(off int) table { return t.at(int(t.u32(off))) }

// coverage is g's index in a Coverage table, -1 when not covered.
func (t table) coverage(g uint16) int {
	switch t.u16(0) {
	case 1:
		lo, hi := 0, min(int(t.u16(2)), (len(t)-4)/2)
		for lo < hi {
			m := int(uint(lo+hi) >> 1)
			switch v := t.u16(4 + 2*m); {
			case v < g:
				lo = m + 1
			case v > g:
				hi = m
			default:
				return m
			}
		}
	case 2:
		lo, hi := 0, min(int(t.u16(2)), (len(t)-4)/6)
		for lo < hi {
			m := int(uint(lo+hi) >> 1)
			r := 4 + 6*m
			switch start, end := t.u16(r), t.u16(r+2); {
			case end < g:
				lo = m + 1
			case start > g:
				hi = m
			default:
				return int(t.u16(r+4)) + int(g-start)
			}
		}
	}
	return -1
}

// classOf is g's class in a ClassDef table, 0 for none.
func (t table) classOf(g uint16) int {
	switch t.u16(0) {
	case 1:
		start, n := t.u16(2), int(t.u16(4))
		if g >= start && int(g-start) < n {
			return int(t.u16(6 + 2*int(g-start)))
		}
	case 2:
		lo, hi := 0, min(int(t.u16(2)), (len(t)-4)/6)
		for lo < hi {
			m := int(uint(lo+hi) >> 1)
			r := 4 + 6*m
			switch start, end := t.u16(r), t.u16(r+2); {
			case end < g:
				lo = m + 1
			case start > g:
				hi = m
			default:
				return int(t.u16(r + 4))
			}
		}
	}
	return 0
}

// deviceDelta is a Device table's adjustment in pixels at ppem. Variation
// indices (format 0x8000) apply only to variable fonts and give 0.
func (t table) deviceDelta(ppem int) int {
	start, end, format := int(t.u16(0)), int(t.u16(2)), int(t.u16(4))
	if ppem == 0 || ppem < start || ppem > end || format < 1 || format > 3 {
		return 0
	}
	bits := 1 << format // 2, 4 or 8 bits per value
	i := ppem - start
	perWord := 16 / bits
	word := t.u16(6 + 2*(i/perWord))
	shift := 16 - bits*(i%perWord+1)
	v := int(word>>shift) & (1<<bits - 1)
	if v >= 1<<(bits-1) {
		v -= 1 << bits
	}
	return v
}
