package shape

import (
	"slices"

	"github.com/nerdwave-nick/logis/internal/ot"
	"github.com/nerdwave-nick/logis/internal/ucd"
)

// Joining actions, indexing arabicFeatureTags.
const (
	arabicIsol = iota
	arabicFina
	arabicFin2
	arabicFin3
	arabicMedi
	arabicMed2
	arabicInit
	arabicNone
)

var arabicFeatureTags = [...]string{"isol", "fina", "fin2", "fin3", "medi", "med2", "init"}

// arabicFeatures asks for the joining forms, each in its own stage, then
// the ligatures, all with ZWJ left to the font (HarfBuzz's order).
func arabicFeatures(b *ot.MapBuilder, script ucd.Script) {
	b.EnableFeature(tag("stch"), 0, 1)
	b.AddGSUBPause(nil)
	b.EnableFeature(tag("ccmp"), ot.FeatureManualZWJ, 1)
	b.EnableFeature(tag("locl"), ot.FeatureManualZWJ, 1)
	b.AddGSUBPause(nil)
	for _, t := range arabicFeatureTags {
		flags := uint32(ot.FeatureManualZWJ)
		if script == ucd.Arabic && t[3] != '2' && t[3] != '3' {
			flags |= ot.FeatureHasFallback
		}
		b.AddFeature(tag(t), flags, 1)
		b.AddGSUBPause(nil)
	}
	b.AddGSUBPause(nil)
	b.EnableFeature(tag("rlig"), ot.FeatureManualZWJ|ot.FeatureHasFallback, 1)
	if script == ucd.Arabic {
		b.AddGSUBPause(nil)
	}
	b.EnableFeature(tag("calt"), ot.FeatureManualZWJ, 1)
	if !b.HasFeature(tag("rclt")) {
		b.AddGSUBPause(nil)
	}
	b.EnableFeature(tag("liga"), ot.FeatureManualZWJ, 1)
	b.EnableFeature(tag("clig"), ot.FeatureManualZWJ, 1)
	b.EnableFeature(tag("mset"), ot.FeatureManualZWJ, 1)
}

// joiningState is HarfBuzz's Arabic joining state machine: for each state
// and joining type (U L R D Alaph DalathRish), the action for the previous
// character, the action for this one, and the next state.
var joiningState = [7][6][3]uint8{
	{{arabicNone, arabicNone, 0}, {arabicNone, arabicIsol, 2}, {arabicNone, arabicIsol, 1}, {arabicNone, arabicIsol, 2}, {arabicNone, arabicIsol, 1}, {arabicNone, arabicIsol, 6}},
	{{arabicNone, arabicNone, 0}, {arabicNone, arabicIsol, 2}, {arabicNone, arabicIsol, 1}, {arabicNone, arabicIsol, 2}, {arabicNone, arabicFin2, 5}, {arabicNone, arabicIsol, 6}},
	{{arabicNone, arabicNone, 0}, {arabicNone, arabicIsol, 2}, {arabicInit, arabicFina, 1}, {arabicInit, arabicFina, 3}, {arabicInit, arabicFina, 4}, {arabicInit, arabicFina, 6}},
	{{arabicNone, arabicNone, 0}, {arabicNone, arabicIsol, 2}, {arabicMedi, arabicFina, 1}, {arabicMedi, arabicFina, 3}, {arabicMedi, arabicFina, 4}, {arabicMedi, arabicFina, 6}},
	{{arabicNone, arabicNone, 0}, {arabicNone, arabicIsol, 2}, {arabicMed2, arabicIsol, 1}, {arabicMed2, arabicIsol, 2}, {arabicMed2, arabicFin2, 5}, {arabicMed2, arabicIsol, 6}},
	{{arabicNone, arabicNone, 0}, {arabicNone, arabicIsol, 2}, {arabicIsol, arabicIsol, 1}, {arabicIsol, arabicIsol, 2}, {arabicIsol, arabicFin2, 5}, {arabicIsol, arabicIsol, 6}},
	{{arabicNone, arabicNone, 0}, {arabicNone, arabicIsol, 2}, {arabicNone, arabicIsol, 1}, {arabicNone, arabicIsol, 2}, {arabicNone, arabicFin3, 5}, {arabicNone, arabicIsol, 6}},
}

// joiningType is r's joining type; unlisted characters are transparent if
// they are marks or format characters.
func joiningType(info *ot.Info) ucd.JoiningType {
	t := ucd.Joining(rune(info.Glyph))
	if t != ucd.JoinX {
		return t
	}
	switch info.GenCat() {
	case ot.NonSpacingMark, ot.EnclosingMark, ot.Format:
		return ucd.JoinT
	}
	return ucd.JoinU
}

// setupArabicMasks gives each character the mask of its joining form.
// Runs have no context outside themselves, as cosmic-text shapes them.
func (p *plan) setupArabicMasks(b *ot.Buffer) {
	infos := b.Info
	prev, state := -1, 0
	for i := range infos {
		t := joiningType(&infos[i])
		if t == ucd.JoinT {
			infos[i].ShaperVar = arabicNone
			continue
		}
		entry := joiningState[state][t]
		if entry[0] != arabicNone && prev >= 0 {
			infos[prev].ShaperVar = entry[0]
		}
		infos[i].ShaperVar = entry[1]
		prev, state = i, int(entry[2])
	}
	if p.script == ucd.Mongolian {
		for i := 1; i < len(infos); i++ {
			if r := infos[i].Glyph; r >= 0x180B && r <= 0x180D || r == 0x180F {
				infos[i].ShaperVar = infos[i-1].ShaperVar
			}
		}
	}
	for i := range infos {
		infos[i].Mask |= p.arabicMasks[infos[i].ShaperVar]
	}
}

// modifierCombiningMarks move to the front of their class (UTR #53).
var modifierCombiningMarks = []uint32{
	0x0654, 0x0655, 0x0658, 0x06DC, 0x06E3, 0x06E7, 0x06E8, 0x08CA,
	0x08CB, 0x08CD, 0x08CE, 0x08CF, 0x08D3, 0x08F3,
}

// reorderArabicMarks moves modifier combining marks of classes 220 and
// 230 before the other marks, renumbering them so the sequence stays
// sorted (HarfBuzz's reorder_marks_arabic).
func reorderArabicMarks(b *ot.Buffer, start, end int) {
	infos := b.Info
	i := start
	for _, cc := range []uint8{220, 230} {
		for i < end && infos[i].CombiningClass() < cc {
			i++
		}
		if i == end {
			break
		}
		if infos[i].CombiningClass() > cc {
			continue
		}
		j := i
		for j < end && infos[j].CombiningClass() == cc && slices.Contains(modifierCombiningMarks, infos[j].Glyph) {
			j++
		}
		if i == j {
			continue
		}
		b.MergeClusters(start, j)
		moved := append([]ot.Info(nil), infos[i:j]...)
		copy(infos[start+j-i:j], infos[start:i])
		copy(infos[start:], moved)
		newStart := start + j - i
		newCC := uint8(22)
		if cc == 230 {
			newCC = 26
		}
		for ; start < newStart; start++ {
			infos[start].SetCombiningClass(newCC)
		}
		i = j
	}
}
