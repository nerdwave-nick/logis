package ot

// LookupMap is one lookup a plan applies: the glyphs whose mask meets Mask
// take part; AutoZWNJ/AutoZWJ let matching skip the joiners.
type LookupMap struct {
	Index       int
	Mask        uint32
	AutoZWNJ    bool
	AutoZWJ     bool
	Random      bool
	PerSyllable bool
}

// Stage is a group of lookups applied together, then an optional pause
// (a shaper's own step between features); a pause returning true has
// changed glyphs.
type Stage struct {
	Lookups []LookupMap
	Pause   func(*Buffer) bool
}

// applyContext is HarfBuzz's hb_ot_apply_context_t.
type applyContext struct {
	face                    *Face
	layout                  *Layout
	gsub                    bool
	b                       *Buffer
	ppem                    int
	lookupMask              uint32
	lookupProps             uint32
	nestingLeft             int
	autoZWNJ, autoZWJ       bool
	random, perSyllable     bool
	randomState             uint32
	lastBase                int
	lastBaseUntil           int
	matcher, contextMatcher matcher
	matchPositions          []int
}

func (c *applyContext) newMatcher(context bool) matcher {
	m := matcher{
		props:        c.lookupProps,
		ignoreZWNJ:   !c.gsub || context && c.autoZWNJ,
		ignoreZWJ:    context || c.autoZWJ,
		ignoreHidden: !c.gsub,
		mask:         c.lookupMask,
		perSyllable:  c.gsub && c.perSyllable,
	}
	if context {
		m.mask = ^uint32(0)
	}
	return m
}

func (c *applyContext) updateMatchers() {
	c.matcher = c.newMatcher(false)
	c.contextMatcher = c.newMatcher(true)
}

func (c *applyContext) setLookupMask(mask uint32) {
	c.lookupMask = mask
	c.lastBase, c.lastBaseUntil = -1, 0
}

func (c *applyContext) randomNumber() uint32 {
	c.randomState = uint32(uint64(c.randomState) * 48271 % 0x7fffffff)
	return c.randomState
}

// Substitute applies GSUB stages to b, which holds glyph ids with their
// properties set (SetGlyphProps).
func (f *Face) Substitute(b *Buffer, stages []Stage) {
	f.applyStages(f.gsub, true, b, stages, 0)
}

// Position applies GPOS stages to b, whose Pos hold advances; ppem > 0
// applies hinting device tables as HarfBuzz does for a font with a ppem.
func (f *Face) Position(b *Buffer, stages []Stage, ppem int) {
	f.applyStages(f.gpos, false, b, stages, ppem)
}

func (f *Face) applyStages(l *Layout, gsub bool, b *Buffer, stages []Stage, ppem int) {
	c := &applyContext{
		face: f, layout: l, gsub: gsub, b: b, ppem: ppem,
		lookupMask: 1, lookupProps: ^uint32(0), nestingLeft: maxNesting,
		autoZWNJ: true, autoZWJ: true, randomState: 1, lastBase: -1,
	}
	for _, stage := range stages {
		if l != nil {
			for _, lm := range stage.Lookups {
				if lm.Index >= len(l.lookups) {
					continue
				}
				c.setLookupMask(lm.Mask)
				c.autoZWNJ, c.autoZWJ = lm.AutoZWNJ, lm.AutoZWJ
				c.random, c.perSyllable = lm.Random, lm.PerSyllable
				c.applyString(&l.lookups[lm.Index])
			}
		}
		if stage.Pause != nil {
			stage.Pause(b)
		}
	}
}

func (c *applyContext) applyString(lk *lookup) {
	b := c.b
	if len(b.Info) == 0 || c.lookupMask == 0 {
		return
	}
	c.lookupProps = lk.props
	c.updateMatchers()
	if !c.gsub || lk.kind != 8 {
		if c.gsub {
			b.ClearOutput()
		}
		b.idx = 0
		for b.successful && b.idx < len(b.Info) {
			if c.wants(b.cur()) && c.applyLookup(lk) {
				continue
			}
			b.nextGlyph()
		}
		if c.gsub {
			b.Sync()
		}
		return
	}
	// Reverse chaining substitutes in place, last glyph first.
	for b.idx = len(b.Info) - 1; b.idx >= 0; b.idx-- {
		if c.wants(b.cur()) {
			c.applyLookup(lk)
		}
	}
	b.idx = 0
}

func (c *applyContext) wants(info *Info) bool {
	return info.Mask&c.lookupMask != 0 && c.face.checkGlyphProperty(info, c.lookupProps)
}

// applyHook, when set (by tests), hears of every subtable that applied:
// its table, lookup type and format, and whether it sat in an extension.
var applyHook func(gsub bool, kind, format int, extension bool)

// applyLookup applies the first subtable that matches at the current glyph.
func (c *applyContext) applyLookup(lk *lookup) bool {
	for _, st := range lk.subtables {
		var ok bool
		if c.gsub {
			ok = c.applySubst(lk.kind, st)
		} else {
			ok = c.applyPos(lk.kind, st)
		}
		if ok {
			if applyHook != nil {
				applyHook(c.gsub, lk.kind, int(st.u16(0)), lk.extension)
			}
			return true
		}
	}
	return false
}

// recurse applies lookup index at the current glyph, for a context
// lookup's records.
func (c *applyContext) recurse(index int) bool {
	b := c.b
	if c.nestingLeft == 0 {
		b.successful = false
		return false
	}
	b.maxOps--
	if b.maxOps < 0 || index >= len(c.layout.lookups) {
		b.successful = b.maxOps >= 0
		return false
	}
	c.nestingLeft--
	savedProps := c.lookupProps
	savedPositions := append([]int(nil), c.matchPositions...)
	lk := &c.layout.lookups[index]
	c.lookupProps = lk.props
	c.updateMatchers()
	applied := c.applyLookup(lk)
	c.lookupProps = savedProps
	c.updateMatchers()
	c.matchPositions = savedPositions
	c.nestingLeft++
	return applied
}

// sequenceLookup is a context rule's (sequence index, lookup index).
type sequenceLookup struct{ seq, lookup int }

func sequenceLookups(t table, off, n int) []sequenceLookup {
	out := make([]sequenceLookup, 0, n)
	for i := range n {
		out = append(out, sequenceLookup{int(t.u16(off + 4*i)), int(t.u16(off + 4*i + 2))})
	}
	return out
}

// applyRecords applies a matched context's lookups at their positions,
// tracking how nested substitutions change the sequence's length.
func (c *applyContext) applyRecords(count, matchEnd int, records []sequenceLookup) {
	b := c.b
	pos := c.matchPositions
	backtrack := b.backtrackLen()
	delta := backtrack - b.idx
	for j := range count {
		pos[j] += delta
	}
	end := backtrack + matchEnd - b.idx
	for _, r := range records {
		if !b.successful {
			break
		}
		idx := r.seq
		if idx >= count {
			continue
		}
		origLen := b.backtrackLen() + b.lookaheadLen()
		if pos[idx] >= origLen {
			continue
		}
		if !b.moveTo(pos[idx]) || b.maxOps <= 0 {
			break
		}
		c.matchPositions = pos
		if !c.recurse(r.lookup) {
			continue
		}
		pos = c.matchPositions
		newLen := b.backtrackLen() + b.lookaheadLen()
		d := newLen - origLen
		if d == 0 {
			continue
		}
		end += d
		if end < pos[idx] {
			d += pos[idx] - end
			end = pos[idx]
		}
		next := idx + 1
		if d > 0 {
			if d+count > maxContextLength {
				break
			}
			for len(pos) < count+d {
				pos = append(pos, 0)
			}
		} else {
			d = max(d, next-count)
			next -= d
		}
		copy(pos[next+d:count+d], pos[next:count])
		next += d
		count += d
		pos = pos[:count]
		for j := idx + 1; j < next; j++ {
			pos[j] = pos[j-1] + 1
		}
		for ; next < count; next++ {
			pos[next] += d
		}
	}
	c.matchPositions = pos
	b.moveTo(end)
}
