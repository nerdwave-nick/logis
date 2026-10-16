package theme

import (
	"errors"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// Reading a theme document, and the words and places of its refusals.

func doc(t *testing.T, text string) node {
	t.Helper()
	n, err := parseDoc([]byte(text))
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return n
}

// at is the value at a dotted path through sections and parts that are on.
func at(n node, path string) any {
	var v any = n
	for _, key := range strings.Split(path, ".") {
		switch x := v.(type) {
		case node:
			v = x[key]
		case toggle:
			v = x.on[key]
		case map[string]node:
			v = x[key]
		default:
			return nil
		}
	}
	return v
}

func TestASectionLiesOverItsBaseKeyByKey(t *testing.T) {
	mine := doc(t, `{"glow": {"radius": 9}, "window": {"rows": 4}}`)
	base := doc(t, `{"glow": {"radius": 22, "pulse": 0.15}, "motion": {"open-ms": 150}}`)
	both := mine.over(base)
	if at(both, "glow.radius") != float32(9) || at(both, "glow.pulse") != float32(0.15) {
		t.Errorf("glow %v", both["glow"])
	}
	if at(both, "window.rows") != uint32(4) || at(both, "motion.open-ms") != uint32(150) {
		t.Errorf("%v", both)
	}
}

func TestFalseTurnsAPartOffWhateverTheBaseSays(t *testing.T) {
	mine := doc(t, `{"decorations": {"rivets": false}}`)
	base := doc(t, `{"decorations": {"rivets": {"size": 6, "inset": 10}}}`)
	if r := at(mine.over(base), "decorations.rivets").(toggle); !r.off {
		t.Errorf("%+v", r)
	}
}

func TestSettingsOverAPartThatIsOnMergeAndOverOffReplace(t *testing.T) {
	mine := doc(t, `{"decorations": {"rivets": {"size": 4}}}`)
	on := doc(t, `{"decorations": {"rivets": {"size": 6, "inset": 10}}}`)
	r := at(mine.over(on), "decorations.rivets").(toggle)
	if r.off || r.on["size"] != float32(4) || r.on["inset"] != float32(10) {
		t.Errorf("%+v", r)
	}
	off := doc(t, `{"decorations": {"rivets": false}}`)
	r = at(mine.over(off), "decorations.rivets").(toggle)
	if r.off || r.on["size"] != float32(4) || r.on["inset"] != nil {
		t.Errorf("%+v", r)
	}
}

func TestFontsMergeByRoleAndPalettesByName(t *testing.T) {
	mine := doc(t, `{"fonts": {"body": {"size": 20}}, "palette": {"brass": "#000000"}}`)
	base := doc(t, `{"fonts": {"body": {"family": "Mono", "size": 16}, "label": {"size": 9}},
		"palette": {"brass": "#d4a017", "gold": "#f2c14e"}}`)
	both := mine.over(base)
	if at(both, "fonts.body.size") != float32(20) {
		t.Errorf("%v", at(both, "fonts.body"))
	}
	if f, _ := at(both, "fonts.body.family").([]string); len(f) != 1 || f[0] != "Mono" {
		t.Errorf("%v", f)
	}
	if at(both, "fonts.label") == nil || len(both["palette"].(palette)) != 2 {
		t.Errorf("%v", both)
	}
}

func TestNullSaysNothing(t *testing.T) {
	n := doc(t, `{"glow": null, "row": {"columns": null}, "decorations": {"rivets": null}}`)
	if _, ok := n["glow"]; ok {
		t.Errorf("%v", n)
	}
	if at(n, "row.columns") != nil || at(n, "decorations.rivets") != nil {
		t.Errorf("%v", n)
	}
}

func TestAPartTakesItsSettingsAsAnArrayInOrderToo(t *testing.T) {
	// serde fills a struct's fields in order from an array.
	n := doc(t, `{"glow": ["#ff0000", 3], "decorations": {"rivets": [6]}}`)
	if at(n, "glow.radius") != float32(3) || at(n, "decorations.rivets.size") != float32(6) {
		t.Errorf("%v", n)
	}
	if at(n, "glow.pulse") != nil {
		t.Errorf("%v", n)
	}
}

func TestADashIsTwoLengthsOrFalse(t *testing.T) {
	if d := at(doc(t, `{"rule": {"dash": [3, 2.5]}}`), "rule.dash").(toggle); d.off || d.pair != [2]float32{3, 2.5} {
		t.Errorf("%+v", d)
	}
	if d := at(doc(t, `{"rule": {"dash": false}}`), "rule.dash").(toggle); !d.off {
		t.Errorf("%+v", d)
	}
}

// refusals are texts and the verdict on them, "LINE:COLUMN: message".
var refusals = []struct{ text, want string }{
	{"{\n \"decorations\": {\"rivets\": {\"sise\": 4}}}", "2:39: data did not match any variant of untagged enum Either"},
	{`{"decorations": {"rivets": {"sise": 4} }}`, "1:40: data did not match any variant of untagged enum Either"},
	{`{"decorations": {"rivets": true}}`, "1:32: true is not a setting here: give the settings as an object, or false to turn it off"},
	{`{"decorations": {"rivets": 1}}`, "1:29: data did not match any variant of untagged enum Either"},
	{`{"decorations": {"rivets": {"size": 4, "size": 5}}}`, "1:50: data did not match any variant of untagged enum Either"},
	{`{"prompt": {"sigil": {"image": 1}}}`, "1:34: data did not match any variant of untagged enum Either"},
	{`{"rule": {"dash": [1]}}`, "1:22: data did not match any variant of untagged enum Either"},
	{`{"rule": {"dash": [1,2,3]}}`, "1:26: data did not match any variant of untagged enum Either"},
	{`{"rule": {"dash": "x"}}`, "1:22: data did not match any variant of untagged enum Either"},
	{`{"rule": {"dash": [1, null]}}`, "1:28: data did not match any variant of untagged enum Either"},
	{`{"rule": {"dash": true}}`, "1:23: true is not a setting here: give the settings as an object, or false to turn it off"},
	{`{"query": {"caret": {"shape": "star"}}}`, "1:36: unknown variant `star`, expected one of `bar`, `block`, `underscore`"},
	{`{"query": {"caret": {"shape": 1}}}`, "1:31: expected value"},
	{`{"query": {"caret": {"shape": 12}}}`, "1:31: expected value"},
	{`{"query": {"caret": {"shape": true}}}`, "1:31: expected value"},
	{`{"query": {"caret": {"shape": [1]}}}`, "1:31: expected value"},
	{`{"query": {"caret": {"shape": {"bar": 1}}}}`, "1:39: invalid type: integer `1`, expected unit"},
	{`{"query": {"caret": {"shape": {}}}}`, "1:32: expected value"},
	{`{"query": {"caret": {"shape": {"x": null}}}}`, "1:34: unknown variant `x`, expected one of `bar`, `block`, `underscore`"},
	{`{"query": {"caret": {"shape": {"bar": null, "x": 1}}}}`, "1:42: expected value"},
	{`{"badge": {"style": "x"}}`, "1:23: unknown variant `x`, expected one of `outline`, `fill`, `bracket`"},
	{`{"palette": {"Brass": "#000000"}}`, "1:33: invalid value: string \"Brass\", expected " + nameWants},
	{`{"palette": {"brass": "gold"}, "x": 1}`, "1:28: invalid value: string \"gold\", expected " + swatchWants},
	{`{"palette": {"brass": "#00000"}}`, "1:30: invalid value: string \"#00000\", expected " + swatchWants},
	{`{"palette": {"brass": []}}`, "1:24: invalid length 0, expected " + swatchWants},
	{`{"palette": {"brass": [1]}}`, "1:24: invalid type: integer `1`, expected a string"},
	{`{"palette": {"brass": ["#zz", "#000000"]}}`, "1:30: invalid value: string \"#zz\", expected " + swatchWants},
	{`{"palette": {"brass": ["#000000", "#zz" ] }}`, "1:41: invalid value: string \"#zz\", expected " + swatchWants},
	{`{"palette": {"brass": null}}`, "1:26: invalid type: null, expected " + swatchWants},
	{`{"palette": 1}`, "1:13: invalid type: integer `1`, expected a map"},
	{`{"glow": {"color": "brass/0.5"}, "palette": {"x": "#fff"}}`, "1:56: invalid value: string \"#fff\", expected " + swatchWants},
	{`{"glow": {"period-ms": 1.5}}`, "1:26: invalid type: floating point `1.5`, expected u32"},
	{`{"glow": {"period-ms": -1}}`, "1:25: invalid value: integer `-1`, expected u32"},
	{`{"glow": {"period-ms": 2.0}}`, "1:26: invalid type: floating point `2.0`, expected u32"},
	{`{"glow": {"period-ms": 1e3}}`, "1:26: invalid type: floating point `1000.0`, expected u32"},
	{`{"window": {"rows": 5000000000}}`, "1:30: invalid value: integer `5000000000`, expected u32"},
	{`{"window": {"rows": -0}}`, "1:22: invalid type: floating point `-0.0`, expected u32"},
	{`{"glow": {"radius": "x"}}`, "1:23: invalid type: string \"x\", expected f32"},
	{`{"glow": {"radius": 1, "radius": 2}}`, "1:31: duplicate field `radius`"},
	{`{"glow":{}, "glow":{}}`, "1:18: duplicate field `glow`"},
	{`{"palette":{},"palette":{}}`, "1:23: duplicate field `palette`"},
	{`{"rule": {"dash": [1, 2], "dash": false}}`, "1:32: duplicate field `dash`"},
	{`{"panel": {"blur": 1}}`, "1:20: invalid type: integer `1`, expected a boolean"},
	{`{"fonts": {"body": {"family": []}}}`, "1:32: invalid length 0, expected a font family, or an array of families in order of preference"},
	{`{"fonts": {"body": {"family": ""}}}`, "1:32: invalid value: string \"\", expected a font family, or an array of families in order of preference"},
	{`{"fonts": {"body": {"family": [1]}}}`, "1:32: invalid type: integer `1`, expected a string"},
	{`{"fonts": {"body": {"family": 3}}}`, "1:31: invalid type: integer `3`, expected a font family, or an array of families in order of preference"},
	{`{"fonts": {"body": {"family": ["", "a"]}}}`, "1:35: invalid value: string \"\", expected a font family, or an array of families in order of preference"},
	{`{"fonts": {"body": {"family": ["a", ""]}}}`, "1:39: invalid value: string \"\", expected a font family, or an array of families in order of preference"},
	{`{"fonts": {"body": {"family": ["a", "" ]}}}`, "1:40: invalid value: string \"\", expected a font family, or an array of families in order of preference"},
	{`{"fonts": {"body": {"famly": 3}}}`, "1:27: unknown field `famly`, expected one of `family`, `size`, `weight`, `tracking`"},
	{`{"fonts": {"body": 1}}`, "1:20: invalid type: integer `1`, expected struct FontDoc"},
	{`{"fonts": {"body": null}}`, "1:23: invalid type: null, expected struct FontDoc"},
	{`{"fonts": 1}`, "1:11: invalid type: integer `1`, expected a map"},
	{`{"border": {"paint": []}}`, "1:23: invalid length 0, expected " + paintWants},
	{`{"border": {"paint": 3}}`, "1:22: invalid type: integer `3`, expected " + paintWants},
	{`{"border": {"paint": "bad name"}}`, "1:31: invalid value: string \"bad name\", expected " + colourWants},
	{`{"border": {"paint": ["brass", "Bad"]}}`, "1:36: invalid value: string \"Bad\", expected " + colourWants},
	{`{"border": {"paint": [1, 2]}}`, "1:23: invalid type: integer `1`, expected " + colourWants},
	{`{"border": {"paint": ["brass","brass","brass","brass","brass"]}}`, "1:62: invalid length 5, expected " + paintWants},
	{`{"row": {"columns": "brass"}}`, "1:27: invalid type: string \"brass\", expected a sequence"},
	{`{"row": {"columns": [1]}}`, "1:22: invalid type: integer `1`, expected " + colourWants},
	{`{"glow": {"color": 1}}`, "1:20: invalid type: integer `1`, expected " + colourWants},
	{`{"prompt": {"label": 1}}`, "1:22: invalid type: integer `1`, expected a string"},
	{`{"x": 1}`, "1:4: unknown field `x`, expected one of `$schema`, `extends`, `palette`, `fonts`, `window`, `glow`, " +
		"`border`, `panel`, `decorations`, `prompt`, `query`, `counter`, `rule`, `row`, `selection`, `match`, `badge`, " +
		"`tab`, `pending`, `effects`, `motion`"},
	{`{"$schema": 1}`, "1:13: invalid type: integer `1`, expected a string"},
	{`{"extends": 1}`, "1:13: invalid type: integer `1`, expected a string"},
	{`{"glow": 1}`, "1:10: invalid type: integer `1`, expected struct GlowDoc"},
	{`[1]`, "1:1: the theme must be a JSON object: { \"key\": value, … }"},
}

func TestUnknownKeysAndWrongKindsAreRefusedWithTheirWordsAndPlaces(t *testing.T) {
	for _, c := range refusals {
		_, err := parseDoc([]byte(c.text))
		var invalid *jsonc.Invalid
		if !errors.As(err, &invalid) {
			t.Errorf("%s: want a refusal, got %v", c.text, err)
			continue
		}
		if got := invalid.Error(); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.text, got, c.want)
		}
	}
}

func TestOddButSoundTextsAreAccepted(t *testing.T) {
	for _, text := range []string{
		``, `// nothing at all`, `{"rule": {"dash": false}}`, `{"query": {"caret": {"shape": null}}}`,
		`{"query": {"caret": {"shape": {"bar": null}}}}`, `{"glow": {"radius": 1.5e1}}`,
		`{"palette": {"brass": "#000000", "brass": "#ffffff"}}`, `{"decorations": {"rivets": {}}}`,
		`{"glow": []}`, `{"glow": ["#ff0000", 3, 0, 100]}`,
	} {
		doc(t, text)
	}
	if got := at(doc(t, `{"palette": {"brass": "#000000", "brass": "#ffffff"}}`), "palette").(palette)["brass"][0]; got.R != 1 {
		t.Errorf("a repeated palette name: the last stands, got %v", got)
	}
}
