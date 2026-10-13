package fuzzy

import "bytes"

// Matcher holds the scratch memory of matching; one per goroutine. The
// zero value is ready.
type Matcher struct {
	// normalize: the word being matched allows accent folding.
	normalize bool
	// wantIndices: positions are collected into indices.
	wantIndices bool
	indices     []uint32
	dp          dpScratch
}

// NewMatcher returns a matcher.
func NewMatcher() *Matcher { return new(Matcher) }

// score matches one word (nucleo's Atom::score and Atom::indices).
func (m *Matcher) score(a *atom, t Text, indices bool) (uint16, bool) {
	m.normalize = a.normalize
	m.wantIndices = indices && !a.negative
	mark := len(m.indices)
	var s uint16
	var ok bool
	switch a.kind {
	case kindFuzzy:
		s, ok = m.fuzzy(t, a)
	case kindSubstring:
		s, ok = m.substring(t, a)
	case kindPrefix:
		s, ok = m.prefix(t, a)
	case kindPostfix:
		s, ok = m.postfix(t, a)
	default:
		s, ok = m.exact(t, a)
	}
	if !ok {
		m.indices = m.indices[:mark]
	}
	if a.negative {
		return 0, !ok
	}
	return s, ok
}

func (m *Matcher) fuzzy(t Text, a *atom) (uint16, bool) {
	n, h := len(a.needle), t.Len()
	switch {
	case n > h:
		return 0, false
	case n == h:
		return m.exactRange(t, a, 0, h)
	case t.isASCII() && !a.ascii:
		return 0, false // an ASCII text never matches a non-ASCII word
	case t.isASCII():
		if n == 1 {
			return m.substring1ASCII(t.ascii, byte(a.needle[0]))
		}
		start, greedyEnd, end, ok := prefilterASCII(t.ascii, a.needle, false)
		if !ok {
			return 0, false
		}
		if n == end-start {
			return calculateScore(m, t.ascii, a.needle, start, greedyEnd), true
		}
		return optimal(m, t.ascii, a.needle, start, greedyEnd, end, true)
	}
	if n == 1 {
		start, _, ok := m.prefilterRunes(t.runes, a.needle, true)
		if !ok {
			return 0, false
		}
		return m.substring1Runes(t.runes, a.needle[0], start), true
	}
	start, end, ok := m.prefilterRunes(t.runes, a.needle, false)
	if !ok {
		return 0, false
	}
	if n == end-start {
		return m.exactRange(t, a, start, end)
	}
	return optimal(m, t.runes, a.needle, start, start+1, end, false)
}

// prefilterASCII finds the first place needle can start, where its greedy
// match ends, and (unless onlyGreedy) where its last character last
// occurs; ok is false when it cannot match.
func prefilterASCII(hay []byte, needle []rune, onlyGreedy bool) (start, greedyEnd, end int, ok bool) {
	start = indexFolded(hay[:len(hay)-len(needle)+1], byte(needle[0]))
	if start < 0 {
		return 0, 0, 0, false
	}
	greedyEnd = start + 1
	for _, c := range needle[1:] {
		i := indexFolded(hay[greedyEnd:], byte(c))
		if i < 0 {
			return 0, 0, 0, false
		}
		greedyEnd += i + 1
	}
	if onlyGreedy {
		return start, greedyEnd, greedyEnd, true
	}
	return start, greedyEnd, greedyEnd + lastIndexFolded(hay[greedyEnd:], byte(needle[len(needle)-1])) + 1, true
}

// indexFolded finds c (lower case) in hay, either case of a letter.
func indexFolded(hay []byte, c byte) int {
	i := bytes.IndexByte(hay, c)
	if c < 'a' || c > 'z' {
		return i
	}
	upper := hay
	if i >= 0 {
		upper = hay[:i]
	}
	if j := bytes.IndexByte(upper, c-('a'-'A')); j >= 0 {
		return j
	}
	return i
}

func lastIndexFolded(hay []byte, c byte) int {
	i := bytes.LastIndexByte(hay, c)
	if c < 'a' || c > 'z' {
		return i
	}
	if j := bytes.LastIndexByte(hay[i+1:], c-('a'-'A')); j >= 0 {
		return i + 1 + j
	}
	return i
}

// prefilterRunes finds the first place needle can start and (unless
// onlyGreedy) the end of the last occurrence of its last character.
func (m *Matcher) prefilterRunes(hay []rune, needle []rune, onlyGreedy bool) (start, end int, ok bool) {
	n := len(needle)
	start = -1
	for i, c := range hay[:len(hay)-n+1] {
		if m.folded(c) == needle[0] {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	if onlyGreedy {
		return start, start + 1, len(hay)-start >= n
	}
	for end = len(hay); end > start+1; end-- {
		if m.folded(hay[end-1]) == needle[n-1] {
			return start, end, end-start >= n
		}
	}
	return 0, 0, false
}

// substring1ASCII scores the best place of one character: the first with
// the highest bonus.
func (m *Matcher) substring1ASCII(hay []byte, c byte) (uint16, bool) {
	best, at := uint16(0), 0
	for i := indexFolded(hay, c); i >= 0; {
		bonus := bonusFor(classAt(hay, i-1), asciiClass[hay[i]])
		if s := bonus*bonusFirstCharMul + scoreMatch; s > best {
			best, at = s, i
			if bonus >= bonusWhite {
				break
			}
		}
		next := indexFolded(hay[i+1:], c)
		if next < 0 {
			break
		}
		i += 1 + next
	}
	if best == 0 {
		return 0, false
	}
	if m.wantIndices {
		m.indices = append(m.indices, uint32(at))
	}
	return best, true
}

// substring1Runes is substring1ASCII for other texts, from start on.
func (m *Matcher) substring1Runes(hay []rune, c rune, start int) uint16 {
	best, at := uint16(0), 0
	prev := classAt(hay, start-1)
	for i := start; i < len(hay); i++ {
		r, cls := m.classAndFold(hay[i])
		bonus := bonusFor(prev, cls)
		prev = cls
		if r != c {
			continue
		}
		if s := bonus*bonusFirstCharMul + scoreMatch; s > best {
			best, at = s, i-start
			if bonus >= bonusWhite {
				break
			}
		}
	}
	if m.wantIndices {
		m.indices = append(m.indices, uint32(at+start))
	}
	return best
}

func (m *Matcher) substring(t Text, a *atom) (uint16, bool) {
	n, h := len(a.needle), t.Len()
	switch {
	case n > h:
		return 0, false
	case n == h:
		return m.exactRange(t, a, 0, h)
	case t.isASCII() && !a.ascii:
		return 0, false
	case t.isASCII() && n == 1:
		return m.substring1ASCII(t.ascii, byte(a.needle[0]))
	case t.isASCII():
		return m.substringASCII(t.ascii, a.needle)
	case n == 1:
		start, _, ok := m.prefilterRunes(t.runes, a.needle, true)
		if !ok {
			return 0, false
		}
		return m.substring1Runes(t.runes, a.needle[0], start), true
	}
	start, _, ok := m.prefilterRunes(t.runes, a.needle, false)
	if !ok {
		return 0, false
	}
	return m.substringRunes(t.runes, a.needle, start)
}

// substringASCII scores the best place where needle occurs as a whole:
// the first with the highest bonus.
func (m *Matcher) substringASCII(hay []byte, needle []rune) (uint16, bool) {
	best, at := uint16(0), 0
	for i := 0; i+len(needle) <= len(hay); i++ {
		if !equalFoldASCII(hay[i:i+len(needle)], needle) {
			continue
		}
		bonus := bonusFor(classAt(hay, i-1), asciiClass[hay[i]])
		if s := bonus*bonusFirstCharMul + scoreMatch; s > best {
			best, at = s, i
			if bonus >= bonusWhite {
				break
			}
		}
	}
	if best == 0 {
		return 0, false
	}
	return calculateScore(m, hay, needle, at, at+len(needle)), true
}

func equalFoldASCII(hay []byte, needle []rune) bool {
	for i, b := range hay {
		if rune(lowerASCII(b)) != needle[i] {
			return false
		}
	}
	return true
}

// substringRunes is substringASCII for other texts, from start on.
func (m *Matcher) substringRunes(hay []rune, needle []rune, start int) (uint16, bool) {
	best, at := uint16(0), 0
	prev := classAt(hay, start-1)
	for i := start; i+len(needle) <= len(hay); i++ {
		r, cls := m.classAndFold(hay[i])
		bonus := bonusFor(prev, cls)
		prev = cls
		if r != needle[0] {
			continue
		}
		if s := bonus*bonusFirstCharMul + scoreMatch; s > best && m.equalFolded(hay[i+1:i+len(needle)], needle[1:]) {
			best, at = s, i
			if bonus >= bonusWhite {
				break
			}
		}
	}
	if best == 0 {
		return 0, false
	}
	return calculateScore(m, hay, needle, at, at+len(needle)), true
}

func (m *Matcher) equalFolded(hay []rune, needle []rune) bool {
	for i, c := range hay {
		if m.folded(c) != needle[i] {
			return false
		}
	}
	return true
}
