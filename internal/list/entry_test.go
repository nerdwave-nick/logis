package list

import "testing"

func TestEntriesMatchByTheirMatchTextOrTheirText(t *testing.T) {
	if got := (Entry{Text: "a"}).Matched(); got != "a" {
		t.Errorf("Matched = %q", got)
	}
	if got := (Entry{Text: "a", MatchText: "b", HasMatchText: true}).Matched(); got != "b" {
		t.Errorf("Matched = %q", got)
	}
}

func TestDecorMakesAnEntryPendingOrEmblemed(t *testing.T) {
	plain := Entry{Text: "line"}
	if plain.IsPending() || plain.HasEmblem() {
		t.Error("a plain dmenu line is neither pending nor emblemed")
	}
	for _, tc := range []struct {
		decor           Decor
		pending, emblem bool
	}{
		{Decor{Pending: true}, true, false},
		{Decor{Glyph: "⚙"}, false, true},
		{Decor{Icon: "forge-cog"}, false, true},
		{Decor{Badges: []string{"current"}, Boost: 3}, false, false},
	} {
		e := Entry{Text: "x", Decor: &tc.decor}
		if e.IsPending() != tc.pending || e.HasEmblem() != tc.emblem {
			t.Errorf("%+v: pending %v emblem %v", tc.decor, e.IsPending(), e.HasEmblem())
		}
	}
}

func TestOptionsDefaultToFuzzelsPromptAndTheThemesRows(t *testing.T) {
	o := DefaultOptions()
	if o.Prompt != "> " || o.Rows() != DefaultRows || DefaultRows != 9 {
		t.Errorf("%+v rows %d", o, o.Rows())
	}
	lines := uint32(0)
	o.Lines = &lines
	if o.Rows() != 0 {
		t.Errorf("lines 0: rows %d", o.Rows())
	}
}
