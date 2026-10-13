package shape

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/ot"
)

// hbRun is one HarfBuzz reference run (testdata/hbshape.py).
type hbRun struct {
	Font     string     `json:"font"`
	Text     string     `json:"text"`
	RTL      bool       `json:"rtl"`
	PPEM     int        `json:"ppem"`
	Features []string   `json:"features"`
	Glyphs   [][5]int32 `json:"glyphs"` // id, cluster, x advance, x offset, y offset
}

type hbData struct {
	HarfBuzz string            `json:"harfbuzz"`
	Fonts    map[string]string `json:"fonts"`
	Runs     []hbRun           `json:"runs"`
}

func loadOracle(t testing.TB) hbData {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "hb.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d hbData
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func fontData(t testing.TB, name string) []byte {
	t.Helper()
	for _, dir := range []string{filepath.Join("..", "..", "tests", "fonts"), filepath.Join("testdata", "fonts")} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return data
		}
	}
	t.Fatalf("no font %s", name)
	return nil
}

// parseFeature reads HarfBuzz's "tag", "-tag" and "tag=value".
func parseFeature(s string) Feature {
	v := uint32(1)
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		s, v = rest, 0
	}
	if tag, val, ok := strings.Cut(s, "="); ok {
		n, _ := strconv.Atoi(val)
		s, v = tag, uint32(n)
	}
	return Feature{Tag: ot.MakeTag(s), Value: v}
}

func describe(gs [][5]int32) string {
	var b strings.Builder
	for _, g := range gs {
		fmt.Fprintf(&b, " %d@%d+%d(%d,%d)", g[0], g[1], g[2], g[3], g[4])
	}
	return b.String()
}

// Every reference run shapes to HarfBuzz's glyphs, clusters, advances and
// offsets.
func TestRunsMatchHarfBuzz(t *testing.T) {
	d := loadOracle(t)
	fonts := map[string]*Font{}
	failed := 0
	for i, run := range d.Runs {
		f, ok := fonts[run.Font]
		if !ok {
			data := fontData(t, run.Font)
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != d.Fonts[run.Font] {
				t.Fatalf("%s changed since hb.json was made; rerun testdata/hbshape.py", run.Font)
			}
			face, err := ot.Parse(data, 0)
			if err != nil {
				t.Fatal(err)
			}
			f = NewFont(face)
			fonts[run.Font] = f
		}
		opt := Options{RTL: run.RTL, PPEM: run.PPEM}
		for _, s := range run.Features {
			opt.Features = append(opt.Features, parseFeature(s))
		}
		var got [][5]int32
		for _, g := range f.Shape(run.Text, opt) {
			got = append(got, [5]int32{int32(g.ID), int32(g.Cluster), g.XAdvance, g.XOffset, g.YOffset})
		}
		if describe(got) != describe(run.Glyphs) {
			failed++
			if failed <= 25 {
				t.Errorf("run %d %s %q rtl=%v %v:\n  got %s\n want %s", i, run.Font, run.Text, run.RTL, run.Features, describe(got), describe(run.Glyphs))
			}
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d runs differ from HarfBuzz %s", failed, len(d.Runs), d.HarfBuzz)
	}
}
