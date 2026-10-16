package dbus

// The byte-level coding of values: alignment, both byte orders when
// reading, signatures split into complete types.

import (
	"encoding/binary"
	"fmt"
	"math"
)

// order is the byte order a message's first byte marks.
func order(mark byte) (binary.ByteOrder, error) {
	switch mark {
	case 'l':
		return binary.LittleEndian, nil
	case 'B':
		return binary.BigEndian, nil
	}
	return nil, &WireError{fmt.Sprintf("no byte order (%#x)", mark)}
}

// alignment is a type's alignment by its signature's first character.
func alignment(sig string) int {
	if sig == "" {
		return 1
	}
	switch sig[0] {
	case 'n', 'q':
		return 2
	case 'b', 'u', 'i', 's', 'o', 'a', 'h':
		return 4
	case 'x', 't', 'd', '(', '{':
		return 8
	}
	return 1
}

// firstType splits the first complete type off a signature.
func firstType(sig string) (one, rest string, err error) {
	bad := &WireError{fmt.Sprintf("the signature '%s' is malformed", sig)}
	if sig == "" {
		return "", "", bad
	}
	end := 1
	switch open := sig[0]; open {
	case 'a':
		elem, _, err := firstType(sig[1:])
		if err != nil {
			return "", "", err
		}
		end = 1 + len(elem)
	case '(', '{':
		closing := byte(')')
		if open == '{' {
			closing = '}'
		}
		depth, found := 0, false
		for i := 0; i < len(sig) && !found; i++ {
			switch sig[i] {
			case open:
				depth++
			case closing:
				depth--
				if depth == 0 {
					end, found = i+1, true
				}
			}
		}
		if !found {
			return "", "", bad
		}
	}
	return sig[:end], sig[end:], nil
}

// enc writes values little-endian.
type enc struct{ buf []byte }

func (e *enc) pad(align int) {
	for len(e.buf)%align != 0 {
		e.buf = append(e.buf, 0)
	}
}

func (e *enc) u32(v uint32) {
	e.pad(4)
	e.buf = binary.LittleEndian.AppendUint32(e.buf, v)
}

func (e *enc) u64(v uint64) {
	e.pad(8)
	e.buf = binary.LittleEndian.AppendUint64(e.buf, v)
}

func (e *enc) str(s string) {
	e.u32(uint32(len(s)))
	e.buf = append(append(e.buf, s...), 0)
}

func (e *enc) sig(s string) {
	e.buf = append(append(append(e.buf, byte(len(s))), s...), 0)
}

func (e *enc) value(v Value) {
	switch v := v.(type) {
	case Byte:
		e.buf = append(e.buf, byte(v))
	case Bool:
		b := uint32(0)
		if v {
			b = 1
		}
		e.u32(b)
	case U32:
		e.u32(uint32(v))
	case Int:
		e.u64(uint64(v))
	case Double:
		e.u64(math.Float64bits(float64(v)))
	case Str:
		e.str(string(v))
	case ObjectPath:
		e.str(string(v))
	case Sig:
		e.sig(string(v))
	case Array:
		e.u32(0)
		at := len(e.buf) - 4
		e.pad(alignment(v.Elem))
		start := len(e.buf)
		for _, item := range v.Items {
			e.value(item)
		}
		binary.LittleEndian.PutUint32(e.buf[at:], uint32(len(e.buf)-start))
	case Struct:
		e.pad(8)
		for _, f := range v {
			e.value(f)
		}
	case Entry:
		e.pad(8)
		e.value(v.Key)
		e.value(v.Value)
	case Variant:
		e.sig(v.Value.Signature())
		e.value(v.Value)
	default:
		panic(fmt.Sprintf("dbus: cannot encode %T", v))
	}
}
