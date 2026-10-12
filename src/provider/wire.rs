//! The provider protocol on the wire (spec §4.8; documented with examples
//! in docs/provider-protocol.md): JSON lines over a provider process's
//! stdin (logis → provider) and stdout (provider → logis), one object per
//! line, tagged by `type`. Unknown fields are ignored, so later versions can
//! add optional ones; a line of an unknown `type` is skipped.

use serde::{Deserialize, Serialize};

pub use logis::list::Level;

/// The protocol version logis speaks; it is named in both `hello` lines.
pub const PROTOCOL: u32 = 1;

/// A line from the provider.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "lowercase")]
pub enum FromProvider {
    /// Its first line: the protocol version it speaks.
    Hello { protocol: u32 },
    /// A new list in the same window: the first stage shows the window.
    Stage(Stage),
    /// Items added to the current stage, or replacing items of the same id
    /// in their place; `select` names the item to select once it is there
    /// and settled (unless the user has moved).
    Items {
        #[serde(default)]
        items: Vec<Item>,
        #[serde(default)]
        select: Option<String>,
    },
    /// Items of the current stage that are gone.
    Remove { ids: Vec<String> },
    /// A short notice over the list, for `seconds` (default 3, at most 60).
    Toast {
        text: String,
        #[serde(default)]
        level: Level,
        #[serde(default)]
        seconds: Option<u32>,
    },
    /// After an `activate` or `submit`: the current stage takes Return again.
    Ready,
    /// The flow is over: the window closes (after a showing toast's time).
    Close,
}

/// One list of a multi-stage flow.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Stage {
    /// The provider's name for the stage, echoed in `activate`/`submit`.
    pub id: String,
    /// Shown before the query; `> ` when absent.
    pub prompt: Option<String>,
    /// Shown in place of an empty query.
    pub placeholder: String,
    /// The query the stage opens with (such as a decreed default).
    pub query: String,
    /// Whether Return without a matching item submits the typed text.
    pub input: Input,
    /// The id of the item to select once it is there and settled.
    pub select: Option<String>,
    /// The rows the window has room for (first stage only).
    pub lines: Option<u32>,
    /// The window's width in characters (first stage only).
    pub width: Option<u32>,
    /// The stage's first items.
    pub items: Vec<Item>,
}

/// What Return does when no item matches the query.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Input {
    /// Nothing: only items can be chosen.
    #[default]
    None,
    /// The typed text is submitted, even when empty.
    Optional,
    /// The typed text is submitted once it is not empty.
    Required,
}

/// One row.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct Item {
    /// Stable within its stage: updates, removals and `activate` name it.
    pub id: String,
    /// Shown left to right, lined up with the other rows' columns.
    pub columns: Vec<String>,
    /// Matched instead of the columns.
    #[serde(rename = "match")]
    pub match_text: Option<String>,
    /// A freedesktop icon name or an absolute path.
    pub icon: Option<String>,
    /// A short text for the icon column (used when there is no icon).
    pub glyph: Option<String>,
    /// Short labels at the row's end.
    pub badges: Vec<String>,
    /// Not settled yet: shown as pending, never activated or preselected.
    pub pending: bool,
}

/// A line to the provider.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "lowercase")]
pub enum ToProvider {
    /// logis's first line: the protocol version it speaks.
    Hello { protocol: u32 },
    /// The user chose item `id` of stage `stage`.
    Activate { stage: String, id: String },
    /// The user submitted typed text in stage `stage`.
    Submit { stage: String, text: String },
    /// The window is gone (dismissed, replaced, or its caller left); the
    /// provider should finish what it started and exit.
    Cancel,
}

/// How a provider's line reads.
#[derive(Debug, PartialEq, Eq)]
pub enum Line {
    /// A message logis knows.
    Message(FromProvider),
    /// A message of a later version, skipped (its `type`).
    Unknown(String),
}

/// Parses one line from a provider.
pub fn parse(line: &[u8]) -> Result<Line, String> {
    let value: serde_json::Value =
        serde_json::from_slice(line).map_err(|e| format!("not a JSON line: {e}"))?;
    let kind = value
        .get("type")
        .and_then(serde_json::Value::as_str)
        .ok_or("a line without a \"type\"")?
        .to_owned();
    const KNOWN: [&str; 7] = [
        "hello", "stage", "items", "remove", "toast", "ready", "close",
    ];
    if !KNOWN.contains(&kind.as_str()) {
        return Ok(Line::Unknown(kind));
    }
    serde_json::from_value(value)
        .map(Line::Message)
        .map_err(|e| format!("a malformed '{kind}': {e}"))
}

/// One line to a provider, newline included.
pub fn line(message: &ToProvider) -> Vec<u8> {
    let mut bytes = serde_json::to_vec(message).expect("serialisable");
    bytes.push(b'\n');
    bytes
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn read(value: serde_json::Value) -> Result<Line, String> {
        parse(value.to_string().as_bytes())
    }

    #[test]
    fn a_stage_line_reads_with_defaults_for_what_it_omits() {
        let line = read(json!({"type": "stage", "id": "rite", "items": [
            {"id": "oil", "columns": ["rite of oil", "unguent"], "glyph": "⚙",
             "badges": ["current"], "pending": true, "future": 1}
        ]}));
        let Ok(Line::Message(FromProvider::Stage(stage))) = line else {
            panic!("{line:?}");
        };
        assert_eq!((stage.id.as_str(), stage.prompt), ("rite", None));
        assert_eq!(stage.input, Input::None);
        let item = &stage.items[0];
        assert_eq!(item.columns, ["rite of oil", "unguent"]);
        assert_eq!((item.glyph.as_deref(), item.pending), (Some("⚙"), true));
        assert_eq!(item.badges, ["current"]);
    }

    #[test]
    fn every_message_type_reads() {
        let cases = [
            (
                json!({"type": "hello", "protocol": 1}),
                FromProvider::Hello { protocol: 1 },
            ),
            (
                json!({"type": "items", "items": [{"id": "a", "columns": ["A"]}], "select": "a"}),
                FromProvider::Items {
                    items: vec![Item {
                        id: "a".into(),
                        columns: vec!["A".into()],
                        ..Item::default()
                    }],
                    select: Some("a".into()),
                },
            ),
            (
                json!({"type": "remove", "ids": ["a"]}),
                FromProvider::Remove {
                    ids: vec!["a".into()],
                },
            ),
            (
                json!({"type": "toast", "text": "done", "level": "error", "seconds": 8}),
                FromProvider::Toast {
                    text: "done".into(),
                    level: Level::Error,
                    seconds: Some(8),
                },
            ),
            (
                json!({"type": "toast", "text": "hm"}),
                FromProvider::Toast {
                    text: "hm".into(),
                    level: Level::Info,
                    seconds: None,
                },
            ),
            (json!({"type": "ready"}), FromProvider::Ready),
            (json!({"type": "close"}), FromProvider::Close),
        ];
        for (value, want) in cases {
            assert_eq!(read(value.clone()), Ok(Line::Message(want)), "{value}");
        }
    }

    #[test]
    fn unknown_types_are_skipped_and_garbage_is_an_error() {
        assert_eq!(
            read(json!({"type": "omen", "x": 1})),
            Ok(Line::Unknown("omen".into()))
        );
        assert!(parse(b"not json").unwrap_err().contains("JSON"));
        assert!(read(json!({"id": "x"})).unwrap_err().contains("type"));
        let bad = read(json!({"type": "remove", "ids": "a"})).unwrap_err();
        assert!(bad.contains("remove"), "{bad}");
    }

    #[test]
    fn lines_to_the_provider_have_a_stable_shape() {
        let activate = ToProvider::Activate {
            stage: "rite".into(),
            id: "oil".into(),
        };
        assert_eq!(
            line(&activate),
            b"{\"type\":\"activate\",\"stage\":\"rite\",\"id\":\"oil\"}\n"
        );
        assert_eq!(line(&ToProvider::Cancel), b"{\"type\":\"cancel\"}\n");
        let hello = ToProvider::Hello { protocol: PROTOCOL };
        assert_eq!(line(&hello), b"{\"type\":\"hello\",\"protocol\":1}\n");
        let submit = ToProvider::Submit {
            stage: "s".into(),
            text: "v".into(),
        };
        assert_eq!(
            line(&submit),
            b"{\"type\":\"submit\",\"stage\":\"s\",\"text\":\"v\"}\n"
        );
    }
}
