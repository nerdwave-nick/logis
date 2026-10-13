package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
)

func u32(n uint32) *uint32 { return &n }

func TestListOptionsRoundTripEveryField(t *testing.T) {
	for _, o := range []list.Options{
		list.DefaultOptions(),
		{
			Prompt: "rite? ", Placeholder: "seek", Password: '•', Lines: u32(0), Width: u32(40),
			Counter: true, Search: "ab", Select: &list.Preselect{Text: "Second"},
			OnlyMatch: true, Required: true,
		},
		{Prompt: "", Lines: u32(4), Select: &list.Preselect{Index: 7, ByIndex: true}},
	} {
		raw := EncodeOptions(o)
		got, err := DecodeOptions(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if !reflect.DeepEqual(got, o) {
			t.Fatalf("%s:\n got %#v\nwant %#v", raw, got, o)
		}
	}
}

func TestListOptionsAreKebabCaseAndOmitUnsetSizes(t *testing.T) {
	o := list.DefaultOptions()
	o.OnlyMatch = true
	o.Select = &list.Preselect{Index: 2, ByIndex: true}
	var m map[string]any
	if err := json.Unmarshal(EncodeOptions(o), &m); err != nil {
		t.Fatal(err)
	}
	if m["only-match"] != true || m["prompt"] != "> " {
		t.Fatalf("%v", m)
	}
	if _, ok := m["lines"]; ok {
		t.Fatalf("unset lines on the wire: %v", m)
	}
	if sel, _ := m["select"].(map[string]any); sel["index"] != float64(2) {
		t.Fatalf("select %v", m["select"])
	}
}

func TestMissingListOptionsAreTheDefaults(t *testing.T) {
	for _, raw := range []string{"", "{}", "null"} {
		got, err := DecodeOptions(json.RawMessage(raw))
		if err != nil || !reflect.DeepEqual(got, list.DefaultOptions()) {
			t.Fatalf("%q: %#v, %v", raw, got, err)
		}
	}
	got, err := DecodeOptions(json.RawMessage(`{"counter": true}`))
	if err != nil || !got.Counter || got.Prompt != "> " {
		t.Fatalf("%#v, %v", got, err)
	}
}

func TestBadListOptionsAreRefused(t *testing.T) {
	for _, raw := range []string{`{"password": "ab"}`, `{"select": {}}`, `{"lines": -1}`, `[1]`} {
		if _, err := DecodeOptions(json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), "options") {
			t.Fatalf("%s accepted (%v)", raw, err)
		}
	}
}
