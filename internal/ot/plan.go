package ot

import "slices"

// Feature flags (HarfBuzz's hb_ot_map_feature_flags_t).
const (
	FeatureGlobal       = 0x01 // applies to all glyphs: no mask bit of its own
	FeatureHasFallback  = 0x02 // keep a mask bit even if the font lacks it
	FeatureManualZWNJ   = 0x04 // do not skip ZWNJ when matching context
	FeatureManualZWJ    = 0x08 // do not skip ZWJ when matching input
	FeatureGlobalSearch = 0x10 // look in the whole feature list if missing
	FeatureRandom       = 0x20 // pick alternates at random
	FeaturePerSyllable  = 0x40 // contain matching within a syllable

	FeatureManualJoiners = FeatureManualZWNJ | FeatureManualZWJ
)

// maxFeatureValue is the largest value a feature takes (8 mask bits).
const maxFeatureValue = 255

const (
	globalBitShift = 31
	globalBitMask  = 1 << globalBitShift
	firstBit       = 4 // after HarfBuzz's three glyph flag bits
)

// MapBuilder collects the features a shaper asks for, in stages, and
// compiles them into a Map for one face, script and language.
type MapBuilder struct {
	face     *Face
	tables   [2]*Layout
	script   [2]Script
	found    [2]bool
	lang     [2]int
	features []featureInfo
	pauses   [2][]stageInfo
	current  [2]int
}

type featureInfo struct {
	tag          Tag
	seq          int
	maxValue     uint32
	flags        uint32
	defaultValue uint32
	stage        [2]int
}

type stageInfo struct {
	index int
	pause func(*Buffer) bool
}

// NewMapBuilder selects the first of scripts each table has (else its
// default script) and the first of langs (else the default language).
func NewMapBuilder(face *Face, scripts, langs []Tag) *MapBuilder {
	m := &MapBuilder{face: face, tables: [2]*Layout{face.gsub, face.gpos}, lang: [2]int{-1, -1}}
	for t, l := range m.tables {
		m.script[t].Index = -1
		if l == nil {
			continue
		}
		if s, ok := l.SelectScript(scripts); ok {
			m.script[t], m.found[t] = s, !s.Fallback
			m.lang[t] = l.SelectLanguage(s.Index, langs)
		}
	}
	return m
}

// ChosenScript is the script tag GSUB (gpos false) or GPOS chose, 0 if
// the table is absent.
func (m *MapBuilder) ChosenScript(gpos bool) Tag { return m.script[b2i(gpos)].Tag }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// HasFeature reports a feature either table's language system has.
func (m *MapBuilder) HasFeature(tag Tag) bool {
	for t, l := range m.tables {
		if l != nil && m.script[t].Index >= 0 && l.FindFeature(m.script[t].Index, m.lang[t], tag) >= 0 {
			return true
		}
	}
	return false
}

// AddFeature asks for a feature with a value; EnableFeature makes it
// global.
func (m *MapBuilder) AddFeature(tag Tag, flags, value uint32) {
	if tag == 0 {
		return
	}
	def := uint32(0)
	if flags&FeatureGlobal != 0 {
		def = value
	}
	m.features = append(m.features, featureInfo{
		tag: tag, seq: len(m.features), maxValue: value, flags: flags, defaultValue: def, stage: m.current,
	})
}

// EnableFeature adds a global feature.
func (m *MapBuilder) EnableFeature(tag Tag, flags, value uint32) {
	m.AddFeature(tag, flags|FeatureGlobal, value)
}

// AddGSUBPause ends a GSUB stage, running pause after it.
func (m *MapBuilder) AddGSUBPause(pause func(*Buffer) bool) { m.addPause(0, pause) }

// AddGPOSPause ends a GPOS stage.
func (m *MapBuilder) AddGPOSPause(pause func(*Buffer) bool) { m.addPause(1, pause) }

func (m *MapBuilder) addPause(t int, pause func(*Buffer) bool) {
	m.pauses[t] = append(m.pauses[t], stageInfo{index: m.current[t], pause: pause})
	m.current[t]++
}

// Map is a compiled feature plan.
type Map struct {
	GlobalMask uint32
	features   []featureMap
	stages     [2][]Stage
	script     [2]Script
	found      [2]bool
}

type featureMap struct {
	tag           Tag
	index         [2]int
	stage         [2]int
	shift         uint32
	mask, oneMask uint32
	autoZWNJ      bool
	autoZWJ       bool
	random        bool
	perSyllable   bool
}

// Compile allocates mask bits and orders each table's lookups by stage.
func (m *MapBuilder) Compile() *Map {
	var required [2]int
	var requiredTag [2]Tag
	for t, l := range m.tables {
		required[t] = -1
		if l != nil && m.script[t].Index >= 0 {
			required[t], requiredTag[t] = l.RequiredFeature(m.script[t].Index, m.lang[t])
		}
	}
	out := &Map{GlobalMask: globalBitMask, script: m.script, found: m.found}
	requiredStage := m.collectFeatures(out, requiredTag)
	m.AddGSUBPause(nil)
	m.AddGPOSPause(nil)
	for t := range 2 {
		m.collectStages(out, t, required[t], requiredStage[t])
	}
	return out
}

func (m *MapBuilder) collectFeatures(out *Map, requiredTag [2]Tag) [2]int {
	var requiredStage [2]int
	slices.SortStableFunc(m.features, func(a, b featureInfo) int {
		if a.tag != b.tag {
			return cmpTag(a.tag, b.tag)
		}
		return a.seq - b.seq
	})
	// Merge repeated features: a global request wins over a masked one.
	var merged []featureInfo
	for _, f := range m.features {
		n := len(merged)
		if n == 0 || merged[n-1].tag != f.tag {
			merged = append(merged, f)
			continue
		}
		j := &merged[n-1]
		if f.flags&FeatureGlobal != 0 {
			j.flags |= FeatureGlobal
			j.maxValue, j.defaultValue = f.maxValue, f.defaultValue
		} else {
			j.flags &^= FeatureGlobal
			j.maxValue = max(j.maxValue, f.maxValue)
		}
		j.flags |= f.flags & FeatureHasFallback
		j.stage[0], j.stage[1] = min(j.stage[0], f.stage[0]), min(j.stage[1], f.stage[1])
	}
	next := uint32(firstBit)
	for _, f := range merged {
		bits := uint32(0)
		if f.flags&FeatureGlobal == 0 || f.maxValue != 1 {
			for v := f.maxValue; v != 0; v >>= 1 {
				bits++
			}
			bits = min(bits, 8)
		}
		if f.maxValue == 0 || next+bits >= globalBitShift {
			continue
		}
		found := false
		index := [2]int{-1, -1}
		for t, l := range m.tables {
			if requiredTag[t] == f.tag {
				requiredStage[t] = f.stage[t]
			}
			if l != nil && m.script[t].Index >= 0 {
				if i := l.FindFeature(m.script[t].Index, m.lang[t], f.tag); i >= 0 {
					index[t], found = i, true
				}
			}
		}
		if !found && f.flags&FeatureGlobalSearch != 0 {
			for t, l := range m.tables {
				if l != nil {
					if i := l.FeatureIndex(f.tag); i >= 0 {
						index[t], found = i, true
					}
				}
			}
		}
		if !found && f.flags&FeatureHasFallback == 0 {
			continue
		}
		fm := featureMap{
			tag: f.tag, index: index, stage: f.stage,
			autoZWNJ: f.flags&FeatureManualZWNJ == 0, autoZWJ: f.flags&FeatureManualZWJ == 0,
			random: f.flags&FeatureRandom != 0, perSyllable: f.flags&FeaturePerSyllable != 0,
		}
		if f.flags&FeatureGlobal != 0 && f.maxValue == 1 {
			fm.shift, fm.mask = globalBitShift, globalBitMask
		} else {
			fm.shift = next
			fm.mask = 1<<(next+bits) - 1<<next
			next += bits
			out.GlobalMask |= f.defaultValue << fm.shift & fm.mask
		}
		fm.oneMask = 1 << fm.shift & fm.mask
		out.features = append(out.features, fm)
	}
	return requiredStage
}

func cmpTag(a, b Tag) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (m *MapBuilder) collectStages(out *Map, t, required, requiredStage int) {
	l := m.tables[t]
	pauses := m.pauses[t]
	p := 0
	for stage := range m.current[t] {
		var lookups []LookupMap
		if l != nil {
			if required >= 0 && requiredStage == stage {
				lookups = addLookups(lookups, l, required, globalBitMask, true, true, false, false)
			}
			for _, f := range out.features {
				if f.index[t] >= 0 && f.stage[t] == stage {
					lookups = addLookups(lookups, l, f.index[t], f.mask, f.autoZWNJ, f.autoZWJ, f.random, f.perSyllable)
				}
			}
		}
		slices.SortStableFunc(lookups, func(a, b LookupMap) int { return a.Index - b.Index })
		var merged []LookupMap
		for _, lm := range lookups {
			if n := len(merged); n > 0 && merged[n-1].Index == lm.Index {
				merged[n-1].Mask |= lm.Mask
				merged[n-1].AutoZWNJ = merged[n-1].AutoZWNJ && lm.AutoZWNJ
				merged[n-1].AutoZWJ = merged[n-1].AutoZWJ && lm.AutoZWJ
				continue
			}
			merged = append(merged, lm)
		}
		s := Stage{Lookups: merged}
		if p < len(pauses) && pauses[p].index == stage {
			s.Pause = pauses[p].pause
			p++
		}
		out.stages[t] = append(out.stages[t], s)
	}
}

func addLookups(lookups []LookupMap, l *Layout, feature int, mask uint32, zwnj, zwj, random, syllable bool) []LookupMap {
	for _, i := range l.FeatureLookups(feature) {
		lookups = append(lookups, LookupMap{Index: i, Mask: mask, AutoZWNJ: zwnj, AutoZWJ: zwj, Random: random, PerSyllable: syllable})
	}
	return lookups
}

func (m *Map) feature(tag Tag) *featureMap {
	i, ok := slices.BinarySearchFunc(m.features, tag, func(f featureMap, t Tag) int { return cmpTag(f.tag, t) })
	if !ok {
		return nil
	}
	return &m.features[i]
}

// Mask is a feature's mask and shift, 0 when the plan lacks it.
func (m *Map) Mask(tag Tag) (uint32, uint32) {
	if f := m.feature(tag); f != nil {
		return f.mask, f.shift
	}
	return 0, 0
}

// OneMask is the mask that sets a feature to 1.
func (m *Map) OneMask(tag Tag) uint32 {
	if f := m.feature(tag); f != nil {
		return f.oneMask
	}
	return 0
}

// HasFeatureIn reports whether GSUB (gpos false) or GPOS has the feature.
func (m *Map) HasFeatureIn(gpos bool, tag Tag) bool {
	f := m.feature(tag)
	return f != nil && f.index[b2i(gpos)] >= 0
}

// ChosenScript is the script tag a table chose, 0 when absent.
func (m *Map) ChosenScript(gpos bool) Tag { return m.script[b2i(gpos)].Tag }

// FoundScript reports a table that has the script itself, not a fallback.
func (m *Map) FoundScript(gpos bool) bool { return m.found[b2i(gpos)] }

// GSUBStages and GPOSStages are the compiled stages.
func (m *Map) GSUBStages() []Stage { return m.stages[0] }

// GPOSStages: see GSUBStages.
func (m *Map) GPOSStages() []Stage { return m.stages[1] }
