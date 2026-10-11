//! The published schema (`schema/logis.schema.json`) and the parser agree:
//! the same keys, defaults, words and kinds of value.

use serde::Deserialize;
use serde::de::{self, Deserializer, Visitor};
use serde_json::{Map, Value, json};

use super::fields::{Document, is_theme_name};
use super::{Config, parse};

const SCHEMA: &str = include_str!("../../schema/logis.schema.json");

fn schema() -> Value {
    serde_json::from_str(SCHEMA).expect("the schema is JSON")
}

fn properties() -> Map<String, Value> {
    schema()["properties"].as_object().unwrap().clone()
}

fn parse_value(doc: &Value) -> Result<Config, super::Invalid> {
    parse(&doc.to_string())
}

/// The keys serde reads, as `#[derive(Deserialize)]` declares them.
fn parser_keys() -> Vec<&'static str> {
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
    let mut keys: &'static [&'static str] = &[];
    let _ = Document::deserialize(Keys(&mut keys));
    let mut keys = keys.to_vec();
    keys.sort_unstable();
    keys
}

#[test]
fn the_schema_lists_exactly_the_keys_the_parser_reads() {
    let mut listed: Vec<String> = properties().keys().cloned().collect();
    listed.sort_unstable();
    assert!(!parser_keys().is_empty());
    assert_eq!(listed, parser_keys());
    assert_eq!(schema()["additionalProperties"], json!(false));
    assert_eq!(schema()["type"], "object");
}

#[test]
fn the_schema_defaults_are_the_parser_defaults() {
    let defaults: Map<String, Value> = properties()
        .into_iter()
        .filter_map(|(key, p)| p.get("default").map(|d| (key, d.clone())))
        .collect();
    assert!(defaults.len() >= 4, "{defaults:?}");
    assert_eq!(parse_value(&Value::Object(defaults)), Ok(Config::default()));
}

#[test]
fn every_example_in_the_schema_is_accepted() {
    for (key, p) in properties() {
        for example in p["examples"].as_array().into_iter().flatten() {
            let doc = json!({ key.clone(): example });
            assert!(parse_value(&doc).is_ok(), "{doc}: {:?}", parse_value(&doc));
        }
    }
}

#[test]
fn the_words_of_the_schema_are_the_words_of_the_parser() {
    for (key, p) in properties() {
        let Some(words) = p["enum"].as_array() else {
            continue;
        };
        for word in words {
            assert!(
                parse_value(&json!({ key.clone(): word })).is_ok(),
                "{key}: {word}"
            );
        }
        let heresy = json!({ key.clone(): "heresy" });
        assert!(parse_value(&heresy).is_err(), "{heresy}");
    }
}

#[test]
fn values_of_a_kind_the_schema_forbids_are_refused() {
    let samples = [
        ("string", json!("x")),
        ("number", json!(1)),
        ("boolean", json!(true)),
        ("null", Value::Null),
        ("array", json!(["x"])),
        ("object", json!({})),
    ];
    for (key, p) in properties() {
        let allowed = match (&p["type"], &p["enum"]) {
            (Value::String(t), _) => t.clone(),
            (_, Value::Array(_)) => "string".to_owned(),
            other => panic!("{key}: no type in the schema: {other:?}"),
        };
        for (kind, sample) in &samples {
            if *kind != allowed {
                let doc = json!({ key.clone(): sample });
                assert!(parse_value(&doc).is_err(), "{doc} was accepted");
            }
        }
    }
}

#[test]
fn the_theme_pattern_is_the_rule_the_parser_applies() {
    // The rule written as the schema's pattern; `is_theme_name` must agree.
    assert_eq!(properties()["theme"]["pattern"], "^[^/.][^/]*$");
    for (name, valid) in [
        ("forge-crimson-brass", true),
        ("a", true),
        ("my forge.v2", true),
        ("", false),
        (".hidden", false),
        ("..", false),
        ("a/b", false),
        ("/abs", false),
    ] {
        assert_eq!(is_theme_name(name), valid, "{name:?}");
    }
}

#[test]
fn the_lower_bounds_of_the_schema_hold_in_the_parser() {
    assert_eq!(properties()["idle-timeout"]["minimum"], 0);
    assert!(parse_value(&json!({"idle-timeout": 0})).is_ok());
    assert!(parse_value(&json!({"idle-timeout": -0.001})).is_err());
    assert_eq!(properties()["icon-theme"]["minLength"], 1);
    assert!(parse_value(&json!({"icon-theme": ""})).is_err());
    assert_eq!(properties()["terminal"]["minItems"], 1);
    assert!(parse_value(&json!({"terminal": []})).is_err());
    assert_eq!(properties()["terminal"]["prefixItems"][0]["minLength"], 1);
    assert!(parse_value(&json!({"terminal": [""]})).is_err());
}
