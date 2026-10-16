package fuzzy

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
)

// divergence is a case where logis deliberately scores differently from
// nucleo 0.3.1 (testdata/divergences.json; each one reviewed, with why).
type divergence struct {
	Item      string   `json:"item"`
	Query     string   `json:"query"`
	Score     *uint32  `json:"score"`
	Positions []uint32 `json:"positions"`
	Why       string   `json:"why"`
}

func divergences(t *testing.T) map[[2]string]*divergence {
	t.Helper()
	b, err := os.ReadFile("testdata/divergences.json")
	if err != nil {
		t.Fatal(err)
	}
	var list []*divergence
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	out := map[[2]string]*divergence{}
	for _, d := range list {
		if d.Why == "" {
			t.Errorf("divergence %q / %q has no reason", d.Item, d.Query)
		}
		out[[2]string{d.Item, d.Query}] = d
	}
	return out
}

func show(score uint32, ok bool) string {
	if !ok {
		return "none"
	}
	return fmt.Sprint(score)
}

// Every case of the corpus scores and highlights exactly as nucleo did, but
// for the reviewed divergences.
func TestScoresAndPositionsAreNucleos(t *testing.T) {
	o := nucleo(t)
	div := divergences(t)
	used := map[*divergence]bool{}
	texts := make([]Text, len(o.Items))
	for i, s := range o.Items {
		texts[i] = NewText(s)
	}
	m := NewMatcher()
	failed := 0
	for _, c := range o.Cases {
		item, query := int(c[0].(float64)), int(c[1].(float64))
		wantOK := c[2] != nil
		var want uint32
		if wantOK {
			want = uint32(c[2].(float64))
		}
		var wantPos []uint32
		for _, p := range c[3].([]any) {
			wantPos = append(wantPos, uint32(p.(float64)))
		}
		if d := div[[2]string{o.Items[item], o.Queries[query]}]; d != nil {
			used[d] = true
			wantOK, want, wantPos = d.Score != nil, 0, d.Positions
			if d.Score != nil {
				want = *d.Score
			}
		}
		p := Parse(o.Queries[query])
		got, ok := p.Score(m, texts[item])
		pos := p.Positions(m, texts[item], nil)
		if ok != wantOK || got != want || !slices.Equal(pos, wantPos) {
			failed++
			if failed <= 25 {
				t.Errorf("%q in %.60q: score %s %v, want %s %v", o.Queries[query], o.Items[item],
					show(got, ok), pos, show(want, wantOK), wantPos)
			}
		}
	}
	if failed > 0 {
		t.Errorf("%d of %d cases differ", failed, len(o.Cases))
	}
	for _, d := range div {
		if !used[d] {
			t.Errorf("divergence %q / %q is not in the corpus", d.Item, d.Query)
		}
	}
}
