package theme

import (
	"strconv"

	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// The theme file's values that are more than one kind of JSON: font
// families, the enums' words, and parts that may be off.

func (r reader) families(v *jsonc.Value) ([]string, error) {
	switch v.Kind {
	case jsonc.String:
		if v.String == "" {
			return nil, r.wrongValue(v, familiesWants)
		}
		return []string{v.String}, nil
	case jsonc.Array:
		names := make([]string, 0, len(v.Elems))
		for i, e := range v.Elems {
			switch {
			case e.Kind != jsonc.String:
				return nil, r.wrongType(e, "a string")
			case e.String == "":
				return nil, r.inSeq(v, i, "invalid value: string \"\", expected %s", familiesWants)
			}
			names = append(names, e.String)
		}
		if len(names) == 0 {
			return nil, r.doc.Errorf(v.End, "invalid length 0, expected %s", familiesWants)
		}
		return names, nil
	}
	return nil, r.wrongAny(v, familiesWants)
}

// word reads an enum's variant: its name, or an object naming it with a
// null (serde's unit variant); its index in words.
func (r reader) word(v *jsonc.Value, words []string) (int, error) {
	unknown := func(at int, w string) error {
		return r.doc.Errorf(at, "unknown variant `%s`, %s", w, oneOf(words))
	}
	switch {
	case v.Kind == jsonc.String:
		for i, w := range words {
			if w == v.String {
				return i, nil
			}
		}
		return 0, unknown(v.End, v.String)
	case v.Kind != jsonc.Object:
		return 0, r.doc.Errorf(v.Start, "expected value")
	case len(v.Members) == 0:
		return 0, r.doc.Errorf(v.End, "expected value")
	}
	m := v.Members[0]
	i, known := 0, false
	for j, w := range words {
		if w == m.Key {
			i, known = j, true
		}
	}
	switch {
	case !known:
		return 0, unknown(m.KeyEnd, m.Key)
	case m.Value.Kind != jsonc.Null:
		return 0, r.wrongType(m.Value, "unit")
	case len(v.Members) > 1:
		return 0, r.doc.Errorf(m.Value.End, "expected value")
	}
	return i, nil
}

// toggle reads a part that may be off: false, or its settings (a section's,
// or a pair of lengths when s is nil). Rust read it as an untagged enum,
// which loses the place and words of what is wrong within.
func (r reader) toggle(v *jsonc.Value, s *section) (toggle, error) {
	switch {
	case v.Kind == jsonc.Bool && !v.Bool:
		return toggle{off: true}, nil
	case v.Kind == jsonc.Bool:
		return toggle{}, r.doc.Errorf(r.doc.Settled(v, '}'), trueWords)
	case s == nil && v.Kind == jsonc.Array && len(v.Elems) == 2 &&
		v.Elems[0].Kind == jsonc.Number && v.Elems[1].Kind == jsonc.Number:
		var t toggle
		for i, e := range v.Elems {
			f, _ := strconv.ParseFloat(string(e.Number), 64)
			t.pair[i] = float32(f)
		}
		return t, nil
	case s != nil && (v.Kind == jsonc.Object || v.Kind == jsonc.Array):
		if on, err := r.section(v, s); err == nil {
			return toggle{on: on}, nil
		}
	}
	return toggle{}, r.doc.Errorf(r.doc.Settled(v, '}'), untagged)
}
