package provider

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/list"
)

// A provider's stages as the window takes them.

func item(id, text string) Item { return Item{ID: id, Columns: []string{text}} }

func entryTexts(entries []list.Entry) []string {
	texts := []string{}
	for _, e := range entries {
		texts = append(texts, e.Text)
	}
	return texts
}

func staged(t *testing.T, s *Stages, stage Stage) (list.Options, []list.Entry) {
	t.Helper()
	steps, err := s.Apply(stage)
	if err != nil || len(steps) != 1 || steps[0].Kind != StepStage {
		t.Fatalf("%+v, %v", steps, err)
	}
	return steps[0].Options, steps[0].Entries
}

func apply(t *testing.T, s *Stages, m Message) []Step {
	t.Helper()
	steps, err := s.Apply(m)
	if err != nil {
		t.Fatal(err)
	}
	return steps
}

func TestAStageBecomesAListWithItsOptions(t *testing.T) {
	var s Stages
	oil := item("oil", "")
	oil.Columns, oil.Glyph = []string{"rite\tof oil", "unguent"}, ptr("⚙")
	options, entries := staged(t, &s, Stage{ID: "rite", Prompt: ptr("rite: "), Query: "oi",
		Items: []Item{oil, item("fire", "rite of fire")}})
	if options.Prompt != "rite: " || options.Search != "oi" {
		t.Fatalf("%+v", options)
	}
	if !options.OnlyMatch {
		t.Fatal("typed text by default")
	}
	if got := entryTexts(entries); !reflect.DeepEqual(got, []string{"rite of oil\tunguent", "rite of fire"}) {
		t.Fatalf("%q", got)
	}
	if d := entries[0].Decor; d == nil || d.Glyph != "⚙" || !d.HasGlyph {
		t.Fatalf("%+v", d)
	}
	if entries[1].Decor != nil {
		t.Fatal("a plain item did not stay plain")
	}
}

func TestAStageWithoutAPromptGetsTheDefaultsAndItsOwnSize(t *testing.T) {
	var s Stages
	options, _ := staged(t, &s, Stage{Placeholder: "choose a rite", Lines: ptr(uint32(6)), Width: ptr(uint32(44))})
	want := list.DefaultOptions()
	want.Placeholder, want.OnlyMatch = "choose a rite", true
	want.Lines, want.Width = ptr(uint32(6)), ptr(uint32(44))
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("%+v, want %+v", options, want)
	}
}

func TestAnItemsMatchIconAndEmptyGlyphReachItsEntry(t *testing.T) {
	var s Stages
	it := item("a", "A\nline\rbreak")
	it.Match, it.Icon, it.Glyph, it.Badges, it.Pending = ptr("key"), ptr("forge-cog"), ptr(""), []string{"edict"}, true
	_, entries := staged(t, &s, Stage{Items: []Item{it}})
	e := entries[0]
	want := list.Entry{Text: "A line break", MatchText: "key", HasMatchText: true,
		Decor: &list.Decor{Icon: "forge-cog", HasIcon: true, HasGlyph: true, Badges: []string{"edict"}, Pending: true}}
	if !reflect.DeepEqual(e, want) {
		t.Fatalf("%+v %+v, want %+v %+v", e, e.Decor, want, want.Decor)
	}
}

func TestItemsUpsertByIDAndRemovalsNameTheirIndices(t *testing.T) {
	var s Stages
	staged(t, &s, Stage{ID: "s", Items: []Item{item("a", "A"), item("b", "B")}})
	steps := apply(t, &s, Items{Items: []Item{item("c", "C"), item("a", "A2")}})
	want := []Step{
		{Kind: StepAppend, Entries: []list.Entry{{Text: "C"}}},
		{Kind: StepUpdate, Updates: []Update{{Index: 0, Entry: list.Entry{Text: "A2"}}}},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("%+v", steps)
	}
	gone := apply(t, &s, Remove{IDs: []string{"c", "nobody"}})
	if !reflect.DeepEqual(gone, []Step{{Kind: StepRemove, Removed: []uint32{2}}}) {
		t.Fatalf("%+v", gone)
	}
	if steps := apply(t, &s, Remove{IDs: []string{"nobody"}}); len(steps) != 0 {
		t.Fatalf("%+v", steps)
	}
}

func TestAStageNamingAnIDTwiceKeepsItsFirstPlace(t *testing.T) {
	var s Stages
	_, entries := staged(t, &s, Stage{Items: []Item{item("a", "A"), item("b", "B"), item("a", "A2")}})
	if got := entryTexts(entries); !reflect.DeepEqual(got, []string{"A2", "B"}) {
		t.Fatalf("%q", got)
	}
}

func TestAnswersNameTheStageAndTheItemID(t *testing.T) {
	var s Stages
	if line, ok := s.Answer(Answer{Kind: AnswerItem}); ok {
		t.Fatalf("no stage yet: %#v", line)
	}
	staged(t, &s, Stage{ID: "aspect", Items: []Item{item("on", "On"), item("off", "Off")}})
	for _, c := range []struct {
		answer Answer
		want   ToProvider
	}{
		{Answer{Kind: AnswerItem, Index: 1}, Activate{Stage: "aspect", ID: "off"}},
		{Answer{Kind: AnswerDescend, Index: 1}, Activate{Stage: "aspect", ID: "off"}},
		{Answer{Kind: AnswerTyped, Text: "x"}, Submit{Stage: "aspect", Text: "x"}},
	} {
		if got, ok := s.Answer(c.answer); !ok || got != c.want {
			t.Errorf("%+v: %#v", c.answer, got)
		}
	}
	for _, a := range []Answer{{Kind: AnswerItem, Index: 7}, {Kind: AnswerItem, Index: -1}, {Kind: AnswerDismissed}} {
		if got, ok := s.Answer(a); ok {
			t.Errorf("%+v: %#v", a, got)
		}
	}
}

func TestANewStageStartsItsOwnIDs(t *testing.T) {
	var s Stages
	staged(t, &s, Stage{ID: "one", Items: []Item{item("x", "X")}})
	staged(t, &s, Stage{ID: "two", Items: []Item{item("y", "Y")}})
	if got, _ := s.Answer(Answer{Kind: AnswerItem}); got != (Activate{Stage: "two", ID: "y"}) {
		t.Fatalf("%#v", got)
	}
}

func TestSelectNamesAnItemNowOrOnceItArrives(t *testing.T) {
	var s Stages
	options, _ := staged(t, &s, Stage{ID: "s", Select: ptr("b"), Items: []Item{item("a", "A"), item("b", "B")}})
	if options.Select == nil || *options.Select != (list.Preselect{Index: 1, ByIndex: true}) {
		t.Fatalf("%+v", options.Select)
	}
	options, _ = staged(t, &s, Stage{ID: "t", Select: ptr("late")})
	if options.Select != nil {
		t.Fatalf("%+v", options.Select)
	}
	steps := apply(t, &s, Items{Items: []Item{item("late", "L")}})
	if last := steps[len(steps)-1]; last.Kind != StepPreselect || last.Index != 0 {
		t.Fatalf("%+v", steps)
	}
}

func TestItemsMayNameTheItemToSelectOnceKnown(t *testing.T) {
	var s Stages
	staged(t, &s, Stage{ID: "aspect", Items: []Item{item("on", "On"), item("off", "Off")}})
	steps := apply(t, &s, Items{Items: []Item{item("off", "Off")}, Select: ptr("off")})
	if last := steps[len(steps)-1]; last.Kind != StepPreselect || last.Index != 1 {
		t.Fatalf("%+v", steps)
	}
	steps = apply(t, &s, Items{Select: ptr("on")})
	if !reflect.DeepEqual(steps, []Step{{Kind: StepPreselect, Index: 0}}) {
		t.Fatalf("%+v", steps)
	}
}

func TestAToastShowsForItsSecondsWithinBounds(t *testing.T) {
	var s Stages
	for _, c := range []struct {
		seconds *uint32
		want    time.Duration
	}{{nil, 3 * time.Second}, {ptr(uint32(8)), 8 * time.Second}, {ptr(uint32(0)), time.Second}, {ptr(uint32(3600)), 60 * time.Second}} {
		steps := apply(t, &s, Toast{Text: "t", Level: list.Success, Seconds: c.seconds})
		want := []Step{{Kind: StepToast, Notice: list.Notice{Text: "t", Level: list.Success}, Span: c.want}}
		if !reflect.DeepEqual(steps, want) {
			t.Errorf("%v: %+v", c.seconds, steps)
		}
	}
}

func TestInputDecidesWhatTypedTextMayDo(t *testing.T) {
	var s Stages
	optional, _ := staged(t, &s, Stage{Input: InputOptional})
	if optional.OnlyMatch || optional.Required {
		t.Fatalf("%+v", optional)
	}
	required, _ := staged(t, &s, Stage{Input: InputRequired})
	if required.OnlyMatch || !required.Required {
		t.Fatalf("%+v", required)
	}
}

func TestReadyIsAStepAndHelloAndCloseAreNone(t *testing.T) {
	var s Stages
	staged(t, &s, Stage{})
	if steps := apply(t, &s, Ready{}); !reflect.DeepEqual(steps, []Step{{Kind: StepReady}}) {
		t.Fatalf("%+v", steps)
	}
	for _, m := range []Message{Hello{Protocol: 1}, Close{}, Unknown{Type: "omen"}} {
		if steps := apply(t, &s, m); len(steps) != 0 {
			t.Fatalf("%#v: %+v", m, steps)
		}
	}
}

func TestItemsBeforeAnyStageAreAProtocolError(t *testing.T) {
	var s Stages
	_, err := s.Apply(Items{})
	if err == nil || !strings.Contains(err.Error(), "before any 'stage'") {
		t.Fatalf("%v", err)
	}
	if _, err := s.Apply(Remove{}); err == nil {
		t.Fatal("remove before any stage")
	}
}
