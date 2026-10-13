package dbus

import "fmt"

// The wire format (D-Bus Specification, "Message Protocol"), as much as
// launching needs. Calls are written little-endian with their header fields
// in the order GLib writes them (path, interface, destination, signature,
// member), so tests can compare them byte for byte with GLib's encoding
// (tests/fixtures/dbus/glib-reference.py). Replies are read in either byte
// order; a signal is recognised and its body never read.

// Message types.
const (
	MethodCall   byte = 1
	MethodReturn byte = 2
	Error        byte = 3
	Signal       byte = 4
)

// NoReplyExpected is the flag of a call whose caller wants no reply.
const NoReplyExpected byte = 0x1

// MaxMessage is the longest message logis reads.
const MaxMessage = 64 << 20

// A Call is a method call to send.
type Call struct {
	Destination, Path, Interface, Member string
	Body                                 []Value
	Flags                                byte
}

// A Message is a message received.
type Message struct {
	Kind   byte
	Serial uint32
	// ReplySerial is the call this answers (replies and errors); 0 if none.
	ReplySerial uint32
	// ErrorName is an error's name.
	ErrorName string
	// Member, Interface, Path and Destination are a call's or signal's.
	Member, Interface, Path, Destination string
	// Body is empty for a signal, whose body is never read.
	Body []Value
}

// A WireError says why bytes are not a message logis can read.
type WireError struct{ Reason string }

func (e *WireError) Error() string { return "a malformed D-Bus message: " + e.Reason }

// A field is a header field: its code and value.
type field struct {
	code  byte
	value Value
}

// EncodeCall is a method call's bytes, numbered serial.
func EncodeCall(c Call, serial uint32) []byte {
	fields := []field{{1, ObjectPath(c.Path)}, {2, Str(c.Interface)}, {6, Str(c.Destination)}}
	if sig := signature(c.Body); sig != "" {
		fields = append(fields, field{8, Sig(sig)})
	}
	fields = append(fields, field{3, Str(c.Member)})
	return encode(MethodCall, c.Flags, serial, fields, c.Body)
}

// EncodeReply is the bytes of a reply to call replySerial: a method return,
// or with errorName non-empty an error of that name.
func EncodeReply(serial, replySerial uint32, errorName string, body []Value) []byte {
	fields := []field{{5, U32(replySerial)}}
	kind := MethodReturn
	if errorName != "" {
		fields = append(fields, field{4, Str(errorName)})
		kind = Error
	}
	if sig := signature(body); sig != "" {
		fields = append(fields, field{8, Sig(sig)})
	}
	return encode(kind, 0, serial, fields, body)
}

// encode is a message of kind with header fields and body.
func encode(kind, flags byte, serial uint32, fields []field, body []Value) []byte {
	var coded enc
	for _, v := range body {
		coded.value(v)
	}
	header := make([]Value, len(fields))
	for i, f := range fields {
		header[i] = Struct{Byte(f.code), Variant{f.value}}
	}
	head := enc{buf: []byte{'l', kind, flags, 1}}
	head.u32(uint32(len(coded.buf)))
	head.u32(serial)
	head.value(Array{"(yv)", header})
	head.pad(8)
	return append(head.buf, coded.buf...)
}

// TotalLen is the whole length of a message from its first 16 bytes.
func TotalLen(head [16]byte) (int, error) {
	o, err := order(head[0])
	if err != nil {
		return 0, err
	}
	body, fields := uint64(o.Uint32(head[4:])), uint64(o.Uint32(head[12:]))
	total := (16+fields+7)/8*8 + body
	if total > MaxMessage {
		return 0, &WireError{fmt.Sprintf("%d bytes is too long", total)}
	}
	return int(total), nil
}

// Decode reads one whole message.
func Decode(b []byte) (Message, error) {
	if len(b) < 16 {
		return Message{}, &WireError{"shorter than its header"}
	}
	total, err := TotalLen([16]byte(b))
	if err != nil {
		return Message{}, err
	}
	if total != len(b) {
		return Message{}, &WireError{"its length does not match its header"}
	}
	o, _ := order(b[0])
	d := dec{data: b, pos: 12, order: o}
	fields, err := d.value("a(yv)")
	if err != nil {
		return Message{}, err
	}
	m := Message{Kind: b[1], Serial: o.Uint32(b[8:])}
	sig := m.header(fields.(Array).Items)
	if err := d.align(8); err != nil {
		return Message{}, err
	}
	if m.Kind == Signal {
		return m, nil
	}
	for rest := sig; rest != ""; {
		one, more, err := firstType(rest)
		if err != nil {
			return Message{}, err
		}
		v, err := d.value(one)
		if err != nil {
			return Message{}, err
		}
		m.Body, rest = append(m.Body, v), more
	}
	if d.pos != len(b) {
		return Message{}, &WireError{"its body is longer than its signature"}
	}
	return m, nil
}

// header takes the header fields logis reads into m; the body's signature.
func (m *Message) header(fields []Value) (sig string) {
	for _, f := range fields {
		parts, ok := f.(Struct)
		if !ok || len(parts) != 2 {
			continue
		}
		code, ok1 := parts[0].(Byte)
		v, ok2 := parts[1].(Variant)
		if !ok1 || !ok2 {
			continue
		}
		switch value := v.Value.(type) {
		case ObjectPath:
			if code == 1 {
				m.Path = string(value)
			}
		case Str:
			switch code {
			case 2:
				m.Interface = string(value)
			case 3:
				m.Member = string(value)
			case 4:
				m.ErrorName = string(value)
			case 6:
				m.Destination = string(value)
			}
		case U32:
			if code == 5 {
				m.ReplySerial = uint32(value)
			}
		case Sig:
			if code == 8 {
				sig = string(value)
			}
		}
	}
	return sig
}
