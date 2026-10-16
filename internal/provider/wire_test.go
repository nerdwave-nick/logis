package provider

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
)

// Rust src/provider/wire.rs tests.

func ptr[T any](v T) *T { return &v }

func TestAStageLineReadsWithDefaultsForWhatItOmits(t *testing.T) {
	m, err := Parse([]byte(`{"type":"stage","id":"rite","items":[
		{"id":"oil","columns":["rite of oil","unguent"],"glyph":"⚙","badges":["current"],"pending":true,"future":1}]}`))
	stage, ok := m.(Stage)
	if err != nil || !ok {
		t.Fatalf("%#v, %v", m, err)
	}
	if stage.ID != "rite" || stage.Prompt != nil || stage.Input != InputNone {
		t.Fatalf("%+v", stage)
	}
	item := stage.Items[0]
	if !reflect.DeepEqual(item.Columns, []string{"rite of oil", "unguent"}) || item.Glyph == nil || *item.Glyph != "⚙" ||
		!item.Pending || !reflect.DeepEqual(item.Badges, []string{"current"}) {
		t.Fatalf("%+v", item)
	}
}

func TestEveryMessageTypeReads(t *testing.T) {
	cases := []struct {
		line string
		want Message
	}{
		{`{"type":"hello","protocol":1}`, Hello{Protocol: 1}},
		{`{"type":"items","items":[{"id":"a","columns":["A"]}],"select":"a"}`,
			Items{Items: []Item{{ID: "a", Columns: []string{"A"}}}, Select: ptr("a")}},
		{`{"type":"items"}`, Items{}},
		{`{"type":"remove","ids":["a"]}`, Remove{IDs: []string{"a"}}},
		{`{"type":"toast","text":"done","level":"error","seconds":8}`, Toast{Text: "done", Level: list.Error, Seconds: ptr(uint32(8))}},
		{`{"type":"toast","text":"hm"}`, Toast{Text: "hm", Level: list.Info}},
		{`{"type":"toast","text":"ok","level":"success"}`, Toast{Text: "ok", Level: list.Success}},
		{`{"type":"ready"}`, Ready{}},
		{`{"type":"close"}`, Close{}},
		{`{"type":"stage","id":"s","prompt":"p: ","placeholder":"ph","query":"q","input":"required","select":"x","lines":6,"width":44}`,
			Stage{ID: "s", Prompt: ptr("p: "), Placeholder: "ph", Query: "q", Input: InputRequired, Select: ptr("x"),
				Lines: ptr(uint32(6)), Width: ptr(uint32(44))}},
		{`{"type":"stage","input":"optional","items":[{"id":"i","match":"m","icon":"forge"}]}`,
			Stage{Input: InputOptional, Items: []Item{{ID: "i", Match: ptr("m"), Icon: ptr("forge")}}}},
	}
	for _, c := range cases {
		got, err := Parse([]byte(c.line))
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %#v, %v; want %#v", c.line, got, err, c.want)
		}
	}
}

func TestUnknownTypesAreSkippedAndGarbageIsAnError(t *testing.T) {
	if got, err := Parse([]byte(`{"type":"omen","x":1}`)); err != nil || got != (Unknown{Type: "omen"}) {
		t.Fatalf("%#v, %v", got, err)
	}
	for line, want := range map[string]string{
		`not json`:                    "JSON",
		`{"id":"x"}`:                  "type",
		`[1,2]`:                       "type",
		`{"type":3}`:                  "type",
		`{"type":"remove","ids":"a"}`: "remove",
		`{"type":"remove"}`:           "remove",
		`{"type":"hello"}`:            "hello",
		`{"type":"toast"}`:            "toast",
		`{"type":"toast","text":"t","level":"dire"}`: "toast",
		`{"type":"stage","input":"maybe"}`:           "stage",
		`{"type":"hello","protocol":-1}`:             "hello",
		"{\"type\":\"toast\",\"text\":\"\xff\"}":     "JSON",
		`{"type":"close"} trailing`:                  "JSON",
	} {
		if _, err := Parse([]byte(line)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error naming %q", line, err, want)
		}
	}
}

func TestLinesToTheProviderHaveAStableShape(t *testing.T) {
	for _, c := range []struct {
		message ToProvider
		want    string
	}{
		{Activate{Stage: "rite", ID: "oil"}, `{"type":"activate","stage":"rite","id":"oil"}` + "\n"},
		{Cancel{}, `{"type":"cancel"}` + "\n"},
		{Greeting(), `{"type":"hello","protocol":1}` + "\n"},
		{Submit{Stage: "s", Text: "v"}, `{"type":"submit","stage":"s","text":"v"}` + "\n"},
		// Bytes as written, as serde writes them: no HTML escapes.
		{Activate{Stage: "<a&b>", ID: ">oil"}, `{"type":"activate","stage":"<a&b>","id":">oil"}` + "\n"},
	} {
		if got := string(Encode(c.message)); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
}
