package ot

// GeneralCategory is a Unicode general category in HarfBuzz's numbering.
type GeneralCategory uint8

// General categories.
const (
	Control GeneralCategory = iota
	Format
	Unassigned
	PrivateUse
	Surrogate
	LowercaseLetter
	ModifierLetter
	OtherLetter
	TitlecaseLetter
	UppercaseLetter
	SpacingMark
	EnclosingMark
	NonSpacingMark
	DecimalNumber
	LetterNumber
	OtherNumber
	ConnectPunctuation
	DashPunctuation
	ClosePunctuation
	FinalPunctuation
	InitialPunctuation
	OtherPunctuation
	OpenPunctuation
	CurrencySymbol
	ModifierSymbol
	MathSymbol
	OtherSymbol
	LineSeparator
	ParagraphSeparator
	SpaceSeparator
)

// IsMark reports Mn, Mc and Me.
func (g GeneralCategory) IsMark() bool {
	return g == SpacingMark || g == EnclosingMark || g == NonSpacingMark
}

// IsLetter reports the L* categories.
func (g GeneralCategory) IsLetter() bool {
	return g >= LowercaseLetter && g <= UppercaseLetter
}

// Unicode property bits of Info.UProps (HarfBuzz's unicode_props).
const (
	UPropsGenCat       = 0x001F
	UPropsIgnorable    = 0x0020
	UPropsHidden       = 0x0040
	UPropsContinuation = 0x0080
	// With a Format category, the top byte holds:
	UPropsZWJ  = 0x0100
	UPropsZWNJ = 0x0200
	UPropsVS   = 0x0400
)

// Info is one glyph of a Buffer: a character until the cmap maps it, then
// a glyph id, with its feature mask, cluster and shaping properties.
type Info struct {
	Glyph   uint32
	Mask    uint32
	Cluster uint32
	// UProps holds the character's general category and flags; the top
	// byte is its modified combining class for marks.
	UProps uint16
	// Props are the glyph's GDEF class and substitution history.
	Props    uint16
	ligProps uint8
	// Syllable is a shaper's grouping; ShaperVar a shaper's own byte
	// (Arabic: the joining action). Norm is the normaliser's glyph.
	Syllable  uint8
	ShaperVar uint8
	Norm      uint32
}

// Pos is a glyph's advance and offset in font units.
type Pos struct {
	XAdvance, YAdvance, XOffset, YOffset int32
	attachChain                          int16
	attachType                           uint8
}

// GenCat is the character's general category.
func (i *Info) GenCat() GeneralCategory { return GeneralCategory(i.UProps & UPropsGenCat) }

// SetGenCat changes the category, clearing the top byte.
func (i *Info) SetGenCat(g GeneralCategory) {
	i.UProps = uint16(g) | i.UProps&(0xFF&^UPropsGenCat)
}

// IsUnicodeMark reports a mark character.
func (i *Info) IsUnicodeMark() bool { return i.GenCat().IsMark() }

// CombiningClass is a mark's modified combining class, 0 otherwise.
func (i *Info) CombiningClass() uint8 {
	if !i.IsUnicodeMark() {
		return 0
	}
	return uint8(i.UProps >> 8)
}

// SetCombiningClass changes a mark's modified combining class.
func (i *Info) SetCombiningClass(c uint8) {
	if i.IsUnicodeMark() {
		i.UProps = uint16(c)<<8 | i.UProps&0xFF
	}
}

// IsDefaultIgnorable reports an ignorable character GSUB did not replace.
func (i *Info) IsDefaultIgnorable() bool {
	return i.UProps&UPropsIgnorable != 0 && !i.Substituted()
}

// ClearDefaultIgnorable makes the glyph count as an ordinary one.
func (i *Info) ClearDefaultIgnorable() { i.UProps &^= UPropsIgnorable }

// IsHidden reports characters hidden yet not skipped in GSUB (CGJ, Mongolian
// free variation selectors, tags).
func (i *Info) IsHidden() bool { return i.UProps&UPropsHidden != 0 }

// Unhide clears IsHidden.
func (i *Info) Unhide() { i.UProps &^= UPropsHidden }

// IsContinuation reports a character continuing its grapheme.
func (i *Info) IsContinuation() bool { return i.UProps&UPropsContinuation != 0 }

// SetContinuation marks a grapheme continuation.
func (i *Info) SetContinuation() { i.UProps |= UPropsContinuation }

// IsZWJ and IsZWNJ report the joiners.
func (i *Info) IsZWJ() bool { return i.GenCat() == Format && i.UProps&UPropsZWJ != 0 }

// IsZWNJ: see IsZWJ.
func (i *Info) IsZWNJ() bool { return i.GenCat() == Format && i.UProps&UPropsZWNJ != 0 }

// IsVariationSelector reports a selector the normaliser could not apply.
func (i *Info) IsVariationSelector() bool {
	return i.GenCat() == Format && i.UProps&UPropsVS != 0
}

// SetVariationSelector marks (or unmarks) such a selector.
func (i *Info) SetVariationSelector(on bool) {
	if on {
		i.SetGenCat(Format)
		i.UProps |= UPropsVS
	} else {
		i.SetGenCat(NonSpacingMark)
	}
}

// IsBaseGlyph, IsLigature and IsMark report the GDEF class.
func (i *Info) IsBaseGlyph() bool { return i.Props&PropBaseGlyph != 0 }

// IsLigature: see IsBaseGlyph.
func (i *Info) IsLigature() bool { return i.Props&PropLigature != 0 }

// IsMark: see IsBaseGlyph.
func (i *Info) IsMark() bool { return i.Props&PropMark != 0 }

// Substituted reports a glyph GSUB produced.
func (i *Info) Substituted() bool { return i.Props&PropSubstituted != 0 }

// Ligated reports a glyph a ligature produced.
func (i *Info) Ligated() bool { return i.Props&PropLigated != 0 }

// Multiplied reports a glyph a multiple substitution produced.
func (i *Info) Multiplied() bool { return i.Props&PropMultiplied != 0 }

// LigatedAndDidntMultiply reports a ligature that was not expanded again.
func (i *Info) LigatedAndDidntMultiply() bool { return i.Ligated() && !i.Multiplied() }

// ClearLigatedAndMultiplied forgets both.
func (i *Info) ClearLigatedAndMultiplied() { i.Props &^= PropLigated | PropMultiplied }

// ClearSubstituted forgets GSUB's substitution.
func (i *Info) ClearSubstituted() { i.Props &^= PropSubstituted }

// Ligature component bookkeeping (HarfBuzz's lig_props): a ligature has a
// fresh id and its component count; marks between its components share
// the id and record their component.
const isLigBase = 0x10

func (i *Info) setLigPropsForLigature(id, comps uint8) {
	i.ligProps = id<<5 | isLigBase | comps&0x0F
}

func (i *Info) setLigPropsForMark(id, comp uint8) { i.ligProps = id<<5 | comp&0x0F }

// SetLigComponent sets a multiple substitution's component number.
func (i *Info) SetLigComponent(comp uint8) { i.setLigPropsForMark(0, comp) }

// ClearLigProps forgets ligature membership.
func (i *Info) ClearLigProps() { i.ligProps = 0 }

// LigID is the ligature id the glyph belongs to, 0 for none.
func (i *Info) LigID() uint8 { return i.ligProps >> 5 }

func (i *Info) ligatedInternal() bool { return i.ligProps&isLigBase != 0 }

// LigComp is the ligature component a mark belongs to.
func (i *Info) LigComp() uint8 {
	if i.ligatedInternal() {
		return 0
	}
	return i.ligProps & 0x0F
}

// LigNumComps is a ligature's number of components, 1 otherwise.
func (i *Info) LigNumComps() uint8 {
	if i.Props&PropLigature != 0 && i.ligatedInternal() {
		return i.ligProps & 0x0F
	}
	return 1
}
