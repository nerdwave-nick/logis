package ot

// HarfBuzz's glyph matching: which glyphs a lookup skips (by its flags,
// mark filtering set or attachment type, and default ignorables), and the
// iterator that walks the buffer over them.

const (
	maxNesting       = 64
	maxContextLength = 64
)

// matcher decides whether a glyph matches or may be skipped.
type matcher struct {
	props                               uint32
	mask                                uint32
	ignoreZWNJ, ignoreZWJ, ignoreHidden bool
	perSyllable                         bool
}

type skip int

const (
	skipNo skip = iota
	skipYes
	skipMaybe
)

type step int

const (
	stepMatch step = iota
	stepNoMatch
	stepSkip
)

// checkGlyphProperty reports whether a glyph takes part in a lookup with
// props (flags and mark filtering set).
func (f *Face) checkGlyphProperty(info *Info, props uint32) bool {
	gp := info.Props
	if gp&uint16(props)&FlagIgnoreFlags != 0 {
		return false
	}
	if gp&PropMark != 0 {
		if uint16(props)&FlagUseMarkFilterSet != 0 {
			return f.IsInMarkSet(int(props>>16), uint16(info.Glyph))
		}
		if t := uint16(props) & FlagMarkAttachTypeMsk; t != 0 {
			return t == gp&FlagMarkAttachTypeMsk
		}
	}
	return true
}

func (m *matcher) maySkip(f *Face, info *Info, props uint32) skip {
	if !f.checkGlyphProperty(info, props) {
		return skipYes
	}
	if info.IsDefaultIgnorable() &&
		(m.ignoreZWNJ || !info.IsZWNJ()) &&
		(m.ignoreZWJ || !info.IsZWJ()) &&
		(m.ignoreHidden || !info.IsHidden()) {
		return skipMaybe
	}
	return skipNo
}

// matchFunc reports whether info matches the pattern's element i.
type matchFunc func(info *Info, i int) bool

// iterator walks glyphs from a position, skipping what the matcher skips.
type iterator struct {
	c        *applyContext
	m        *matcher
	props    uint32
	idx, end int
	match    matchFunc
	data     int
	syllable uint8
}

func (c *applyContext) iter(context bool, match matchFunc) iterator {
	m := &c.matcher
	if context {
		m = &c.contextMatcher
	}
	return iterator{c: c, m: m, props: m.props, end: len(c.b.Info), match: match}
}

// reset starts at start in the input; resetBack at start in the output.
func (it *iterator) reset(start int) {
	it.idx = start
	it.end = len(it.c.b.Info)
	it.syllable = it.c.b.Info[it.c.b.idx].Syllable
}

func (it *iterator) resetBack(start int) {
	it.idx = start
	it.syllable = it.c.b.Info[it.c.b.idx].Syllable
}

func (it *iterator) maySkip(info *Info) skip { return it.m.maySkip(it.c.face, info, it.props) }

func (it *iterator) at(info *Info) step {
	sk := it.maySkip(info)
	if sk == skipYes {
		return stepSkip
	}
	matched := info.Mask&it.m.mask != 0 &&
		(!it.m.perSyllable || it.syllable == 0 || it.syllable == info.Syllable)
	maybe := matched && it.match == nil
	if matched && it.match != nil {
		matched = it.match(info, it.data)
	}
	if matched && !maybe || maybe && sk == skipNo {
		return stepMatch
	}
	if sk == skipNo {
		return stepNoMatch
	}
	return stepSkip
}

// next moves to the next matching input glyph; false at a mismatch or
// the end.
func (it *iterator) next() bool {
	for it.idx+1 < it.end {
		it.idx++
		switch it.at(&it.c.b.Info[it.idx]) {
		case stepMatch:
			it.data++
			return true
		case stepNoMatch:
			return false
		}
	}
	return false
}

// prev moves to the previous matching output glyph.
func (it *iterator) prev() bool {
	out := it.c.b.outInfo()
	for it.idx > 0 {
		it.idx--
		switch it.at(&out[it.idx]) {
		case stepMatch:
			it.data++
			return true
		case stepNoMatch:
			return false
		}
	}
	return false
}

// matchInput matches count-1 glyphs after the current one, recording
// their positions; it enforces HarfBuzz's ligature component rules and
// returns the end of the match and the total component count.
func (c *applyContext) matchInput(count int, match matchFunc) (end int, comps uint8, ok bool) {
	b := c.b
	c.matchPositions = c.matchPositions[:0]
	c.matchPositions = append(c.matchPositions, b.idx)
	if count == 1 {
		return b.idx + 1, b.cur().LigNumComps(), true
	}
	if count > maxContextLength {
		return 0, 0, false
	}
	it := c.iter(false, match)
	it.reset(b.idx)
	first := *b.cur()
	firstID, firstComp := first.LigID(), first.LigComp()
	total := first.LigNumComps()
	const unchecked, mayNotSkip, maySkip = 0, 1, 2
	ligbase := unchecked
	for i := 1; i < count; i++ {
		if !it.next() {
			return it.idx + 1, 0, false
		}
		c.matchPositions = append(c.matchPositions, it.idx)
		this := &b.Info[it.idx]
		thisID, thisComp := this.LigID(), this.LigComp()
		if firstID != 0 && firstComp != 0 {
			// Every component must sit on the same component of the same
			// ligature, unless that ligature is itself skipped.
			if firstID != thisID || firstComp != thisComp {
				if ligbase == unchecked {
					ligbase = mayNotSkip
					out := b.outInfo()
					j := b.outLen
					for j > 0 && out[j-1].LigID() == firstID {
						j--
						if out[j].LigComp() == 0 {
							if it.maySkip(&out[j]) == skipYes {
								ligbase = maySkip
							}
							break
						}
					}
				}
				if ligbase == mayNotSkip {
					return 0, 0, false
				}
			}
		} else if thisID != 0 && thisComp != 0 && thisID != firstID {
			return 0, 0, false
		}
		total += this.LigNumComps()
	}
	return it.idx + 1, total, true
}

// matchBacktrack matches count glyphs before the current one in the
// output; it returns where the match starts.
func (c *applyContext) matchBacktrack(count int, match matchFunc) (int, bool) {
	if count == 0 {
		return c.b.backtrackLen(), true
	}
	it := c.iter(true, match)
	it.resetBack(c.b.backtrackLen())
	for range count {
		if !it.prev() {
			return 0, false
		}
	}
	return it.idx, true
}

// matchLookahead matches count glyphs from start on; it returns where the
// match ends.
func (c *applyContext) matchLookahead(count int, match matchFunc, start int) (int, bool) {
	if count == 0 {
		return start, true
	}
	it := c.iter(true, match)
	it.reset(start - 1)
	for range count {
		if !it.next() {
			return 0, false
		}
	}
	return it.idx + 1, true
}
