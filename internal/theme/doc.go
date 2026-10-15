package theme

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// Reading a theme file (Rust src/theme/doc.rs through serde_json): each
// refusal has serde's words and serde_json's place, the last byte it had
// read — a scalar of the wrong kind's last character, the byte before a
// container of the wrong kind (left unread), a key's closing quote for an
// unknown or repeated key, and where reading on stopped (jsonc Settled,
// Opened) for what is judged once a value or its first byte was read.

const (
	familiesWants = "a font family, or an array of families in order of preference"
	trueWords     = "true is not a setting here: give the settings as an object, or false to turn it off"
	untagged      = "data did not match any variant of untagged enum Either"
)

// parseDoc reads a theme file's text into a node; a refused text is a
// *jsonc.Invalid.
func parseDoc(text []byte) (node, error) {
	d, err := jsonc.Parse(text, "theme")
	if err != nil {
		return nil, err
	}
	if d.Root == nil {
		return node{}, nil
	}
	return reader{d}.section(d.Root, docSection)
}

type reader struct{ doc *jsonc.Doc }

// wrongType refuses v as a reader that wants a kind refuses what it peeks
// at: a scalar is read, a container left unread.
func (r reader) wrongType(v *jsonc.Value, wants string) error {
	at := v.End
	if v.Kind == jsonc.Array || v.Kind == jsonc.Object {
		at = v.Start - 1
	}
	return r.doc.Errorf(at, "invalid type: %s, expected %s", v.Unexpected(), wants)
}

// wrongAny refuses v as a visitor of any value refuses a kind it takes no
// part of: a container is opened first.
func (r reader) wrongAny(v *jsonc.Value, wants string) error {
	at := v.End
	if v.Kind == jsonc.Array || v.Kind == jsonc.Object {
		at = r.doc.Opened(v)
	}
	return r.doc.Errorf(at, "invalid type: %s, expected %s", v.Unexpected(), wants)
}

func (r reader) wrongValue(v *jsonc.Value, wants string) error {
	return r.doc.Errorf(v.End, "invalid value: %s, expected %s", v.Unexpected(), wants)
}

// inSeq is a refusal of element i of array v by the array's reader, once
// the element was read.
func (r reader) inSeq(v *jsonc.Value, i int, format string, args ...any) error {
	return r.doc.Errorf(r.doc.Settled(v.Elems[i], ']'), format, args...)
}

// oneOf lists names as serde's "expected …" does.
func oneOf(names []string) string {
	quoted := "`" + strings.Join(names, "`, `") + "`"
	switch len(names) {
	case 0:
		return "there are no fields"
	case 1:
		return "expected " + quoted
	case 2:
		return "expected `" + names[0] + "` or `" + names[1] + "`"
	}
	return "expected one of " + quoted
}

// section reads s's keys from an object, or positionally from an array
// (serde fills a struct's fields in order).
func (r reader) section(v *jsonc.Value, s *section) (node, error) {
	n := node{}
	switch v.Kind {
	case jsonc.Object:
		seen := map[string]bool{}
		for _, m := range v.Members {
			k, known := s.key(m.Key)
			switch {
			case !known:
				names := make([]string, len(s.keys))
				for i, k := range s.keys {
					names[i] = k.name
				}
				return nil, r.doc.Errorf(m.KeyEnd, "unknown field `%s`, %s", m.Key, oneOf(names))
			case seen[m.Key]:
				return nil, r.doc.Errorf(m.KeyEnd, "duplicate field `%s`", m.Key)
			}
			seen[m.Key] = true
			if err := r.set(n, k, m.Value); err != nil {
				return nil, err
			}
		}
	case jsonc.Array:
		for i, e := range v.Elems {
			if i >= len(s.keys) {
				return nil, r.doc.Errorf(r.doc.Settled(v.Elems[i-1], ']'), "trailing characters")
			}
			if err := r.set(n, s.keys[i], e); err != nil {
				return nil, err
			}
		}
	default:
		return nil, r.wrongType(v, "struct "+s.name)
	}
	return n, nil
}

// set reads key k's value v into n; null says nothing.
func (r reader) set(n node, k key, v *jsonc.Value) error {
	if v.Kind == jsonc.Null {
		return nil
	}
	value, err := r.value(v, k)
	if err == nil {
		n[k.name] = value
	}
	return err
}

func (r reader) value(v *jsonc.Value, k key) (any, error) {
	switch k.kind {
	case kText:
		if v.Kind != jsonc.String {
			return nil, r.wrongType(v, "a string")
		}
		return v.String, nil
	case kFloat:
		if v.Kind != jsonc.Number {
			return nil, r.wrongType(v, "f32")
		}
		f, _ := strconv.ParseFloat(string(v.Number), 64)
		return float32(f), nil
	case kUint:
		return r.uint32(v)
	case kFlag:
		if v.Kind != jsonc.Bool {
			return nil, r.wrongType(v, "a boolean")
		}
		return v.Bool, nil
	case kColour:
		return r.colour(v)
	case kPaint:
		return r.paint(v)
	case kColours:
		if v.Kind != jsonc.Array {
			return nil, r.wrongType(v, "a sequence")
		}
		return r.colours(v)
	case kFamilies:
		return r.families(v)
	case kCaret:
		i, err := r.word(v, caretWords)
		return CaretShape(i), err
	case kBadge:
		i, err := r.word(v, badgeWords)
		return BadgeStyle(i), err
	case kSection:
		return r.section(v, k.sub)
	case kToggle:
		return r.toggle(v, k.sub)
	case kPalette:
		return r.palette(v)
	case kRoles:
		return r.roles(v, k.sub)
	case kEffects:
		return r.effects(v, k.sub)
	case kGroup:
		return r.group(v, k)
	case kMask:
		i, err := r.word(v, effects.MaskWords)
		return effects.MaskKind(i), err
	}
	panic(fmt.Sprintf("theme: key %s of no kind", k.name))
}

func (r reader) uint32(v *jsonc.Value) (uint32, error) {
	if v.Kind == jsonc.Number {
		if u, err := strconv.ParseUint(string(v.Number), 10, 32); err == nil {
			return uint32(u), nil
		}
		if strings.HasPrefix(v.Unexpected(), "integer") {
			return 0, r.wrongValue(v, "u32")
		}
	}
	return 0, r.wrongType(v, "u32")
}

func (r reader) colour(v *jsonc.Value) (colourRef, error) {
	if v.Kind != jsonc.String {
		return colourRef{}, r.wrongAny(v, colourWants)
	}
	c, ok := parseColour(v.String)
	if !ok {
		return c, r.wrongValue(v, colourWants)
	}
	return c, nil
}

func (r reader) colours(v *jsonc.Value) ([]colourRef, error) {
	out := make([]colourRef, 0, len(v.Elems))
	for _, e := range v.Elems {
		c, err := r.colour(e)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r reader) paint(v *jsonc.Value) (paint, error) {
	switch v.Kind {
	case jsonc.String:
		c, err := r.colour(v)
		return paint{c}, err
	case jsonc.Array:
		stops, err := r.colours(v)
		if err == nil && (len(stops) == 0 || len(stops) > 4) {
			err = r.doc.Errorf(v.End, "invalid length %d, expected %s", len(stops), paintWants)
		}
		return stops, err
	}
	return nil, r.wrongAny(v, paintWants)
}
