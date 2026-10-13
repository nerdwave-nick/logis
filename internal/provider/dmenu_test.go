package provider

import (
	"reflect"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// Rust src/provider/dmenu.rs tests.

func lines(texts ...string) []protocol.Item {
	items := make([]protocol.Item, len(texts))
	for i, t := range texts {
		items[i] = protocol.Item{Text: t, Raw: []byte(t)}
	}
	return items
}

func TestLinesBeforeTheShowComeWithTheStageAndLaterOnesAppend(t *testing.T) {
	options := list.DefaultOptions()
	options.Prompt = "rite? "
	d := NewDmenu(options)
	if _, ok := d.Push(lines("a", "b")); ok {
		t.Fatal("a step before the show")
	}
	stage := d.Show()
	if stage.Kind != StepStage || !reflect.DeepEqual(entryTexts(stage.Entries), []string{"a", "b"}) ||
		!reflect.DeepEqual(stage.Options, options) {
		t.Fatalf("%+v", stage)
	}
	step, ok := d.Push(lines("c"))
	if !ok || step.Kind != StepAppend || !reflect.DeepEqual(entryTexts(step.Entries), []string{"c"}) {
		t.Fatalf("%+v", step)
	}
	if _, ok := d.Push(nil); ok {
		t.Fatal("a step for no lines")
	}
}

func TestALinesMatchTextComesWithItsEntry(t *testing.T) {
	match := "key"
	d := NewDmenu(list.DefaultOptions())
	d.Push([]protocol.Item{{Text: "shown", Match: &match, Raw: []byte("raw")}})
	want := []list.Entry{{Text: "shown", MatchText: "key", HasMatchText: true}}
	if got := d.Show().Entries; !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}

func TestAnAnswerIsTheChosenLinesBytesOrTheTypedText(t *testing.T) {
	d := NewDmenu(list.DefaultOptions())
	d.Push([]protocol.Item{{Text: "x", Raw: []byte("x")}, {Text: "� y", Raw: []byte("\xff y")}})
	chosen := protocol.Outcome{Kind: protocol.Selected, Index: 1, Item: protocol.Item{Text: "� y", Raw: []byte("\xff y")}}
	for _, c := range []struct {
		answer Answer
		want   protocol.Outcome
	}{
		{Answer{Kind: AnswerItem, Index: 1}, chosen},
		{Answer{Kind: AnswerDescend, Index: 1}, chosen},
		{Answer{Kind: AnswerTyped, Text: "free"}, protocol.Outcome{Kind: protocol.Typed, Text: "free"}},
		{Answer{Kind: AnswerDismissed}, protocol.Outcome{Kind: protocol.Dismissed}},
		{Answer{Kind: AnswerItem, Index: 2}, protocol.Outcome{Kind: protocol.Dismissed}},
		{Answer{Kind: AnswerItem, Index: -1}, protocol.Outcome{Kind: protocol.Dismissed}},
	} {
		if got := d.Outcome(c.answer); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: %+v", c.answer, got)
		}
	}
}
