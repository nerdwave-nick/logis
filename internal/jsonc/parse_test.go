package jsonc

import (
	"errors"
	"strings"
	"testing"
)

func mustParse(t *testing.T, text string) *Doc {
	t.Helper()
	doc, err := Parse([]byte(text), "config")
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return doc
}

func refused(t *testing.T, text string) *Invalid {
	t.Helper()
	_, err := Parse([]byte(text), "config")
	var invalid *Invalid
	if !errors.As(err, &invalid) {
		t.Fatalf("%q: want *Invalid, got %v", text, err)
	}
	return invalid
}

func TestOnlyWhitespaceAndCommentsIsNoDocument(t *testing.T) {
	for _, text := range []string{"", "  \n\t", "// nothing\n/* at all */"} {
		if doc := mustParse(t, text); doc.Root != nil {
			t.Errorf("%q: %+v", text, doc.Root)
		}
	}
}

func TestValuesKeepTheirKindsAndOrder(t *testing.T) {
	doc := mustParse(t, `{"s": "⚙", "n": 1.5, "i": -2, "b": true, "z": null, "a": [1, "x",], "o": {"k": false},}`)
	root := doc.Root
	if root.Kind != Object || len(root.Members) != 7 {
		t.Fatalf("%+v", root)
	}
	want := []struct {
		key  string
		kind Kind
	}{{"s", String}, {"n", Number}, {"i", Number}, {"b", Bool}, {"z", Null}, {"a", Array}, {"o", Object}}
	for i, w := range want {
		m := root.Members[i]
		if m.Key != w.key || m.Value.Kind != w.kind {
			t.Errorf("member %d: %q %v, want %q %v", i, m.Key, m.Value.Kind, w.key, w.kind)
		}
	}
	m := root.Members
	if m[0].Value.String != "⚙" || m[1].Value.Number != "1.5" || !m[3].Value.Bool {
		t.Errorf("scalars: %+v %+v %+v", m[0].Value, m[1].Value, m[3].Value)
	}
	if a := m[5].Value.Elems; len(a) != 2 || a[1].String != "x" {
		t.Errorf("array: %+v", a)
	}
	if o := m[6].Value.Members; len(o) != 1 || o[0].Key != "k" {
		t.Errorf("object: %+v", o)
	}
}

func TestRepeatedKeysAreAllKept(t *testing.T) {
	doc := mustParse(t, `{"a": 1, "a": 2}`)
	if len(doc.Root.Members) != 2 {
		t.Errorf("%+v", doc.Root.Members)
	}
}

// Errors point at the last character of the token they concern, counted in
// characters (serde_json's convention, which Rust logis v1 reported).
func TestErrorsAboutAValuePointAtItsLastCharacter(t *testing.T) {
	text := "{\n  \"⚙\": \"soon\",\n  \"thme\": [1]\n}"
	doc := mustParse(t, text)
	soon, thme := doc.Root.Members[0], doc.Root.Members[1]
	for _, tc := range []struct {
		at           int
		line, column int
	}{
		{soon.Value.End, 2, 13},
		{soon.KeyEnd, 2, 5},
		{thme.KeyEnd, 3, 8},
		{thme.Value.End, 3, 13},
		{doc.Root.End, 4, 1},
	} {
		e := doc.Errorf(tc.at, "heresy %d", 7)
		if e.Line != tc.line || e.Column != tc.column || e.Message != "heresy 7" {
			t.Errorf("at %d: %+v, want %d:%d", tc.at, e, tc.line, tc.column)
		}
	}
}

func TestTheDocumentMustBeAnObject(t *testing.T) {
	for _, text := range []string{"[]", `"theme"`, "null", "7"} {
		e := refused(t, text)
		if !strings.Contains(e.Message, "the config must be a JSON object") {
			t.Errorf("%q: %v", text, e)
		}
	}
	e := refused(t, "  // c\n 7")
	if e.Line != 2 || e.Column != 2 {
		t.Errorf("%v", e)
	}
	_, err := Parse([]byte("[1]"), "theme")
	if err == nil || !strings.Contains(err.Error(), "the theme must be a JSON object: { \"key\": value, … }") {
		t.Errorf("%v", err)
	}
}

func TestSyntaxErrorsAreLocatedAndWordedAsBefore(t *testing.T) {
	for _, tc := range []struct {
		text         string
		line, column int
		message      string
	}{
		{"{\n  \"theme\": \"x\"\n  \"icon-theme\": \"y\"\n}", 3, 3, "expected `,` or `}`"},
		{`{"a" 1}`, 1, 6, "expected `:`"},
		{`{1: 2}`, 1, 2, "key must be a string"},
		{`{"a": ]}`, 1, 7, "expected value"},
		{`{"a": [1 2]}`, 1, 10, "expected `,` or `]`"},
		{`{"a": tru}`, 1, 10, "expected ident"},
		{`{"a": 1.}`, 1, 9, "invalid number"},
		{`{"a": "\q"}`, 1, 9, "invalid escape"},
		{"{\"a\": \"x\x01\"}", 1, 9, "control character (\\u0000-\\u001F) found while parsing a string"},
		{`{"a": 1} x`, 1, 10, "trailing characters"},
		{`{"a": 1} {}`, 1, 10, "trailing characters"},
		{`{"a": 1`, 1, 7, "EOF while parsing an object"},
		{`{"a": [1`, 1, 8, "EOF while parsing a list"},
		{`{"a": "xx`, 1, 9, "EOF while parsing a string"},
		{"{/}", 1, 2, "key must be a string"},
	} {
		e := refused(t, tc.text)
		if e.Line != tc.line || e.Column != tc.column || e.Message != tc.message {
			t.Errorf("%q: %d:%d %q, want %d:%d %q", tc.text, e.Line, e.Column, e.Message,
				tc.line, tc.column, tc.message)
		}
	}
}

func TestAnUnterminatedCommentIsLocatedInCharacters(t *testing.T) {
	e := refused(t, "{\n ⚙ /* the rest")
	if e.Line != 2 || e.Column != 4 || !strings.Contains(e.Message, "comment") {
		t.Errorf("%+v", e)
	}
}

func TestTheMessageCarriesItsPlace(t *testing.T) {
	e := refused(t, `{"a" 1}`)
	if e.Error() != "1:6: expected `:`" {
		t.Errorf("%q", e.Error())
	}
}

func TestUnexpectedNamesAValueAsSerdeDid(t *testing.T) {
	doc := mustParse(t, `{"a": "x\"y", "b": 7, "c": -1, "d": 1.5, "e": true, "f": null, "g": [], "h": {}}`)
	want := []string{"string \"x\\\"y\"", "integer `7`", "integer `-1`", "floating point `1.5`",
		"boolean `true`", "null", "sequence", "map"}
	for i, m := range doc.Root.Members {
		if got := m.Value.Unexpected(); got != want[i] {
			t.Errorf("%s: %q, want %q", m.Key, got, want[i])
		}
	}
}
