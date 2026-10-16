package dbus

// Reading values in either byte order.

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"
)

// maxDepth is how deeply containers may nest (the spec allows 64 in all).
const maxDepth = 64

// dec reads values in a message's byte order.
type dec struct {
	data  []byte
	pos   int
	order binary.ByteOrder
	depth int
}

var errShort = &WireError{"it ends too early"}

func (d *dec) align(align int) error {
	pos := (d.pos + align - 1) / align * align
	if pos > len(d.data) {
		return errShort
	}
	d.pos = pos
	return nil
}

func (d *dec) take(n int) ([]byte, error) {
	if n < 0 || n > len(d.data)-d.pos {
		return nil, errShort
	}
	b := d.data[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}

func (d *dec) fixed(n int) ([]byte, error) {
	if err := d.align(n); err != nil {
		return nil, err
	}
	return d.take(n)
}

func (d *dec) u32() (uint32, error) {
	b, err := d.fixed(4)
	if err != nil {
		return 0, err
	}
	return d.order.Uint32(b), nil
}

func (d *dec) text(n int) (string, error) {
	b, err := d.take(n + 1)
	if err != nil {
		return "", err
	}
	if b[n] != 0 {
		return "", &WireError{"a string without its NUL"}
	}
	if !utf8.Valid(b[:n]) {
		return "", &WireError{"a string not in UTF-8"}
	}
	return string(b[:n]), nil
}

// value reads one value of the single complete type sig.
func (d *dec) value(sig string) (Value, error) {
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > maxDepth {
		return nil, &WireError{"nested too deeply"}
	}
	switch sig[0] {
	case 'y':
		b, err := d.take(1)
		if err != nil {
			return nil, err
		}
		return Byte(b[0]), nil
	case 'b', 'u', 'i', 'h':
		v, err := d.u32()
		if err != nil {
			return nil, err
		}
		switch sig[0] {
		case 'b':
			return Bool(v != 0), nil
		case 'u':
			return U32(v), nil
		case 'i':
			return Int(int32(v)), nil
		}
		return Int(v), nil
	case 'n', 'q':
		b, err := d.fixed(2)
		if err != nil {
			return nil, err
		}
		if sig[0] == 'n' {
			return Int(int16(d.order.Uint16(b))), nil
		}
		return Int(d.order.Uint16(b)), nil
	case 'x', 't', 'd':
		b, err := d.fixed(8)
		if err != nil {
			return nil, err
		}
		if sig[0] == 'd' {
			return Double(math.Float64frombits(d.order.Uint64(b))), nil
		}
		return Int(d.order.Uint64(b)), nil
	case 's', 'o':
		n, err := d.u32()
		if err != nil {
			return nil, err
		}
		s, err := d.text(int(n))
		if err != nil {
			return nil, err
		}
		if sig[0] == 'o' {
			return ObjectPath(s), nil
		}
		return Str(s), nil
	case 'g', 'v':
		return d.sigOrVariant(sig[0] == 'v')
	case 'a':
		return d.array(sig[1:])
	case '(', '{':
		return d.container(sig)
	}
	return nil, &WireError{fmt.Sprintf("the type '%c' is unknown", sig[0])}
}

func (d *dec) sigOrVariant(variant bool) (Value, error) {
	n, err := d.take(1)
	if err != nil {
		return nil, err
	}
	s, err := d.text(int(n[0]))
	if err != nil {
		return nil, err
	}
	if !variant {
		return Sig(s), nil
	}
	one, rest, err := firstType(s)
	if err != nil {
		return nil, err
	}
	if rest != "" {
		return nil, &WireError{"a variant of more than one type"}
	}
	inner, err := d.value(one)
	if err != nil {
		return nil, err
	}
	return Variant{inner}, nil
}

func (d *dec) array(elem string) (Value, error) {
	n, err := d.u32()
	if err != nil {
		return nil, err
	}
	if err := d.align(alignment(elem)); err != nil {
		return nil, err
	}
	if int(n) > len(d.data)-d.pos {
		return nil, errShort
	}
	end := d.pos + int(n)
	var items []Value
	for d.pos < end {
		item, err := d.value(elem)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if d.pos != end {
		return nil, &WireError{"an array overruns its length"}
	}
	return Array{elem, items}, nil
}

func (d *dec) container(sig string) (Value, error) {
	if err := d.align(8); err != nil {
		return nil, err
	}
	var fields Struct
	for rest := sig[1 : len(sig)-1]; rest != ""; {
		one, more, err := firstType(rest)
		if err != nil {
			return nil, err
		}
		f, err := d.value(one)
		if err != nil {
			return nil, err
		}
		fields, rest = append(fields, f), more
	}
	if sig[0] == '(' {
		return fields, nil
	}
	if len(fields) != 2 {
		return nil, &WireError{"a dict entry not of two"}
	}
	return Entry{fields[0], fields[1]}, nil
}
