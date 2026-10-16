package shape

import (
	"github.com/nerdwave-nick/logis/internal/ot"
	"github.com/nerdwave-nick/logis/internal/ucd"
)

// HarfBuzz's normaliser in its "composed diacritics" mode: characters the
// font maps stay as they are; others decompose (to what the font has);
// marks are sorted by modified combining class; then marks compose with
// their base when the font has the composite.

const maxCombiningMarks = 32

type normalizer struct {
	p    *plan
	face *ot.Face
	b    *ot.Buffer
}

func (n *normalizer) glyph(r rune) (uint32, bool) {
	g, ok := n.face.Glyph(r)
	return uint32(g), ok
}

func (n *normalizer) compose(a, b rune) (rune, bool) {
	if n.p.kind == shaperHebrew {
		return n.p.composeHebrew(a, b)
	}
	return ucd.Compose(a, b)
}

// output emits character r (glyph g) as a copy of the current one.
func (n *normalizer) output(r rune, g uint32) {
	b := n.b
	b.Cur(0).Norm = g
	b.OutputGlyph(uint32(r))
	initProps(b.Out(b.OutLen()-1), b)
}

func (n *normalizer) next(g uint32) {
	n.b.Cur(0).Norm = g
	n.b.NextGlyph()
}

// decompose outputs ab's decomposition, to its shortest form the font
// covers if shortest, else fully; it returns how many characters it
// output, 0 if the font lacks them.
func (n *normalizer) decompose(shortest bool, ab rune) int {
	a, b, ok := ucd.Decompose(ab)
	if !ok {
		return 0
	}
	ag, aOK := n.glyph(a)
	var bg uint32
	if b != 0 {
		var bOK bool
		if bg, bOK = n.glyph(b); !bOK {
			return 0
		}
	}
	emit := func(count int) int {
		if b != 0 {
			n.output(b, bg)
			return count + 1
		}
		return count
	}
	if aOK && shortest {
		n.output(a, ag)
		return emit(1)
	}
	if ret := n.decompose(shortest, a); ret != 0 {
		return emit(ret)
	}
	if aOK {
		n.output(a, ag)
		return emit(1)
	}
	return 0
}

func (n *normalizer) decomposeCurrent(shortest bool) {
	b := n.b
	cur := b.Cur(0)
	u := rune(cur.Glyph)
	g, ok := n.glyph(u)
	if ok && shortest {
		n.next(g)
		return
	}
	if n.decompose(shortest, u) > 0 {
		b.SkipGlyph()
		return
	}
	if ok {
		n.next(g)
		return
	}
	if cur.GenCat() == ot.SpaceSeparator {
		if st := spaceType(u); st != 0 {
			if sg, ok := n.glyph(' '); ok {
				cur.UProps = uint16(st)<<8 | cur.UProps&0xFF
				n.next(sg)
				b.ScratchFlags |= ot.ScratchHasSpaceFallback
				return
			}
		}
	}
	if u == 0x2011 {
		if hg, ok := n.glyph(0x2010); ok {
			n.next(hg)
			return
		}
	}
	n.next(0)
}

func (n *normalizer) setGlyph(info *ot.Info) {
	if g, ok := n.glyph(rune(info.Glyph)); ok {
		info.Norm = g
	}
}

// variationSelectors handles a cluster with variation selectors: the
// font's variant glyph replaces base and selector, else both are kept and
// the selector is hidden later.
func (n *normalizer) variationSelectors(end int) {
	b := n.b
	for b.Idx() < end-1 && b.Successful() {
		if !isVariationSelector(rune(b.Cur(1).Glyph)) {
			n.setGlyph(b.Cur(0))
			b.NextGlyph()
			continue
		}
		if g, ok := n.face.VariantGlyph(rune(b.Cur(0).Glyph), rune(b.Cur(1).Glyph)); ok {
			b.Cur(0).Norm = uint32(g)
			b.ReplaceGlyphs(2, b.Cur(0).Glyph)
		} else {
			n.setGlyph(b.Cur(0))
			b.NextGlyph()
			b.ScratchFlags |= ot.ScratchHasVSFallback
			b.Cur(0).SetVariationSelector(true)
			n.setGlyph(b.Cur(0))
			b.NextGlyph()
		}
		for b.Idx() < end && isVariationSelector(rune(b.Cur(0).Glyph)) {
			n.setGlyph(b.Cur(0))
			b.NextGlyph()
		}
	}
	if b.Idx() < end {
		n.setGlyph(b.Cur(0))
		b.NextGlyph()
	}
}

func (n *normalizer) multiCharCluster(end int) {
	b := n.b
	for i := b.Idx(); i < end; i++ {
		if isVariationSelector(rune(b.Info[i].Glyph)) {
			n.variationSelectors(end)
			return
		}
	}
	for b.Idx() < end && b.Successful() {
		n.decomposeCurrent(false)
	}
}

func normalize(p *plan, face *ot.Face, b *ot.Buffer) {
	if len(b.Info) == 0 {
		return
	}
	n := &normalizer{p: p, face: face, b: b}
	allSimple := true
	b.ClearOutput()
	count := len(b.Info)
	for {
		end := b.Idx() + 1
		for end < count && !b.Info[end].IsUnicodeMark() {
			end++
		}
		if end < count {
			end-- // leave the base of the marks that follow
		}
		done := 0
		for done < end-b.Idx() {
			cur := b.Cur(done)
			g, ok := n.glyph(rune(cur.Glyph))
			if !ok {
				break
			}
			cur.Norm = g
			done++
		}
		b.NextGlyphs(done)
		for b.Idx() < end && b.Successful() {
			n.decomposeCurrent(true)
		}
		if b.Idx() == count || !b.Successful() {
			break
		}
		allSimple = false
		end = b.Idx() + 1
		for end < count && b.Info[end].IsUnicodeMark() {
			end++
		}
		n.multiCharCluster(end)
		if b.Idx() >= count || !b.Successful() {
			break
		}
	}
	b.Sync()
	if !allSimple {
		reorderMarks(p, b)
	}
	unhideCGJ(b)
	if !allSimple {
		n.recompose()
	}
}

func reorderMarks(p *plan, b *ot.Buffer) {
	infos := b.Info
	for i := 0; i < len(infos); {
		if infos[i].CombiningClass() == 0 {
			i++
			continue
		}
		end := i + 1
		for end < len(infos) && infos[end].CombiningClass() != 0 {
			end++
		}
		if end-i <= maxCombiningMarks {
			b.Sort(i, end, func(a, b *ot.Info) bool { return a.CombiningClass() > b.CombiningClass() })
			switch p.kind {
			case shaperHebrew:
				reorderHebrewMarks(b, i, end)
			case shaperArabic:
				reorderArabicMarks(b, i, end)
			}
		}
		i = end + 1
	}
}

// unhideCGJ makes a combining grapheme joiner visible to GSUB where it
// does not separate marks that would otherwise reorder.
func unhideCGJ(b *ot.Buffer) {
	if b.ScratchFlags&ot.ScratchHasCGJ == 0 {
		return
	}
	infos := b.Info
	for i := 1; i+1 < len(infos); i++ {
		if infos[i].Glyph == 0x034F {
			last, next := infos[i-1].CombiningClass(), infos[i+1].CombiningClass()
			if next == 0 || last <= next {
				infos[i].Unhide()
			}
		}
	}
}

// recompose composes each mark with its starter where the font has the
// composite and no mark of the same or higher class lies between.
func (n *normalizer) recompose() {
	b := n.b
	if !b.Successful() {
		return
	}
	count := len(b.Info)
	starter := 0
	b.ClearOutput()
	b.NextGlyph()
	for b.Idx() < count && b.Successful() {
		cur := b.Cur(0)
		if cur.IsUnicodeMark() && (starter == b.OutLen()-1 || b.Out(b.OutLen()-1).CombiningClass() < cur.CombiningClass()) {
			if c, ok := n.compose(rune(b.Out(starter).Glyph), rune(cur.Glyph)); ok {
				if g, ok := n.glyph(c); ok {
					b.NextGlyph()
					if !b.Successful() {
						return
					}
					b.MergeOutClusters(starter, b.OutLen())
					b.SetOutLen(b.OutLen() - 1)
					info := b.Out(starter)
					info.Glyph, info.Norm = uint32(c), g
					initProps(info, b)
					continue
				}
			}
		}
		b.NextGlyph()
		if b.Out(b.OutLen()-1).CombiningClass() == 0 {
			starter = b.OutLen() - 1
		}
	}
	b.Sync()
}
