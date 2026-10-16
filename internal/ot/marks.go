package ot

// Mark attachment (GPOS 4–6) and the final resolution of attachment
// chains into offsets.

// attachMark places the current mark's anchor on the anchor of the glyph
// at base.
func (c *applyContext) attachMark(baseAnchor, markAnchor table, base int) bool {
	bx, by := c.anchor(baseAnchor)
	mx, my := c.anchor(markAnchor)
	b := c.b
	p := &b.Pos[b.idx]
	p.XOffset, p.YOffset = bx-mx, by-my
	p.attachType = attachMark
	p.attachChain = int16(base - b.idx)
	b.ScratchFlags |= ScratchHasGPOSAttachment
	b.idx++
	return true
}

// findBase finds the glyph a mark attaches to: the closest preceding
// glyph that is not a mark (and, with accept, the first glyph of a
// multiple substitution), cached for the marks that follow.
func (c *applyContext) findBase(accept func(j int) bool) int {
	b := c.b
	if c.lastBaseUntil > b.idx {
		c.lastBaseUntil, c.lastBase = 0, -1
	}
	it := c.iter(false, nil)
	it.props = FlagIgnoreMarks
	for j := b.idx; j > c.lastBaseUntil; j-- {
		s := it.at(&b.Info[j-1])
		if s == stepMatch && accept != nil && !accept(j-1) {
			s = stepSkip
		}
		if s == stepMatch {
			c.lastBase = j - 1
			break
		}
	}
	c.lastBaseUntil = b.idx
	return c.lastBase
}

// acceptBase rejects all but the first glyph of a multiple substitution
// (stopping at marks inside it), as HarfBuzz does.
func (b *Buffer) acceptBase(i int) bool {
	info := &b.Info[i]
	if !info.Multiplied() || info.LigComp() == 0 || i == 0 {
		return true
	}
	prev := &b.Info[i-1]
	return prev.IsMark() || !prev.Multiplied() ||
		info.LigID() != prev.LigID() || info.LigComp() != prev.LigComp()+1
}

// markRecord is the class and anchor of a mark in a MarkArray.
func markRecord(array table, i int) (int, table) {
	if i < 0 || i >= int(array.u16(0)) {
		return -1, nil
	}
	return int(array.u16(2 + 4*i)), array.sub(2 + 4*i + 2)
}

func (c *applyContext) markBasePos(t table) bool {
	b := c.b
	mark := t.sub(2).coverage(uint16(b.cur().Glyph))
	if t.u16(0) != 1 || mark < 0 {
		return false
	}
	baseCov := t.sub(4)
	base := c.findBase(func(j int) bool {
		return b.acceptBase(j) || baseCov.coverage(uint16(b.Info[j].Glyph)) >= 0
	})
	if base < 0 {
		return false
	}
	bi := baseCov.coverage(uint16(b.Info[base].Glyph))
	if bi < 0 {
		return false
	}
	classes := int(t.u16(6))
	class, markAnchor := markRecord(t.sub(8), mark)
	baseArray := t.sub(10)
	if class < 0 || class >= classes || bi >= int(baseArray.u16(0)) {
		return false
	}
	baseAnchor := baseArray.sub(2 + 2*(bi*classes+class))
	if baseAnchor == nil || markAnchor == nil {
		return false
	}
	return c.attachMark(baseAnchor, markAnchor, base)
}

func (c *applyContext) markLigPos(t table) bool {
	b := c.b
	mark := t.sub(2).coverage(uint16(b.cur().Glyph))
	if t.u16(0) != 1 || mark < 0 {
		return false
	}
	base := c.findBase(nil)
	if base < 0 {
		return false
	}
	li := t.sub(4).coverage(uint16(b.Info[base].Glyph))
	ligArray := t.sub(10)
	if li < 0 || li >= int(ligArray.u16(0)) {
		return false
	}
	attach := ligArray.sub(2 + 2*li)
	comps := int(attach.u16(0))
	if comps == 0 {
		return false
	}
	// Attach to the component the mark followed, else the last one.
	ligID, markID, markComp := b.Info[base].LigID(), b.cur().LigID(), int(b.cur().LigComp())
	comp := comps
	if ligID != 0 && ligID == markID && markComp > 0 {
		comp = min(markComp, comps)
	}
	comp--
	classes := int(t.u16(6))
	class, markAnchor := markRecord(t.sub(8), mark)
	if class < 0 || class >= classes {
		return false
	}
	ligAnchor := attach.sub(2 + 2*(comp*classes+class))
	if ligAnchor == nil || markAnchor == nil {
		return false
	}
	return c.attachMark(ligAnchor, markAnchor, base)
}

func (c *applyContext) markMarkPos(t table) bool {
	b := c.b
	m1 := t.sub(2).coverage(uint16(b.cur().Glyph))
	if t.u16(0) != 1 || m1 < 0 {
		return false
	}
	it := c.iter(false, nil)
	it.idx = b.idx
	it.props = c.lookupProps &^ FlagIgnoreFlags
	if !it.prev() {
		return false
	}
	j := it.idx
	if !b.Info[j].IsMark() {
		return false
	}
	id1, id2 := b.cur().LigID(), b.Info[j].LigID()
	comp1, comp2 := b.cur().LigComp(), b.Info[j].LigComp()
	ok := id1 == 0 || comp1 == comp2
	if id1 != id2 {
		ok = id1 > 0 && comp1 == 0 || id2 > 0 && comp2 == 0
	}
	if !ok {
		return false
	}
	m2 := t.sub(4).coverage(uint16(b.Info[j].Glyph))
	if m2 < 0 {
		return false
	}
	classes := int(t.u16(6))
	class, markAnchor := markRecord(t.sub(8), m1)
	mark2 := t.sub(10)
	if class < 0 || class >= classes || m2 >= int(mark2.u16(0)) {
		return false
	}
	anchor := mark2.sub(2 + 2*(m2*classes+class))
	if anchor == nil || markAnchor == nil {
		return false
	}
	return c.attachMark(anchor, markAnchor, j)
}

// StartPositioning clears attachment chains before GPOS.
func (b *Buffer) StartPositioning() {
	for i := range b.Pos {
		b.Pos[i].attachChain, b.Pos[i].attachType = 0, 0
	}
}

// FinishOffsets turns attachment chains into offsets relative to each
// glyph's own pen position.
func (b *Buffer) FinishOffsets() {
	if b.ScratchFlags&ScratchHasGPOSAttachment == 0 {
		return
	}
	n := len(b.Pos)
	if !b.RTL {
		for i := range n {
			if b.Pos[i].attachChain != 0 {
				b.propagate(i, maxNesting)
			}
		}
		return
	}
	for i := n - 1; i >= 0; i-- {
		if b.Pos[i].attachChain != 0 {
			b.propagate(i, maxNesting)
		}
	}
}

func (b *Buffer) propagate(i, nesting int) {
	pos := b.Pos
	chain, kind := pos[i].attachChain, pos[i].attachType
	pos[i].attachChain = 0
	j := i + int(chain)
	if j < 0 || j >= len(pos) || nesting == 0 {
		return
	}
	if pos[j].attachChain != 0 {
		b.propagate(j, nesting-1)
	}
	switch kind {
	case attachMark:
		pos[i].XOffset += pos[j].XOffset
		pos[i].YOffset += pos[j].YOffset
		switch {
		case j < i && !b.RTL:
			for k := j; k < i; k++ {
				pos[i].XOffset -= pos[k].XAdvance
			}
		case j < i:
			for k := j + 1; k <= i; k++ {
				pos[i].XOffset += pos[k].XAdvance
			}
		case !b.RTL:
			for k := i; k < j; k++ {
				pos[i].XOffset += pos[k].XAdvance
			}
		default:
			for k := i + 1; k <= j; k++ {
				pos[i].XOffset -= pos[k].XAdvance
			}
		}
	case attachCursive:
		pos[i].YOffset += pos[j].YOffset
	}
}
