package dbus

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Whatever a bus sends, reading it never panics; a message it reads whole
// is exactly as long as its header says.
func FuzzDecode(f *testing.F) {
	text, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "dbus", "glib-reference.hex"))
	if err != nil {
		f.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(text)), "\n") {
		b, err := hex.DecodeString(line)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := Decode(b)
		if err != nil {
			return
		}
		if n, err := TotalLen([16]byte(b)); err != nil || n != len(b) {
			t.Fatalf("decoded a message of %d bytes whose header says %d (%v)", len(b), n, err)
		}
		if m.Kind == MethodCall {
			again, err := Decode(EncodeCall(Call{m.Destination, m.Path, m.Interface, m.Member, m.Body, 0}, m.Serial))
			if err != nil || !reflect.DeepEqual(again.Body, m.Body) {
				t.Fatalf("a call read back differently: %v", err)
			}
		}
	})
}
