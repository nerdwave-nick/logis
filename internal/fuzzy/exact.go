package fuzzy

import "unicode"

// exact matches ^word$: the whole text, but for leading and trailing
// whitespace the word does not ask for.
func (m *Matcher) exact(t Text, a *atom) (uint16, bool) {
	lead, trail := 0, 0
	if !isSpace(a.needle[0]) {
		lead = leadingSpace(t)
	}
	if !isSpace(a.needle[len(a.needle)-1]) {
		trail = trailingSpace(t)
	}
	if trail == t.Len() {
		return 0, false
	}
	return m.exactRange(t, a, lead, t.Len()-trail)
}

// prefix matches ^word: the text's start, after whitespace the word does
// not ask for.
func (m *Matcher) prefix(t Text, a *atom) (uint16, bool) {
	lead := 0
	if !isSpace(a.needle[0]) {
		lead = leadingSpace(t)
	}
	if t.Len()-lead < len(a.needle) {
		return 0, false
	}
	return m.exactRange(t, a, lead, lead+len(a.needle))
}

// postfix matches word$: the text's end, before whitespace the word does
// not ask for.
func (m *Matcher) postfix(t Text, a *atom) (uint16, bool) {
	trail := 0
	if !isSpace(a.needle[len(a.needle)-1]) {
		trail = trailingSpace(t)
	}
	if t.Len()-trail < len(a.needle) {
		return 0, false
	}
	return m.exactRange(t, a, t.Len()-len(a.needle)-trail, t.Len()-trail)
}

// exactRange matches the word as exactly t[start:end].
func (m *Matcher) exactRange(t Text, a *atom, start, end int) (uint16, bool) {
	if len(a.needle) != end-start {
		return 0, false
	}
	if t.isASCII() {
		if !a.ascii || !equalFoldASCII(t.ascii[start:end], a.needle) {
			return 0, false
		}
		return calculateScore(m, t.ascii, a.needle, start, end), true
	}
	for i, c := range t.runes[start:end] {
		if m.folded(c) != m.folded(a.needle[i]) {
			return 0, false
		}
	}
	return calculateScore(m, t.runes, a.needle, start, end), true
}

// leadingSpace counts the whitespace t starts with (0 when it is all
// whitespace, as nucleo counts).
func leadingSpace(t Text) int {
	for i := 0; i < t.Len(); i++ {
		if !spaceAt(t, i) {
			return i
		}
	}
	return 0
}

func trailingSpace(t Text) int {
	for i := t.Len() - 1; i >= 0; i-- {
		if !spaceAt(t, i) {
			return t.Len() - 1 - i
		}
	}
	return 0
}

func spaceAt(t Text, i int) bool {
	if t.isASCII() {
		return asciiClass[t.ascii[i]] == classWhitespace
	}
	return unicode.Is(unicode.White_Space, t.runes[i])
}
