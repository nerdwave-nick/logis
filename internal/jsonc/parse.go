package jsonc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
	// point there (serde_json's convention).
	End int
	// Start is the offset of the value's first byte.
	Start int
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
	// plain is text with its comments, BOM and trailing commas blanked.
	plain []byte
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
	doc.plain = plain
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
	// Between the last token and this one lie only blanks, ':' and ','.
	start := int(p.dec.InputOffset())
	for start < len(p.doc.plain) && strings.IndexByte(" \t\n\r\f:,", p.doc.plain[start]) >= 0 {
		start++
	}
	tok, err := p.dec.Token()
	if err != nil {
		return nil, p.syntax(err, atValue)
	}
	v := &Value{Start: start, End: p.end()}
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
		return number(string(v.Number))
	case String:
		return "string " + strconv.Quote(v.String)
	case Array:
		return "sequence"
	case Object:
		return "map"
	}
	return "null"
}

// number names a JSON number as serde_json parsed it: an integer while it
// fits u64 (or i64, negative), else an f64 ("-0" is one), shown without an
// exponent and with a decimal point.
func number(text string) string {
	if !strings.ContainsAny(text, ".eE") && text != "-0" {
		_, uerr := strconv.ParseUint(text, 10, 64)
		_, ierr := strconv.ParseInt(text, 10, 64)
		if uerr == nil || ierr == nil {
			return "integer `" + text + "`"
		}
	}
	f, _ := strconv.ParseFloat(text, 64)
	return "floating point `" + ShortestDecimal(f, 64) + "`"
}

// ShortestDecimal formats f, an f64 (bits 64) or f32 (bits 32), as the
// messages show a number: the shortest digits that read back, no
// exponent, "inf" for an infinity; f64s always with a decimal point.
func ShortestDecimal(f float64, bits int) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "NaN"
	}
	s := strconv.FormatFloat(f, 'f', -1, bits)
	if bits == 64 && !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// IsInteger says a number was written without a fraction or exponent.
func (v *Value) IsInteger() bool {
	return v.Kind == Number && !strings.ContainsAny(string(v.Number), ".eE")
}

// Settled is the offset of the last byte serde_json had read when it
// placed an error raised once v was read inside a container that closes
// with closer: past the blanks after v, then past the closer if it comes
// next or, in an array, past a comma and the blanks after it.
func (d *Doc) Settled(v *Value, closer byte) int {
	next := d.significant(v.End + 1)
	switch {
	case next < len(d.plain) && d.plain[next] == closer:
		return next
	case closer == ']' && next < len(d.plain) && d.plain[next] == ',':
		return d.significant(next+1) - 1
	}
	return next - 1
}

// Opened is the offset of the last byte serde_json had read when a visitor
// refused container v once opened: past the blanks after its opening
// bracket, and its closer when it is empty.
func (d *Doc) Opened(v *Value) int {
	if next := d.significant(v.Start + 1); next != v.End {
		return next - 1
	}
	return v.End
}

// significant is the offset of the first byte at or after at that is no
// blank (a comment is blank in plain), or the text's length.
func (d *Doc) significant(at int) int {
	for at < len(d.plain) && isSpace(rune(d.plain[at])) {
		at++
	}
	return at
}
