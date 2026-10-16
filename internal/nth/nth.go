// Package nth selects columns of dmenu input lines (--with-nth,
// --accept-nth, --match-nth, --nth-delimiter): a line is split into columns
// at the delimiter (tab by default), and a selection is either a column
// number (1-based) or a format string in which {N}, {N..M} and {N..} expand
// to columns. Ranges are joined by the delimiter. Everything works on
// bytes, so the output keeps the input's bytes exactly.
package nth

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
)

// piece is one part of a selection: literal text, or the columns from
// through to (inclusive, 1-based; to 0 = the last).
type piece struct {
	literal  []byte
	from, to int
}

// Nth is a parsed --*-nth selection.
type Nth struct {
	pieces []piece
}

// Parse parses a selection. "0" means none (nil, nil), as if the flag were
// not given. The error's text is the reason.
func Parse(text string) (*Nth, error) {
	if text == "0" {
		return nil, nil
	}
	if n, ok := number(strings.TrimPrefix(text, "+")); ok {
		p, err := columns(n, n, text)
		if err != nil {
			return nil, err
		}
		return &Nth{pieces: []piece{p}}, nil
	}
	var pieces []piece
	var literal []byte
	rest := text
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		literal = append(literal, rest[:open]...)
		after := rest[open+1:]
		end := strings.IndexByte(after, '}')
		var p piece
		var isRef bool
		if end >= 0 {
			var err error
			p, isRef, err = reference(after[:end], text)
			if err != nil {
				return nil, err
			}
		}
		if !isRef {
			literal = append(literal, '{')
			rest = after
			continue
		}
		if len(literal) > 0 {
			pieces = append(pieces, piece{literal: literal})
			literal = nil
		}
		pieces = append(pieces, p)
		rest = after[end+1:]
	}
	literal = append(literal, rest...)
	if len(literal) > 0 {
		pieces = append(pieces, piece{literal: literal})
	}
	for _, p := range pieces {
		if p.literal == nil {
			return &Nth{pieces: pieces}, nil
		}
	}
	return nil, errors.New("'" + text + "' names no column: it wants a column number or a format with {N}, {N..M} or {N..}")
}

// Apply returns the selection applied to line, its columns split at
// delimiter. Columns past the line's last are empty.
func (n *Nth) Apply(line []byte, delimiter byte) []byte {
	cols := bytes.Split(line, []byte{delimiter})
	var out []byte
	for _, p := range n.pieces {
		if p.literal != nil {
			out = append(out, p.literal...)
			continue
		}
		last := len(cols)
		if p.to != 0 && p.to < last {
			last = p.to
		}
		if p.from-1 >= last {
			continue
		}
		out = append(out, bytes.Join(cols[p.from-1:last], []byte{delimiter})...)
	}
	if out == nil {
		out = []byte{}
	}
	return out
}

// number parses digits only (no sign).
func number(s string) (int, bool) {
	if s == "" || strings.TrimLeft(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// reference reads N, N..M or N.. inside braces; isRef is false when inner
// is no column reference at all (the braces are then literal text).
func reference(inner, text string) (p piece, isRef bool, err error) {
	from, to, ranged := strings.Cut(inner, "..")
	n, ok := number(from)
	if !ok {
		return piece{}, false, nil
	}
	switch {
	case !ranged:
		p, err = columns(n, n, text)
	case to == "":
		p, err = columns(n, 0, text)
	default:
		m, ok := number(to)
		if !ok {
			return piece{}, false, nil
		}
		if m == 0 { // an explicit end of 0 runs backwards (to 0 is open-ended)
			m = -1
		}
		p, err = columns(n, m, text)
	}
	return p, err == nil, err
}

// columns refuses columns counted from 0 and ranges that run backwards;
// to 0 means through the last column.
func columns(from, to int, text string) (piece, error) {
	if from == 0 || (to != 0 && to < from) {
		return piece{}, errors.New("'" + text + "' is no column: columns are counted from 1, and a range runs forwards")
	}
	return piece{from: from, to: to}, nil
}

// Delimiter parses --nth-delimiter: exactly one ASCII character.
func Delimiter(text string) (byte, bool) {
	if len(text) != 1 || text[0] >= 0x80 {
		return 0, false
	}
	return text[0], true
}
