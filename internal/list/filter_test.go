package list

import (
	"fmt"
	"slices"
	"testing"

	"github.com/nerdwave-nick/logis/internal/fuzzy"
)

func filter(items ...string) *Filter {
	f := NewFilter()
	f.Push(items...)
	return f
}

func indices(f *Filter) []uint32 {
	out := []uint32{}
	for _, m := range f.Matches() {
		out = append(out, m.Index)
	}
	return out
}

func sorted(xs []uint32) []uint32 {
	xs = slices.Clone(xs)
	slices.Sort(xs)
	return xs
}

func want(t *testing.T, got, want []uint32, why string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: %v, want %v", why, got, want)
	}
}

func TestAnEmptyQueryMatchesEverythingInInputOrder(t *testing.T) {
	f := filter("b", "a", "c")
	want(t, indices(f), []uint32{0, 1, 2}, "matches")
	if f.Len() != 3 {
		t.Errorf("Len = %d", f.Len())
	}
}

func TestMatchingIsFuzzyAndBestFirst(t *testing.T) {
	f := filter("xaxbxc", "abc", "nothing", "a-b-c")
	f.SetQuery("abc")
	got := indices(f)
	if len(got) != 3 || got[0] != 1 || slices.Contains(got, 2) {
		t.Errorf("%v: want the contiguous match first and no 'nothing'", got)
	}
}

func TestMatchingIgnoresCaseAndAccents(t *testing.T) {
	f := filter("Café Noir", "CAFE", "tea")
	f.SetQuery("cafe")
	want(t, sorted(indices(f)), []uint32{0, 1}, "cafe")
	f.SetQuery("CAFÉ")
	want(t, indices(f), []uint32{0}, "a typed accent is meant")
}

func TestABoostRaisesAMatchButNeverMakesOne(t *testing.T) {
	f := NewFilter()
	f.PushBoosted([]Boosted{{"rite of oil", 0}, {"rite of oil", 30}, {"litany", 48}})
	f.SetQuery("rite")
	want(t, indices(f), []uint32{1, 0}, "the boosted twin first; no match from a boost")
	f.SetQuery("")
	want(t, indices(f), []uint32{0, 1, 2}, "an empty query keeps input order")
	g := NewFilter()
	g.PushBoosted([]Boosted{{"firefox", 0}, {"foreign rite fox", 8}})
	g.SetQuery("firefox")
	want(t, indices(g), []uint32{0, 1}, "a much better match beats a small boost")
}

func TestEqualScoresKeepInputOrder(t *testing.T) {
	f := filter("ab 2", "ab 1", "ab 3")
	f.SetQuery("ab")
	want(t, indices(f), []uint32{0, 1, 2}, "matches")
}

func TestFzfSyntaxApplies(t *testing.T) {
	f := filter("rite of oil", "oil rite", "litany")
	f.SetQuery("^rite")
	want(t, indices(f), []uint32{0}, "^rite")
	f.SetQuery("!oil")
	want(t, indices(f), []uint32{2}, "!oil")
	f.SetQuery("rite oil")
	if len(f.Matches()) != 2 {
		t.Errorf("rite oil: %v", indices(f))
	}
}

func TestFzfSuffixAndExactTermsApply(t *testing.T) {
	f := filter("rite of oil", "oil rite", "orbital")
	f.SetQuery("oil$")
	want(t, indices(f), []uint32{0}, "oil$")
	f.SetQuery("'rbit")
	want(t, indices(f), []uint32{2}, "'rbit")
	f.SetQuery("rbt")
	want(t, indices(f), []uint32{2}, "fuzzy without the quote")
	f.SetQuery("'rbt")
	want(t, indices(f), []uint32{}, "'rbt")
}

func TestItemsPushedLaterAreMatchedAndMergedInOrder(t *testing.T) {
	f := filter("xxa", "b")
	f.SetQuery("a")
	f.Push("a", "zzzzzzzza", "c")
	if f.Len() != 5 {
		t.Errorf("Len = %d", f.Len())
	}
	whole := filter("xxa", "b", "a", "zzzzzzzza", "c")
	whole.SetQuery("a")
	if !slices.Equal(f.Matches(), whole.Matches()) {
		t.Errorf("%v, want %v", f.Matches(), whole.Matches())
	}
	if indices(f)[0] != 2 {
		t.Errorf("%v: the exact match ranks first", indices(f))
	}
}

func TestAGrowingQueryNarrowsAndAShrinkingOneWidens(t *testing.T) {
	f := filter("abc", "abd", "xyz")
	f.SetQuery("ab")
	if len(f.Matches()) != 2 {
		t.Errorf("ab: %v", indices(f))
	}
	f.SetQuery("abc")
	want(t, indices(f), []uint32{0}, "abc")
	f.SetQuery("a")
	if len(f.Matches()) != 2 {
		t.Errorf("a: %v", indices(f))
	}
	f.SetQuery("")
	want(t, indices(f), []uint32{0, 1, 2}, "empty")
	// A negation that grows excludes less: it must not narrow from the
	// last matches.
	g := filter("abc", "acb", "xyz")
	g.SetQuery("!a")
	want(t, indices(g), []uint32{2}, "!a")
	g.SetQuery("!ab")
	want(t, indices(g), []uint32{1, 2}, "!ab")
}

// serial is what one matcher over every item gives, sorted best first
// and then in input order: the reference for the parallel path.
func serial(items []string, query string) []Match {
	p := fuzzy.Parse(query)
	m := fuzzy.NewMatcher()
	var out []Match
	for i, s := range items {
		if score, ok := p.Score(m, fuzzy.NewText(s)); ok {
			out = append(out, Match{Index: uint32(i), Score: score})
		}
	}
	slices.SortStableFunc(out, func(a, b Match) int { return int(b.Score) - int(a.Score) })
	return out
}

func TestLargeSetsMatchInParallelExactlyAsOneThreadWould(t *testing.T) {
	var items []string
	for i := range 3 * parallelMin {
		items = append(items, fmt.Sprintf("%s-%d", []string{"cogitator", "rite", "aspect"}[i%3], i))
	}
	f := NewFilter()
	f.Push(items...)
	for _, q := range []string{"cog1", "e", "rite 2", "!a"} {
		f.SetQuery(q)
		if got, w := f.Matches(), serial(items, q); !slices.Equal(got, w) {
			t.Errorf("%q: %d matches, want %d (or a different order)", q, len(got), len(w))
		}
	}
}

func TestARemovedItemNeverMatchesAgain(t *testing.T) {
	f := filter("abc", "abd", "xyz")
	f.Remove(1)
	want(t, indices(f), []uint32{0, 2}, "gone at once from an empty query")
	f.SetQuery("ab")
	want(t, indices(f), []uint32{0}, "ab")
	f.SetQuery("")
	want(t, indices(f), []uint32{0, 2}, "empty")
	f.SetQuery("x")
	f.SetQuery("")
	want(t, indices(f), []uint32{0, 2}, "and after a full rematch")
	if f.Len() != 3 {
		t.Errorf("Len = %d: indices stay stable", f.Len())
	}
	f.Push("abe")
	f.SetQuery("ab")
	want(t, indices(f), []uint32{0, 3}, "after a push")
	f.Remove(7) // no such item: nothing happens
	f.Remove(0)
	f.Remove(0)
	want(t, indices(f), []uint32{3}, "removed twice")
}

func TestAnUpdatedItemIsMatchedByItsNewTextInItsPlace(t *testing.T) {
	f := filter("x-x-ab", "xyz", "x-x-abd")
	f.SetQuery("ab")
	f.Update(1, "ab")
	if got := indices(f); got[0] != 1 || len(got) != 3 {
		t.Errorf("%v: the exact match ranks first", got)
	}
	f.Update(1, "nothing")
	want(t, indices(f), []uint32{0, 2}, "no longer matching")
	f.SetQuery("")
	f.Update(2, "q")
	want(t, indices(f), []uint32{0, 1, 2}, "input order kept")
	f.Remove(0)
	f.Update(0, "ab")
	want(t, indices(f), []uint32{1, 2}, "a removed item stays removed")
	f.Update(9, "ab") // no such item
	want(t, indices(f), []uint32{1, 2}, "unknown index")
}

func TestPositionsAreGraphemesTheQueryMatched(t *testing.T) {
	f := filter("xaxbc", "étude")
	f.SetQuery("abc")
	want(t, f.Positions(0), []uint32{1, 3, 4}, "xaxbc")
	want(t, f.Positions(1), []uint32{}, "étude does not match")
	f.SetQuery("tud")
	want(t, f.Positions(1), []uint32{1, 2, 3}, "é is one grapheme")
	want(t, f.Positions(5), []uint32{}, "no such item")
	f.SetQuery("")
	want(t, f.Positions(0), []uint32{}, "an empty query")
}

func TestTheQueryInForceIsKept(t *testing.T) {
	f := filter("a")
	f.SetQuery("x")
	if f.Query() != "x" {
		t.Errorf("Query = %q", f.Query())
	}
	f.SetQuery("x") // the same query: nothing to do
	want(t, indices(f), []uint32{}, "x")
}

// A character the match never touches changes nothing: the row
// "power-profile\t·\t…" scores for "o" as its twin with "-" for "·" does
// (the o of "of" after a space, a word start), so it keeps its input place
// among the rows tied with it (the theme window golden shows this order).
func TestARowWithANonASCIICharacterTiesAsItsASCIITwin(t *testing.T) {
	f := filter(
		"do-not-disturb\ton\tSilence the vox of the desktop",
		"keyboard-layout\tus\tThe tongue of the keys",
		"mouse-autohide-toggle\toff\tVeil the cursor when idle",
		"power-profile\t·\tThe hunger of the machine spirit",
		"vox\tauto\tWho hears the machine",
		"night-light\toff\tDim the light of the screens",
		"theme\tforge\tThe visage of the machine",
		"power-profile\t-\tThe hunger of the machine spirit",
	)
	f.SetQuery("o")
	var scores []uint32
	for _, m := range f.Matches() {
		scores = append(scores, m.Score)
	}
	if got, want := indices(f), []uint32{0, 1, 2, 3, 5, 6, 7, 4}; !slices.Equal(got, want) {
		t.Errorf("order %v (scores %v), want %v", got, scores, want)
	}
}
