package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Kind is the kind of a JSON value.
type Kind int

const (
	Null Kind = iota
	Bool
	Number
	String
	Array
	Object
)

// Value is one JSON value and where it ends in the text.
type Value struct {
	Kind   Kind
	Bool   bool
	Number json.Number
	String string
	// Elems are an array's values; Members an object's, in text order,
	// repeated keys included.
	Elems   []*Value
	Members []Member
	// End is the offset of the value's last byte: a scalar's last
	// character, a container's closing bracket. Errors about the value
	// point there (serde_json's convention, which Rust logis v1 reported).
	End int
}

// Member is one key of an object and its value.
type Member struct {
	Key string
	// KeyEnd is the offset of the key's closing quote.
	KeyEnd int
	Value  *Value
}

// Doc is a parsed text; it locates errors in it.
type Doc struct {
	text []byte
	// Root is the document's object; nil when the text holds only
	// whitespace and comments.
	Root *Value
}

// Parse reads a JSONC object; what names it in errors ("config", "theme").
// Every error is an *Invalid that points into text.
func Parse(text []byte, what string) (*Doc, error) {
	doc := &Doc{text: text}
	plain, err := Strip(text)
	if err != nil {
		var u *UnterminatedError
		errors.As(err, &u)
		return nil, doc.Errorf(u.offset(text), "this comment never closes: '/*' wants its '*/'")
	}
	start := bytes.IndexFunc(plain, func(r rune) bool { return !isSpace(r) })
	if start < 0 {
		return doc, nil
	}
	if plain[start] != '{' {
		return nil, doc.Errorf(start, "the %s must be a JSON object: { \"key\": value, … }", what)
	}
	p := &parser{doc: doc, dec: newDecoder(plain)}
	if doc.Root, err = p.value(); err != nil {
		return nil, err
	}
	if _, err := p.dec.Token(); err != io.EOF {
		return nil, doc.Errorf(p.offending(err), "trailing characters")
	}
	return doc, nil
}

func newDecoder(plain []byte) *json.Decoder {
	dec := json.NewDecoder(bytes.NewReader(plain))
	dec.UseNumber()
	return dec
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }

// parser builds Values from the decoder's tokens. depth holds the open
// containers, for the wording of an early end.
type parser struct {
	doc   *Doc
	dec   *json.Decoder
	depth []Kind
}

// end is the offset of the last byte of the token just read.
func (p *parser) end() int { return int(p.dec.InputOffset()) - 1 }

// value reads one value.
func (p *parser) value() (*Value, error) {
	tok, err := p.dec.Token()
	if err != nil {
		return nil, p.syntax(err, atValue)
	}
	v := &Value{End: p.end()}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			return p.object(v)
		}
		return p.array(v)
	case bool:
		v.Kind, v.Bool = Bool, t
	case json.Number:
		v.Kind, v.Number = Number, t
	case string:
		v.Kind, v.String = String, t
	case nil:
		v.Kind = Null
	}
	return v, nil
}

func (p *parser) object(v *Value) (*Value, error) {
	v.Kind = Object
	p.depth = append(p.depth, Object)
	for p.dec.More() {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, p.syntax(err, atKey)
		}
		key, ok := tok.(string)
		if !ok {
			return nil, p.doc.Errorf(p.end(), "key must be a string")
		}
		m := Member{Key: key, KeyEnd: p.end()}
		if m.Value, err = p.value(); err != nil {
			return nil, err
		}
		v.Members = append(v.Members, m)
	}
	return p.close(v)
}

func (p *parser) array(v *Value) (*Value, error) {
	v.Kind = Array
	p.depth = append(p.depth, Array)
	for p.dec.More() {
		elem, err := p.value()
		if err != nil {
			return nil, err
		}
		v.Elems = append(v.Elems, elem)
	}
	return p.close(v)
}

// close reads the closing bracket that More saw (or the error in its place).
func (p *parser) close(v *Value) (*Value, error) {
	if _, err := p.dec.Token(); err != nil {
		return nil, p.syntax(err, atClose)
	}
	p.depth = p.depth[:len(p.depth)-1]
	v.End = p.end()
	return v, nil
}

// Unexpected names the value as serde did in "invalid type" and "invalid
// value" messages.
func (v *Value) Unexpected() string {
	switch v.Kind {
	case Bool:
		return fmt.Sprintf("boolean `%t`", v.Bool)
	case Number:
		if v.IsInteger() {
			return fmt.Sprintf("integer `%s`", v.Number)
		}
		return fmt.Sprintf("floating point `%s`", v.Number)
	case String:
		return "string " + strconv.Quote(v.String)
	case Array:
		return "sequence"
	case Object:
		return "map"
	}
	return "null"
}

// IsInteger says a number was written without a fraction or exponent.
func (v *Value) IsInteger() bool {
	return v.Kind == Number && !strings.ContainsAny(string(v.Number), ".eE")
}
