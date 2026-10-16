package jsonc

import "testing"

// serde_json names a number by how it parsed it: integers that fit u64 or
// i64 stay integers; any other number is an f64, shown without an exponent
// and with a decimal point (feat/logis-go G18: `1e3` is "floating point
// `1000.0`").
func TestUnexpectedNamesNumbersAsSerdeJSONParsedThem(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"7", "integer `7`"},
		{"-1", "integer `-1`"},
		{"-0", "floating point `-0.0`"},
		{"2.0", "floating point `2.0`"},
		{"1e3", "floating point `1000.0`"},
		{"1.5e1", "floating point `15.0`"},
		{"0.000001", "floating point `0.000001`"},
		{"18446744073709551615", "integer `18446744073709551615`"},
		{"18446744073709551616", "floating point `18446744073709552000.0`"},
		{"-9223372036854775809", "floating point `-9223372036854776000.0`"},
	} {
		doc := mustParse(t, `{"n": `+c.text+`}`)
		if got := doc.Root.Members[0].Value.Unexpected(); got != c.want {
			t.Errorf("%s: %q, want %q", c.text, got, c.want)
		}
	}
}

// Start is where a value begins: its first character, a container's
// opening bracket.
func TestAValueKnowsWhereItStarts(t *testing.T) {
	text := `{"a": [1, "x\"y"], "b": {"c": true}, "d": -12.5, "e": null}`
	doc := mustParse(t, text)
	at := func(v *Value) string { return text[v.Start : v.End+1] }
	m := doc.Root.Members
	for _, c := range []struct {
		v    *Value
		want string
	}{
		{doc.Root, text},
		{m[0].Value, `[1, "x\"y"]`},
		{m[0].Value.Elems[0], `1`},
		{m[0].Value.Elems[1], `"x\"y"`},
		{m[1].Value, `{"c": true}`},
		{m[1].Value.Members[0].Value, `true`},
		{m[2].Value, `-12.5`},
		{m[3].Value, `null`},
	} {
		if got := at(c.v); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
}

// Settled is where serde_json had read to when it placed an error raised
// once a value was read inside a container: past the blanks (comments
// too) that follow, and past the container's closer if it comes next, or,
// in an array, past a comma and the blanks after it; the offset of the
// last byte read.
func TestSettledIsWhereSerdeJSONStoodAfterAValue(t *testing.T) {
	text := "{\"a\": {\"b\": 1} /* c */ // d\n , \"e\": [\"x\" , \"y\",\"z\" ], \"f\": 2}"
	doc := mustParse(t, text)
	m := doc.Root.Members
	for _, c := range []struct {
		v      *Value
		closer byte
		want   string // the text up to and with the last byte read
	}{
		{m[0].Value, '}', "{\"a\": {\"b\": 1} /* c */ // d\n "},
		{m[0].Value.Members[0].Value, '}', "{\"a\": {\"b\": 1}"},
		{m[1].Value.Elems[0], ']', "{\"a\": {\"b\": 1} /* c */ // d\n , \"e\": [\"x\" , "},
		{m[1].Value.Elems[1], ']', "{\"a\": {\"b\": 1} /* c */ // d\n , \"e\": [\"x\" , \"y\","},
		{m[1].Value.Elems[2], ']', "{\"a\": {\"b\": 1} /* c */ // d\n , \"e\": [\"x\" , \"y\",\"z\" ]"},
		{m[2].Value, '}', text},
	} {
		if got := text[:doc.Settled(c.v, c.closer)+1]; got != c.want {
			t.Errorf("\n got %q\nwant %q", got, c.want)
		}
	}
}

// Opened is where serde_json stood when a visitor refused a container it
// had opened: past the blanks after the opening bracket, and on the closer
// of an empty one.
func TestOpenedIsWhereSerdeJSONStoodInARefusedContainer(t *testing.T) {
	text := `{"a": [ 1], "b": {}, "c": [  ], "d": {"x": 1}}`
	doc := mustParse(t, text)
	for i, want := range []string{`{"a": [ `, `{"a": [ 1], "b": {}`, `{"a": [ 1], "b": {}, "c": [  ]`, `{"a": [ 1], "b": {}, "c": [  ], "d": {`} {
		if got := text[:doc.Opened(doc.Root.Members[i].Value)+1]; got != want {
			t.Errorf("\n got %q\nwant %q", got, want)
		}
	}
}
