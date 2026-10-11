//! Providers (spec §4.8): whatever produces the items a session shows. The
//! built-in dmenu provider streams the client's stdin; external providers
//! are processes speaking JSON lines ([`wire`], docs/provider-protocol.md).
//! Both drive a session through the same [`Step`]s, and get the user's
//! [`Answer`]s back.

pub mod dmenu;
pub mod external;
#[cfg(test)]
pub mod fake;
pub mod process;
pub mod stages;
pub mod wire;

use std::time::Duration;

use logis::list::{Entry, Notice, Options};

/// What a provider asks of its session's window. Items are addressed by
/// index: the order they first arrived in the current stage.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Step {
    /// A new list: the first one shows the window, a later one replaces
    /// the list in the same window (the next stage of a flow).
    Stage {
        options: Options,
        entries: Vec<Entry>,
    },
    /// Items after the last.
    Append(Vec<Entry>),
    /// Items replaced in their place.
    Update(Vec<(u32, Entry)>),
    /// Items gone for good.
    Remove(Vec<u32>),
    /// Select this item once it is settled, unless the user has moved.
    Preselect(u32),
    /// A short notice over the list, shown for `span`.
    Toast { notice: Notice, span: Duration },
    /// The current stage takes Return again after an answer.
    Ready,
}

/// How the user answered the current stage.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Answer {
    /// Return on the item at this index.
    Item(u32),
    /// Return on typed text that matched no item.
    Typed(String),
    /// Dismissal (Escape, focus loss, or the compositor closed the window).
    Dismissed,
}
