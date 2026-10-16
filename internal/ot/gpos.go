package ot

// GPOS subtables.

const (
	attachMark    = 1
	attachCursive = 2
)

func (c *applyContext) applyPos(kind int, t table) bool {
	switch kind {
	case 1:
		return c.singlePos(t)
	case 2:
		return c.pairPos(t)
	case 3:
		return c.cursivePos(t)
	case 4:
		return c.markBasePos(t)
	case 5:
		return c.markLigPos(t)
	case 6:
		return c.markMarkPos(t)
	case 7:
		return c.context(t)
	case 8:
		return c.chainContext(t)
	}
	return false
}

// valueSize is a value record's size in bytes.
func valueSize(format uint16) int {
	n := 0
	for f := format & 0xFF; f != 0; f &= f - 1 {
		n++
	}
	return 2 * n
}

// device is a device table's delta in font units at the context's ppem
// (pixels × upem / ppem, as HarfBuzz scales it), 0 without a ppem.
func (c *applyContext) device(t table) int32 {
	if c.ppem == 0 || t == nil {
		return 0
	}
	return int32(int64(t.deviceDelta(c.ppem)) * int64(c.face.upem) / int64(c.ppem))
}

// applyValue adds the value record at base[off:] to glyph i; it reports
// whether anything changed.
func (c *applyContext) applyValue(i int, base table, off int, format uint16) bool {
	p := &c.b.Pos[i]
	worked := false
	read := func() int32 {
		v := int32(base.i16(off))
		off += 2
		worked = worked || v != 0
		return v
	}
	if format&0x01 != 0 {
		p.XOffset += read()
	}
	if format&0x02 != 0 {
		p.YOffset += read()
	}
	if format&0x04 != 0 {
		p.XAdvance += read()
	}
	if format&0x08 != 0 {
		off += 2 // vertical advance
	}
	dev := func() int32 {
		d := c.device(base.sub(off))
		off += 2
		worked = worked || d != 0
		return d
	}
	if format&0x10 != 0 {
		p.XOffset += dev()
	}
	if format&0x20 != 0 {
		p.YOffset += dev()
	}
	if format&0x40 != 0 {
		p.XAdvance += dev()
	}
	return worked
}

// anchor is an anchor's position in font units.
func (c *applyContext) anchor(t table) (int32, int32) {
	x, y := int32(t.i16(2)), int32(t.i16(4))
	if t.u16(0) == 3 {
		x += c.device(t.sub(6))
		y += c.device(t.sub(8))
	}
	return x, y
}

func (c *applyContext) singlePos(t table) bool {
	b := c.b
	i := t.sub(2).coverage(uint16(b.cur().Glyph))
	if i < 0 {
		return false
	}
	format := t.u16(4)
	switch t.u16(0) {
	case 1:
		c.applyValue(b.idx, t, 6, format)
	case 2:
		if i >= int(t.u16(6)) {
			return false
		}
		c.applyValue(b.idx, t, 8+i*valueSize(format), format)
	default:
		return false
	}
	b.idx++
	return true
}

func (c *applyContext) pairPos(t table) bool {
	b := c.b
	i := t.sub(2).coverage(uint16(b.cur().Glyph))
	if i < 0 {
		return false
	}
	it := c.iter(false, nil)
	it.reset(b.idx)
	if !it.next() {
		return false
	}
	second := it.idx
	g2 := uint16(b.Info[second].Glyph)
	f1, f2 := t.u16(4), t.u16(6)
	s1, s2 := valueSize(f1), valueSize(f2)
	switch t.u16(0) {
	case 1:
		if i >= int(t.u16(8)) {
			return false
		}
		set := t.sub(10 + 2*i)
		size := 2 + s1 + s2
		lo, hi := 0, min(int(set.u16(0)), max(0, len(set)-2)/max(size, 1))
		for lo < hi {
			m := int(uint(lo+hi) >> 1)
			rec := 2 + m*size
			switch v := set.u16(rec); {
			case v < g2:
				lo = m + 1
			case v > g2:
				hi = m
			default:
				c.pairApplied(set, rec+2, rec+2+s1, f1, f2, second)
				return true
			}
		}
		return false
	case 2:
		k1 := t.sub(8).classOf(uint16(b.cur().Glyph))
		k2 := t.sub(10).classOf(g2)
		n1, n2 := int(t.u16(12)), int(t.u16(14))
		if k1 >= n1 || k2 >= n2 {
			return false
		}
		rec := 16 + (k1*n2+k2)*(s1+s2)
		c.pairApplied(t, rec, rec+s1, f1, f2, second)
		return true
	}
	return false
}

// pairApplied applies a pair's two value records and moves past the
// pair: onto the second glyph, or beyond it if it was adjusted too.
func (c *applyContext) pairApplied(base table, off1, off2 int, f1, f2 uint16, second int) {
	if f1 != 0 {
		c.applyValue(c.b.idx, base, off1, f1)
	}
	if f2 != 0 {
		c.applyValue(second, base, off2, f2)
		second++
	}
	c.b.idx = second
}

func (c *applyContext) cursivePos(t table) bool {
	b := c.b
	cov := t.sub(2)
	this := cov.coverage(uint16(b.cur().Glyph))
	if t.u16(0) != 1 || this < 0 || this >= int(t.u16(4)) {
		return false
	}
	entry := t.sub(6 + 4*this)
	if entry == nil {
		return false
	}
	it := c.iter(false, nil)
	it.idx = b.idx
	if !it.prev() {
		return false
	}
	i := it.idx
	prev := cov.coverage(uint16(b.Info[i].Glyph))
	if prev < 0 || prev >= int(t.u16(4)) {
		return false
	}
	exit := t.sub(6 + 4*prev + 2)
	if exit == nil {
		return false
	}
	exitX, _ := c.anchor(exit)
	entryX, entryY := c.anchor(entry)
	_, exitY := c.anchor(exit)
	j := b.idx
	pos := b.Pos
	if !b.RTL {
		pos[i].XAdvance = exitX + pos[i].XOffset
		d := entryX + pos[j].XOffset
		pos[j].XAdvance -= d
		pos[j].XOffset -= d
	} else {
		d := exitX + pos[i].XOffset
		pos[i].XAdvance -= d
		pos[i].XOffset -= d
		pos[j].XAdvance = entryX + pos[j].XOffset
	}
	child, parent := i, j
	yOffset := entryY - exitY
	if c.lookupProps&FlagRightToLeft == 0 {
		child, parent = parent, child
		yOffset = -yOffset
	}
	reverseCursiveMinorOffset(pos, child, parent)
	pos[child].attachType = attachCursive
	pos[child].attachChain = int16(parent - child)
	b.ScratchFlags |= ScratchHasGPOSAttachment
	pos[child].YOffset = yOffset
	if pos[parent].attachChain == -pos[child].attachChain {
		pos[parent].attachChain = 0
		pos[parent].YOffset = 0
	}
	b.idx++
	return true
}

// reverseCursiveMinorOffset turns a cursive chain around when a glyph gets
// a new parent.
func reverseCursiveMinorOffset(pos []Pos, i, newParent int) {
	chain, kind := pos[i].attachChain, pos[i].attachType
	if chain == 0 || kind&attachCursive == 0 {
		return
	}
	pos[i].attachChain = 0
	j := i + int(chain)
	if j == newParent || j < 0 || j >= len(pos) {
		return
	}
	reverseCursiveMinorOffset(pos, j, newParent)
	pos[j].YOffset = -pos[i].YOffset
	pos[j].attachChain = -chain
	pos[j].attachType = kind
}
