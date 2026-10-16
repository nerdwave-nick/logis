package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// jsonEqual compares two values by their JSON encoding.
func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var va, vb any
	if err := json.Unmarshal(ja, &va); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(jb, &vb); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(va, vb)
}

// wire is the JSON text of a message.
func wire(t *testing.T, message any) string {
	t.Helper()
	b, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func wantWire(t *testing.T, message any, want string) {
	t.Helper()
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad expectation %s: %v", want, err)
	}
	if !jsonEqual(t, message, w) {
		t.Errorf("wire %s, want %s", wire(t, message), want)
	}
}

// Ported from Rust src/protocol requests_have_a_stable_wire_shape.
func TestRequestsHaveAStableWireShape(t *testing.T) {
	wantWire(t, Hello(), `{"type":"hello","version":4}`)
	wantWire(t, Open(ModeDmenu, nil), `{"type":"open","mode":"dmenu"}`)
	wantWire(t, Open(ModeApps, json.RawMessage(`{"prompt":"> "}`)),
		`{"type":"open","mode":"apps","options":{"prompt":"> "}}`)
	wantWire(t, End(), `{"type":"end"}`)
	wantWire(t, Cancel(), `{"type":"cancel"}`)
	if Version != 4 {
		t.Errorf("Version %d: a changed protocol says so", Version)
	}
}

// Ported from an_item_carries_its_raw_bytes_apart_from_its_text.
func TestAnItemCarriesItsRawBytesApartFromItsText(t *testing.T) {
	item := Item{Text: "caf�\tx", Raw: []byte("caf\xe9\tx")}
	message := Items([]Item{item})
	wantWire(t, message, `{"type":"items","items":[{"text":"caf�\tx","raw":"Y2Fm6Ql4"}]}`)
	back := decodeRequest(t, wire(t, message))
	if !reflect.DeepEqual(back, message) {
		t.Errorf("round trip %+v, want %+v", back, message)
	}
	match := "x"
	matched := Item{Text: item.Text, Match: &match, Raw: item.Raw}
	wantWire(t, matched, `{"text":"caf�\tx","match":"x","raw":"Y2Fm6Ql4"}`)
}

// Ported from replies_have_a_stable_wire_shape.
func TestRepliesHaveAStableWireShape(t *testing.T) {
	wantWire(t, Welcome(7), `{"type":"welcome","version":4,"pid":7}`)
	wantWire(t, Queued(), `{"type":"queued"}`)
	wantWire(t, Visible(), `{"type":"visible"}`)
	wantWire(t, Error("heresy"), `{"type":"error","message":"heresy"}`)
	wantWire(t, Result(Outcome{Kind: Dismissed}), `{"type":"result","outcome":{"kind":"dismissed"}}`)
	wantWire(t, Result(Outcome{Kind: Typed, Text: "x"}),
		`{"type":"result","outcome":{"kind":"typed","text":"x"}}`)
	selected := Outcome{Kind: Selected, Index: 0, Item: Item{Text: "a", Raw: []byte("a")}}
	wantWire(t, Result(selected),
		`{"type":"result","outcome":{"kind":"selected","index":0,"item":{"text":"a","raw":"YQ=="}}}`)
	for _, kind := range []OutcomeKind{Replaced, Cancelled, Closed} {
		wantWire(t, Outcome{Kind: kind}, `{"kind":"`+string(kind)+`"}`)
	}
}

// Every outcome comes back as it went out.
func TestOutcomesRoundTrip(t *testing.T) {
	for _, o := range []Outcome{
		{Kind: Selected, Index: 3, Item: Item{Text: "b", Raw: []byte{0xff}}},
		{Kind: Typed, Text: "free"},
		{Kind: Dismissed}, {Kind: Replaced}, {Kind: Cancelled}, {Kind: Closed},
	} {
		var reply Reply
		if err := json.Unmarshal([]byte(wire(t, Result(o))), &reply); err != nil {
			t.Fatalf("%s: %v", o.Kind, err)
		}
		if reply.Outcome == nil || !reflect.DeepEqual(*reply.Outcome, o) {
			t.Errorf("%s: %+v", o.Kind, reply.Outcome)
		}
	}
}

// Ported from only_dmenu_sessions_queue.
func TestOnlyDmenuSessionsQueue(t *testing.T) {
	if ModeDmenu.Interactive() {
		t.Error("dmenu is interactive")
	}
	for _, m := range []Mode{ModeApps, ModeServitor, ModeProvider} {
		if !m.Interactive() {
			t.Errorf("%s is not interactive", m)
		}
	}
}

// Ported from a_provider_open_carries_its_launch_as_bytes.
func TestAProviderOpenCarriesItsLaunchAsBytes(t *testing.T) {
	open := Open(ModeProvider, nil)
	open.Provider = &Launch{
		Argv: [][]byte{[]byte("/bin/rite"), {0xff}},
		Cwd:  []byte("/"),
		Env:  [][]byte{[]byte("A=1")},
	}
	wantWire(t, open, `{"type":"open","mode":"provider",
		"provider":{"argv":["L2Jpbi9yaXRl","/w=="],"cwd":"Lw==","env":["QT0x"]}}`)
	if back := decodeRequest(t, wire(t, open)); !reflect.DeepEqual(back, open) {
		t.Errorf("round trip %+v, want %+v", back, open)
	}
}

// Ported from the_socket_lives_in_an_absolute_runtime_dir.
func TestTheSocketLivesInAnAbsoluteRuntimeDir(t *testing.T) {
	path, err := SocketPath("/run/user/1000")
	if err != nil || path != "/run/user/1000/logis.sock" {
		t.Errorf("SocketPath = %q, %v", path, err)
	}
	for _, dir := range []string{"", "run/user"} {
		if _, err := SocketPath(dir); err == nil {
			t.Errorf("%q was accepted", dir)
		}
	}
}

func decodeRequest(t *testing.T, text string) Request {
	t.Helper()
	var r Request
	if err := DecodeRequest([]byte(text), &r); err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return r
}

// A message the daemon cannot understand is refused with a reason, as serde
// refuses an unknown variant or a missing field.
func TestUnknownOrIncompleteRequestsAreRefused(t *testing.T) {
	for _, tc := range []struct{ text, needle string }{
		{`{"type":"heresy"}`, "heresy"},
		{`{"version":4}`, "type"},
		{`{"type":"open"}`, "mode"},
		{`{"type":"open","mode":"bogus"}`, "bogus"},
		{`{"type":"items"}`, "items"},
		{`{"type":"hello"}`, "version"},
		{`[1]`, "cannot unmarshal"},
	} {
		var r Request
		err := DecodeRequest([]byte(tc.text), &r)
		if err == nil || !strings.Contains(err.Error(), tc.needle) {
			t.Errorf("%s: %v, want an error naming %q", tc.text, err, tc.needle)
		}
	}
}
