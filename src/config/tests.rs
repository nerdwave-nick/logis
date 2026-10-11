//! Parsing and validating config texts.

use super::*;

fn err(text: &str) -> Invalid {
    parse(text).expect_err("the config was accepted")
}

fn secs(s: f64) -> Option<Duration> {
    Some(Duration::from_secs_f64(s))
}

#[test]
fn an_empty_or_comment_only_config_means_the_defaults() {
    for text in [
        "",
        "  \n\t",
        "// nothing yet\n/* at all */",
        "{}",
        "{ } // ok",
    ] {
        assert_eq!(parse(text), Ok(Config::default()), "{text:?}");
    }
}

#[test]
fn the_defaults_are_the_documented_ones() {
    let config = Config::default();
    assert_eq!(config.theme, "forge-crimson-brass");
    assert_eq!(config.idle_timeout, None);
    assert_eq!(config.terminal, None);
    assert_eq!(config.icon_theme, "hicolor");
    assert_eq!(config.focus_loss, FocusLoss::Dismiss);
}

#[test]
fn every_setting_is_read_from_jsonc() {
    let text = r#"{
        // the editor's guide; logis passes over it
        "$schema": "https://example.org/logis.schema.json",
        "theme": "cogitator-phosphor",
        "idle-timeout": 1.5, /* seconds */
        "terminal": ["foot", "-e",],
        "icon-theme": "Papirus-Dark",
        "focus-loss": "keep",
    }"#;
    let config = parse(text).unwrap();
    assert_eq!(
        config,
        Config {
            theme: "cogitator-phosphor".into(),
            idle_timeout: secs(1.5),
            terminal: Some(vec!["foot".into(), "-e".into()]),
            icon_theme: "Papirus-Dark".into(),
            focus_loss: FocusLoss::Keep,
        }
    );
}

#[test]
fn absent_keys_keep_their_defaults() {
    let config = parse(r#"{"focus-loss": "dismiss", "idle-timeout": 600}"#).unwrap();
    assert_eq!(
        config,
        Config {
            idle_timeout: secs(600.0),
            ..Config::default()
        }
    );
}

#[test]
fn an_idle_timeout_of_zero_keeps_the_daemon_resident() {
    assert_eq!(parse(r#"{"idle-timeout": 0}"#).unwrap().idle_timeout, None);
    assert_eq!(
        parse(r#"{"idle-timeout": 0.0}"#).unwrap().idle_timeout,
        None
    );
}

#[test]
fn an_unknown_key_is_named_with_its_line_and_column() {
    // serde_json locates a token by its last character: the closing quote.
    let e = err("{\n  \"theme\": \"x\",\n  \"thme\": \"y\"\n}");
    assert_eq!((e.line, e.column), (3, 8), "{e}");
    assert!(e.message.contains("unknown field `thme`"), "{e}");
    assert!(e.message.contains("`theme`"), "lists the known keys: {e}");
}

#[test]
fn a_value_of_the_wrong_kind_names_its_key() {
    let cases = [
        (r#"{"theme": 7}"#, "`theme`"),
        (r#"{"idle-timeout": "soon"}"#, "`idle-timeout`"),
        (r#"{"terminal": "foot -e"}"#, "`terminal`"),
        (r#"{"terminal": ["foot", 1]}"#, "`terminal`"),
        (r#"{"icon-theme": null}"#, "`icon-theme`"),
        (r#"{"focus-loss": true}"#, "`focus-loss`"),
        (r#"{"$schema": 1}"#, "`$schema`"),
    ];
    for (text, key) in cases {
        let e = err(text);
        assert_eq!(e.line, 1, "{text}: {e}");
        assert!(e.message.contains("invalid type"), "{text}: {e}");
        assert!(e.message.contains(key), "{text}: {e}");
    }
}

#[test]
fn the_column_of_a_wrong_value_points_at_its_end() {
    let e = err("{\n  \"idle-timeout\": \"soon\"\n}");
    assert_eq!((e.line, e.column), (2, 24), "{e}");
}

#[test]
fn invalid_values_are_refused_with_what_is_wanted() {
    let cases = [
        (r#"{"idle-timeout": -1}"#, "`idle-timeout`"),
        (r#"{"idle-timeout": -0.5}"#, "`idle-timeout`"),
        (r#"{"idle-timeout": 1e300}"#, "`idle-timeout`"),
        (r#"{"theme": ""}"#, "`theme`"),
        (r#"{"theme": "a/b"}"#, "`theme`"),
        (r#"{"theme": ".hidden"}"#, "`theme`"),
        (r#"{"terminal": []}"#, "`terminal`"),
        (r#"{"terminal": [""]}"#, "`terminal`"),
        (r#"{"icon-theme": ""}"#, "`icon-theme`"),
        (r#"{"focus-loss": "stay"}"#, "\"dismiss\" or \"keep\""),
    ];
    for (text, wanted) in cases {
        let e = err(text);
        assert_eq!(e.line, 1, "{text}: {e}");
        assert!(e.message.contains(wanted), "{text}: {e}");
    }
}

#[test]
fn a_theme_name_may_contain_dots_and_spaces_inside() {
    let config = parse(r#"{"theme": "my forge.v2"}"#).unwrap();
    assert_eq!(config.theme, "my forge.v2");
}

#[test]
fn a_terminal_may_have_empty_arguments_after_its_program() {
    let config = parse(r#"{"terminal": ["kitty", "--title", ""]}"#).unwrap();
    assert_eq!(
        config.terminal,
        Some(vec!["kitty".into(), "--title".into(), String::new()])
    );
}

#[test]
fn a_repeated_key_is_refused() {
    let e = err(r#"{"theme": "a", "theme": "b"}"#);
    assert!(e.message.contains("duplicate field `theme`"), "{e}");
}

#[test]
fn the_config_must_be_an_object() {
    for text in ["[]", "\"theme\"", "null", "  // c\n 7"] {
        let e = err(text);
        assert!(e.message.contains("object"), "{text}: {e}");
    }
    let e = err("  // c\n 7");
    assert_eq!((e.line, e.column), (2, 2), "{e}");
}

#[test]
fn broken_json_is_located() {
    let e = err("{\n  \"theme\": \"x\"\n  \"icon-theme\": \"y\"\n}");
    assert_eq!(e.line, 3, "{e}");
    assert!(e.message.contains("expected `,` or `}`"), "{e}");
}

#[test]
fn an_unterminated_comment_is_located() {
    let e = err("{\n /* the rest");
    assert_eq!((e.line, e.column), (2, 2), "{e}");
    assert!(e.message.contains("comment"), "{e}");
}

#[test]
fn columns_count_characters_not_bytes() {
    // "⚙" is three bytes; the unknown key "x" ends at the 18th character.
    let e = err("{\"theme\": \"⚙\", \"x\": 1}");
    assert_eq!((e.line, e.column), (1, 18), "{e}");
}

#[test]
fn the_message_carries_no_second_position() {
    let e = err(r#"{"thme": 1}"#);
    assert!(!e.message.contains("line"), "{e}");
    assert_eq!(e.to_string(), format!("1:{}: {}", e.column, e.message));
}
