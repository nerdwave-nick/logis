package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"regexp"
	"slices"
	"testing"
	"unicode/utf8"
)

// The published schema (schema/logis.schema.json) and the parser agree:
// the same keys, defaults, words and kinds of value, and the same verdict
// on a corpus of documents through a validator for the part of JSON Schema
// the file uses.

type schemaDoc map[string]any

func loadSchema(t *testing.T) schemaDoc {
	t.Helper()
	text, err := os.ReadFile("../../schema/logis.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s schemaDoc
	if err := json.Unmarshal(text, &s); err != nil {
		t.Fatalf("the schema is JSON: %v", err)
	}
	return s
}

func (s schemaDoc) properties() map[string]map[string]any {
	props := map[string]map[string]any{}
	for key, p := range s["properties"].(map[string]any) {
		props[key] = p.(map[string]any)
	}
	return props
}

func accepts(doc any) error {
	text, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = Parse(text)
	return err
}

func TestTheSchemaListsExactlyTheKeysTheParserReads(t *testing.T) {
	s := loadSchema(t)
	var listed []string
	for key := range s.properties() {
		listed = append(listed, key)
	}
	slices.Sort(listed)
	parsed := slices.Clone(keys)
	slices.Sort(parsed)
	if len(parsed) == 0 || !slices.Equal(listed, parsed) {
		t.Errorf("schema %q, parser %q", listed, parsed)
	}
	if s["additionalProperties"] != false || s["type"] != "object" {
		t.Errorf("%v %v", s["additionalProperties"], s["type"])
	}
}

func TestTheSchemaDefaultsAreTheParserDefaults(t *testing.T) {
	defaults := map[string]any{}
	for key, p := range loadSchema(t).properties() {
		if d, ok := p["default"]; ok {
			defaults[key] = d
		}
	}
	if len(defaults) < 4 {
		t.Fatalf("%v", defaults)
	}
	text, _ := json.Marshal(defaults)
	c, err := Parse(text)
	if err != nil || !c.Equal(Default()) {
		t.Errorf("%s: %+v %v", text, c, err)
	}
}

func TestEveryExampleInTheSchemaIsAccepted(t *testing.T) {
	for key, p := range loadSchema(t).properties() {
		examples, _ := p["examples"].([]any)
		for _, example := range examples {
			if err := accepts(map[string]any{key: example}); err != nil {
				t.Errorf("%s: %v: %v", key, example, err)
			}
		}
	}
}

func TestTheWordsOfTheSchemaAreTheWordsOfTheParser(t *testing.T) {
	for key, p := range loadSchema(t).properties() {
		words, ok := p["enum"].([]any)
		if !ok {
			continue
		}
		for _, word := range words {
			if err := accepts(map[string]any{key: word}); err != nil {
				t.Errorf("%s: %v: %v", key, word, err)
			}
		}
		if accepts(map[string]any{key: "heresy"}) == nil {
			t.Errorf("%s: heresy was accepted", key)
		}
	}
}

func TestValuesOfAKindTheSchemaForbidsAreRefused(t *testing.T) {
	samples := map[string]any{
		"string": "x", "number": 1, "boolean": true, "null": nil, "array": []any{"x"}, "object": map[string]any{},
	}
	for key, p := range loadSchema(t).properties() {
		allowed, ok := p["type"].(string)
		if _, isEnum := p["enum"]; !ok && isEnum {
			allowed = "string"
		} else if !ok {
			t.Fatalf("%s: no type in the schema", key)
		}
		for kind, sample := range samples {
			if kind != allowed && accepts(map[string]any{key: sample}) == nil {
				t.Errorf("%s: a %s was accepted", key, kind)
			}
		}
	}
}

func TestTheThemePatternIsTheRuleTheParserApplies(t *testing.T) {
	pattern := loadSchema(t).properties()["theme"]["pattern"]
	if pattern != "^[^/.][^/]*$" {
		t.Fatalf("%v", pattern)
	}
	for name, valid := range map[string]bool{
		"forge-crimson-brass": true, "a": true, "my forge.v2": true,
		"": false, ".hidden": false, "..": false, "a/b": false, "/abs": false,
	} {
		if isThemeName(name) != valid {
			t.Errorf("%q", name)
		}
	}
}

func TestTheLowerBoundsOfTheSchemaHoldInTheParser(t *testing.T) {
	p := loadSchema(t).properties()
	terminal := p["terminal"]
	prefix := terminal["prefixItems"].([]any)[0].(map[string]any)
	if p["idle-timeout"]["minimum"] != 0.0 || p["icon-theme"]["minLength"] != 1.0 ||
		terminal["minItems"] != 1.0 || prefix["minLength"] != 1.0 {
		t.Fatalf("the bounds changed: %v", p)
	}
	for _, tc := range []struct {
		doc map[string]any
		ok  bool
	}{
		{map[string]any{"idle-timeout": 0}, true},
		{map[string]any{"idle-timeout": -0.001}, false},
		{map[string]any{"icon-theme": ""}, false},
		{map[string]any{"terminal": []any{}}, false},
		{map[string]any{"terminal": []any{""}}, false},
	} {
		if (accepts(tc.doc) == nil) != tc.ok {
			t.Errorf("%v", tc.doc)
		}
	}
}

// valid checks v against the subset of JSON Schema logis.schema.json uses:
// type, enum, minimum, minLength, minItems, items, prefixItems, pattern,
// properties and additionalProperties.
func valid(schema map[string]any, v any) error {
	if want, ok := schema["type"].(string); ok && kindOf(v) != want &&
		(want != "number" || kindOf(v) != "integer") {
		return fmt.Errorf("%v is no %s", v, want)
	}
	if words, ok := schema["enum"].([]any); ok && !slices.ContainsFunc(words, func(w any) bool {
		return reflect.DeepEqual(w, v)
	}) {
		return fmt.Errorf("%v is none of %v", v, words)
	}
	if min, ok := schema["minimum"].(float64); ok && v.(float64) < min {
		return fmt.Errorf("%v < %v", v, min)
	}
	if s, ok := v.(string); ok {
		if n, ok := schema["minLength"].(float64); ok && float64(utf8.RuneCountInString(s)) < n {
			return fmt.Errorf("%q too short", s)
		}
		if p, ok := schema["pattern"].(string); ok && !regexp.MustCompile(p).MatchString(s) {
			return fmt.Errorf("%q does not match %s", s, p)
		}
	}
	if a, ok := v.([]any); ok {
		if n, ok := schema["minItems"].(float64); ok && float64(len(a)) < n {
			return fmt.Errorf("%v too short", a)
		}
		prefix, _ := schema["prefixItems"].([]any)
		for i, elem := range a {
			sub, _ := schema["items"].(map[string]any)
			if i < len(prefix) {
				sub = prefix[i].(map[string]any)
			}
			if err := valid(sub, elem); sub != nil && err != nil {
				return err
			}
		}
	}
	if o, ok := v.(map[string]any); ok {
		props, _ := schema["properties"].(map[string]any)
		for key, value := range o {
			sub, known := props[key].(map[string]any)
			if !known && schema["additionalProperties"] == false {
				return fmt.Errorf("unknown %q", key)
			}
			if err := valid(sub, value); known && err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	return nil
}

func kindOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if x == math.Trunc(x) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	}
	return "object"
}

func TestTheSchemaAndTheParserGiveTheSameVerdict(t *testing.T) {
	s := loadSchema(t)
	values := []any{
		nil, true, false, 0.0, 1.0, 1.5, -1.0, -0.001, 600.0, "", "x", "keep", "dismiss", "heresy",
		".hidden", "a/b", "my forge.v2", "Papirus-Dark", []any{}, []any{""}, []any{"foot"},
		[]any{"foot", "-e"}, []any{"foot", 1.0}, []any{1.0}, []any{"kitty", ""}, map[string]any{},
	}
	keys := append(slices.Clone(keys), "thme", "heresy")
	checked := 0
	for _, key := range keys {
		for _, value := range values {
			doc := map[string]any{key: value}
			schemaSays := valid(s, doc)
			parserSays := accepts(doc)
			if (schemaSays == nil) != (parserSays == nil) {
				t.Errorf("%v: schema %v, parser %v", doc, schemaSays, parserSays)
			}
			checked++
		}
	}
	if checked < 200 {
		t.Errorf("only %d documents", checked)
	}
}
