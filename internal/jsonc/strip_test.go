package jsonc

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// decoded strips text, checks that no offset moved, and decodes the rest.
func decoded(t *testing.T, text string) any {
	t.Helper()
	stripped, err := Strip([]byte(text))
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	if len(stripped) != len(text) {
		t.Fatalf("offsets moved: %q", stripped)
	}
	var v any
	if err := json.Unmarshal(stripped, &v); err != nil {
		t.Fatalf("%q stripped to %q: %v", text, stripped, err)
	}
	return v
}

func stripped(t *testing.T, text string) string {
	t.Helper()
	out, err := Strip([]byte(text))
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return string(out)
}

func same(t *testing.T, got any, wantJSON string) {
	t.Helper()
	var want any
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %s", got, wantJSON)
	}
}

func TestPlainJSONPassesUnchanged(t *testing.T) {
	text := `{"a": [1, 2.5, "x"], "b": {"c": null}}`
	if got := stripped(t, text); got != text {
		t.Errorf("%q", got)
	}
}

func TestLineAndBlockCommentsBecomeSpaces(t *testing.T) {
	got := stripped(t, "// head\n{ /* a */ \"a\": 1 // tail\n}")
	if want := "       \n{         \"a\": 1        \n}"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}

func TestNewlinesInsideBlockCommentsAreKept(t *testing.T) {
	got := stripped(t, "{/* one\ntwo */\"a\": 1}")
	if want := "{      \n      \"a\": 1}"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}

func TestCommentMarkersInsideStringsAreText(t *testing.T) {
	text := `{"url": "http://x/*y*/", "q": "say \"// hi\""}`
	if got := stripped(t, text); got != text {
		t.Errorf("%q", got)
	}
	same(t, decoded(t, text), `{"url": "http://x/*y*/", "q": "say \"// hi\""}`)
}

func TestAnEscapedBackslashEndsBeforeTheClosingQuote(t *testing.T) {
	same(t, decoded(t, `{"a": "\\", "b": 1, /* c */}`), `{"a": "\\", "b": 1}`)
}

func TestTrailingCommasBeforeClosingBracketsAreRemoved(t *testing.T) {
	same(t, decoded(t, `{"a": [1, 2,], "b": {"c": 3,},}`), `{"a": [1, 2], "b": {"c": 3}}`)
}

func TestATrailingCommaFollowedByCommentsIsRemoved(t *testing.T) {
	same(t, decoded(t, "{\"a\": 1, // last\n /* really */ }"), `{"a": 1}`)
}

func TestACommaBetweenValuesIsKept(t *testing.T) {
	same(t, decoded(t, `{"a": 1, /* b */ "b": 2}`), `{"a": 1, "b": 2}`)
}

func TestADoubleCommaStaysAnErrorForTheDecoder(t *testing.T) {
	var v any
	if err := json.Unmarshal([]byte(stripped(t, "[1,,]")), &v); err == nil {
		t.Error("[1,,] was accepted")
	}
}

func TestMultibyteTextInCommentsKeepsByteOffsets(t *testing.T) {
	same(t, decoded(t, "{\"a\": 1 /* ⚙ Omnissiah */, \"b\": \"⚙\" // ⚙\n}"), `{"a": 1, "b": "⚙"}`)
}

func TestAByteOrderMarkIsBlanked(t *testing.T) {
	same(t, decoded(t, "\xef\xbb\xbf{\"a\": 1}"), `{"a": 1}`)
}

func TestALoneSlashIsLeftForTheDecoderToRefuse(t *testing.T) {
	if got := stripped(t, "{/}"); got != "{/}" {
		t.Errorf("%q", got)
	}
}

func TestAnUnterminatedBlockCommentIsLocated(t *testing.T) {
	_, err := Strip([]byte("{\n  \"a\": 1 /* never\n closed"))
	var u *UnterminatedError
	if !errors.As(err, &u) {
		t.Fatalf("%v", err)
	}
	if u.Line != 2 || u.Column != 10 {
		t.Errorf("at %d:%d, want 2:10", u.Line, u.Column)
	}
}

func TestAnUnterminatedStringIsLeftForTheDecoder(t *testing.T) {
	// The scanner must not run past the end looking for the closing quote.
	for _, text := range []string{`{"a": "x`, `{"a": "\`, `"`} {
		if got := stripped(t, text); got != text {
			t.Errorf("%q became %q", text, got)
		}
	}
}
