package provider

import (
	"reflect"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
)

// A built-in provider (servitor mode) says its lines as Rust's serde does
// for FromProvider: the "type" first, every field present, null for what
// is absent.

func TestALineSaidReadsBackAsTheSameMessage(t *testing.T) {
	glyph, sel, prompt := "⚙", "on", "rite: "
	for _, m := range []Message{
		Hello{Protocol: 1},
		Stage{ID: "rite", Prompt: &prompt, Placeholder: "choose a rite", Query: "q", Input: InputRequired, Select: &sel,
			Lines: ptr(uint32(6)), Width: ptr(uint32(44)),
			Items: []Item{{ID: "a", Columns: []string{"A", "b"}, Match: ptr("m"), Icon: ptr("i"), Glyph: &glyph, Badges: []string{"x"}, Pending: true}}},
		Items{Items: []Item{{ID: "on", Columns: []string{"on"}, Badges: []string{"current"}}}, Select: &sel},
		Remove{IDs: []string{"a"}},
		Toast{Text: "trouble", Level: list.Error, Seconds: ptr(uint32(6))},
		Toast{Text: "fine", Level: list.Success},
		Toast{Text: "hm", Level: list.Info},
		Ready{},
		Close{},
	} {
		line := Say(m)
		if line[len(line)-1] != '\n' {
			t.Fatalf("%q has no newline", line)
		}
		got, err := Parse(line[:len(line)-1])
		if err != nil || !reflect.DeepEqual(got, m) {
			t.Errorf("%s: %#v, %v; want %#v", line, got, err, m)
		}
	}
}

func TestALineSaidHasSerdesShape(t *testing.T) {
	prompt, glyph := "rite: ", "⚙"
	for _, c := range []struct {
		m    Message
		want string
	}{
		{Hello{Protocol: 1}, `{"type":"hello","protocol":1}`},
		{Ready{}, `{"type":"ready"}`},
		{Close{}, `{"type":"close"}`},
		{Toast{Text: "a <b>", Level: list.Error, Seconds: ptr(uint32(6))}, `{"type":"toast","text":"a <b>","level":"error","seconds":6}`},
		{Stage{ID: "rite", Prompt: &prompt, Placeholder: "choose a rite"},
			`{"type":"stage","id":"rite","prompt":"rite: ","placeholder":"choose a rite","query":"","input":"none","select":null,"lines":null,"width":null,"items":[]}`},
		{Items{Items: []Item{{ID: "dnd", Columns: []string{"dnd"}, Glyph: &glyph}}},
			`{"type":"items","items":[{"id":"dnd","columns":["dnd"],"match":null,"icon":null,"glyph":"⚙","badges":[],"pending":false}],"select":null}`},
	} {
		if got := string(Say(c.m)); got != c.want+"\n" {
			t.Errorf("%s, want %s", got, c.want)
		}
	}
}
