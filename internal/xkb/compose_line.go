package xkb

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// errSkip marks a line libxkbcommon skips without counting it as an error.
var errSkip = errors.New("skipped")

// cscan scans one line of a Compose file.
type cscan struct {
	s   string
	pos int
}

func (c *cscan) space() {
	for c.pos < len(c.s) && (c.s[c.pos] == ' ' || c.s[c.pos] == '\t' || c.s[c.pos] == '\r') {
		c.pos++
	}
}

func (c *cscan) eol() bool {
	c.space()
	return c.pos >= len(c.s) || c.s[c.pos] == '#'
}

func (c *cscan) ident() string {
	start := c.pos
	for c.pos < len(c.s) && (isIdentStart(c.s[c.pos]) || c.s[c.pos] >= '0' && c.s[c.pos] <= '9') {
		c.pos++
	}
	return c.s[start:c.pos]
}

// str reads a "..." literal with the XCompose escapes.
func (c *cscan) str() (string, error) {
	c.pos++ // the opening quote
	var b []byte
	for c.pos < len(c.s) && c.s[c.pos] != '"' {
		ch := c.s[c.pos]
		c.pos++
		if ch != '\\' {
			b = append(b, ch)
			continue
		}
		if c.pos >= len(c.s) {
			break
		}
		b = c.escape(b)
	}
	if c.pos >= len(c.s) {
		return "", fmt.Errorf("unterminated string literal")
	}
	c.pos++
	if !utf8.Valid(b) {
		return "", fmt.Errorf("string literal is not valid UTF-8")
	}
	return string(b), nil
}

// escape appends the escape after a backslash: \\ \" \xHH \OOO; an
// unknown escape is dropped, and a zero byte is never written.
func (c *cscan) escape(b []byte) []byte {
	switch e := c.s[c.pos]; {
	case e == '\\' || e == '"':
		c.pos++
		return append(b, e)
	case e == 'x' || e == 'X':
		c.pos++
		j := c.pos
		for j < len(c.s) && j < c.pos+2 && isHexDigit(c.s[j]) {
			j++
		}
		if v, err := strconv.ParseUint(c.s[c.pos:j], 16, 8); err == nil && v != 0 {
			b = append(b, byte(v))
		}
		c.pos = j
	case e >= '0' && e <= '7':
		j := c.pos
		for j < len(c.s) && j < c.pos+3 && c.s[j] >= '0' && c.s[j] <= '7' {
			j++
		}
		if v, err := strconv.ParseUint(c.s[c.pos:j], 8, 16); err == nil && v != 0 && v < 256 {
			b = append(b, byte(v))
		}
		c.pos = j
	}
	return b
}

var composeMods = map[string]bool{"Ctrl": true, "Lock": true, "Caps": true, "Shift": true, "Alt": true, "Meta": true}

// line parses one line: an include, a sequence, or nothing.
func (t *ComposeTable) line(line string, depth int) error {
	c := &cscan{s: line}
	if c.eol() {
		return nil
	}
	if strings.HasPrefix(c.s[c.pos:], "include") {
		c.pos += len("include")
		c.space()
		if c.pos >= len(c.s) || c.s[c.pos] != '"' {
			return fmt.Errorf("include without a path")
		}
		p, err := c.str()
		if err != nil {
			return err
		}
		return t.include(p, depth)
	}
	lhs, err := c.sequence()
	if err != nil {
		return err
	}
	text, hasText, sym, hasSym, err := c.result()
	if err != nil {
		return err
	}
	if !hasText && !hasSym {
		return errSkip
	}
	t.add(lhs, text, hasText, sym)
	return nil
}

// sequence reads the left-hand side up to the colon.
func (c *cscan) sequence() ([]Keysym, error) {
	var lhs []Keysym
	for {
		c.space()
		if c.pos >= len(c.s) {
			return nil, fmt.Errorf("unexpected end of line")
		}
		ch := c.s[c.pos]
		switch {
		case ch == ':':
			c.pos++
			if len(lhs) == 0 {
				return nil, errSkip
			}
			return lhs, nil
		case ch == '<':
			end := strings.IndexByte(c.s[c.pos:], '>')
			if end < 0 {
				return nil, fmt.Errorf("unterminated keysym")
			}
			ks := KeysymFromName(c.s[c.pos+1 : c.pos+end])
			c.pos += end + 1
			if ks == NoSymbol {
				return nil, fmt.Errorf("unrecognized keysym on the left-hand side")
			}
			if len(lhs)+1 > composeMaxLHS {
				return nil, errSkip
			}
			lhs = append(lhs, ks)
		case ch == '!' || ch == '~':
			c.pos++
		case isIdentStart(ch):
			if id := c.ident(); id != "None" && !composeMods[id] {
				return nil, fmt.Errorf("unrecognized modifier %q", id)
			}
		default:
			return nil, fmt.Errorf("unexpected token")
		}
	}
}

// result reads the right-hand side: a string, a keysym, or both.
func (c *cscan) result() (text string, hasText bool, sym Keysym, hasSym bool, err error) {
	for !c.eol() {
		switch ch := c.s[c.pos]; {
		case ch == '"':
			if hasText {
				return "", false, 0, false, errSkip
			}
			s, err := c.str()
			if err != nil {
				return "", false, 0, false, err
			}
			if len(s) >= composeMaxString {
				return "", false, 0, false, errSkip
			}
			text, hasText = s, true
		case isIdentStart(ch):
			ks := KeysymFromName(c.ident())
			if ks == NoSymbol {
				return "", false, 0, false, fmt.Errorf("unrecognized keysym on the right-hand side")
			}
			if hasSym {
				return "", false, 0, false, errSkip
			}
			return text, hasText, ks, true, nil
		default:
			return "", false, 0, false, fmt.Errorf("unexpected token")
		}
	}
	return text, hasText, sym, hasSym, nil
}
