//! The config document as serde reads it. Each value is checked while it is
//! read, so serde_json can locate every error, and each expectation names its
//! key, so the message says which setting is wrong.

use std::fmt;
use std::time::Duration;

use serde::Deserialize;
use serde::de::{self, DeserializeSeed, Deserializer, SeqAccess, Unexpected, Visitor};

use super::{Config, FocusLoss};

/// The keys as written; `None` where a key is absent.
#[derive(Deserialize)]
#[serde(deny_unknown_fields)]
pub(super) struct Document {
    #[serde(rename = "$schema", default, deserialize_with = "schema")]
    _schema: (),
    #[serde(default, deserialize_with = "theme")]
    theme: Option<String>,
    #[serde(rename = "idle-timeout", default, deserialize_with = "idle_timeout")]
    idle_timeout: Option<Option<Duration>>,
    #[serde(default, deserialize_with = "terminal")]
    terminal: Option<Vec<String>>,
    #[serde(rename = "icon-theme", default, deserialize_with = "icon_theme")]
    icon_theme: Option<String>,
    #[serde(rename = "focus-loss", default, deserialize_with = "focus_loss")]
    focus_loss: Option<FocusLoss>,
    #[serde(default, deserialize_with = "blur")]
    blur: Option<bool>,
}

impl From<Document> for Config {
    fn from(doc: Document) -> Self {
        let defaults = Config::default();
        Self {
            theme: doc.theme.unwrap_or(defaults.theme),
            idle_timeout: doc.idle_timeout.unwrap_or(defaults.idle_timeout),
            terminal: doc.terminal.or(defaults.terminal),
            icon_theme: doc.icon_theme.unwrap_or(defaults.icon_theme),
            focus_loss: doc.focus_loss.unwrap_or(defaults.focus_loss),
            blur: doc.blur.unwrap_or(defaults.blur),
        }
    }
}

/// A theme name is a file stem in `themes/`: not empty, no `/`, and not
/// starting with `.` (no hidden files, no `..`). The schema's pattern for
/// `theme` says the same.
pub(super) fn is_theme_name(name: &str) -> bool {
    !name.is_empty() && !name.starts_with('.') && !name.contains('/')
}

fn schema<'de, D: Deserializer<'de>>(d: D) -> Result<(), D::Error> {
    d.deserialize_any(Text {
        key: "$schema",
        wants: "the URL or path of the JSON Schema",
        valid: |_| true,
    })
    .map(drop)
}

fn theme<'de, D: Deserializer<'de>>(d: D) -> Result<Option<String>, D::Error> {
    d.deserialize_any(Text {
        key: "theme",
        wants: "a theme's name (its file name in themes/ without .jsonc: \
                not empty, no '/', not starting with '.')",
        valid: is_theme_name,
    })
    .map(Some)
}

fn icon_theme<'de, D: Deserializer<'de>>(d: D) -> Result<Option<String>, D::Error> {
    d.deserialize_any(Text {
        key: "icon-theme",
        wants: "the name of an icon theme, not empty",
        valid: |name| !name.is_empty(),
    })
    .map(Some)
}

fn focus_loss<'de, D: Deserializer<'de>>(d: D) -> Result<Option<FocusLoss>, D::Error> {
    let word = d.deserialize_any(Text {
        key: "focus-loss",
        wants: "\"dismiss\" or \"keep\"",
        valid: |word| matches!(word, "dismiss" | "keep"),
    })?;
    Ok(Some(if word == "keep" {
        FocusLoss::Keep
    } else {
        FocusLoss::Dismiss
    }))
}

fn blur<'de, D: Deserializer<'de>>(d: D) -> Result<Option<bool>, D::Error> {
    d.deserialize_any(Flag { key: "blur" }).map(Some)
}

fn idle_timeout<'de, D: Deserializer<'de>>(d: D) -> Result<Option<Option<Duration>>, D::Error> {
    d.deserialize_any(Seconds).map(Some)
}

fn terminal<'de, D: Deserializer<'de>>(d: D) -> Result<Option<Vec<String>>, D::Error> {
    d.deserialize_any(Command).map(Some)
}

/// A string that `valid` accepts.
#[derive(Clone, Copy)]
struct Text {
    key: &'static str,
    wants: &'static str,
    valid: fn(&str) -> bool,
}

impl Visitor<'_> for Text {
    type Value = String;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "`{}` as {}", self.key, self.wants)
    }

    fn visit_str<E: de::Error>(self, v: &str) -> Result<String, E> {
        if (self.valid)(v) {
            Ok(v.to_owned())
        } else {
            Err(E::invalid_value(Unexpected::Str(v), &self))
        }
    }
}

impl<'de> DeserializeSeed<'de> for Text {
    type Value = String;

    fn deserialize<D: Deserializer<'de>>(self, d: D) -> Result<String, D::Error> {
        d.deserialize_any(self)
    }
}

/// `true` or `false`.
struct Flag {
    key: &'static str,
}

impl Visitor<'_> for Flag {
    type Value = bool;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "`{}` as true or false", self.key)
    }

    fn visit_bool<E: de::Error>(self, v: bool) -> Result<bool, E> {
        Ok(v)
    }
}

/// A number of seconds, 0 or more; 0 means "never".
struct Seconds;

impl Visitor<'_> for Seconds {
    type Value = Option<Duration>;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("`idle-timeout` as seconds, 0 or more (0 keeps the daemon resident)")
    }

    fn visit_u64<E: de::Error>(self, v: u64) -> Result<Self::Value, E> {
        Ok(Some(Duration::from_secs(v)).filter(|d| !d.is_zero()))
    }

    fn visit_i64<E: de::Error>(self, v: i64) -> Result<Self::Value, E> {
        match u64::try_from(v) {
            Ok(v) => self.visit_u64(v),
            Err(_) => Err(E::invalid_value(Unexpected::Signed(v), &self)),
        }
    }

    fn visit_f64<E: de::Error>(self, v: f64) -> Result<Self::Value, E> {
        match Duration::try_from_secs_f64(v) {
            Ok(d) => Ok(Some(d).filter(|d| !d.is_zero())),
            Err(_) => Err(E::invalid_value(Unexpected::Float(v), &self)),
        }
    }
}

/// A command line as an array: the program (not empty), then its arguments.
struct Command;

impl Command {
    const ITEM: Text = Text {
        key: "terminal",
        wants: "an array of strings",
        valid: |_| true,
    };
}

impl<'de> Visitor<'de> for Command {
    type Value = Vec<String>;

    fn expecting(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(
            "`terminal` as a command line array such as [\"foot\", \"-e\"], \
             its program not empty",
        )
    }

    fn visit_seq<A: SeqAccess<'de>>(self, mut seq: A) -> Result<Self::Value, A::Error> {
        let mut argv = Vec::new();
        while let Some(arg) = seq.next_element_seed(Self::ITEM)? {
            argv.push(arg);
        }
        match argv.first().map(String::as_str) {
            None => Err(de::Error::invalid_length(0, &self)),
            Some("") => Err(de::Error::invalid_value(Unexpected::Str(""), &self)),
            Some(_) => Ok(argv),
        }
    }
}
