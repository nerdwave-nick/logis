package dbus

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// reference is line n of the messages GLib encoded
// (tests/fixtures/dbus/glib-reference.py), an implementation independent of
// this one.
func reference(t *testing.T, n int) []byte {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "dbus", "glib-reference.hex"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(text)), "\n")
	b, err := hex.DecodeString(lines[n])
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func variant(v Value) Value { return Variant{v} }

func property(name string, v Value) Value { return Struct{Str(name), variant(v)} }

func entry(key string, v Value) Value { return Entry{Str(key), variant(v)} }

func head(b []byte) [16]byte { return [16]byte(b[:16]) }

func TestATransientUnitCallIsEncodedAsGLibEncodesIt(t *testing.T) {
	call := Call{
		Destination: "org.freedesktop.systemd1",
		Path:        "/org/freedesktop/systemd1",
		Interface:   "org.freedesktop.systemd1.Manager",
		Member:      "StartTransientUnit",
		Body: []Value{
			Str(`app-logis-forge\x2danoint-4242.scope`),
			Str("fail"),
			Array{"(sv)", []Value{
				property("PIDs", Array{"u", []Value{U32(4242)}}),
				property("Slice", Str("app.slice")),
				property("Description", Str("Anoint")),
			}},
			Array{Elem: "(sa(sv))"},
		},
	}
	if got, want := EncodeCall(call, 7), reference(t, 0); !bytes.Equal(got, want) {
		t.Errorf("EncodeCall =\n%x\nGLib:\n%x", got, want)
	}
}

func TestAnApplicationActionCallIsEncodedAsGLibEncodesIt(t *testing.T) {
	call := Call{
		Destination: "org.forge.Bus",
		Path:        "/org/forge/Bus",
		Interface:   "org.freedesktop.Application",
		Member:      "ActivateAction",
		Body: []Value{
			Str("wake"),
			Array{Elem: "v"},
			Array{"{sv}", []Value{entry("activation-token", Str("tok")), entry("desktop-startup-id", Str("tok"))}},
		},
	}
	if got, want := EncodeCall(call, 3), reference(t, 1); !bytes.Equal(got, want) {
		t.Errorf("EncodeCall =\n%x\nGLib:\n%x", got, want)
	}
}

func TestACallWithoutABodyHasNoSignature(t *testing.T) {
	call := Call{
		Destination: "org.freedesktop.DBus",
		Path:        "/org/freedesktop/DBus",
		Interface:   "org.freedesktop.DBus",
		Member:      "Hello",
	}
	if got, want := EncodeCall(call, 1), reference(t, 2); !bytes.Equal(got, want) {
		t.Errorf("EncodeCall =\n%x\nGLib:\n%x", got, want)
	}
}

func TestABigEndianReplyIsReadWithItsVariant(t *testing.T) {
	b := reference(t, 3)
	if n, err := TotalLen(head(b)); n != len(b) || err != nil {
		t.Fatalf("TotalLen = %d, %v; want %d", n, err, len(b))
	}
	reply, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Kind != MethodReturn || reply.Serial != 31 || reply.ReplySerial != 9 {
		t.Errorf("kind, serial, reply serial = %d, %d, %d; want 2, 31, 9", reply.Kind, reply.Serial, reply.ReplySerial)
	}
	env := Array{"s", []Value{Str("PATH=/usr/bin"), Str("WAYLAND_DISPLAY=wayland-1"), Str("LANG=de_DE.UTF-8")}}
	if want := []Value{variant(env)}; !reflect.DeepEqual(reply.Body, want) {
		t.Errorf("body = %#v; want %#v", reply.Body, want)
	}
}

func TestAnErrorIsReadWithItsNameAndMessage(t *testing.T) {
	e, err := Decode(reference(t, 4))
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != Error || e.ReplySerial != 9 {
		t.Errorf("kind, reply serial = %d, %d; want 3, 9", e.Kind, e.ReplySerial)
	}
	if e.ErrorName != "org.freedesktop.DBus.Error.ServiceUnknown" {
		t.Errorf("error name = %q", e.ErrorName)
	}
	if want := []Value{Str("The name is not activatable")}; !reflect.DeepEqual(e.Body, want) {
		t.Errorf("body = %#v; want %#v", e.Body, want)
	}
}

func TestOurOwnCallsReadBack(t *testing.T) {
	call := Call{
		Destination: "d.e",
		Path:        "/p",
		Interface:   "i.f",
		Member:      "M",
		Body: []Value{
			Byte(7),
			Bool(true),
			Struct{U32(1), ObjectPath("/o")},
			Array{"{sv}", []Value{entry("k", Sig("as"))}},
			Int(-3),
			Double(0.5),
		},
		Flags: NoReplyExpected,
	}
	back, err := Decode(EncodeCall(call, 5))
	if err != nil {
		t.Fatal(err)
	}
	if back.Kind != MethodCall || back.Serial != 5 {
		t.Errorf("kind, serial = %d, %d; want 1, 5", back.Kind, back.Serial)
	}
	names := [4]string{back.Destination, back.Path, back.Interface, back.Member}
	if names != [4]string{"d.e", "/p", "i.f", "M"} {
		t.Errorf("names = %q", names)
	}
	if !reflect.DeepEqual(back.Body, call.Body) {
		t.Errorf("body = %#v; want %#v", back.Body, call.Body)
	}
}

func TestRepliesAndErrorsReadBack(t *testing.T) {
	reply, err := Decode(EncodeReply(4, 2, "", []Value{ObjectPath("/job/1")}))
	if err != nil {
		t.Fatal(err)
	}
	if reply.Kind != MethodReturn || reply.Serial != 4 || reply.ReplySerial != 2 ||
		!reflect.DeepEqual(reply.Body, []Value{ObjectPath("/job/1")}) {
		t.Errorf("reply = %#v", reply)
	}
	e, err := Decode(EncodeReply(5, 3, "x.Failed", []Value{Str("why")}))
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != Error || e.ErrorName != "x.Failed" || !reflect.DeepEqual(e.Body, []Value{Str("why")}) {
		t.Errorf("error = %#v", e)
	}
}

func TestASignalIsRecognisedWithoutReadingItsBody(t *testing.T) {
	b := EncodeCall(Call{Destination: ":1.42", Path: "/org/freedesktop/DBus",
		Interface: "org.freedesktop.DBus", Member: "NameAcquired", Body: []Value{Str(":1.42")}}, 2)
	b[1] = Signal
	m, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != Signal || m.Member != "NameAcquired" || m.Body != nil {
		t.Errorf("signal = %#v", m)
	}
}

func TestTruncatedOrAbsurdMessagesAreRefused(t *testing.T) {
	b := reference(t, 3)
	var we *WireError
	if _, err := Decode(b[:len(b)-3]); !errors.As(err, &we) {
		t.Errorf("a truncated message: %v", err)
	}
	huge := head(b)
	copy(huge[4:8], []byte{0x7f, 0xff, 0xff, 0xff})
	if _, err := TotalLen(huge); !errors.As(err, &we) {
		t.Errorf("an absurd length: %v", err)
	}
	var x [16]byte
	for i := range x {
		x[i] = 'x'
	}
	if _, err := TotalLen(x); err == nil || err.Error() != "a malformed D-Bus message: no byte order (0x78)" {
		t.Errorf("no byte order: %v", err)
	}
}

func TestMalformedBodiesAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name, sig string
		body      []byte
		reason    string
	}{
		{"unknown type", "z", nil, "the type 'z' is unknown"},
		{"open struct", "(s", nil, "the signature '(s' is malformed"},
		{"bare array", "a", nil, "the signature '' is malformed"},
		{"string without NUL", "s", []byte{1, 0, 0, 0, 'a', 'b', 0, 0}, "a string without its NUL"},
		{"string not UTF-8", "s", []byte{1, 0, 0, 0, 0xff, 0, 0, 0}, "a string not in UTF-8"},
		{"short", "u", []byte{1, 0}, "it ends too early"},
		{"longer than its signature", "y", []byte{1, 2}, "its body is longer than its signature"},
		{"variant of two", "v", []byte{2, 'y', 'y', 0, 1, 2, 0, 0}, "a variant of more than one type"},
		{"entry not of two", "a{s}", []byte{8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "a dict entry not of two"},
		{"array past the message", "ay", []byte{2, 0, 0, 0, 1}, "it ends too early"},
		{"array overruns its length", "au", []byte{2, 0, 0, 0, 1, 0, 0, 0}, "an array overruns its length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := rawReturn(tc.sig, tc.body)
			_, err := Decode(b)
			var we *WireError
			if !errors.As(err, &we) || we.Reason != tc.reason {
				t.Errorf("Decode = %v; want %q", err, tc.reason)
			}
		})
	}
}

func TestDeepNestingIsRefused(t *testing.T) {
	var body []byte
	for range 70 {
		body = append(body, 1, 'v', 0)
	}
	body = append(body, 1, 'y', 0, 7)
	_, err := Decode(rawReturn("v", body))
	var we *WireError
	if !errors.As(err, &we) || we.Reason != "nested too deeply" {
		t.Errorf("Decode = %v; want nested too deeply", err)
	}
}

// rawReturn is a little-endian method return to serial 1 whose header
// claims signature sig and whose body is body, as given.
func rawReturn(sig string, body []byte) []byte {
	fields := []byte{5, 1, 'u', 0, 1, 0, 0, 0, 8, 1, 'g', 0, byte(len(sig))}
	fields = append(append(fields, sig...), 0)
	b := []byte{'l', MethodReturn, 0, 1}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(body)))
	b = binary.LittleEndian.AppendUint32(b, 9)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(fields)))
	b = append(b, fields...)
	for len(b)%8 != 0 {
		b = append(b, 0)
	}
	return append(b, body...)
}
