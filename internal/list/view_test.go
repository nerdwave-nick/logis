package list

import (
	"reflect"
	"testing"

	"github.com/nerdwave-nick/logis/internal/input"
)

// The View: the query, its matches, the selection and the rows in view.

func plain(texts ...string) []Entry {
	out := make([]Entry, len(texts))
	for i, t := range texts {
		out[i] = Entry{Text: t}
	}
	return out
}

func lines(n uint32) *uint32 { return &n }

func viewOf(o Options, texts ...string) *View {
	v := NewView(o)
	v.Push(plain(texts...))
	return v
}

func rowTexts(l Look) []string {
	out := []string{}
	for _, r := range l.Rows {
		out = append(out, r.Text)
	}
	return out
}

func typed(v *View, text string) Reaction { return v.Apply(input.Insert(text)) }

func edit(kind input.EditKind) input.Action { return input.EditAction(input.Edit{Kind: kind}) }

func selected(t *testing.T, v *View, want uint32) {
	t.Helper()
	if got, ok := v.Selected(); !ok || got != want {
		t.Fatalf("selected %d (%v), want %d", got, ok, want)
	}
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

var (
	unchanged = Reaction{Kind: Unchanged}
	changed   = Reaction{Kind: Changed}
	dismiss   = Reaction{Kind: Dismiss}
)

func accept(c Choice) Reaction { return Reaction{Kind: Accept, Choice: c} }

func item(i uint32) Choice        { return Choice{Kind: ChoseItem, Index: i} }
func descend(i uint32) Choice     { return Choice{Kind: ChoseDescend, Index: i} }
func typedText(s string) Choice   { return Choice{Kind: ChoseTyped, Text: s} }
func pending(text string) Entry   { return Entry{Text: text, Decor: &Decor{Pending: true}} }
func settledEntry(s string) Entry { return Entry{Text: s} }

func TestRowsShowAtMostLinesWithTheFirstSelected(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Lines: lines(2)}, "a", "b", "c")
	look := v.Look()
	eq(t, rowTexts(look), []string{"a", "b"})
	if !look.Rows[0].Selected || look.Rows[1].Selected {
		t.Fatalf("selection %+v", look.Rows)
	}
	selected(t, v, 0)
}

func TestTheSelectionMovesAndTheRowsScrollToKeepItInView(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Lines: lines(2)}, "a", "b", "c")
	eq(t, v.Apply(input.ActPrevious), unchanged)
	eq(t, v.Apply(input.ActNext), changed)
	eq(t, v.Apply(input.ActNext), changed)
	eq(t, v.Apply(input.ActNext), unchanged) // the last item stops it
	look := v.Look()
	eq(t, rowTexts(look), []string{"b", "c"})
	if !look.Rows[1].Selected {
		t.Fatal("c is not selected")
	}
	v.Apply(input.ActPrevious)
	v.Apply(input.ActPrevious)
	eq(t, rowTexts(v.Look()), []string{"a", "b"})
}

func TestTypingFiltersHighlightsAndReselectsTheBest(t *testing.T) {
	v := viewOf(DefaultOptions(), "xaxbc", "abc", "zzz")
	v.Apply(input.ActNext)
	typed(v, "abc")
	look := v.Look()
	eq(t, rowTexts(look), []string{"abc", "xaxbc"})
	if !look.Rows[0].Selected {
		t.Fatal("the best match is not selected")
	}
	eq(t, look.Rows[0].Highlights, []Range{{0, 3}})
	eq(t, look.Rows[1].Highlights, []Range{{1, 2}, {3, 5}})
	eq(t, [3]any{look.Input, look.Cursor, look.HasCursor}, [3]any{"abc", 3, true})
}

func TestReturnAcceptsTheSelectedMatch(t *testing.T) {
	v := viewOf(DefaultOptions(), "a", "b")
	v.Apply(input.ActNext)
	eq(t, v.Apply(input.ActAccept), accept(item(1)))
	eq(t, v.Apply(input.ActDismiss), dismiss)
}

func TestReturnWithoutAMatchAcceptsTheTypedTextUnlessOnlyMatch(t *testing.T) {
	v := viewOf(DefaultOptions(), "a")
	typed(v, "zz top")
	eq(t, v.Apply(input.ActAccept), accept(typedText("zz top")))
	strict := viewOf(Options{Prompt: "> ", OnlyMatch: true}, "a")
	typed(strict, "zz")
	eq(t, strict.Apply(input.ActAccept), unchanged)
}

func TestAnEmptyListAcceptsTheTypedText(t *testing.T) {
	v := viewOf(DefaultOptions())
	eq(t, v.Apply(input.ActAccept), accept(typedText("")))
}

func TestItemsStreamingInKeepTheItemTheUserMovedTo(t *testing.T) {
	v := viewOf(DefaultOptions(), "xaxb1", "xaxb2")
	typed(v, "ab")
	v.Apply(input.ActNext)
	selected(t, v, 1)
	// A better match arrives and sorts first.
	if !v.Push(plain("ab")) {
		t.Fatal("the push changed nothing")
	}
	selected(t, v, 1) // still xaxb2
	eq(t, rowTexts(v.Look()), []string{"ab", "xaxb1", "xaxb2"})
}

func TestSelectPreselectsTheFirstEqualItemIgnoringCaseWhenItArrives(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Select: &Preselect{Text: "SECOND"}}, "first")
	selected(t, v, 0)
	v.Push(plain("second", "Second"))
	selected(t, v, 1)
}

func TestSelectIndexPreselectsOnceItArrivesUnlessTheUserMovedFirst(t *testing.T) {
	o := Options{Prompt: "> ", Select: &Preselect{Index: 2, ByIndex: true}}
	v := viewOf(o, "a", "b")
	v.Push(plain("c"))
	selected(t, v, 2)
	moved := viewOf(o, "a", "b")
	moved.Apply(input.ActNext)
	moved.Push(plain("c"))
	selected(t, moved, 1)
}

func TestTypingEndsAPreselectionStillWaitingForItsItem(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Select: &Preselect{Text: "beta"}}, "alpha", "alpine")
	typed(v, "x")
	v.Apply(edit(input.Clear))
	v.Push(plain("beta"))
	eq(t, rowTexts(v.Look()), []string{"alpha", "alpine", "beta"})
	selected(t, v, 0) // the edit ended the preselection
}

func TestSearchOpensWithAQuery(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Search: "b"}, "a", "b")
	eq(t, v.Query(), "b")
	eq(t, rowTexts(v.Look()), []string{"b"})
}

func TestAPasswordIsMaskedPerCharacter(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Password: '*'})
	typed(v, "pä")
	v.Apply(edit(input.Left))
	look := v.Look()
	eq(t, [3]any{look.Input, look.Cursor, look.HasCursor}, [3]any{"**", 1, true})
	eq(t, v.Query(), "pä")
	wide := viewOf(Options{Prompt: "> ", Password: '•'})
	typed(wide, "ab")
	look = wide.Look()
	eq(t, [2]any{look.Input, look.Cursor}, [2]any{"••", 6})
}

func TestAnEmptyQueryShowsThePlaceholderWithoutACursor(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Placeholder: "seek the rite"})
	look := v.Look()
	eq(t, [2]any{look.Input, look.HasCursor}, [2]any{"seek the rite", false})
	typed(v, "x")
	look = v.Look()
	eq(t, [2]any{look.Cursor, look.HasCursor}, [2]any{1, true})
}

func TestTheCounterCountsMatchesOfAll(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Counter: true}, "ab", "b", "c")
	typed(v, "b")
	look := v.Look()
	eq(t, [3]any{look.Counter, look.Matched, look.Total}, [3]any{true, 2, 3})
	if viewOf(DefaultOptions(), "a").Look().Counter {
		t.Fatal("a counter without --counter")
	}
}

func decorated(text string, d Decor) Entry { return Entry{Text: text, Decor: &d} }

func TestAPendingItemIsNeverAccepted(t *testing.T) {
	v := NewView(DefaultOptions())
	v.Push([]Entry{pending("awaited"), settledEntry("ready")})
	selected(t, v, 0)
	eq(t, v.Apply(input.ActAccept), unchanged)
	if !v.Look().Rows[0].Pending {
		t.Fatal("the row is not pending")
	}
	v.Update(0, settledEntry("settled"))
	eq(t, v.Apply(input.ActAccept), accept(item(0)))
}

func TestAPendingItemIsPreselectedOnlyOnceItSettles(t *testing.T) {
	v := NewView(Options{Prompt: "> ", Select: &Preselect{Index: 1, ByIndex: true}})
	v.Push([]Entry{settledEntry("a"), pending("current")})
	selected(t, v, 0) // pending: not preselected
	v.Update(1, settledEntry("current"))
	selected(t, v, 1) // settled: preselected
}

func TestALatePreselectionAppliesUntilTheUserMoves(t *testing.T) {
	v := viewOf(DefaultOptions(), "a", "b", "c")
	if !v.Preselect(2) {
		t.Fatal("not preselected")
	}
	selected(t, v, 2)
	v.Push([]Entry{pending("d")})
	if v.Preselect(3) {
		t.Fatal("a pending item was preselected")
	}
	v.Update(3, settledEntry("d"))
	selected(t, v, 3) // applied once settled
	v.Apply(input.ActPrevious)
	if v.Preselect(0) {
		t.Fatal("preselected after the user moved")
	}
	selected(t, v, 2)
}

func TestAnUpdateChangesAnItemInPlaceAndKeepsTheUsersChoice(t *testing.T) {
	v := viewOf(DefaultOptions(), "a", "b", "c")
	v.Apply(input.ActNext)
	if !v.Update(2, settledEntry("c2")) {
		t.Fatal("no update")
	}
	eq(t, rowTexts(v.Look()), []string{"a", "b", "c2"})
	selected(t, v, 1)
	typed(v, "c")
	v.Update(0, settledEntry("ccc"))
	eq(t, rowTexts(v.Look()), []string{"ccc", "c2"})
	if v.Update(9, settledEntry("x")) {
		t.Fatal("updated an item that does not exist")
	}
}

func TestARemovedItemLeavesTheListAndTheSelectionStaysOnAMatch(t *testing.T) {
	v := viewOf(DefaultOptions(), "a", "b", "c")
	v.Apply(input.ActNext)
	v.Apply(input.ActNext)
	if !v.Remove(2) {
		t.Fatal("not removed")
	}
	eq(t, rowTexts(v.Look()), []string{"a", "b"})
	selected(t, v, 1)
	if v.Remove(2) {
		t.Fatal("removed twice")
	}
	v.Update(2, settledEntry("back"))
	eq(t, rowTexts(v.Look()), []string{"a", "b"}) // a removed item stays gone
}

func TestRequiredTextIsNeverEmpty(t *testing.T) {
	v := viewOf(Options{Prompt: "> ", Required: true})
	eq(t, v.Apply(input.ActAccept), unchanged)
	typed(v, "x")
	eq(t, v.Apply(input.ActAccept), accept(typedText("x")))
}

func TestRowsCarryGlyphsBadgesAndPendingAndTheListReservesAGlyphColumn(t *testing.T) {
	v := NewView(DefaultOptions())
	if v.Look().Glyphs {
		t.Fatal("a glyph column for plain items")
	}
	v.Push([]Entry{decorated("oil", Decor{Glyph: "⚙", Badges: []string{"current"}}), settledEntry("plain")})
	look := v.Look()
	if !look.Glyphs {
		t.Fatal("no glyph column")
	}
	eq(t, look.Rows[0].Glyph, "⚙")
	eq(t, look.Rows[0].Badges, []string{"current"})
	eq(t, [2]any{look.Rows[1].Glyph, look.Rows[1].Pending}, [2]any{"", false})
}

func TestAnEmptyGlyphStillKeepsTheGlyphColumnUntilItGoes(t *testing.T) {
	v := NewView(DefaultOptions())
	v.Push([]Entry{decorated("oil", Decor{HasGlyph: true}), settledEntry("plain")})
	look := v.Look()
	if !look.Glyphs {
		t.Fatal("an empty glyph gave up the glyph column")
	}
	eq(t, look.Rows[0].Glyph, "")
	v.Update(0, settledEntry("oil"))
	if v.Look().Glyphs {
		t.Fatal("the glyph column outlived its last glyph")
	}
	v.Update(0, decorated("oil", Decor{HasIcon: true}))
	v.Remove(0)
	if v.Look().Glyphs {
		t.Fatal("the glyph column outlived its removed icon")
	}
}

func TestItemsMatchedByAnotherTextAreNotHighlighted(t *testing.T) {
	v := NewView(DefaultOptions())
	v.Push([]Entry{{Text: "Shown", MatchText: "hidden key", HasMatchText: true}})
	typed(v, "key")
	look := v.Look()
	eq(t, rowTexts(look), []string{"Shown"})
	if len(look.Rows[0].Highlights) != 0 {
		t.Fatalf("highlights %v", look.Rows[0].Highlights)
	}
	typed(v, "x")
	if len(v.Look().Rows) != 0 {
		t.Fatal("rows remain")
	}
}

func TestAMatchTextThatBeginsWithTheShownTextHighlightsIt(t *testing.T) {
	v := NewView(DefaultOptions())
	v.Push([]Entry{{Text: "Anoint", MatchText: "Anoint\tHoly Oil unguent", HasMatchText: true}})
	typed(v, "ano")
	eq(t, v.Look().Rows[0].Highlights, []Range{{0, 3}})
	typed(v, " ung")
	look := v.Look()
	eq(t, rowTexts(look), []string{"Anoint"}) // matched beyond the shown text
	eq(t, look.Rows[0].Highlights, []Range{{0, 3}})
}

func TestABoostRanksAnItemHigherAndRowsCarryIcons(t *testing.T) {
	v := NewView(DefaultOptions())
	v.Push([]Entry{settledEntry("rite of oil"), decorated("rite of oil!", Decor{Icon: "forge-cog", Boost: 30})})
	typed(v, "rite")
	look := v.Look()
	eq(t, rowTexts(look), []string{"rite of oil!", "rite of oil"})
	eq(t, look.Rows[0].Icon, "forge-cog")
	if !look.Glyphs {
		t.Fatal("an icon needs the icon column")
	}
}

func TestDescendChoosesTheItemForItsNextStage(t *testing.T) {
	v := viewOf(DefaultOptions(), "oil", "fire")
	v.Apply(input.ActNext)
	eq(t, v.Apply(input.ActDescend), accept(descend(1)))
	typed(v, "zz")
	// Typed text has no next stage: Shift+Return is Return.
	eq(t, v.Apply(input.ActDescend), accept(typedText("zz")))
	p := NewView(DefaultOptions())
	p.Push([]Entry{pending("oil")})
	eq(t, p.Apply(input.ActDescend), unchanged)
}

func TestAnEditThatChangesNothingIsUnchanged(t *testing.T) {
	v := viewOf(DefaultOptions(), "a")
	eq(t, v.Apply(edit(input.DeleteBack)), unchanged)
	typed(v, "a")
	eq(t, v.Apply(edit(input.Left)), changed) // the cursor moved
}

func TestSetLinesKeepsTheSelectionInView(t *testing.T) {
	v := viewOf(DefaultOptions(), "a", "b", "c", "d")
	v.Apply(input.ActNext)
	v.Apply(input.ActNext)
	v.Apply(input.ActNext)
	v.SetLines(2)
	look := v.Look()
	eq(t, rowTexts(look), []string{"c", "d"})
	eq(t, look.Lines, uint32(2))
}

func TestNoLinesShowNoRows(t *testing.T) {
	v := viewOf(Options{Prompt: "pin: ", Lines: lines(0)}, "a")
	look := v.Look()
	eq(t, [3]any{len(look.Rows), look.Lines, look.Prompt}, [3]any{0, uint32(0), "pin: "})
	selected(t, v, 0)
}
