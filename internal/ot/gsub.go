package ot

// GSUB subtables.

func (c *applyContext) applySubst(kind int, t table) bool {
	switch kind {
	case 1:
		return c.singleSubst(t)
	case 2:
		return c.multipleSubst(t)
	case 3:
		return c.alternateSubst(t)
	case 4:
		return c.ligatureSubst(t)
	case 5:
		return c.context(t)
	case 6:
		return c.chainContext(t)
	case 8:
		return c.reverseChainSubst(t)
	}
	return false
}

// setGlyphClass updates the current glyph's properties for a substitute
// g: GDEF's class if the font has one, else classGuess.
func (c *applyContext) setGlyphClass(g uint16, classGuess uint16, ligature, component bool) {
	cur := c.b.cur()
	props := cur.Props | PropSubstituted
	if ligature {
		props |= PropLigated
		props &^= PropMultiplied
	}
	if component {
		props |= PropMultiplied
	}
	switch {
	case c.face.HasGlyphClasses():
		props = props&PropPreserve | c.face.GlyphProps(g)
	case classGuess != 0:
		props = props&PropPreserve | classGuess
	}
	cur.Props = props
}

func (c *applyContext) replaceGlyph(g uint16) {
	c.setGlyphClass(g, 0, false, false)
	c.b.replaceGlyph(uint32(g))
}

func (c *applyContext) singleSubst(t table) bool {
	g := uint16(c.b.cur().Glyph)
	i := t.sub(2).coverage(g)
	if i < 0 {
		return false
	}
	switch t.u16(0) {
	case 1:
		c.replaceGlyph(uint16(int(g) + int(t.i16(4))))
		return true
	case 2:
		if i >= int(t.u16(4)) {
			return false
		}
		c.replaceGlyph(t.u16(6 + 2*i))
		return true
	}
	return false
}

func (c *applyContext) multipleSubst(t table) bool {
	b := c.b
	i := t.sub(2).coverage(uint16(b.cur().Glyph))
	if t.u16(0) != 1 || i < 0 || i >= int(t.u16(4)) {
		return false
	}
	seq := t.sub(6 + 2*i)
	n := int(seq.u16(0))
	switch n {
	case 0:
		b.deleteGlyph()
	case 1:
		c.replaceGlyph(seq.u16(2))
	default:
		var class uint16
		if b.cur().IsLigature() {
			class = PropBaseGlyph
		}
		ligID := b.cur().LigID()
		for k := range n {
			if ligID == 0 {
				b.cur().SetLigComponent(uint8(k))
			}
			g := seq.u16(2 + 2*k)
			c.setGlyphClass(g, class, false, true)
			b.outputGlyph(uint32(g))
		}
		b.skipGlyph()
	}
	return true
}

func (c *applyContext) alternateSubst(t table) bool {
	b := c.b
	i := t.sub(2).coverage(uint16(b.cur().Glyph))
	if t.u16(0) != 1 || i < 0 || i >= int(t.u16(4)) {
		return false
	}
	set := t.sub(6 + 2*i)
	n := uint32(set.u16(0))
	if n == 0 {
		return false
	}
	shift := uint32(0)
	for c.lookupMask>>shift&1 == 0 && shift < 32 {
		shift++
	}
	alt := (c.lookupMask & b.cur().Mask) >> shift
	if alt == maxFeatureValue && c.random {
		alt = c.randomNumber()%n + 1
	}
	if alt == 0 || alt > n {
		return false
	}
	c.replaceGlyph(set.u16(2 + 2*int(alt-1)))
	return true
}

func (c *applyContext) ligatureSubst(t table) bool {
	b := c.b
	i := t.sub(2).coverage(uint16(b.cur().Glyph))
	if t.u16(0) != 1 || i < 0 || i >= int(t.u16(4)) {
		return false
	}
	set := t.sub(6 + 2*i)
	for k := range int(set.u16(0)) {
		lig := set.sub(2 + 2*k)
		comps := int(lig.u16(2))
		if comps == 0 {
			continue
		}
		if comps == 1 {
			c.replaceGlyph(lig.u16(0))
			return true
		}
		end, total, ok := c.matchInput(comps, func(info *Info, j int) bool {
			return uint16(info.Glyph) == lig.u16(4+2*j)
		})
		if ok {
			c.ligate(comps, end, total, lig.u16(0))
			return true
		}
	}
	return false
}

// ligate replaces the matched glyphs with g, keeping the marks between
// them and renumbering the ligature components they attach to.
func (c *applyContext) ligate(count, matchEnd int, totalComps uint8, g uint16) {
	b := c.b
	b.MergeClusters(b.idx, matchEnd)
	pos := c.matchPositions
	isBase, isMark := b.Info[pos[0]].IsBaseGlyph(), b.Info[pos[0]].IsMark()
	for i := 1; i < count; i++ {
		if !b.Info[pos[i]].IsMark() {
			isBase, isMark = false, false
		}
	}
	isLigature := !isBase && !isMark
	var class uint16
	var ligID uint8
	if isLigature {
		class = PropLigature
		ligID = b.allocateLigID()
	}
	first := b.cur()
	lastID, lastComps := first.LigID(), first.LigNumComps()
	compsSoFar := lastComps
	if isLigature {
		first.setLigPropsForLigature(ligID, totalComps)
		if first.GenCat() == NonSpacingMark {
			first.SetGenCat(OtherLetter)
		}
	}
	c.setGlyphClass(g, class, true, false)
	b.replaceGlyph(uint32(g))
	for i := 1; i < count; i++ {
		for b.idx < pos[i] && b.successful {
			if isLigature {
				cur := b.cur()
				comp := cur.LigComp()
				if comp == 0 {
					comp = lastComps
				}
				cur.setLigPropsForMark(ligID, compsSoFar-lastComps+min(comp, lastComps))
			}
			b.nextGlyph()
		}
		lastID, lastComps = b.cur().LigID(), b.cur().LigNumComps()
		compsSoFar += lastComps
		b.skipGlyph() // the component itself
	}
	if !isMark && lastID != 0 {
		// Marks after the last component that belonged to it.
		for i := b.idx; i < len(b.Info); i++ {
			info := &b.Info[i]
			if info.LigID() != lastID {
				break
			}
			comp := info.LigComp()
			if comp == 0 {
				break
			}
			info.setLigPropsForMark(ligID, compsSoFar-lastComps+min(comp, lastComps))
		}
	}
}

func (c *applyContext) reverseChainSubst(t table) bool {
	b := c.b
	if c.nestingLeft != maxNesting || t.u16(0) != 1 {
		return false
	}
	i := t.sub(2).coverage(uint16(b.cur().Glyph))
	nBack := int(t.u16(4))
	ahead := 6 + 2*nBack
	nAhead := int(t.u16(ahead))
	subst := ahead + 2 + 2*nAhead
	if i < 0 || i >= int(t.u16(subst)) {
		return false
	}
	g := t.u16(subst + 2 + 2*i)
	_, ok := c.matchBacktrack(nBack, func(info *Info, j int) bool {
		return t.sub(6+2*j).coverage(uint16(info.Glyph)) >= 0
	})
	if !ok {
		return false
	}
	if _, ok := c.matchLookahead(nAhead, func(info *Info, j int) bool {
		return t.sub(ahead+2+2*j).coverage(uint16(info.Glyph)) >= 0
	}, b.idx+1); !ok {
		return false
	}
	c.setGlyphClass(g, 0, false, false)
	b.cur().Glyph = uint32(g)
	return true
}
