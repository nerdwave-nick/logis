package jsonc

import (
	"bytes"
	"errors"
	"testing"
	"unicode/utf8"
)

// FuzzParse: Strip never moves a byte offset or a newline, and Parse either
// accepts a text or refuses it with a place inside it; it never panics.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"", "{}", `{"a": [1, 2,], /* c */ "b": {"c": "\\"}, // d` + "\n}",
		"{\n /* open", `{"a": "x`, `{"a": 1} x`, "\xef\xbb\xbf{}", `{"⚙": tru}`, "[1,,]",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, text []byte) {
		if !utf8.Valid(text) {
			return // logis reads only UTF-8 (an unreadable file otherwise)
		}
		if out, err := Strip(text); err == nil {
			if len(out) != len(text) || bytes.Count(out, []byte("\n")) != bytes.Count(text, []byte("\n")) {
				t.Fatalf("%q stripped to %q", text, out)
			}
		}
		_, err := Parse(text, "config")
		var invalid *Invalid
		if err != nil && !errors.As(err, &invalid) {
			t.Fatalf("%q: %T %v", text, err, err)
		}
		if invalid != nil {
			lines := bytes.Count(text, []byte("\n")) + 1
			if invalid.Line < 1 || invalid.Line > lines || invalid.Column < 1 {
				t.Fatalf("%q: placed at %d:%d", text, invalid.Line, invalid.Column)
			}
		}
	})
}
