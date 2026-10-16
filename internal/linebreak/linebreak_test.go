package linebreak

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Every case of UCD 17's LineBreakTest.txt: the break opportunities
// between characters (and at the end) are exactly the test's ÷ marks.
func TestLineBreakConformance(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "LineBreakTest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	cases, failed := 0, 0
	for s.Scan() {
		line, _, _ := strings.Cut(s.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		var text strings.Builder
		var want []int
		for _, f := range fields {
			switch f {
			case "÷":
				if text.Len() > 0 {
					want = append(want, text.Len())
				}
			case "×":
			default:
				v, _ := strconv.ParseUint(f, 16, 32)
				text.WriteRune(rune(v))
			}
		}
		got := Breaks(text.String())
		cases++
		if fmt.Sprint(got) != fmt.Sprint(want) {
			failed++
			if failed <= 15 {
				t.Errorf("%s\n  got %v, want %v", s.Text(), got, want)
			}
		}
	}
	if cases < 10000 {
		t.Fatalf("only %d cases read", cases)
	}
	if failed > 0 {
		t.Errorf("%d of %d cases differ", failed, cases)
	}
}

func TestWordsBreakAfterSpacesAndHyphens(t *testing.T) {
	cases := map[string]string{
		"do-not-disturb on":          "[3 7 15 17]",
		"The machine spirit":         "[4 12 18]",
		"logis: (42) ok":             "[7 12 14]",
		"機械の精霊":                      "[3 6 9 12 15]",
		"":                           "[]",
		"a\u00a0b":                   "[4]",
		"مرحبا بالعالم":              "[11 25]",
		"1.5 kg, -3 (x)":             "[4 8 11 14]",
		"e\u0301 x":                  "[4 5]",
		"\u05e9\u05dc\u05d5\u05dd-x": "[10]",
	}
	for s, want := range cases {
		if got := fmt.Sprint(Breaks(s)); got != want {
			t.Errorf("Breaks(%q) = %s, want %s", s, got, want)
		}
	}
}
