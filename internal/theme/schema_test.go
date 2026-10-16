package theme

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/jsonc"
	"github.com/nerdwave-nick/logis/themes"
)

// The published theme schema (schema/theme.schema.json, Rust v1's but for
// the row fit: window.height deprecated, the default window.rows 11) and the
// parser agree: the same sections and keys, the bundled
// default's values as the schema's defaults, the same words and the same
// colour syntax (ported from Rust src/theme/schema_tests.rs).

type object = map[string]any

func themeSchema(t *testing.T) object {
	t.Helper()
	text, err := os.ReadFile("../../schema/theme.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s object
	if err := json.Unmarshal(text, &s); err != nil {
		t.Fatalf("the theme schema is JSON: %v", err)
	}
	return s
}

// forgeJSON is the bundled default as JSON.
func forgeJSON(t *testing.T) object {
	t.Helper()
	plain, err := jsonc.Strip([]byte(themes.Bundled[0].Text))
	if err != nil {
		t.Fatal(err)
	}
	var forge object
	if err := json.Unmarshal(plain, &forge); err != nil {
		t.Fatal(err)
	}
	return forge
}

// follow resolves a $ref into $defs, and the object branch of a part that
// may be false.
func follow(root object, n any) object {
	o, _ := n.(object)
	if ref, ok := o["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		return follow(root, root["$defs"].(object)[name])
	}
	if branches, ok := o["oneOf"].([]any); ok {
		for _, b := range branches {
			if f := follow(root, b); f["type"] == "object" {
				return f
			}
		}
	}
	return o
}

// objectAt is the object schema at path (section keys from the root).
func objectAt(root object, path []string) object {
	n := root
	for _, key := range path {
		n = follow(root, follow(root, n)["properties"].(object)[key])
	}
	return follow(root, n)
}

func sortedKeys(o object) []string {
	keys := make([]string, 0, len(o))
	for k := range o {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// parserKeys are the keys the parser reads for the section at path.
func parserKeys(path []string) []string {
	s := docSection
	for _, name := range path {
		k, _ := s.key(name)
		s = k.sub
	}
	keys := make([]string, len(s.keys))
	for i, k := range s.keys {
		keys[i] = k.name
	}
	slices.Sort(keys)
	return keys
}

func TestTheSchemaListsExactlyTheKeysTheParserReads(t *testing.T) {
	root := themeSchema(t)
	var walk func(path []string, s *section)
	walk = func(path []string, s *section) {
		object := objectAt(root, path)
		if object["additionalProperties"] != false {
			t.Errorf("%v: additional properties allowed", path)
		}
		if got, want := sortedKeys(object["properties"].(map[string]any)), parserKeys(path); !reflect.DeepEqual(got, want) {
			t.Errorf("%v: schema %v, parser %v", path, got, want)
		}
		for _, k := range s.keys {
			if k.sub != nil && k.kind != kRoles {
				walk(append(slices.Clone(path), k.name), k.sub)
			}
		}
	}
	walk(nil, docSection)
	// A font role is an object of its own under any name.
	role := follow(root, root["properties"].(object)["fonts"].(object)["additionalProperties"])
	want := make([]string, len(fontDoc.keys))
	for i, k := range fontDoc.keys {
		want[i] = k.name
	}
	slices.Sort(want)
	if got := sortedKeys(role["properties"].(object)); !reflect.DeepEqual(got, want) {
		t.Errorf("font role: schema %v, parser %v", got, want)
	}
}

func TestTheSchemaDefaultsAreTheBundledDefault(t *testing.T) {
	root := themeSchema(t)
	forge := forgeJSON(t)
	defaults := object{}
	for key, p := range root["properties"].(object) {
		d, has := p.(object)["default"]
		if key == "$schema" || key == "extends" {
			if has {
				t.Errorf("%s has a default", key)
			}
			continue
		}
		if !reflect.DeepEqual(d, forge[key]) {
			t.Errorf("the default of %s: %v, the bundled default says %v", key, d, forge[key])
		}
		defaults[key] = d
	}
	// And the defaults alone are a whole theme.
	text, _ := json.Marshal(defaults)
	d, err := parseDoc(text)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolve("defaults", "", d); err != nil {
		t.Error(err)
	}
}

func TestTheWordsOfTheSchemaAreTheWordsOfTheParser(t *testing.T) {
	root := themeSchema(t)
	for _, c := range []struct {
		path  []string
		key   string
		words []string
	}{{[]string{"query", "caret"}, "shape", caretWords}, {[]string{"badge"}, "style", badgeWords}} {
		var enum []string
		for _, w := range objectAt(root, c.path)["properties"].(object)[c.key].(object)["enum"].([]any) {
			enum = append(enum, w.(string))
		}
		if !reflect.DeepEqual(enum, c.words) {
			t.Errorf("%v.%s: schema %v, parser %v", c.path, c.key, enum, c.words)
		}
	}
}

func TestTheColourPatternIsTheRuleTheParserApplies(t *testing.T) {
	root := themeSchema(t)
	pattern := root["$defs"].(object)["colour"].(object)["pattern"].(string)
	if want := `^(#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?|transparent|[a-z][a-z0-9_-]*(\.[0-9]+)?)(/(0|1|0\.[0-9]+|1\.0+))?$`; pattern != want {
		t.Errorf("colour pattern %s", pattern)
	}
	names := root["properties"].(object)["palette"].(object)["propertyNames"].(object)["pattern"].(string)
	if names != "^[a-z][a-z0-9_-]*$" {
		t.Errorf("palette names %s", names)
	}
	colour, name := regexp.MustCompile(pattern), regexp.MustCompile(names)
	for text, valid := range map[string]bool{
		"#d4a017": true, "#d4a01780": true, "transparent": true, "brass": true, "soot.1": true,
		"crimson/0.55": true, "crimson/1": true, "crimson/1.0": true, "#ffffff/0": true,
		"#fff": false, "Brass": false, "brass/": false, "brass/.5": false, "brass/1.5": false, "soot.x": false,
	} {
		if _, ok := parseColour(text); ok != valid || colour.MatchString(text) != valid {
			t.Errorf("%q: parser %v, schema %v, want %v", text, ok, colour.MatchString(text), valid)
		}
	}
	for text, valid := range map[string]bool{"brass": true, "band-2": true, "Brass": false, "2x": false} {
		if isName(text) != valid || name.MatchString(text) != valid {
			t.Errorf("%q: parser %v, schema %v", text, isName(text), name.MatchString(text))
		}
	}
}

func TestEveryExampleInTheSchemaIsAccepted(t *testing.T) {
	root := themeSchema(t)
	examples, _ := root["examples"].([]any)
	if len(examples) == 0 {
		t.Fatal("whole-theme examples")
	}
	base, err := parseDoc([]byte(themes.Bundled[0].Text))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range examples {
		text, _ := json.Marshal(e)
		d, err := parseDoc(text)
		if err != nil {
			t.Errorf("%s: %v", text, err)
			continue
		}
		if _, err := resolve("example", "", d.over(base)); err != nil {
			t.Errorf("%s: %v", text, err)
		}
	}
}

func TestTheBundledThemesNameTheSchema(t *testing.T) {
	for _, b := range themes.Bundled {
		if !strings.Contains(b.Text, `"$schema": "../schema/theme.schema.json"`) {
			t.Errorf("%s names no schema", b.Name)
		}
	}
	if id := themeSchema(t)["$id"]; id != "https://raw.githubusercontent.com/nerdwave-nick/logis/main/schema/theme.schema.json" {
		t.Errorf("$id %v", id)
	}
}
