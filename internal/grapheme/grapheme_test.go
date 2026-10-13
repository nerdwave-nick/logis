package grapheme

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestConformance runs the UCD 17.0.0 GraphemeBreakTest.txt: every case
// splits exactly as the file says.
func TestConformance(t *testing.T) {
	f, err := os.Open("testdata/GraphemeBreakTest.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	cases, failed := 0, 0
	for s.Scan() {
		line, _, _ := strings.Cut(s.Text(), "#")
		if strings.TrimSpace(line) == "" {
			continue
		}
		var text, cur strings.Builder
		var want []string
		for _, tok := range strings.Fields(line) {
			switch tok {
			case "÷":
				if cur.Len() > 0 {
					want = append(want, cur.String())
					cur.Reset()
				}
			case "×":
			default:
				v, err := strconv.ParseUint(tok, 16, 32)
				if err != nil {
					t.Fatal(err)
				}
				cur.WriteRune(rune(v))
				text.WriteRune(rune(v))
			}
		}
		cases++
		got := Clusters(text.String())
		if strings.Join(got, "|") != strings.Join(want, "|") {
			failed++
			if failed < 10 {
				t.Errorf("%s: got %q want %q", strings.TrimSpace(line), got, want)
			}
		}
	}
	if cases < 700 {
		t.Fatalf("only %d cases", cases)
	}
	if failed > 0 {
		t.Errorf("%d of %d cases failed", failed, cases)
	}
}

func TestNextIsTheFirstClustersLength(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"ab", 1},
		{"étude", 3},
		{"👍🏽 up", 8},
		{"🇩🇪🇫🇷", 8},
		{"\r\nx", 2},
		{"\xffa", 1},
	} {
		if got := Next(tc.in); got != tc.want {
			t.Errorf("Next(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
