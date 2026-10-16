package xkb

import (
	"strconv"
	"strings"
)

// The keymap text format (xkb_keymap, "text v1") as libxkbcommon
// serialises it: a lexer and a small generic parser (parse.go) producing
// statements with expressions. The compiler gives them meaning; what it
// does not need (actions, indicators, geometry) parses and is ignored.

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNum
	tStr
	tKeyName
	tPunct
)

type token struct {
	kind tokKind
	text string
	line int
}

// lexer scans tokens on demand: the parser needs one token of lookahead,
// so no token list is ever built. The first error ends the scan (later
// tokens are EOF) and is kept in err.
type lexer struct {
	src  string
	i    int
	line int
	err  error
}

func newLexer(src string) *lexer { return &lexer{src: src, line: 1} }

func (l *lexer) fail(e *Error) token {
	if l.err == nil {
		l.err = e
	}
	l.i = len(l.src)
	return token{tEOF, "", l.line}
}

// scan returns the next token.
func (l *lexer) scan() token {
	src := l.src
	for l.i < len(src) {
		i, c := l.i, src[l.i]
		switch {
		case c == '\n':
			l.line++
			l.i++
		case c == ' ' || c == '\t' || c == '\r' || c == 0:
			// NUL: the C string's terminator, as wl_keyboard sends it.
			l.i++
		case c == '#' || (c == '/' && i+1 < len(src) && src[i+1] == '/'):
			for l.i < len(src) && src[l.i] != '\n' {
				l.i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return l.fail(errorf(l.line, "unterminated comment"))
			}
			l.line += strings.Count(src[i:i+2+end], "\n")
			l.i = i + end + 4
		case c == '"':
			s, j, lines, ok := lexString(src, i+1)
			if !ok {
				return l.fail(errorf(l.line, "unterminated string"))
			}
			t := token{tStr, s, l.line}
			l.line += lines
			l.i = j + 1
			return t
		case c == '<':
			// libxkbcommon: printable ASCII up to the closing '>'.
			j := i + 1
			for j < len(src) && src[j] > ' ' && src[j] < 0x7f && src[j] != '>' {
				j++
			}
			if j >= len(src) || src[j] != '>' {
				return l.fail(errorf(l.line, "unterminated key name"))
			}
			l.i = j + 1
			return token{tKeyName, src[i+1 : j], l.line}
		case c >= '0' && c <= '9':
			l.i = lexNumber(src, i)
			return token{tNum, src[i:l.i], l.line}
		case isIdentStart(c):
			j := i
			for j < len(src) && (isIdentStart(src[j]) || src[j] >= '0' && src[j] <= '9') {
				j++
			}
			l.i = j
			return token{tIdent, src[i:j], l.line}
		default:
			l.i++
			return token{tPunct, src[i : i+1], l.line}
		}
	}
	return token{tEOF, "", l.line}
}

// lexString reads a string literal's body from src[i:] up to its closing
// quote at j, with libxkbcommon's escapes; lines counts the newlines in it.
func lexString(src string, i int) (s string, j, lines int, ok bool) {
	var b strings.Builder
	for j = i; j < len(src) && src[j] != '"'; j++ {
		if src[j] == '\\' && j+1 < len(src) {
			j++
			if e, ok := simpleEscape(src[j]); ok {
				b.WriteByte(e)
				continue
			}
			if src[j] >= '0' && src[j] <= '7' {
				k := j
				for k < len(src) && k < j+3 && src[k] >= '0' && src[k] <= '7' {
					k++
				}
				v, _ := strconv.ParseUint(src[j:k], 8, 16)
				b.WriteByte(byte(v))
				j = k - 1
				continue
			}
			b.WriteByte(src[j])
			continue
		}
		if src[j] == '\n' {
			lines++
		}
		b.WriteByte(src[j])
	}
	if j >= len(src) {
		return "", j, lines, false
	}
	return b.String(), j, lines, true
}

func simpleEscape(e byte) (byte, bool) {
	switch e {
	case 'n':
		return '\n', true
	case 't':
		return '\t', true
	case 'r':
		return '\r', true
	case 'b':
		return '\b', true
	case 'f':
		return '\f', true
	case 'v':
		return '\v', true
	case 'e':
		return 0x1b, true
	}
	return 0, false
}

// lexNumber is the end of the number starting at src[i]: hex (0x…) or
// decimal (with a fraction, which only geometry uses).
func lexNumber(src string, i int) int {
	j := i
	if src[i] == '0' && i+1 < len(src) && (src[i+1] == 'x' || src[i+1] == 'X') {
		j += 2
		for j < len(src) && isHexDigit(src[j]) {
			j++
		}
		return j
	}
	for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
		j++
	}
	return j
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
