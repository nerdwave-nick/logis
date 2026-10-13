package fuzzy

// The optimal fuzzy match: nucleo's dynamic program (fuzzy_optimal.rs).
// Row k holds the best scores of the needle's first k+1 characters with
// character k matched at each column (M) and of a gap after them (P); rows
// start at the greedy position of their character, as no match can place
// it earlier. Ties prefer the gap (P) and, for the end, the last column.

// The scratch limits of nucleo's matcher: past them it falls back to the
// greedy match (fuzzy_greedy.rs), which scores differently.
const (
	maxMatrixCells = 100 * 1024
	maxNeedle      = 2048
	matcherData    = 2048*4 + 2048 + 2048*2 + 2048*8 + maxMatrixCells
)

// cell is one M entry, packed: its score (bits 0–15), the consecutive
// bonus carried into it (16–23), and whether it continues a match in the
// previous row's previous column (bit 24).
type cell uint32

func newCell(score, consecutive uint16, matched bool) cell {
	c := cell(score) | cell(consecutive)<<16
	if matched {
		c |= 1 << 24
	}
	return c
}

func (c cell) score() uint16       { return uint16(c) }
func (c cell) consecutive() uint16 { return uint16(c>>16) & 0xFF }
func (c cell) matched() bool       { return c&(1<<24) != 0 }

// unmatched marks a column where the row's character is not; nucleo's
// sentinel (score 0, matched), which a real cell never equals.
const unmatched cell = 1 << 24

type dpScratch struct {
	hay        []rune
	bonus      []uint8
	rowOff     []int
	cur, next  []cell
	matrix     []uint8 // per row and column: 1 P came from M, 2 M continued
	matrixRows []int   // where each row starts in matrix
}

// fitsMatrix is nucleo's test whether its scratch holds the window.
func fitsMatrix(window, n int, asciiText bool) bool {
	if window*n > maxMatrixCells || window > 0xFFFF || n > maxNeedle {
		return false
	}
	charSize := 4
	if asciiText {
		charSize = 1
	}
	size := window*charSize + window
	size = (size+1)&^1 + 2*n
	size = (size+7)&^7 + 8*(window+1-n) + (window+1-n)*n
	return size <= matcherData
}

func optimal[H char](m *Matcher, hay []H, needle []rune, start, greedyEnd, end int, asciiText bool) (uint16, bool) {
	w, n := end-start, len(needle)
	if !fitsMatrix(w, n, asciiText) {
		return greedy(m, hay, needle, start, greedyEnd, asciiText)
	}
	d := &m.dp
	d.hay, d.bonus = resize(d.hay, w), resize(d.bonus, w)
	d.rowOff = resize(d.rowOff, n)
	prev := classAt(hay, start-1)
	k, found := 0, false
	for j, h := range hay[start:end] {
		c := rune(h)
		var cls class
		if c < 0x80 {
			if cls = asciiClass[c]; cls == classUpper {
				c += 'a' - 'A'
			}
		} else {
			c, cls = m.classAndFold(c)
		}
		d.hay[j], d.bonus[j] = c, bonusTable[prev][cls]
		prev = cls
		if c != needle[k] {
			continue
		}
		if k < n-1 {
			d.rowOff[k] = j
			k++
		} else if !found {
			d.rowOff[k], found = j, true
		}
	}
	if !found {
		return 0, false
	}
	d.rowOff[0] = 0 // row 0 is scored from the window's start (nucleo too)
	d.cur, d.next = resize(d.cur, w), resize(d.next, w)
	if m.wantIndices {
		d.matrixRows = resize(d.matrixRows, n-1)
		size := 0
		for k := range n - 1 {
			d.matrixRows[k] = size - d.rowOff[k]
			size += w - n + k + 1 - d.rowOff[k]
		}
		d.matrix = resize(d.matrix, size)
	}
	for k := range n - 1 {
		d.row(k, needle, m.wantIndices)
		d.cur, d.next = d.next, d.cur
	}
	best := d.rowOff[n-1]
	for col := best + 1; col < w; col++ {
		if d.cur[col].score() >= d.cur[best].score() {
			best = col
		}
	}
	if m.wantIndices {
		d.path(m, best, start)
	}
	return d.cur[best].score(), true
}

// row scores row k+1 (into next) from row k (in cur; for k 0 computed
// here). Columns before the next row's start only carry the gap score on.
func (d *dpScratch) row(k int, needle []rune, keep bool) {
	n, w := len(needle), len(d.hay)
	from, split, to := d.rowOff[k], d.rowOff[k+1]-1, w-n+k
	first, want := needle[0], needle[k+1]
	hay, bonus, cur, next := d.hay[:w], d.bonus[:w], d.cur[:w], d.next[:w]
	var matrix []uint8
	if keep {
		matrix = d.matrix[d.matrixRows[k]+from : d.matrixRows[k]+to+1]
	}
	var prevP, prevM uint16
	for col := from; col <= to; col++ {
		p, pFromM := gap(prevP, prevM)
		var mc cell
		switch {
		case k > 0:
			mc = cur[col]
		case hay[col] == first:
			b := uint16(bonus[col])
			mc = newCell(b*bonusFirstCharMul+scoreMatch, b, false)
		default:
			mc = unmatched
		}
		if col >= split {
			nc := unmatched
			if hay[col+1] == want {
				nc = extend(p, uint16(bonus[col+1]), mc)
			}
			next[col+1] = nc
		}
		if keep {
			v := uint8(mc>>23) & 2
			if pFromM {
				v |= 1
			}
			matrix[col-from] = v
		}
		prevP, prevM = p, mc.score()
	}
}

// gap is the best score of a gap at a column: opening it after a match or
// extending the previous gap (ties extend).
func gap(prevP, prevM uint16) (uint16, bool) {
	open, extend := satSub(prevM, penaltyGapStart), satSub(prevP, penaltyGapExtend)
	if open > extend {
		return open, true
	}
	return extend, false
}

// extend is the M cell for matching the next character at a column, after
// a gap scoring p or right after the match m (ties take the gap).
func extend(p, bonus uint16, m cell) cell {
	if m == unmatched {
		return newCell(p+bonus+scoreMatch, bonus, false)
	}
	consecutive := max(m.consecutive(), bonusConsecutive)
	if bonus >= bonusBoundary && bonus > consecutive {
		consecutive = bonus
	}
	continued, skipped := m.score()+max(consecutive, bonus), p+bonus
	if continued > skipped {
		return newCell(continued+scoreMatch, consecutive, true)
	}
	return newCell(skipped+scoreMatch, bonus, false)
}

// path walks back from the best end to the positions of the match.
func (d *dpScratch) path(m *Matcher, col, start int) {
	n := len(d.rowOff)
	base := len(m.indices)
	m.indices = append(m.indices, make([]uint32, n)...)
	pos := m.indices[base:]
	pos[n-1] = uint32(start + col)
	matched := d.cur[col].matched()
	row := n - 2
	col--
	for {
		if matched {
			pos[row] = uint32(start + col)
		}
		v := d.matrix[d.matrixRows[row]+col]
		next := v&1 != 0
		if matched {
			next = v&2 != 0
			if row == 0 {
				return
			}
			row--
		}
		col--
		matched = next
	}
}

// greedy is nucleo's fallback past the scratch limits: the first match
// found left to right, shortened from its end, then scored.
func greedy[H char](m *Matcher, hay []H, needle []rune, start, end int, asciiText bool) (uint16, bool) {
	if !asciiText {
		from, k := end, 1
		end = -1
		for i := from; i < len(hay) && k < len(needle); i++ {
			if m.folded(rune(hay[i])) == needle[k] {
				k++
				if k == len(needle) {
					end = i + 1
				}
			}
		}
		if end < 0 {
			return 0, false
		}
	}
	k := len(needle) - 1
	for i := end - 1; i >= start; i-- {
		if m.folded(rune(hay[i])) == needle[k] {
			if k == 0 {
				start = i
				break
			}
			k--
		}
	}
	return calculateScore(m, hay, needle, start, end), true
}

func resize[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
}
