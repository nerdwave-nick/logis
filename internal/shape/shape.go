// Package shape turns one run of text — one font, one direction — into
// positioned glyphs the way HarfBuzz does: Unicode properties and grapheme
// clusters, mirroring, normalisation against the font's cmap with
// HarfBuzz's mark ordering, the default, Hebrew and Arabic shapers, the
// GSUB and GPOS features of internal/ot, fallback spaces and mark
// positioning, and hidden default ignorables. Glyphs come out in visual
// order with clusters as byte offsets and positions in font units.
//
// Complex-script shapers (Indic, USE, Khmer, Myanmar, Thai, Hangul jamo),
// AAT tables, the legacy kern table, vertical text and variable fonts are
// out of scope: those scripts get the default shaper.
package shape

import (
	"fmt"

	"github.com/nerdwave-nick/logis/internal/ot"
	"github.com/nerdwave-nick/logis/internal/ucd"
)

// Feature is a feature applied to the whole run with a value (0 turns a
// default feature off).
type Feature struct {
	Tag   ot.Tag
	Value uint32
}

// Options shape a run: its direction, extra features, and the ppem at
// which hinting device tables apply (0: none, as logis's line layout shapes).
type Options struct {
	RTL      bool
	Features []Feature
	PPEM     int
}

// Glyph is a shaped glyph: its id, the byte offset of the cluster it
// shows, its advance and its offset from the pen, in font units (y up).
type Glyph struct {
	ID               uint16
	Cluster          int
	XAdvance         int32
	XOffset, YOffset int32
}

// Font shapes runs with one face, caching a plan per script, direction and
// features. A Font is not safe for concurrent use.
type Font struct {
	face  *ot.Face
	plans map[planKey]*plan
}

// planKey names a plan; features are rare and keyed as text.
type planKey struct {
	script   ucd.Script
	rtl      bool
	features string
}

// NewFont shapes with face.
func NewFont(face *ot.Face) *Font { return &Font{face: face, plans: map[planKey]*plan{}} }

// Face is the face the font shapes with.
func (f *Font) Face() *ot.Face { return f.face }

func (f *Font) plan(script ucd.Script, rtl bool, features []Feature) *plan {
	key := planKey{script: script, rtl: rtl}
	if len(features) > 0 {
		key.features = fmt.Sprint(features)
	}
	p, ok := f.plans[key]
	if !ok {
		p = newPlan(f.face, script, rtl, features)
		f.plans[key] = p
	}
	return p
}

// Shape shapes text as one run. Invalid UTF-8 shapes as U+FFFD.
func (f *Font) Shape(text string, opt Options) []Glyph {
	infos := make([]ot.Info, 0, len(text))
	for i, r := range text {
		infos = append(infos, ot.Info{Glyph: uint32(r), Cluster: uint32(i)})
	}
	if len(infos) == 0 {
		return nil
	}
	script := guessScript(infos)
	p := f.plan(script, opt.RTL, opt.Features)
	b := ot.NewBuffer(infos, opt.RTL)
	f.shape(p, b, opt)
	out := make([]Glyph, len(b.Info))
	for i, info := range b.Info {
		pos := b.Pos[i]
		out[i] = Glyph{ID: uint16(info.Glyph), Cluster: int(info.Cluster), XAdvance: pos.XAdvance, XOffset: pos.XOffset, YOffset: pos.YOffset}
	}
	return out
}

// shape is HarfBuzz's shape_internal.
func (f *Font) shape(p *plan, b *ot.Buffer, opt Options) {
	for i := range b.Info {
		b.Info[i].Mask = p.m.GlobalMask
	}
	setUnicodeProps(b)
	if b.ScratchFlags&ot.ScratchHasContinuations != 0 {
		for start := 0; start < len(b.Info); {
			end := b.GroupEnd(start, ot.SameGrapheme)
			b.MergeClusters(start, end)
			start = end
		}
	}
	ensureNativeDirection(b, p.script)

	// Substitution.
	f.rotateChars(p, b, opt.RTL)
	normalize(p, f.face, b)
	f.setupMasks(p, b)
	if p.fallbackMarkPositioning {
		recategorizeMarks(b)
	}
	for i := range b.Info {
		info := &b.Info[i]
		info.Glyph = info.Norm
		info.Props = f.face.GlyphProps(uint16(info.Glyph))
		info.ClearLigProps()
	}
	if p.fallbackGlyphClasses {
		for i := range b.Info {
			info := &b.Info[i]
			if info.GenCat() != ot.NonSpacingMark || info.IsDefaultIgnorable() {
				info.Props = ot.PropBaseGlyph
			} else {
				info.Props = ot.PropMark
			}
		}
	}
	f.face.Substitute(b, p.m.GSUBStages())

	f.position(p, b, opt.PPEM)
	f.hideDefaultIgnorables(b)
}

// ensureNativeDirection shapes a run against its script's direction (a
// Latin run in a right-to-left line, say) in the script's own direction,
// graphemes reversed, as HarfBuzz does; numbers alone stay left to right.
func ensureNativeDirection(b *ot.Buffer, script ucd.Script) {
	rtl, known := nativeDirection(script)
	if rtl && !b.RTL {
		number, letter, ri := false, false, false
		for i := range b.Info {
			gc := b.Info[i].GenCat()
			switch {
			case gc == ot.DecimalNumber:
				number = true
			case gc.IsLetter():
				letter = true
			case b.Info[i].Glyph >= 0x1F1E6 && b.Info[i].Glyph <= 0x1F1FF:
				ri = true
			}
			if letter {
				break
			}
		}
		if (number || ri) && !letter {
			rtl = false
		}
	}
	if known && rtl != b.RTL {
		b.ReverseGroups(ot.SameGrapheme, false)
		b.RTL = !b.RTL
	}
}

// rotateChars mirrors characters of a right-to-left run that the font
// has mirrored glyphs for, and asks rtlm for the others.
func (f *Font) rotateChars(p *plan, b *ot.Buffer, rtl bool) {
	if !rtl {
		return
	}
	for i := range b.Info {
		info := &b.Info[i]
		if m, ok := ucd.Mirror(rune(info.Glyph)); ok {
			if _, ok := f.face.Glyph(m); ok {
				info.Glyph = uint32(m)
				continue
			}
		}
		info.Mask |= p.rtlmMask
	}
}

func (f *Font) setupMasks(p *plan, b *ot.Buffer) {
	if b.ScratchFlags&ot.ScratchHasFractionSlash != 0 && p.hasFrac {
		setupFractionMasks(p, b)
	}
	if p.kind == shaperArabic {
		p.setupArabicMasks(b)
	}
}

// setupFractionMasks applies numr/frac/dnom around fraction slashes
// between digits.
func setupFractionMasks(p *plan, b *ot.Buffer) {
	pre, post := p.numrMask|p.fracMask, p.fracMask|p.dnomMask
	if b.RTL {
		pre, post = post, pre
	}
	infos := b.Info
	for i := 0; i < len(infos); {
		if infos[i].Glyph != 0x2044 {
			i++
			continue
		}
		start, end := i, i+1
		for start > 0 && infos[start-1].GenCat() == ot.DecimalNumber {
			start--
		}
		for end < len(infos) && infos[end].GenCat() == ot.DecimalNumber {
			end++
		}
		if start == i || end == i+1 {
			i++
			continue
		}
		for j := start; j < i; j++ {
			infos[j].Mask |= pre
		}
		infos[i].Mask |= p.fracMask
		for j := i + 1; j < end; j++ {
			infos[j].Mask |= post
		}
		i = end
	}
}
