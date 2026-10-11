//! The list a session shows (spec §3 stories 1–4, §4.5): items filtered by
//! the query with nucleo's fzf-like matcher, case- and accent-insensitive,
//! a selection, and the scene that draws it all.
//!
//! - [`Options`]: how a list looks and behaves, as the client asked
//!   (fuzzel-compatible: prompt, placeholder, password, lines, width,
//!   counter, initial search, preselection, only-match).

mod entry;
pub mod filter;
pub mod scene;
mod view;

use serde::{Deserialize, Serialize};

pub use entry::{Decor, Entry};
pub use filter::{Filter, Match};
pub use view::{Choice, ListView, Look, Reaction, Row};

/// A short themed notice over the list (a toast, vox-cast): an action's
/// outcome or an error.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Notice {
    /// What it says.
    pub text: String,
    /// How it is coloured.
    pub level: Level,
}

/// What kind of news a notice brings.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Level {
    /// Plain word.
    #[default]
    Info,
    /// Something succeeded.
    Success,
    /// Something failed.
    Error,
}

/// How a session's list looks and behaves. Every field has a default, so a
/// client names only what it changes.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default, rename_all = "kebab-case")]
pub struct Options {
    /// Shown before the query.
    pub prompt: String,
    /// Shown in place of an empty query.
    pub placeholder: String,
    /// Shows every typed character as this one instead.
    pub password: Option<char>,
    /// The most rows shown at once; 0 shows only the prompt line. `None`:
    /// the theme's window, its height and rows.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub lines: Option<u32>,
    /// The window's width in characters (an estimate for proportional
    /// fonts). `None`: the theme's window width.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub width: Option<u32>,
    /// Shows how many items match, of how many.
    pub counter: bool,
    /// The query the list opens with.
    pub search: String,
    /// The item selected when it arrives, until the user moves.
    pub select: Option<Preselect>,
    /// Return accepts only an item, never the typed text.
    pub only_match: bool,
    /// Return accepts typed text only when it is not empty (a provider's
    /// required inscription).
    pub required: bool,
}

impl Default for Options {
    fn default() -> Self {
        Self {
            prompt: "> ".to_owned(),
            placeholder: String::new(),
            password: None,
            lines: None,
            width: None,
            counter: false,
            search: String::new(),
            select: None,
            only_match: false,
            required: false,
        }
    }
}

impl Options {
    /// The rows a list without a theme to ask shows when no `lines` were
    /// given: the bundled default theme's.
    pub const DEFAULT_ROWS: u32 = 9;

    /// The most rows shown at once.
    pub fn rows(&self) -> u32 {
        self.lines.unwrap_or(Self::DEFAULT_ROWS)
    }
}

/// Which item starts selected.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum Preselect {
    /// The first item whose text equals this, ignoring case.
    Text(String),
    /// The item at this index (0-based, in the order the items arrived).
    Index(u64),
}
