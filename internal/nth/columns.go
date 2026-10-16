package nth

import "unicode/utf8"

// Columns are the column runes of one dmenu session: the selections of
// --with-nth (the shown text), --match-nth (the matched text) and
// --accept-nth (what a chosen item prints), nil where not given, and the
// --nth-delimiter they split lines at.
type Columns struct {
	With, Match, Accept *Nth
	Delimiter           byte
}

// Shown is the text a line is shown as: its --with-nth columns, or the
// whole line.
func (c Columns) Shown(raw []byte) string {
	if c.With == nil {
		return Lossy(raw)
	}
	return Lossy(c.With.Apply(raw, c.Delimiter))
}

// Matched is the text a line is matched by when --match-nth was given;
// ok is false otherwise (the line is then matched as shown).
func (c Columns) Matched(raw []byte) (text string, ok bool) {
	if c.Match == nil {
		return "", false
	}
	return Lossy(c.Match.Apply(raw, c.Delimiter)), true
}

// Accepted is what a chosen line prints: its --accept-nth columns, or the
// line exactly as read.
func (c Columns) Accepted(raw []byte) []byte {
	if c.Accept == nil {
		return raw
	}
	return c.Accept.Apply(raw, c.Delimiter)
}

// Lossy is b as text, every maximal invalid UTF-8 subpart replaced by one
// U+FFFD (Unicode §3.9, "U+FFFD substitution of maximal subparts"). Go's
// own conversions replace every invalid byte instead.
func Lossy(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	out := make([]byte, 0, len(b)+8)
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		if r != utf8.RuneError || n > 1 {
			out = append(out, b[:n]...)
			b = b[n:]
			continue
		}
		out = utf8.AppendRune(out, utf8.RuneError)
		b = b[maximalSubpart(b):]
	}
	return string(out)
}

// maximalSubpart is the length of the invalid sequence at the start of b:
// a lead byte and the continuation bytes that could still have completed
// it (Unicode Table 3-7), at least 1.
func maximalSubpart(b []byte) int {
	var need int
	lo, hi := byte(0x80), byte(0xBF)
	switch c := b[0]; {
	case c >= 0xC2 && c <= 0xDF:
		need = 2
	case c >= 0xE0 && c <= 0xEF:
		need = 3
		switch c {
		case 0xE0:
			lo = 0xA0
		case 0xED:
			hi = 0x9F
		}
	case c >= 0xF0 && c <= 0xF4:
		need = 4
		switch c {
		case 0xF0:
			lo = 0x90
		case 0xF4:
			hi = 0x8F
		}
	default:
		return 1
	}
	n := 1
	for n < need && n < len(b) && b[n] >= lo && b[n] <= hi {
		n++
		lo, hi = 0x80, 0xBF
	}
	return n
}
