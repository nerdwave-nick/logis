package list

import (
	"slices"
	"testing"
)

func TestGraphemePositionsBecomeJoinedByteRanges(t *testing.T) {
	for _, tc := range []struct {
		text      string
		positions []uint32
		want      []Range
	}{
		{"xaxbc", []uint32{1, 3, 4}, []Range{{1, 2}, {3, 5}}},
		{"étude", []uint32{0, 1}, []Range{{0, 4}}},
		{"öλ", []uint32{1}, []Range{{2, 4}}},
		{"ab", nil, nil},
		{"ab", []uint32{0, 1, 2, 9}, []Range{{0, 2}}},
	} {
		if got := ByteRanges(tc.text, tc.positions); !slices.Equal(got, tc.want) {
			t.Errorf("%q %v: %v, want %v", tc.text, tc.positions, got, tc.want)
		}
	}
}

// A match shows on the shown text when that is what is matched, or when
// the match text begins with it (matches beyond it are then not shown).
func TestAnItemIsHighlightedWhenItsShownTextIsWhatWasMatched(t *testing.T) {
	for _, tc := range []struct {
		entry Entry
		want  bool
	}{
		{Entry{Text: "Anoint"}, true},
		{Entry{Text: "Anoint", MatchText: "Anoint\tHoly Oil unguent", HasMatchText: true}, true},
		{Entry{Text: "Shown", MatchText: "secret id", HasMatchText: true}, false},
	} {
		if got := tc.entry.Highlightable(); got != tc.want {
			t.Errorf("%+v: %v", tc.entry, got)
		}
	}
	f := filter("Anoint\tHoly Oil unguent")
	f.SetQuery("ano ung")
	got := ByteRanges("Anoint", f.Positions(0))
	if !slices.Equal(got, []Range{{0, 3}}) {
		t.Errorf("%v: only what is shown is highlighted", got)
	}
}
