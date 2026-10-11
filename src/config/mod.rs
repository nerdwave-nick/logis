//! The configuration (spec §4.9): JSONC at `$XDG_CONFIG_HOME/logis/logis.jsonc`
//! (or `--config PATH`), described by the JSON Schema in
//! `schema/logis.schema.json` that a config names under `"$schema"`.
//!
//! Every key is optional; an absent key, an empty file and a missing default
//! file all mean the defaults. Unknown keys and values of the wrong kind are
//! errors that name the key and its line and column.

mod fields;
pub mod jsonc;
pub mod live;
mod source;
pub mod watch;

use std::fmt;
use std::time::Duration;

pub use self::source::{Error, Problem, Source};

/// The settings of this slice; later slices add their own.
#[derive(Debug, Clone, PartialEq)]
pub struct Config {
    /// The theme's name: `themes/<name>.jsonc`, or a bundled theme.
    pub theme: String,
    /// The daemon exits after this long without a client; `None` keeps it
    /// resident. `logis daemon --idle-timeout` outranks it.
    pub idle_timeout: Option<Duration>,
    /// The program and leading arguments that run a terminal app's command
    /// (`Terminal=true`), such as `["foot", "-e"]`. `None`: `$TERMINAL -e`.
    pub terminal: Option<Vec<String>>,
    /// The freedesktop icon theme; lookups fall back to hicolor.
    pub icon_theme: String,
    /// What losing keyboard focus does to a visible session.
    pub focus_loss: FocusLoss,
    /// Whether the compositor blurs behind the panel (when it offers
    /// ext-background-effect).
    pub blur: bool,
}

/// What losing keyboard focus does to a visible session (spec §4.5).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum FocusLoss {
    /// Dismiss it, as fuzzel does by default.
    #[default]
    Dismiss,
    /// Keep it open until it is answered or dismissed otherwise.
    Keep,
}

impl Config {
    /// The theme used when the config names none.
    pub const DEFAULT_THEME: &str = "forge-crimson-brass";
    /// The icon theme used when the config names none.
    pub const DEFAULT_ICON_THEME: &str = "hicolor";
}

impl Default for Config {
    fn default() -> Self {
        Self {
            theme: Self::DEFAULT_THEME.to_owned(),
            idle_timeout: None,
            terminal: None,
            icon_theme: Self::DEFAULT_ICON_THEME.to_owned(),
            focus_loss: FocusLoss::default(),
            blur: true,
        }
    }
}

/// Why a config text was refused, and where: 1-based line and column, the
/// column counted in characters and pointing at the last character of the
/// offending token (serde_json's convention).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Invalid {
    pub line: usize,
    pub column: usize,
    pub message: String,
}

impl fmt::Display for Invalid {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}: {}", self.line, self.column, self.message)
    }
}

/// `--check-config`: the verdict on the config `source` names, or the
/// flavoured error.
pub fn check(source: &Source) -> Result<String, String> {
    source.load().map_err(|e| heresy(&e))?;
    Ok(match source.path() {
        Some(path) if source.exists() => format!("the config at {} is sound", path.display()),
        Some(path) => format!("no config at {}; the defaults stand", path.display()),
        None => "no config to be found (neither XDG_CONFIG_HOME nor HOME is set); \
                 the defaults stand"
            .to_owned(),
    })
}

/// How an invalid config is reported on the command line and in the log.
pub fn heresy(error: &Error) -> String {
    heresy_text(&error.to_string())
}

/// [`heresy`] for an error already put into words.
pub fn heresy_text(error: &str) -> String {
    format!("the config is heretical: {error}")
}

/// Parses a config text. Only whitespace and comments mean the defaults.
pub fn parse(text: &str) -> Result<Config, Invalid> {
    let json = jsonc::strip(text).map_err(|at| Invalid {
        line: at.line,
        column: char_column(text, at.line, at.column),
        message: "this comment never closes: '/*' wants its '*/'".to_owned(),
    })?;
    let Some(start) = json.find(|c: char| !c.is_ascii_whitespace()) else {
        return Ok(Config::default());
    };
    if !json[start..].starts_with('{') {
        let before = &json[..start];
        let line = before.matches('\n').count() + 1;
        let column = start - before.rfind('\n').map_or(0, |n| n + 1) + 1;
        return Err(Invalid {
            line,
            column: char_column(text, line, column),
            message: "the config must be a JSON object: { \"key\": value, … }".to_owned(),
        });
    }
    match serde_json::from_str::<fields::Document>(&json) {
        Ok(doc) => Ok(doc.into()),
        Err(e) => {
            let message = e.to_string();
            let position = format!(" at line {} column {}", e.line(), e.column());
            let message = message.strip_suffix(&position).unwrap_or(&message);
            Err(Invalid {
                line: e.line(),
                column: char_column(text, e.line(), e.column()),
                message: message.to_owned(),
            })
        }
    }
}

/// The 1-based character column of a 1-based byte column on `line`.
fn char_column(text: &str, line: usize, byte_column: usize) -> usize {
    let line_text = text.split('\n').nth(line.saturating_sub(1)).unwrap_or("");
    match line_text.get(..byte_column.saturating_sub(1)) {
        Some(before) => before.chars().count() + 1,
        None => byte_column,
    }
}

#[cfg(test)]
mod schema_tests;
#[cfg(test)]
mod tests;
