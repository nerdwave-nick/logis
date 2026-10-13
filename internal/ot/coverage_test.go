package ot_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/ot"
	"github.com/nerdwave-nick/logis/internal/shape"
)

// The HarfBuzz reference runs of internal/shape exercise every lookup
// type and format the engine implements: this lists what applied.
func TestTheReferenceRunsExerciseEveryLookupType(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "shape", "testdata", "hb.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Runs []struct {
			Font     string   `json:"font"`
			Text     string   `json:"text"`
			RTL      bool     `json:"rtl"`
			PPEM     int      `json:"ppem"`
			Features []string `json:"features"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	ot.SetApplyHook(func(gsub bool, kind, format int, ext bool) {
		table := "GPOS"
		if gsub {
			table = "GSUB"
		}
		seen[fmt.Sprintf("%s %d.%d", table, kind, format)]++
		if ext {
			seen[table+" extension"]++
		}
	})
	defer ot.SetApplyHook(nil)
	fonts := map[string]*shape.Font{}
	for _, run := range d.Runs {
		f, ok := fonts[run.Font]
		if !ok {
			data := readFont(t, run.Font)
			face, err := ot.Parse(data, 0)
			if err != nil {
				t.Fatal(err)
			}
			f = shape.NewFont(face)
			fonts[run.Font] = f
		}
		opt := shape.Options{RTL: run.RTL, PPEM: run.PPEM}
		for _, s := range run.Features {
			v := uint32(1)
			if rest, ok := strings.CutPrefix(s, "-"); ok {
				s, v = rest, 0
			}
			if tag, val, ok := strings.Cut(s, "="); ok {
				n, _ := strconv.Atoi(val)
				s, v = tag, uint32(n)
			}
			opt.Features = append(opt.Features, shape.Feature{Tag: ot.MakeTag(s), Value: v})
		}
		f.Shape(run.Text, opt)
	}
	var keys []string
	for k := range seen {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		t.Logf("%-16s %d", k, seen[k])
	}
	for _, want := range wantApplied {
		if seen[want] == 0 {
			t.Errorf("%s never applied", want)
		}
	}
}

var wantApplied = []string{
	"GSUB 1.1", "GSUB 1.2", "GSUB 2.1", "GSUB 3.1", "GSUB 4.1", "GSUB 5.1", "GSUB 5.2", "GSUB 5.3",
	"GSUB 6.1", "GSUB 6.2", "GSUB 6.3", "GSUB 8.1", "GSUB extension",
	"GPOS 1.1", "GPOS 1.2", "GPOS 2.1", "GPOS 2.2", "GPOS 3.1", "GPOS 4.1", "GPOS 5.1", "GPOS 6.1",
	"GPOS 7.1", "GPOS 7.2", "GPOS 7.3", "GPOS 8.1", "GPOS 8.2", "GPOS 8.3", "GPOS extension",
}

func readFont(t *testing.T, name string) []byte {
	t.Helper()
	for _, dir := range []string{filepath.Join("..", "..", "tests", "fonts"), filepath.Join("..", "shape", "testdata", "fonts")} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return data
		}
	}
	t.Fatalf("no font %s", name)
	return nil
}
