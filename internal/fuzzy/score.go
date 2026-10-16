package fuzzy

// The scoring constants of logis's matcher (nucleo's defaults, its score
// module's Config::DEFAULT): a match scores scoreMatch plus its bonus.
const (
	scoreMatch        = 16
	penaltyGapStart   = 3
	penaltyGapExtend  = 1
	bonusBoundary     = scoreMatch / 2
	bonusWhite        = bonusBoundary + 2 // after whitespace or at the start
	bonusDelimiter    = bonusBoundary + 1 // after / , : ; |
	bonusCamel123     = bonusBoundary - penaltyGapStart
	bonusNonWord      = bonusBoundary
	bonusConsecutive  = penaltyGapStart + penaltyGapExtend
	bonusFirstCharMul = 2
	initialClass      = classWhitespace
)

// bonusFor is the bonus for matching a character of class cur after one of
// class prev: word starts, camelCase humps and digits after non-digits.
func bonusFor(prev, cur class) uint16 { return uint16(bonusTable[prev][cur]) }

var bonusTable = func() (t [classNumber + 1][classNumber + 1]uint8) {
	for prev := range t {
		for cur := range t[prev] {
			t[prev][cur] = uint8(computeBonus(class(prev), class(cur)))
		}
	}
	return t
}()

func computeBonus(prev, cur class) uint16 {
	if cur > classDelimiter {
		switch prev {
		case classWhitespace:
			return bonusWhite
		case classDelimiter:
			return bonusDelimiter
		case classNonWord:
			return bonusBoundary
		}
	}
	switch {
	case prev == classLower && cur == classUpper, prev != classNumber && cur == classNumber:
		return bonusCamel123
	case cur == classWhitespace:
		return bonusWhite
	case cur == classNonWord:
		return bonusNonWord
	}
	return 0
}

// char is a haystack element: a byte of an ASCII text or a rune of any
// other.
type char interface{ ~byte | ~rune }

// classAndFold is c's class and c as compared with the needle: case-folded,
// and accent-folded too when the word allows it (nucleo's
// char_class_and_normalize).
func (m *Matcher) classAndFold(r rune) (rune, class) {
	if r < 0x80 {
		cls := asciiClass[r]
		if cls == classUpper {
			r += 'a' - 'A'
		}
		return r, cls
	}
	return m.folded(r), classOf(r)
}

// folded is r as compared with the needle where nucleo ignores classes:
// always case-folded, and accent-folded when the word allows it.
func (m *Matcher) folded(r rune) rune {
	if r < 0x80 {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		return r
	}
	r = fold(r)
	if m.normalize {
		r = normalizeFolded(r)
	}
	return r
}

func classAt[H char](hay []H, i int) class {
	if i < 0 {
		return initialClass
	}
	return classOf(rune(hay[i]))
}

// calculateScore scores needle matched in hay[start:end]: the first
// character at start, the others wherever they come next (nucleo's
// calculate_score). Positions are appended when m.wantIndices.
func calculateScore[H char](m *Matcher, hay []H, needle []rune, start, end int) uint16 {
	if m.wantIndices {
		m.indices = append(m.indices, uint32(start))
	}
	prev := classAt(hay, start-1)
	cls := classOf(rune(hay[start]))
	firstBonus := bonusFor(prev, cls)
	score := uint16(scoreMatch + firstBonus*bonusFirstCharMul)
	prev = cls
	next := 1
	want := needle[min(next, len(needle)-1)]
	inGap, consecutive := false, 1
	for i := start + 1; i < end; i++ {
		c, cls := m.classAndFold(rune(hay[i]))
		if c == want {
			if m.wantIndices {
				m.indices = append(m.indices, uint32(i))
			}
			bonus := bonusFor(prev, cls)
			if consecutive != 0 {
				if bonus >= bonusBoundary && bonus > firstBonus {
					firstBonus = bonus
				}
				bonus = max(bonus, firstBonus, bonusConsecutive)
			} else {
				firstBonus = bonus
			}
			score += scoreMatch + bonus
			inGap = false
			consecutive++
			if next+1 < len(needle) {
				next++
				want = needle[next]
			}
		} else {
			penalty := uint16(penaltyGapStart)
			if inGap {
				penalty = penaltyGapExtend
			}
			score = satSub(score, penalty)
			inGap, consecutive = true, 0
		}
		prev = cls
	}
	return score
}

func satSub(a, b uint16) uint16 {
	if a < b {
		return 0
	}
	return a - b
}
