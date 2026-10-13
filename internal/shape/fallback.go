package shape

import "github.com/nerdwave-nick/logis/internal/ot"

// HarfBuzz's fallback mark positioning, for fonts without GPOS: marks are
// stacked on their base by combining class, from glyph extents.

// Combining classes the fallback positions by.
const (
	ccAttachedBelowLeft  = 200
	ccAttachedBelow      = 202
	ccAttachedAbove      = 214
	ccAttachedAboveRight = 216
	ccBelowLeft          = 218
	ccBelow              = 220
	ccBelowRight         = 222
	ccAboveLeft          = 228
	ccAbove              = 230
	ccAboveRight         = 232
	ccDoubleBelow        = 233
	ccDoubleAbove        = 234
)

// recategorize maps Thai, Lao, Hebrew and Arabic fixed-position classes to
// the positional ones.
func recategorize(r rune, c uint8) uint8 {
	if c >= 200 {
		return c
	}
	if r&^0xFF == 0x0E00 {
		switch {
		case c == 0 && (r == 0x0E31 || r >= 0x0E34 && r <= 0x0E37 || r == 0x0E47 || r >= 0x0E4C && r <= 0x0E4E):
			c = ccAboveRight
		case c == 0 && (r == 0x0EB1 || r >= 0x0EB4 && r <= 0x0EB7 || r == 0x0EBB || r == 0x0ECC || r == 0x0ECD):
			c = ccAbove
		case c == 0 && r == 0x0EBC:
			c = ccBelow
		case c != 0 && r == 0x0E3A:
			c = ccBelowRight
		}
	}
	switch c {
	case 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 30, 33, 118, 129, 131:
		return ccBelow // Hebrew points, meteg, kasratan, kasra, Lao/Tibetan below
	case 13:
		return ccAttachedAbove // rafe
	case 10:
		return ccAboveRight // shin dot
	case 11, 14:
		return ccAboveLeft // sin dot, holam
	case 26, 27, 28, 29, 31, 32, 34, 35, 36, 122, 132:
		return ccAbove // varika, Arabic and Syriac above, Lao mai, Tibetan sign i
	case 3:
		return ccBelowRight // sara u
	case 107:
		return ccAboveRight // mai
	}
	return c
}

func recategorizeMarks(b *ot.Buffer) {
	for i := range b.Info {
		info := &b.Info[i]
		if info.GenCat() == ot.NonSpacingMark {
			info.SetCombiningClass(recategorize(rune(info.Glyph), info.CombiningClass()))
		}
	}
}

type extents struct{ xBearing, yBearing, width, height int32 }

func (f *Font) extents(g uint32) (extents, bool) {
	x, y, w, h, ok := f.face.Extents(uint16(g))
	return extents{x, y, w, h}, ok
}

func (f *Font) fallbackMarks(p *plan, b *ot.Buffer, adjust bool) {
	start := 0
	for i := 1; i < len(b.Info); i++ {
		if info := &b.Info[i]; !info.IsUnicodeMark() && !info.IsHidden() && !info.IsDefaultIgnorable() {
			f.positionCluster(p, b, start, i, adjust)
			start = i
		}
	}
	f.positionCluster(p, b, start, len(b.Info), adjust)
}

func (f *Font) positionCluster(p *plan, b *ot.Buffer, start, end int, adjust bool) {
	if end-start < 2 {
		return
	}
	for i := start; i < end; i++ {
		if b.Info[i].IsUnicodeMark() {
			continue
		}
		j := i + 1
		for j < end && (b.Info[j].IsHidden() || b.Info[j].IsDefaultIgnorable() || b.Info[j].IsUnicodeMark()) {
			j++
		}
		f.positionAroundBase(p, b, i, j, adjust)
		i = j - 1
	}
}

func (f *Font) positionAroundBase(p *plan, b *ot.Buffer, base, end int, adjust bool) {
	baseInfo, basePos := &b.Info[base], &b.Pos[base]
	be, ok := f.extents(baseInfo.Glyph)
	if !ok {
		for i := base + 1; i < end; i++ {
			if b.Info[i].GenCat() == ot.NonSpacingMark {
				zeroAdvance(&b.Pos[i], adjust)
			}
		}
		return
	}
	be.yBearing += basePos.YOffset
	be.xBearing = 0
	be.width = int32(f.face.Advance(uint16(baseInfo.Glyph)))
	ligID, comps := baseInfo.LigID(), int32(baseInfo.LigNumComps())
	var xOffset, yOffset int32
	if !b.RTL {
		xOffset -= basePos.XAdvance
		yOffset -= basePos.YAdvance
	}
	lastComp, lastCC := int32(-1), uint8(255)
	component, cluster := be, be
	for i := base + 1; i < end; i++ {
		info, pos := &b.Info[i], &b.Pos[i]
		cc := info.CombiningClass()
		if cc == 0 {
			if !b.RTL {
				xOffset -= pos.XAdvance
				yOffset -= pos.YAdvance
			} else {
				xOffset += pos.XAdvance
				yOffset += pos.YAdvance
			}
			continue
		}
		if comps > 1 {
			comp := int32(info.LigComp()) - 1
			if ligID == 0 || ligID != info.LigID() || comp >= comps {
				comp = comps - 1
			}
			if lastComp != comp {
				lastComp, lastCC = comp, 255
				component = be
				k := comp
				if p.rtl {
					k = comps - 1 - comp
				}
				component.xBearing += k * component.width / comps
				component.width /= comps
			}
		}
		if lastCC != cc {
			lastCC = cc
			cluster = component
		}
		f.positionMark(b.RTL, info.Glyph, pos, &cluster, cc)
		pos.XAdvance, pos.YAdvance = 0, 0
		pos.XOffset += xOffset
		pos.YOffset += yOffset
	}
}

func (f *Font) positionMark(rtl bool, g uint32, pos *ot.Pos, base *extents, cc uint8) {
	mark, ok := f.extents(g)
	if !ok {
		return
	}
	yGap := int32(f.face.UnitsPerEm()) / 16
	pos.XOffset, pos.YOffset = 0, 0
	switch cc {
	case ccDoubleBelow, ccDoubleAbove:
		pos.XOffset += base.xBearing - mark.width/2 - mark.xBearing
		if !rtl {
			pos.XOffset += base.width
		}
	case ccAttachedBelowLeft, ccBelowLeft, ccAboveLeft:
		pos.XOffset += base.xBearing - mark.xBearing
	case ccAttachedAboveRight, ccBelowRight, ccAboveRight:
		pos.XOffset += base.xBearing + base.width - mark.width - mark.xBearing
	default:
		pos.XOffset += base.xBearing + (base.width-mark.width)/2 - mark.xBearing
	}
	attached := cc == ccAttachedBelowLeft || cc == ccAttachedBelow || cc == ccAttachedAbove || cc == ccAttachedAboveRight
	switch cc {
	case ccDoubleBelow, ccBelowLeft, ccBelow, ccBelowRight, ccAttachedBelowLeft, ccAttachedBelow:
		if !attached {
			base.height -= yGap
		}
		pos.YOffset = base.yBearing + base.height - mark.yBearing
		if (yGap > 0) == (pos.YOffset > 0) {
			base.height -= pos.YOffset
			pos.YOffset = 0
		}
		base.height += mark.height
	case ccDoubleAbove, ccAboveLeft, ccAbove, ccAboveRight, ccAttachedAbove, ccAttachedAboveRight:
		if !attached {
			base.yBearing += yGap
			base.height -= yGap
		}
		pos.YOffset = base.yBearing - (mark.yBearing + mark.height)
		if (yGap > 0) != (pos.YOffset > 0) {
			correction := -pos.YOffset / 2
			base.yBearing += correction
			base.height -= correction
			pos.YOffset += correction
		}
		base.yBearing -= mark.height
		base.height += mark.height
	}
}
