// Package dbus is logis's own D-Bus client (spec §5): the wire format as
// much as launching needs (method calls out, replies and errors in, signals
// recognised and skipped) and a connection to the session bus's Unix socket
// with EXTERNAL authentication and blocking calls with a timeout.
//
// No third-party module: logis makes only a few calls per launch, so a
// general client would bring far more than it uses (spec §4).
package dbus

import "strings"

// A Value is a D-Bus value with enough type to write its signature: one of
// Byte, Bool, U32, Int, Double, Str, ObjectPath, Sig, Array, Struct, Entry
// or Variant.
type Value interface {
	// Signature is the value's D-Bus type signature.
	Signature() string
}

type (
	// Byte is `y`.
	Byte uint8
	// Bool is `b`.
	Bool bool
	// U32 is `u`.
	U32 uint32
	// Int is `x` when written; `n`, `q`, `i`, `x`, `t` and `h` read widened.
	Int int64
	// Double is `d`.
	Double float64
	// Str is `s`.
	Str string
	// ObjectPath is `o`.
	ObjectPath string
	// Sig is `g`.
	Sig string
	// Array is `a…`: the element signature, then the elements.
	Array struct {
		Elem  string
		Items []Value
	}
	// Struct is `(…)`.
	Struct []Value
	// Entry is `{…}`: a dict entry, inside an array.
	Entry struct {
		Key, Value Value
	}
	// Variant is `v`.
	Variant struct {
		Value Value
	}
)

func (Byte) Signature() string       { return "y" }
func (Bool) Signature() string       { return "b" }
func (U32) Signature() string        { return "u" }
func (Int) Signature() string        { return "x" }
func (Double) Signature() string     { return "d" }
func (Str) Signature() string        { return "s" }
func (ObjectPath) Signature() string { return "o" }
func (Sig) Signature() string        { return "g" }
func (a Array) Signature() string    { return "a" + a.Elem }
func (Variant) Signature() string    { return "v" }

func (s Struct) Signature() string {
	var b strings.Builder
	b.WriteByte('(')
	for _, f := range s {
		b.WriteString(f.Signature())
	}
	b.WriteByte(')')
	return b.String()
}

func (e Entry) Signature() string {
	return "{" + e.Key.Signature() + e.Value.Signature() + "}"
}

// AsString is the text of a string value (or of a path or signature).
func AsString(v Value) (string, bool) {
	switch v := v.(type) {
	case Str:
		return string(v), true
	case ObjectPath:
		return string(v), true
	case Sig:
		return string(v), true
	}
	return "", false
}

// signature is the concatenated signature of values.
func signature(values []Value) string {
	var b strings.Builder
	for _, v := range values {
		b.WriteString(v.Signature())
	}
	return b.String()
}
