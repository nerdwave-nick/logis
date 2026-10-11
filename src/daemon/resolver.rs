//! The seam between the daemon's session bookkeeping and whatever lets the
//! user answer a session. The real one is the cogitator window (later slices);
//! until then, [`Windowless`] refuses sessions and [`Scripted`] answers them by
//! rule, so the daemon can be driven end to end without a compositor window.

use std::sync::mpsc::Sender;

use super::core::Event;
use super::sessions::SessionId;
use crate::protocol::{Item, Mode, Outcome};

/// Shows sessions and reports how each ended with [`Event::Resolved`].
///
/// The daemon calls these in order for one session: `show`, then any number
/// of `append`, at most one `end`, and `hide` once it is no longer visible.
/// A resolver may resolve a session at any point while it is visible.
pub trait Resolver {
    /// Whether sessions of this mode can be answered at all.
    fn accepts(&self, mode: Mode) -> Result<(), String>;
    /// The session becomes visible with the items received so far;
    /// `complete` says the client has already sent all of them.
    fn show(&mut self, session: SessionId, mode: Mode, items: Vec<Item>, complete: bool);
    /// More items for the visible session.
    fn append(&mut self, session: SessionId, items: Vec<Item>);
    /// The visible session's client has sent all its items.
    fn end(&mut self, session: SessionId);
    /// The session is no longer visible (resolved, replaced or cancelled).
    fn hide(&mut self, session: SessionId);
}

impl<R: Resolver + ?Sized> Resolver for Box<R> {
    fn accepts(&self, mode: Mode) -> Result<(), String> {
        (**self).accepts(mode)
    }
    fn show(&mut self, session: SessionId, mode: Mode, items: Vec<Item>, complete: bool) {
        (**self).show(session, mode, items, complete);
    }
    fn append(&mut self, session: SessionId, items: Vec<Item>) {
        (**self).append(session, items);
    }
    fn end(&mut self, session: SessionId) {
        (**self).end(session);
    }
    fn hide(&mut self, session: SessionId) {
        (**self).hide(session);
    }
}

/// The resolver while logis has no window: it refuses every session.
pub struct Windowless;

impl Resolver for Windowless {
    fn accepts(&self, _: Mode) -> Result<(), String> {
        Err("the cogitator has no window to manifest in yet; \
             this machine spirit can only listen"
            .to_owned())
    }
    fn show(&mut self, _: SessionId, _: Mode, _: Vec<Item>, _: bool) {}
    fn append(&mut self, _: SessionId, _: Vec<Item>) {}
    fn end(&mut self, _: SessionId) {}
    fn hide(&mut self, _: SessionId) {}
}

/// Answers dmenu sessions without a user, for tests and measurements
/// (selected with `LOGIS_RESOLVER=scripted`). Once a visible dmenu session's
/// items are complete, it selects the first item whose text starts with
/// [`Scripted::MARK`], or dismisses the session when none does. Interactive
/// sessions are never answered; they end by replacement, cancel or disconnect.
pub struct Scripted {
    events: Sender<Event>,
    visible: Option<Visible>,
}

struct Visible {
    session: SessionId,
    mode: Mode,
    items: Vec<Item>,
}

impl Scripted {
    /// The prefix that marks the item to select.
    pub const MARK: char = '>';

    pub fn new(events: Sender<Event>) -> Self {
        Self {
            events,
            visible: None,
        }
    }
}

impl Resolver for Scripted {
    fn accepts(&self, _: Mode) -> Result<(), String> {
        Ok(())
    }

    fn show(&mut self, session: SessionId, mode: Mode, items: Vec<Item>, complete: bool) {
        self.visible = Some(Visible {
            session,
            mode,
            items,
        });
        if complete {
            self.end(session);
        }
    }

    fn append(&mut self, session: SessionId, items: Vec<Item>) {
        if let Some(v) = self.visible.as_mut().filter(|v| v.session == session) {
            v.items.extend(items);
        }
    }

    fn end(&mut self, session: SessionId) {
        let Some(v) = self.visible.take_if(|v| v.session == session) else {
            return;
        };
        if v.mode.is_interactive() {
            self.visible = Some(v);
            return;
        }
        let outcome = v
            .items
            .into_iter()
            .zip(0..)
            .find(|(item, _)| item.text.starts_with(Self::MARK))
            .map_or(Outcome::Dismissed, |(item, index)| Outcome::Selected {
                index,
                item,
            });
        // The daemon may already be gone; then nobody waits for the answer.
        let _ = self.events.send(Event::Resolved { session, outcome });
    }

    fn hide(&mut self, session: SessionId) {
        self.visible.take_if(|v| v.session == session);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::mpsc::{Receiver, channel};

    fn items(texts: &[&str]) -> Vec<Item> {
        texts
            .iter()
            .map(|t| Item::from_raw(t.as_bytes().to_vec()))
            .collect()
    }

    fn resolutions(rx: &Receiver<Event>) -> Vec<(SessionId, Outcome)> {
        rx.try_iter()
            .map(|e| match e {
                Event::Resolved { session, outcome } => (session, outcome),
                _ => panic!("unexpected event"),
            })
            .collect()
    }

    fn selected(index: u64, text: &str) -> Outcome {
        let item = Item::from_raw(text.as_bytes().to_vec());
        Outcome::Selected { index, item }
    }

    #[test]
    fn a_complete_session_is_answered_with_the_first_marked_item() {
        let (tx, rx) = channel();
        let mut r = Scripted::new(tx);
        r.show(1, Mode::Dmenu, items(&["a", ">b", ">c"]), true);
        assert_eq!(resolutions(&rx), [(1, selected(1, ">b"))]);
    }

    #[test]
    fn streamed_items_are_answered_at_the_end_counting_every_item() {
        let (tx, rx) = channel();
        let mut r = Scripted::new(tx);
        r.show(4, Mode::Dmenu, items(&["a"]), false);
        r.append(4, items(&["b", ">c"]));
        assert!(resolutions(&rx).is_empty());
        r.end(4);
        assert_eq!(resolutions(&rx), [(4, selected(2, ">c"))]);
    }

    #[test]
    fn without_a_marked_item_the_session_is_dismissed() {
        let (tx, rx) = channel();
        let mut r = Scripted::new(tx);
        r.show(2, Mode::Dmenu, items(&["a", "b"]), true);
        assert_eq!(resolutions(&rx), [(2, Outcome::Dismissed)]);
    }

    #[test]
    fn a_hidden_session_is_never_answered() {
        let (tx, rx) = channel();
        let mut r = Scripted::new(tx);
        r.show(1, Mode::Dmenu, items(&[">a"]), false);
        r.hide(1);
        r.end(1);
        r.append(1, items(&[">b"]));
        assert!(resolutions(&rx).is_empty());
    }

    #[test]
    fn interactive_sessions_are_left_open() {
        let (tx, rx) = channel();
        let mut r = Scripted::new(tx);
        r.show(1, Mode::Apps, Vec::new(), true);
        r.show(2, Mode::Servitor, items(&[">a"]), true);
        assert!(resolutions(&rx).is_empty());
    }

    #[test]
    fn windowless_refuses_every_mode() {
        for mode in [Mode::Dmenu, Mode::Apps, Mode::Servitor] {
            assert!(Windowless.accepts(mode).is_err());
        }
    }
}
