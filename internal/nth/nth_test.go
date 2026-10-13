package nth

import (
	"bytes"
	"testing"
)

func mustParse(t *testing.T, text string) *Nth {
	t.Helper()
	n, err := Parse(text)
	if err != nil || n == nil {
		t.Fatalf("Parse(%q) = %v, %v; want a selection", text, n, err)
	}
	return n
}

func apply(t *testing.T, text, line string) string {
	t.Helper()
	return string(mustParse(t, text).Apply([]byte(line), '\t'))
}

func TestANumberSelectsThatColumnCountedFromOne(t *testing.T) {
	for _, tc := range []struct{ sel, want string }{{"1", "a"}, {"2", "b"}, {"3", "c"}} {
		if got := apply(t, tc.sel, "a\tb\tc"); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.sel, got, tc.want)
		}
	}
}

// A plain number is read as Rust's usize parser reads it: a leading plus
// is allowed.
func TestAPlusSignedNumberIsAColumnToo(t *testing.T) {
	if got := apply(t, "+2", "a\tb"); got != "b" {
		t.Errorf("got %q", got)
	}
	if _, err := Parse("+0"); err == nil {
		t.Error("+0 was accepted")
	}
}

func TestZeroMeansNoSelection(t *testing.T) {
	n, err := Parse("0")
	if n != nil || err != nil {
		t.Fatalf("Parse(0) = %v, %v; want nil, nil", n, err)
	}
}

func TestAColumnPastTheLastIsEmpty(t *testing.T) {
	if got := apply(t, "4", "a\tb\tc"); got != "" {
		t.Errorf("got %q", got)
	}
	if got := apply(t, "2", "no columns"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestAFormatExpandsColumnsInsideFreeText(t *testing.T) {
	if got := apply(t, "{2} ({1})", "id7\tFirst"); got != "First (id7)" {
		t.Errorf("got %q", got)
	}
	if got := apply(t, "[{3}]", "a\tb"); got != "[]" {
		t.Errorf("got %q", got)
	}
}

func TestRangesJoinTheirColumnsWithTheDelimiter(t *testing.T) {
	for _, tc := range []struct{ sel, line, want string }{
		{"{2..3}", "a\tb\tc\td", "b\tc"},
		{"{2..}", "a\tb\tc\td", "b\tc\td"},
		{"{3..9}", "a\tb\tc\td", "c\td"},
		{"{5..}", "a\tb", ""},
	} {
		if got := apply(t, tc.sel, tc.line); got != tc.want {
			t.Errorf("%s on %q: got %q want %q", tc.sel, tc.line, got, tc.want)
		}
	}
}

func TestAnotherDelimiterSplitsAndJoins(t *testing.T) {
	if got := mustParse(t, "{1..2}:{3}").Apply([]byte("a,b,c"), ','); string(got) != "a,b:c" {
		t.Errorf("got %q", got)
	}
}

func TestBytesAreKeptExactly(t *testing.T) {
	got := mustParse(t, "2").Apply([]byte("\xff\t\xfe\r"), '\t')
	if !bytes.Equal(got, []byte("\xfe\r")) {
		t.Errorf("got %q", got)
	}
}

func TestTextThatIsNotAColumnReferenceStaysLiteral(t *testing.T) {
	if got := apply(t, "{x} {} {1}{1", "a"); got != "{x} {} a{1" {
		t.Errorf("got %q", got)
	}
	if got := apply(t, "{{1}}", "a"); got != "{a}" {
		t.Errorf("got %q", got)
	}
}

func TestColumnZeroAndBackwardRangesAreRefused(t *testing.T) {
	for _, tc := range []struct{ bad, want string }{
		{"{0}", "'{0}' is no column: columns are counted from 1, and a range runs forwards"},
		{"{0..2}", "'{0..2}' is no column: columns are counted from 1, and a range runs forwards"},
		{"{3..2}", "'{3..2}' is no column: columns are counted from 1, and a range runs forwards"},
		{"-1", "'-1' names no column: it wants a column number or a format with {N}, {N..M} or {N..}"},
		{"", "'' names no column: it wants a column number or a format with {N}, {N..M} or {N..}"},
		{"{x}", "'{x}' names no column: it wants a column number or a format with {N}, {N..M} or {N..}"},
	} {
		n, err := Parse(tc.bad)
		if err == nil {
			t.Errorf("Parse(%q) = %v, want an error", tc.bad, n)
			continue
		}
		if err.Error() != tc.want {
			t.Errorf("Parse(%q): %q, want %q", tc.bad, err, tc.want)
		}
	}
}

func TestADelimiterIsOneASCIICharacter(t *testing.T) {
	for _, tc := range []struct {
		text string
		want byte
		ok   bool
	}{
		{",", ',', true}, {"\t", '\t', true}, {":", ':', true},
		{"", 0, false}, {"ab", 0, false}, {"é", 0, false}, {"\xff", 0, false},
	} {
		got, ok := Delimiter(tc.text)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Delimiter(%q) = %q, %v; want %q, %v", tc.text, got, ok, tc.want, tc.ok)
		}
	}
}

func TestColumnsShapeTheShownTheMatchedAndTheAcceptedText(t *testing.T) {
	c := Columns{
		With:      mustParse(t, "2"),
		Match:     mustParse(t, "{3} {1}"),
		Delimiter: '\t',
	}
	raw := []byte("id\tShown \xff\tsecret")
	if got := c.Shown(raw); got != "Shown �" {
		t.Errorf("Shown = %q", got)
	}
	if got, ok := c.Matched(raw); !ok || got != "secret id" {
		t.Errorf("Matched = %q, %v", got, ok)
	}
	if !bytes.Equal(raw, []byte("id\tShown \xff\tsecret")) {
		t.Errorf("the bytes changed: %q", raw)
	}
	if got := c.Accepted(raw); !bytes.Equal(got, raw) {
		t.Errorf("Accepted without --accept-nth = %q, want the line as read", got)
	}
}

func TestColumnsWithoutSelectionsUseTheWholeLine(t *testing.T) {
	c := Columns{Delimiter: '\t'}
	raw := []byte("a\tb")
	if got := c.Shown(raw); got != "a\tb" {
		t.Errorf("Shown = %q", got)
	}
	if got, ok := c.Matched(raw); ok || got != "" {
		t.Errorf("Matched = %q, %v; want no match text", got, ok)
	}
}

func TestAcceptNthPrintsTheChosenColumns(t *testing.T) {
	c := Columns{Accept: mustParse(t, "2"), Delimiter: ','}
	if got := c.Accepted([]byte("1,First")); string(got) != "First" {
		t.Errorf("Accepted = %q", got)
	}
}

func TestSelectionsFollowTheirRunesDelimiter(t *testing.T) {
	c := Columns{With: mustParse(t, "2"), Accept: mustParse(t, "{1}"), Match: mustParse(t, "3"), Delimiter: ','}
	line := []byte("a,b,c")
	m, _ := c.Matched(line)
	if c.Shown(line) != "b" || string(c.Accepted(line)) != "a" || m != "c" {
		t.Errorf("shown %q accepted %q matched %q", c.Shown(line), c.Accepted(line), m)
	}
}

// Invalid UTF-8 is shown as Rust's String::from_utf8_lossy shows it: one
// U+FFFD per maximal invalid subpart (Unicode §3.9), not per byte.
func TestInvalidBytesShowAsOneReplacementPerMaximalSubpart(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{"a\xe2\x82b", "a�b"},
		{"\xff\xfe", "��"},
		{"\xf0\x9f\x98", "�"},
		{"\xe0\x80\x80", "���"},
		{"\xed\xa0\x80", "���"},
		{"\xf4\x90\x80\x80", "����"},
		{"\xc0\xaf", "��"},
		{"é\xc3", "é�"},
		{"\xf0\x9f\x98\x80", "😀"},
	} {
		if got := Lossy([]byte(tc.in)); got != tc.want {
			t.Errorf("Lossy(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
