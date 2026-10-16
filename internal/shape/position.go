package shape

import "github.com/nerdwave-nick/logis/internal/ot"

// position gives every glyph its advance, applies GPOS (or HarfBuzz's
// fallbacks) and leaves the glyphs in visual order.
func (f *Font) position(p *plan, b *ot.Buffer, ppem int) {
	b.ClearPositions()
	for i := range b.Info {
		b.Pos[i].XAdvance = int32(f.face.Advance(uint16(b.Info[i].Glyph)))
	}
	if b.ScratchFlags&ot.ScratchHasSpaceFallback != 0 {
		f.fallbackSpaces(b)
	}
	adjust := p.adjustMarkPositioningWhenZeroing && !b.RTL
	b.StartPositioning()
	if p.applyGPOS {
		f.face.Position(b, p.m.GPOSStages(), ppem)
	}
	if p.zeroMarks {
		for i := range b.Info {
			if b.Info[i].IsMark() {
				zeroAdvance(&b.Pos[i], adjust)
			}
		}
	}
	if b.ScratchFlags&ot.ScratchHasDefaultIgnorables != 0 {
		for i := range b.Info {
			if b.Info[i].IsDefaultIgnorable() {
				b.Pos[i].XAdvance, b.Pos[i].YAdvance, b.Pos[i].XOffset = 0, 0, 0
			}
		}
	}
	b.FinishOffsets()
	if p.fallbackMarkPositioning {
		f.fallbackMarks(p, b, adjust)
	}
	if b.RTL {
		b.Reverse()
	}
}

func zeroAdvance(pos *ot.Pos, adjust bool) {
	if adjust {
		pos.XOffset -= pos.XAdvance
		pos.YOffset -= pos.YAdvance
	}
	pos.XAdvance, pos.YAdvance = 0, 0
}

// fallbackSpaces sizes space characters the font lacks, drawn with its
// space glyph.
func (f *Font) fallbackSpaces(b *ot.Buffer) {
	upem := int32(f.face.UnitsPerEm())
	for i := range b.Info {
		info, pos := &b.Info[i], &b.Pos[i]
		if info.GenCat() != ot.SpaceSeparator || info.Ligated() {
			continue
		}
		switch t := int32(info.UProps >> 8); t {
		case 1, 2, 3, 4, 5, 6, 16:
			pos.XAdvance = (upem + t/2) / t
		case space4Em18:
			pos.XAdvance = int32(int64(upem) * 4 / 18)
		case spaceFigure:
			for r := '0'; r <= '9'; r++ {
				if g, ok := f.face.Glyph(r); ok {
					pos.XAdvance = int32(f.face.Advance(g))
					break
				}
			}
		case spacePunctuation:
			g, ok := f.face.Glyph('.')
			if !ok {
				g, ok = f.face.Glyph(',')
			}
			if ok {
				pos.XAdvance = int32(f.face.Advance(g))
			}
		case spaceNarrow:
			pos.XAdvance /= 2
		}
	}
}

// hideDefaultIgnorables shows default ignorables as the space glyph (with
// no advance), or drops them if the font has none.
func (f *Font) hideDefaultIgnorables(b *ot.Buffer) {
	if b.ScratchFlags&ot.ScratchHasDefaultIgnorables == 0 {
		return
	}
	if space, ok := f.face.Glyph(' '); ok {
		for i := range b.Info {
			if b.Info[i].IsDefaultIgnorable() {
				b.Info[i].Glyph = uint32(space)
			}
		}
		return
	}
	b.DeleteGlyphsInPlace((*ot.Info).IsDefaultIgnorable)
}
