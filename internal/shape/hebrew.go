package shape

import (
	"github.com/nerdwave-nick/logis/internal/ot"
	"github.com/nerdwave-nick/logis/internal/ucd"
)

// dageshForms are the presentation forms of U+05D0–05EA with dagesh.
var dageshForms = [...]rune{
	0xFB30, 0xFB31, 0xFB32, 0xFB33, 0xFB34, 0xFB35, 0xFB36, 0, 0xFB38, 0xFB39,
	0xFB3A, 0xFB3B, 0xFB3C, 0, 0xFB3E, 0, 0xFB40, 0xFB41, 0, 0xFB43,
	0xFB44, 0, 0xFB46, 0xFB47, 0xFB48, 0xFB49, 0xFB4A,
}

// composeHebrew composes a and b canonically, else — for fonts that
// cannot position marks — into Hebrew presentation forms.
func (p *plan) composeHebrew(a, b rune) (rune, bool) {
	if c, ok := ucd.Compose(a, b); ok || p.hasGPOSMark {
		return c, ok
	}
	var c rune
	switch b {
	case 0x05B4:
		if a == 0x05D9 {
			c = 0xFB1D
		}
	case 0x05B7:
		switch a {
		case 0x05D9:
			c = 0xFB1F
		case 0x05D0:
			c = 0xFB2E
		}
	case 0x05B8:
		if a == 0x05D0 {
			c = 0xFB2F
		}
	case 0x05B9:
		if a == 0x05D5 {
			c = 0xFB4B
		}
	case 0x05BC:
		switch {
		case a >= 0x05D0 && a <= 0x05EA:
			c = dageshForms[a-0x05D0]
		case a == 0xFB2A:
			c = 0xFB2C
		case a == 0xFB2B:
			c = 0xFB2D
		}
	case 0x05BF:
		switch a {
		case 0x05D1:
			c = 0xFB4C
		case 0x05DB:
			c = 0xFB4D
		case 0x05E4:
			c = 0xFB4E
		}
	case 0x05C1:
		switch a {
		case 0x05E9:
			c = 0xFB2A
		case 0xFB49:
			c = 0xFB2C
		}
	case 0x05C2:
		switch a {
		case 0x05E9:
			c = 0xFB2B
		case 0xFB49:
			c = 0xFB2D
		}
	}
	return c, c != 0
}

// reorderHebrewMarks puts meteg (or a below mark) after patah or qamats
// that a sheva or hiriq follows, as HarfBuzz does.
func reorderHebrewMarks(b *ot.Buffer, start, end int) {
	infos := b.Info
	for i := start + 2; i < end; i++ {
		c0, c1, c2 := infos[i-2].CombiningClass(), infos[i-1].CombiningClass(), infos[i].CombiningClass()
		if (c0 == 20 || c0 == 21) && (c1 == 22 || c1 == 23) && (c2 == 25 || c2 == 220) {
			b.MergeClusters(i-1, i+1)
			infos[i-1], infos[i] = infos[i], infos[i-1]
			break
		}
	}
}
