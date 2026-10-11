//! Keyboard input and query editing (spec §4.5).
//!
//! The model is a pipeline of small parts, each testable without Wayland:
//!
//! 1. [`Keyboard`] is fed the `wl_keyboard` events (keymap, enter, leave,
//!    key, modifiers, repeat info) and turns key presses into [`Action`]s.
//!    It owns:
//!    - [`Layout`]: the compositor's keymap and the xkb state from its
//!      modifier masks; a key resolves to its keysym, its text and the
//!      modifiers it did *not* consume ([`Mods`]);
//!    - [`Compose`]: the locale's Compose table as libxkbcommon finds it
//!      (`XCOMPOSEFILE`, then `$XDG_CONFIG_HOME/XCompose`, then
//!      `~/.XCompose`, then the locale's system table). Modifier keysyms are
//!      never fed to it, so they never cancel a sequence;
//!    - [`Repeat`]: client-side key repeat per the compositor's repeat info
//!      (rate 0 disables it). It is a deadline, never a timer thread: the
//!      event loop sleeps until the deadline while a key is held and
//!      without any timeout otherwise, so an idle window never wakes;
//!    - the focus-loss policy from the config ([`FocusLoss`]).
//! 2. [`bindings`] maps a resolved key to an [`Action`] with consumed-modifier
//!    semantics: Shift+Tab is `ISO_Left_Tab` with Shift consumed, Ctrl+[ on
//!    a layout where `[` needs AltGr is still Ctrl+[, and Caps Lock or Num
//!    Lock never change a shortcut.
//! 3. [`Query`] applies the [`Edit`]s: insert at the cursor, delete a
//!    grapheme or a word, clear, move the cursor.
//!
//! # The text-input-v3 seam (IME preedit, later)
//!
//! An input method talks to the client through `zwp_text_input_v3`, not
//! through `wl_keyboard`: while one is active, the compositor routes the
//! keys it consumes to it, and the client receives only `preedit_string`,
//! `commit_string` and `delete_surrounding_text`, applied together on
//! `done`. The model already separates what that needs:
//!
//! - **Committed text** reaches the query as [`Edit::Insert`], exactly as
//!   typed or composed text does; `delete_surrounding_text` becomes a
//!   bounded delete around [`Query::cursor`] (a new `Edit` variant). Keys
//!   the IME lets through keep flowing through [`Keyboard`] unchanged.
//! - **Preedit** is display state, never part of [`Query::text`] and never
//!   matched: a later `Query` field (text plus cursor range) drawn at the
//!   cursor and replaced on every `done`.
//! - **Surrounding text** for `set_surrounding_text` is [`Query::text`]
//!   with [`Query::cursor`] as both cursor and anchor (byte offsets, as
//!   the protocol wants); the cursor rectangle comes from the renderer.
//! - Enabling text-input on keyboard enter and disabling it on leave sits
//!   beside [`Keyboard::enter`] and [`Keyboard::leave`] in the seat.
//!
//! Nothing here assumes keys are the only source of edits, so adding the
//! protocol adds a feeder, not a rewrite. Paste is already such a feeder:
//! Ctrl+V and Shift+Insert (and the middle button) ask for an
//! [`Action::Paste`]; the window reads the selection ([`paste`]) and hands
//! the text back as an [`Edit::Insert`].
//!
//! # The pointer
//!
//! [`Pointer`] is fed the `wl_pointer` events and yields [`Point`]s
//! (position, the primary button, scrolling) as [`Action::Point`], in the
//! same queue as the keys, so their order is kept. What a point means (which
//! row it is over) is the list's to decide, since only the list knows its
//! layout.

pub mod bindings;
mod compose;
mod editor;
mod keyboard;
mod layout;
pub mod paste;
pub mod pointer;
mod repeat;

pub use bindings::Mods;
pub use compose::{Compose, compose_locale};
pub use editor::{Edit, Query};
pub use keyboard::Keyboard;
pub use layout::{Key, Layout};
pub use paste::Selection;
pub use pointer::{Point, Pointer, Scroll};
pub use repeat::Repeat;

pub use crate::config::FocusLoss;

/// What a key, the pointer (or focus loss) asks of the visible session.
#[derive(Clone, Debug, PartialEq)]
pub enum Action {
    /// End the session without a choice (Escape, Ctrl+G, Ctrl+[, or
    /// focus loss under the default policy).
    Dismiss,
    /// Choose the selected item, or the typed text (Return).
    Accept,
    /// Choose the selected item for its next stage, such as an app's
    /// desktop actions (Shift+Return); otherwise like [`Action::Accept`].
    Descend,
    /// Move the selection (Up/Down, Tab/Shift+Tab, Ctrl+N/P).
    Select(Step),
    /// Change the query.
    Edit(Edit),
    /// Insert a selection's text at the cursor (Ctrl+V, Shift+Insert, the
    /// middle button). The window reads it and turns it into
    /// [`Edit::Insert`]; sessions never see this action.
    Paste(Selection),
    /// The pointer moved, clicked or scrolled over the window.
    Point(Point),
}

/// A direction for the selection.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Step {
    /// Towards the first item.
    Previous,
    /// Towards the last item.
    Next,
}
