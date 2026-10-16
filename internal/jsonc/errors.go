package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Invalid is why a text was refused, and where: 1-based line and column,
// the column counted in characters and pointing at the offending character.
type Invalid struct {
	Line, Column int
	Message      string
}

func (e *Invalid) Error() string { return fmt.Sprintf("%d:%d: %s", e.Line, e.Column, e.Message) }

// Errorf is an Invalid about the byte at offset at of the parsed text.
func (d *Doc) Errorf(at int, format string, args ...any) *Invalid {
	at = max(0, min(at, len(d.text)))
	before := d.text[:at]
	start := bytes.LastIndexByte(before, '\n') + 1
	return &Invalid{
		Line:    bytes.Count(before, []byte("\n")) + 1,
		Column:  utf8.RuneCount(before[start:]) + 1,
		Message: fmt.Sprintf(format, args...),
	}
}

// offending is the offset of the byte err (from the decoder's last Token)
// is about: the token itself when there is no error, the end of the text
// when the text ended early.
func (p *parser) offending(err error) int {
	var syntax *json.SyntaxError
	switch {
	case err == nil:
		return p.end()
	case errors.As(err, &syntax) && !strings.Contains(syntax.Error(), "unexpected end"):
		return int(syntax.Offset) - 1
	}
	return len(p.doc.text) - 1
}

// place is what the parser read when the decoder failed.
type place int

const (
	atValue place = iota // a value
	atKey                // an object's next key, or its end
	atClose              // a container's closing bracket
)

// syntax words a decoder error as serde_json did for Rust logis v1.
func (p *parser) syntax(err error, at place) *Invalid {
	return p.doc.Errorf(p.offending(err), "%s", p.wording(err, at))
}

func (p *parser) wording(err error, at place) string {
	message := err.Error()
	has := func(s string) bool { return strings.Contains(message, s) }
	switch {
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || has("unexpected end"):
		return p.early()
	case has("numeric literal"):
		return "invalid number"
	case has("in literal"):
		return "expected ident"
	case has("escape"):
		return "invalid escape"
	case has("in string"):
		return "control character (\\u0000-\\u001F) found while parsing a string"
	case has("member name") || has("object key string") || at == atKey && has("beginning of"):
		return "key must be a string"
	case has("key:value pair") && at == atValue:
		return "expected value"
	case has("key:value pair"):
		return "expected `,` or `}`"
	case has("after object key"):
		return "expected `:`"
	case has("after array element"):
		return "expected `,` or `]`"
	case has("beginning of value"):
		return "expected value"
	}
	return message
}

// early words a text that ended before its document did: a value was
// awaited after ':' or ',', else the innermost open container was.
func (p *parser) early() string {
	plain, _ := Strip(p.doc.text)
	plain = bytes.TrimRight(plain, " \t\n\r\f")
	switch {
	case endsInString(p.doc.text):
		return "EOF while parsing a string"
	case len(p.depth) == 0 || bytes.HasSuffix(plain, []byte(":")) || bytes.HasSuffix(plain, []byte(",")):
		return "EOF while parsing a value"
	case p.depth[len(p.depth)-1] == Array:
		return "EOF while parsing a list"
	}
	return "EOF while parsing an object"
}

// endsInString says text ends inside an unterminated string.
func endsInString(text []byte) bool {
	plain, err := Strip(text)
	if err != nil {
		return false
	}
	for i := 0; i < len(plain); i++ {
		if plain[i] == '"' {
			end := stringEnd(plain, i+1)
			if end == len(plain) && (end == i+1 || plain[end-1] != '"' || escaped(plain, end-1)) {
				return true
			}
			i = end - 1
		}
	}
	return false
}

// escaped says the quote at i is escaped by an odd run of backslashes.
func escaped(text []byte, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && text[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// offset is where the unterminated comment begins in text.
func (e *UnterminatedError) offset(text []byte) int {
	at := 0
	for line := 1; line < e.Line; line++ {
		at += bytes.IndexByte(text[at:], '\n') + 1
	}
	return at + e.Column - 1
}
