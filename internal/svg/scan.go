package svg

import "strconv"

// scanner reads SVG's number grammar from attribute text: numbers may run
// together ("1-2.5.5" is 1, -2.5, .5) and are separated by whitespace or
// one comma.
type scanner struct {
	s string
	i int
}

func (sc *scanner) done() bool { return sc.i >= len(sc.s) }

func (sc *scanner) peek() byte {
	if sc.done() {
		return 0
	}
	return sc.s[sc.i]
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func (sc *scanner) skipSpace() {
	for !sc.done() && isSpace(sc.s[sc.i]) {
		sc.i++
	}
}

// skipSeparators skips whitespace and at most one comma.
func (sc *scanner) skipSeparators() {
	sc.skipSpace()
	if sc.peek() == ',' {
		sc.i++
		sc.skipSpace()
	}
}

func (sc *scanner) eat(c byte) bool {
	if sc.peek() == c {
		sc.i++
		return true
	}
	return false
}

// ident reads letters (a function or command name).
func (sc *scanner) ident() string {
	start := sc.i
	for !sc.done() {
		c := sc.s[sc.i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			break
		}
		sc.i++
	}
	return sc.s[start:sc.i]
}

// number reads one number after optional whitespace.
func (sc *scanner) number() (float64, bool) {
	sc.skipSpace()
	start := sc.i
	if c := sc.peek(); c == '+' || c == '-' {
		sc.i++
	}
	digits := sc.digits()
	if sc.peek() == '.' {
		sc.i++
		digits += sc.digits()
	}
	if digits == 0 {
		sc.i = start
		return 0, false
	}
	if c := sc.peek(); c == 'e' || c == 'E' {
		mark := sc.i
		sc.i++
		if c := sc.peek(); c == '+' || c == '-' {
			sc.i++
		}
		if sc.digits() == 0 {
			sc.i = mark // "1em": the e belongs to a unit
		}
	}
	v, err := strconv.ParseFloat(sc.s[start:sc.i], 64)
	if err != nil {
		sc.i = start
		return 0, false
	}
	return v, true
}

func (sc *scanner) digits() int {
	n := 0
	for !sc.done() && sc.s[sc.i] >= '0' && sc.s[sc.i] <= '9' {
		sc.i++
		n++
	}
	return n
}

// flag reads an arc flag: a single 0 or 1, which may touch what follows.
func (sc *scanner) flag() (bool, bool) {
	sc.skipSeparators()
	switch sc.peek() {
	case '0':
		sc.i++
		return false, true
	case '1':
		sc.i++
		return true, true
	}
	return false, false
}

// numbers parses a list of numbers ("1 2,3"); !ok if anything else is in
// it.
func numbers(s string) ([]float64, bool) {
	sc := scanner{s: s}
	var out []float64
	for {
		sc.skipSpace()
		if sc.done() {
			return out, true
		}
		v, ok := sc.number()
		if !ok {
			return out, false
		}
		out = append(out, v)
		sc.skipSeparators()
	}
}
