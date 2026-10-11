//! The published theme schema (`schema/theme.schema.json`) and the parser
//! agree: the same sections and keys, the bundled default's values as the
//! schema's defaults, the same words and the same colour syntax.

use serde::Deserialize;
use serde::de::{self, Deserializer, Visitor};
use serde_json::{Map, Value, json};

use super::colour::{ColourRef, is_name};
use super::doc::*;

const SCHEMA: &str = include_str!("../../schema/theme.schema.json");

fn schema() -> Value {
    serde_json::from_str(SCHEMA).expect("the theme schema is JSON")
}

/// The bundled default as JSON.
fn forge() -> Value {
    let (_, text) = super::BUNDLED[0];
    let json = crate::config::jsonc::strip(text).unwrap();
    serde_json::from_str(&json).unwrap()
}

/// The keys serde reads for `T`, as `#[derive(Deserialize)]` declares them.
fn keys<'de, T: Deserialize<'de>>() -> Vec<String> {
    struct Keys<'a>(&'a mut &'static [&'static str]);
    impl<'de> Deserializer<'de> for Keys<'_> {
        type Error = de::value::Error;
        fn deserialize_any<V: Visitor<'de>>(self, _: V) -> Result<V::Value, Self::Error> {
            Err(de::Error::custom("only the keys are wanted"))
        }
        fn deserialize_struct<V: Visitor<'de>>(
            self,
            _: &'static str,
            fields: &'static [&'static str],
            _: V,
        ) -> Result<V::Value, Self::Error> {
            *self.0 = fields;
            Err(de::Error::custom("only the keys are wanted"))
        }
        serde::forward_to_deserialize_any! {
            bool i8 i16 i32 i64 i128 u8 u16 u32 u64 u128 f32 f64 char str string
            bytes byte_buf option unit unit_struct newtype_struct seq tuple
            tuple_struct map enum identifier ignored_any
        }
    }
    let mut fields: &'static [&'static str] = &[];
    let _ = T::deserialize(Keys(&mut fields));
    let mut keys: Vec<String> = fields.iter().map(|k| (*k).to_owned()).collect();
    keys.sort_unstable();
    keys
}

/// The object schema at `path` (section keys from the root): through
/// `properties`, `$ref`s into `$defs`, and the object branch of a part
/// that may be `false`.
fn object_at(path: &[&str]) -> Map<String, Value> {
    let root = schema();
    let mut node = root.clone();
    for key in path {
        node = follow(&root, &node)["properties"][*key].clone();
    }
    follow(&root, &node).as_object().unwrap().clone()
}

fn follow(root: &Value, node: &Value) -> Value {
    if let Some(r) = node["$ref"].as_str() {
        let name = r.strip_prefix("#/$defs/").expect("local refs only");
        return follow(root, &root["$defs"][name]);
    }
    if let Some(branches) = node["oneOf"].as_array()
        && let Some(object) = branches
            .iter()
            .map(|b| follow(root, b))
            .find(|b| b["type"] == "object")
    {
        return object;
    }
    node.clone()
}

fn listed(path: &[&str]) -> Vec<String> {
    let object = object_at(path);
    assert_eq!(object["additionalProperties"], json!(false), "{path:?}");
    let mut keys: Vec<String> = object["properties"]
        .as_object()
        .unwrap_or_else(|| panic!("{path:?} has no properties"))
        .keys()
        .cloned()
        .collect();
    keys.sort_unstable();
    keys
}

#[test]
fn the_schema_lists_exactly_the_keys_the_parser_reads() {
    let sections: Vec<(&[&str], Vec<String>)> = vec![
        (&[], keys::<Doc>()),
        (&["window"], keys::<WindowDoc>()),
        (&["glow"], keys::<GlowDoc>()),
        (&["border"], keys::<BorderDoc>()),
        (&["border", "highlight"], keys::<HighlightDoc>()),
        (&["panel"], keys::<PanelDoc>()),
        (&["decorations"], keys::<DecorationsDoc>()),
        (&["decorations", "rivets"], keys::<RivetsDoc>()),
        (&["prompt"], keys::<PromptDoc>()),
        (&["prompt", "sigil"], keys::<SigilDoc>()),
        (&["query"], keys::<QueryDoc>()),
        (&["query", "caret"], keys::<CaretDoc>()),
        (&["counter"], keys::<CounterDoc>()),
        (&["rule"], keys::<RuleDoc>()),
        (&["rule", "ornament"], keys::<OrnamentDoc>()),
        (&["row"], keys::<RowDoc>()),
        (&["selection"], keys::<SelectionDoc>()),
        (&["match"], keys::<MatchDoc>()),
        (&["badge"], keys::<BadgeDoc>()),
        (&["pending"], keys::<PendingDoc>()),
        (&["effects"], keys::<EffectsDoc>()),
        (&["motion"], keys::<MotionDoc>()),
    ];
    for (path, parser) in sections {
        assert!(!parser.is_empty(), "{path:?}");
        assert_eq!(listed(path), parser, "{path:?}");
    }
    // A font role is an object of its own under any name.
    let root = schema();
    let role = follow(&root, &root["properties"]["fonts"]["additionalProperties"]);
    let mut font: Vec<String> = role["properties"]
        .as_object()
        .unwrap()
        .keys()
        .cloned()
        .collect();
    font.sort_unstable();
    assert_eq!(font, keys::<FontDoc>());
}

#[test]
fn the_schema_defaults_are_the_bundled_default() {
    let forge = forge();
    let root = schema();
    for (key, property) in root["properties"].as_object().unwrap() {
        if matches!(key.as_str(), "$schema" | "extends") {
            assert!(property.get("default").is_none(), "{key}");
            continue;
        }
        assert_eq!(property["default"], forge[key], "the default of {key}");
    }
    // And the default parses as the theme it is.
    let mut defaults = Map::new();
    for (key, property) in root["properties"].as_object().unwrap() {
        if let Some(d) = property.get("default") {
            defaults.insert(key.clone(), d.clone());
        }
    }
    let doc: Doc = serde_json::from_value(Value::Object(defaults)).unwrap();
    assert!(super::resolve::resolve("defaults", None, doc).is_ok());
}

#[test]
fn the_words_of_the_schema_are_the_words_of_the_parser() {
    let words = |path: &[&str], key: &str| -> Vec<Value> {
        object_at(path)["properties"][key]["enum"]
            .as_array()
            .unwrap_or_else(|| panic!("{path:?}.{key} has no enum"))
            .clone()
    };
    for word in words(&["query", "caret"], "shape") {
        assert!(
            serde_json::from_value::<CaretShape>(word.clone()).is_ok(),
            "{word}"
        );
    }
    for word in words(&["badge"], "style") {
        assert!(
            serde_json::from_value::<BadgeStyle>(word.clone()).is_ok(),
            "{word}"
        );
    }
    assert!(serde_json::from_value::<CaretShape>(json!("heresy")).is_err());
    assert_eq!(words(&["query", "caret"], "shape").len(), 3);
    assert_eq!(words(&["badge"], "style").len(), 3);
}

#[test]
fn the_colour_pattern_is_the_rule_the_parser_applies() {
    // The rule written as the schema's patterns; ColourRef and is_name
    // must agree with them.
    let root = schema();
    assert_eq!(
        root["$defs"]["colour"]["pattern"],
        "^(#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?|transparent|[a-z][a-z0-9_-]*(\\.[0-9]+)?)(/(0|1|0\\.[0-9]+|1\\.0+))?$"
    );
    assert_eq!(
        root["properties"]["palette"]["propertyNames"]["pattern"],
        "^[a-z][a-z0-9_-]*$"
    );
    for (text, valid) in [
        ("#d4a017", true),
        ("#d4a01780", true),
        ("transparent", true),
        ("brass", true),
        ("soot.1", true),
        ("crimson/0.55", true),
        ("crimson/1", true),
        ("crimson/1.0", true),
        ("#ffffff/0", true),
        ("#fff", false),
        ("Brass", false),
        ("brass/", false),
        ("brass/.5", false),
        ("brass/1.5", false),
        ("soot.x", false),
    ] {
        assert_eq!(ColourRef::parse(text).is_some(), valid, "{text:?}");
    }
    for (name, valid) in [
        ("brass", true),
        ("band-2", true),
        ("Brass", false),
        ("2x", false),
    ] {
        assert_eq!(is_name(name), valid, "{name:?}");
    }
}

#[test]
fn every_example_in_the_schema_is_accepted() {
    let root = schema();
    let examples = root["examples"].as_array().expect("whole-theme examples");
    assert!(!examples.is_empty());
    for example in examples {
        let doc: Doc =
            serde_json::from_value(example.clone()).unwrap_or_else(|e| panic!("{example}: {e}"));
        let base: Doc = serde_json::from_value(forge()).unwrap();
        let merged = Merge::over(doc, base);
        assert!(
            super::resolve::resolve("example", None, merged).is_ok(),
            "{example}"
        );
    }
}

#[test]
fn the_bundled_themes_name_the_schema() {
    for (name, text) in super::BUNDLED {
        assert!(
            text.contains("\"$schema\": \"../schema/theme.schema.json\""),
            "{name}"
        );
    }
    assert_eq!(
        schema()["$id"],
        "https://raw.githubusercontent.com/nerdwave-nick/logis/main/schema/theme.schema.json"
    );
}
