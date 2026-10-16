package input

import (
	"strings"
	"testing"
)

// Tests of paste: the MIME type chosen and the text that reaches the query.

func TestUTF8TextIsPreferredWhateverElseIsOffered(t *testing.T) {
	got, ok := ChooseMIME([]string{"image/png", "STRING", "text/plain;charset=utf-8", "text/plain"})
	if !ok || got != "text/plain;charset=utf-8" {
		t.Errorf("%q %v", got, ok)
	}
}

func TestTheOwnersSpellingOfTheCharsetIsKept(t *testing.T) {
	got, ok := ChooseMIME([]string{"text/plain; charset=UTF-8"})
	if !ok || got != "text/plain; charset=UTF-8" {
		t.Errorf("%q %v", got, ok)
	}
}

func TestX11NamesAndPlainTextFollowInOrder(t *testing.T) {
	for _, c := range []struct {
		offered []string
		want    string
	}{
		{[]string{"TEXT", "UTF8_STRING"}, "UTF8_STRING"},
		{[]string{"STRING", "text/plain"}, "text/plain"},
		{[]string{"STRING"}, "STRING"},
		{[]string{"STRING", "TEXT"}, "TEXT"},
	} {
		if got, ok := ChooseMIME(c.offered); !ok || got != c.want {
			t.Errorf("%q: %q %v, want %q", c.offered, got, ok, c.want)
		}
	}
}

func TestNoTextOnOfferMeansNothingToRead(t *testing.T) {
	if got, ok := ChooseMIME([]string{"image/png", "text/uri-list"}); ok {
		t.Errorf("chose %q", got)
	}
	if got, ok := ChooseMIME(nil); ok {
		t.Errorf("chose %q from nothing", got)
	}
}

func TestAPasteInsertsItsFirstLine(t *testing.T) {
	for in, want := range map[string]string{
		"Ave Omnissiah\n":                    "Ave Omnissiah",
		"first\r\nsecond":                    "first",
		"\n\nafter blank lines\nnot this":    "after blank lines",
		"\r\n\r\nafter CRLFs\rnot this":      "after CRLFs",
		"":                                   "",
		"\n\n":                               "",
		"no line break":                      "no line break",
		"tail\n":                             "tail",
		"\u0085is a control, not a line end": "is a control, not a line end",
	} {
		if got := PasteText([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestTabsBecomeSpacesAndControlCharactersGo(t *testing.T) {
	if got := PasteText([]byte("a\tb\x07c\x1bd")); got != "a bcd" {
		t.Errorf("%q", got)
	}
}

func TestInvalidUTF8IsReplacedAndACutSequenceDropped(t *testing.T) {
	for in, want := range map[string]string{
		"caf\xc3\xa9 \xff!": "café �!",
		"caf\xc3":           "caf", // cut by the limit
		"caf\xe2\x82":       "caf",
		// One U+FFFD per maximal invalid subpart.
		"a\xe2\x82Xb":   "a�Xb",
		"a\xff\xfeb":    "a��b",
		"a\xed\xa0\x80": "a���", // a surrogate is three
		"a\xf0\x9f\x98": "a",    // cut emoji
		// An earlier error decodes the whole input lossily, the cut tail
		// included (only the first error is checked for a cut).
		"\xff caf\xc3": "� caf�",
		"\xc0\xafx":    "��x", // overlong
		"\xf5\x80x":    "��x",
	} {
		if got := PasteText([]byte(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestReadingStopsAtTheLimit(t *testing.T) {
	var b PasteBytes
	if b.Push([]byte(strings.Repeat("a", PasteLimit-1))) {
		t.Error("the limit is not reached yet")
	}
	if !b.Push([]byte("bc")) {
		t.Error("the limit is reached")
	}
	text := b.Text()
	if len(text) != PasteLimit || !strings.HasSuffix(text, "ab") {
		t.Errorf("%d bytes ending %q", len(text), text[max(len(text)-2, 0):])
	}
}

func TestSelectionsHaveTheirTraceNames(t *testing.T) {
	if Clipboard.Name() != "clipboard" || Primary.Name() != "primary" {
		t.Errorf("%q %q", Clipboard.Name(), Primary.Name())
	}
}
