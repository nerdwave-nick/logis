package bidi

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every case of UCD 17's BidiCharacterTest.txt: paragraph level, the
// levels after rule L1 and the visual order (rule L2), with brackets
// paired (BD16) — which golang.org/x/text's own input preparation fails.
func TestBidiCharacterConformance(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "BidiCharacterTest.txt.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	s := bufio.NewScanner(z)
	cases, failed := 0, 0
	for s.Scan() {
		line, _, _ := strings.Cut(s.Text(), "#")
		fields := strings.Split(line, ";")
		if len(fields) < 5 {
			continue
		}
		var runes []rune
		for _, h := range strings.Fields(fields[0]) {
			v, _ := strconv.ParseUint(h, 16, 32)
			runes = append(runes, rune(v))
		}
		dir := map[string]level{"0": 0, "1": 1, "2": implicitLevel}[fields[1]]
		p, err := paragraphOf(runes, dir)
		if err != nil {
			t.Fatal(err)
		}
		levels := p.getLevels([]int{len(runes)})
		var gotLevels, keep []string
		var kept []int8
		for i, l := range levels {
			if removedByX9(p.initialTypes[i]) {
				gotLevels = append(gotLevels, "x")
				continue
			}
			gotLevels = append(gotLevels, strconv.Itoa(int(l)))
			keep = append(keep, strconv.Itoa(i))
			kept = append(kept, int8(l))
		}
		var order []string
		for _, i := range Reorder(kept) {
			order = append(order, keep[i])
		}
		got := fmt.Sprintf("%d %v %v", p.embeddingLevel, gotLevels, order)
		want := fmt.Sprintf("%s %v %v", strings.TrimSpace(fields[2]), strings.Fields(fields[3]), strings.Fields(fields[4]))
		cases++
		if got != want {
			failed++
			if failed <= 10 {
				t.Errorf("%U dir %s: got %s, want %s", runes, fields[1], got, want)
			}
		}
	}
	if cases < 90000 {
		t.Fatalf("only %d cases read", cases)
	}
	if failed > 0 {
		t.Errorf("%d of %d cases differ", failed, cases)
	}
}

func TestTheCorpusLineKeepsItsBracketsInTheArabicRun(t *testing.T) {
	line := []rune("logis: שָׁלוֹם and مرحبا بالعالم (42) ok")
	levels, rtl := Resolve(line)
	if rtl {
		t.Fatal("the line starts with Latin: left to right")
	}
	open, close := slices.Index(line, '('), slices.Index(line, ')')
	if levels[open] != 1 || levels[close] != 1 || levels[open+1] != 2 {
		t.Fatalf("brackets at %d and %d, 42 at %d: want 1, 1, 2", levels[open], levels[close], levels[open+1])
	}
	if levels[len(line)-1] != 0 {
		t.Fatal("ok is left to right")
	}
}

// Rule L1, as logis applies it: white space before a segment separator
// and at the end of the line takes the paragraph level, while explicit
// formatting characters and boundary neutrals among it are skipped (keep
// their level) instead of reset, a deliberate variation of UAX #9 L1.
func TestTrailingWhitespaceTakesTheParagraphLevel(t *testing.T) {
	levels, rtl := Resolve([]rune("שלום  "))
	if !rtl || fmt.Sprint(levels) != "[1 1 1 1 1 1]" {
		t.Fatalf("RTL with trailing spaces: %v %v", levels, rtl)
	}
	levels, _ = Resolve([]rune("abc אב\u200d "))
	// A ZWJ (BN) right after the Hebrew keeps the Hebrew's level, where
	// rule L1 as x/text applies it would reset it with the final space.
	if levels[5] != 1 || levels[6] != 1 || levels[7] != 0 {
		t.Fatalf("levels %v", levels)
	}
	if levels, rtl := Resolve(nil); levels != nil || rtl {
		t.Fatal("empty")
	}
}

func TestReorderReversesRunsFromTheHighestLevel(t *testing.T) {
	cases := []struct {
		levels []int8
		want   []int
	}{
		{[]int8{0, 0, 0}, []int{0, 1, 2}},
		{[]int8{1, 1, 1}, []int{2, 1, 0}},
		{[]int8{0, 1, 1, 0}, []int{0, 2, 1, 3}},
		{[]int8{1, 2, 2, 1}, []int{3, 1, 2, 0}},
		{[]int8{0, 1, 2, 2, 1, 0}, []int{0, 4, 2, 3, 1, 5}},
		{nil, nil},
	}
	for _, c := range cases {
		if got := Reorder(c.levels); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("Reorder(%v) = %v, want %v", c.levels, got, c.want)
		}
	}
}
