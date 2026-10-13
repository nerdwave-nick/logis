package wltest

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// encode lays out event arguments on the wire; Fd arguments travel
// separately, as ancillary data.
func encode(args []any) (body []byte, fds []int, err error) {
	word := func(v uint32) { body = binary.NativeEndian.AppendUint32(body, v) }
	pad := func() {
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	for _, a := range args {
		switch v := a.(type) {
		case uint32:
			word(v)
		case int32:
			word(uint32(v))
		case string:
			word(uint32(len(v) + 1))
			body = append(append(body, v...), 0)
			pad()
		case []byte:
			word(uint32(len(v)))
			body = append(body, v...)
			pad()
		case Fd:
			fds = append(fds, int(v))
		default:
			return nil, nil, fmt.Errorf("wltest: cannot send %T", a)
		}
	}
	return body, fds, nil
}

// Reader reads request arguments in wire order.
type Reader struct {
	b   []byte
	err error
}

// NewReader reads the arguments of r.
func NewReader(r Request) *Reader { return &Reader{b: r.Body} }

// Uint reads a uint, int, fixed, object or new_id argument.
func (r *Reader) Uint() uint32 {
	if len(r.b) < 4 {
		r.err = errors.New("wltest: request too short")
		return 0
	}
	v := binary.NativeEndian.Uint32(r.b)
	r.b = r.b[4:]
	return v
}

// String reads a string argument.
func (r *Reader) String() string {
	n := int(r.Uint())
	padded := (n + 3) &^ 3
	if n == 0 || len(r.b) < padded {
		r.b = nil
		return ""
	}
	s := string(r.b[:n-1])
	r.b = r.b[padded:]
	return s
}

// Err is the first read error.
func (r *Reader) Err() error { return r.err }
