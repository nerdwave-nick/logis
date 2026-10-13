package shape

import (
	"github.com/nerdwave-nick/logis/internal/ot"
	"github.com/nerdwave-nick/logis/internal/ucd"
)

type shaperKind int

const (
	shaperDefault shaperKind = iota
	shaperHebrew
	shaperArabic
)

// plan is HarfBuzz's shape plan for one face, script, direction and set
// of features.
type plan struct {
	m      *ot.Map
	kind   shaperKind
	script ucd.Script
	rtl    bool

	fracMask, numrMask, dnomMask, rtlmMask uint32
	hasFrac, hasGPOSMark, zeroMarks        bool
	fallbackGlyphClasses, applyGPOS        bool
	fallbackMarkPositioning                bool
	adjustMarkPositioningWhenZeroing       bool
	// arabicMasks are the joining features' masks by joining action.
	arabicMasks [arabicNone + 1]uint32
}

var tag = ot.MakeTag

func newPlan(face *ot.Face, script ucd.Script, rtl bool, features []Feature) *plan {
	b := ot.NewMapBuilder(face, scriptTags(script), nil)
	p := &plan{script: script, rtl: rtl}
	switch script {
	case ucd.Arabic, ucd.Syriac:
		if b.ChosenScript(false) != tag("DFLT") || script == ucd.Arabic {
			p.kind = shaperArabic
		}
	case ucd.Hebrew:
		p.kind = shaperHebrew
	}

	b.EnableFeature(tag("rvrn"), 0, 1)
	b.AddGSUBPause(nil)
	if rtl {
		b.EnableFeature(tag("rtla"), 0, 1)
		b.AddFeature(tag("rtlm"), 0, 1)
	} else {
		b.EnableFeature(tag("ltra"), 0, 1)
		b.EnableFeature(tag("ltrm"), 0, 1)
	}
	for _, t := range []string{"frac", "numr", "dnom"} {
		b.AddFeature(tag(t), 0, 1)
	}
	b.EnableFeature(tag("rand"), ot.FeatureRandom, 255)
	b.EnableFeature(tag("trak"), ot.FeatureHasFallback, 1)
	b.EnableFeature(tag("Harf"), 0, 1)
	b.EnableFeature(tag("HARF"), 0, 1)
	if p.kind == shaperArabic {
		arabicFeatures(b, script)
	}
	b.EnableFeature(tag("Buzz"), 0, 1)
	b.EnableFeature(tag("BUZZ"), 0, 1)
	for _, f := range []struct {
		t     string
		flags uint32
	}{
		{"abvm", 0}, {"blwm", 0}, {"ccmp", 0}, {"locl", 0},
		{"mark", ot.FeatureManualJoiners}, {"mkmk", ot.FeatureManualJoiners}, {"rlig", 0},
		{"calt", 0}, {"clig", 0}, {"curs", 0}, {"dist", 0},
		{"kern", ot.FeatureHasFallback}, {"liga", 0}, {"rclt", 0},
	} {
		b.AddFeature(tag(f.t), ot.FeatureGlobal|f.flags, 1)
	}
	for _, f := range features {
		b.AddFeature(f.Tag, ot.FeatureGlobal, f.Value)
	}
	p.m = b.Compile()

	m := p.m
	p.fracMask, p.numrMask, p.dnomMask = m.OneMask(tag("frac")), m.OneMask(tag("numr")), m.OneMask(tag("dnom"))
	p.hasFrac = p.fracMask != 0 || p.numrMask != 0 && p.dnomMask != 0
	p.rtlmMask = m.OneMask(tag("rtlm"))
	disableGPOS := p.kind == shaperHebrew && m.ChosenScript(true) != tag("hebr")
	p.applyGPOS = !disableGPOS && face.GPOS() != nil
	p.fallbackGlyphClasses = !face.HasGlyphClasses()
	p.zeroMarks = true
	p.hasGPOSMark = m.OneMask(tag("mark")) != 0
	p.adjustMarkPositioningWhenZeroing = !p.applyGPOS
	p.fallbackMarkPositioning = p.adjustMarkPositioningWhenZeroing
	if p.kind == shaperArabic {
		for i, t := range arabicFeatureTags {
			p.arabicMasks[i] = m.OneMask(tag(t))
		}
	}
	return p
}
