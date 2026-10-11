//! The seam between the daemon's session bookkeeping and whatever lets the
//! user answer a session. The real one is the cogitator window
//! ([`super::window`]); [`Scripted`] answers sessions by rule, so the daemon
//! can be driven end to end without a compositor window.

use std::sync::mpsc::Sender;

use logis::config::Config;

use logis::list::Entry;

use super::core::Event;
use super::sessions::SessionId;
use crate::protocol::{Mode, Options};
use crate::provider::{Answer, Step};

/// A short themed notice for the user (CONTEXT.md: toast, vox-cast). The
/// window shows it over a visible session, or on its own; until a window
/// exists, the daemon's log carries the same words.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Toast {
    /// An edit to the config was refused (the error, with its place); the
    /// last sound config stays in force.
    ConfigRejected { error: String },
}

/// Shows sessions and reports the user's answers with [`Event::Answered`].
///
/// The daemon calls these in order for one session: `show` with its first
/// stage, then any number of `step`s (more items, later stages, toasts …),
/// at most one `end` (a dmenu client sent all its lines), and `hide` (or
/// `linger`) once it is no longer visible. A resolver may answer a session
/// at any point while it is visible.
pub trait Resolver {
    /// Whether sessions of this mode can be answered at all.
    fn accepts(&self, mode: Mode) -> Result<(), String>;
    /// The session becomes visible with its first stage: the list `options`
    /// and the items so far. A dmenu answer ends its session; a provider's
    /// leads to its next step.
    fn show(&mut self, session: SessionId, mode: Mode, options: Options, entries: Vec<Entry>);
    /// What the visible session's provider asks next.
    fn step(&mut self, session: SessionId, step: Step);
    /// The visible dmenu session's client has sent all its items.
    fn end(&mut self, session: SessionId);
    /// The session is no longer visible (answered, replaced or cancelled).
    fn hide(&mut self, session: SessionId);
    /// The session is over, but its toast still shows: keep that in view
    /// without taking keys until `hide`.
    fn linger(&mut self, session: SessionId) {
        self.hide(session);
    }
    /// The visible session's toast has had its time.
    fn untoast(&mut self, _session: SessionId) {}
    /// The config in force: once at start, then after every applied reload.
    /// A visible window applies it at once.
    fn configure(&mut self, _config: &Config) {}
    /// A notice for the user, such as a refused config edit.
    fn toast(&mut self, _toast: Toast) {}
    /// The visible session launches app `app_id`: ask the compositor for an
    /// xdg-activation token and send it as [`Event::Activation`]. Without
    /// one in time the daemon launches without a token.
    fn activation(&mut self, _session: SessionId, _app_id: &str) {}
}

impl<R: Resolver + ?Sized> Resolver for Box<R> {
    fn accepts(&self, mode: Mode) -> Result<(), String> {
        (**self).accepts(mode)
    }
    fn show(&mut self, session: SessionId, mode: Mode, options: Options, entries: Vec<Entry>) {
        (**self).show(session, mode, options, entries);
    }
    fn step(&mut self, session: SessionId, step: Step) {
        (**self).step(session, step);
    }
    fn end(&mut self, session: SessionId) {
        (**self).end(session);
    }
    fn hide(&mut self, session: SessionId) {
        (**self).hide(session);
    }
    fn linger(&mut self, session: SessionId) {
        (**self).linger(session);
    }
    fn untoast(&mut self, session: SessionId) {
        (**self).untoast(session);
    }
    fn configure(&mut self, config: &Config) {
        (**self).configure(config);
    }
    fn toast(&mut self, toast: Toast) {
        (**self).toast(toast);
    }
    fn activation(&mut self, session: SessionId, app_id: &str) {
        (**self).activation(session, app_id);
    }
}

/// A resolver that refuses every session (unit tests of the daemon).
#[cfg(test)]
pub struct Windowless;

#[cfg(test)]
impl Resolver for Windowless {
    fn accepts(&self, _: Mode) -> Result<(), String> {
        Err("the cogitator has no window to manifest in yet; \
             this machine spirit can only listen"
            .to_owned())
    }
    fn show(&mut self, _: SessionId, _: Mode, _: Options, _: Vec<Entry>) {}
    fn step(&mut self, _: SessionId, _: Step) {}
    fn end(&mut self, _: SessionId) {}
    fn hide(&mut self, _: SessionId) {}
}

/// Answers sessions without a user, for tests and measurements (selected
/// with `LOGIS_RESOLVER=scripted`), choosing the first item whose text
/// starts with [`Scripted::MARK`]:
///
/// - a dmenu session once its client has sent every line, or dismissed
///   when no line is marked;
/// - a provider's stage as soon as a marked item is there and settled (a
///   pending one never), or, in a stage that takes typed text and has a
///   placeholder starting with the mark, the rest of that placeholder as
///   typed text. After an answer it waits for the provider's next stage
///   or `ready`, as the window does.
///
/// App sessions are answered as provider stages are (an app whose name
/// starts with the mark), and get a stand-in activation token,
/// `scripted-<session>`, at once. Servitor sessions are never answered;
/// they end by replacement, cancel or disconnect.
pub struct Scripted {
    events: Sender<Event>,
    visible: Option<Visible>,
}

struct Visible {
    session: SessionId,
    /// The stage takes typed text: what to type.
    typed: Option<String>,
    entries: Vec<Entry>,
    mode: Mode,
    /// An answer went out; wait for the next stage or `ready`.
    waiting: bool,
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

    fn answer(&mut self, session: SessionId, answer: Answer) {
        // The daemon may already be gone; then nobody waits for the answer.
        let _ = self.events.send(Event::Answered { session, answer });
    }

    /// Answers the visible provider stage if it can be answered now.
    fn try_answer(&mut self) {
        let Some(v) = self
            .visible
            .as_mut()
            .filter(|v| matches!(v.mode, Mode::Provider | Mode::Apps) && !v.waiting)
        else {
            return;
        };
        let marked = v
            .entries
            .iter()
            .position(|e| e.text.starts_with(Self::MARK) && !e.is_pending());
        let answer = match (marked, &v.typed) {
            (Some(i), _) => Answer::Item(i as u32),
            (None, Some(text)) => Answer::Typed(text.clone()),
            (None, None) => return,
        };
        v.waiting = true;
        let session = v.session;
        self.answer(session, answer);
    }
}

impl Resolver for Scripted {
    fn accepts(&self, _: Mode) -> Result<(), String> {
        Ok(())
    }

    fn show(&mut self, session: SessionId, mode: Mode, options: Options, entries: Vec<Entry>) {
        self.visible = Some(Visible {
            session,
            typed: typed(&options),
            entries,
            mode,
            waiting: false,
        });
        self.try_answer();
    }

    fn step(&mut self, session: SessionId, step: Step) {
        let Some(v) = self.visible.as_mut().filter(|v| v.session == session) else {
            return;
        };
        match step {
            Step::Stage { options, entries } => {
                v.typed = typed(&options);
                v.entries = entries;
                v.waiting = false;
            }
            Step::Append(entries) => v.entries.extend(entries),
            Step::Update(changed) => {
                for (i, entry) in changed {
                    if let Some(slot) = v.entries.get_mut(i as usize) {
                        *slot = entry;
                    }
                }
            }
            Step::Remove(gone) => {
                for i in gone {
                    if let Some(slot) = v.entries.get_mut(i as usize) {
                        *slot = Entry::default();
                    }
                }
            }
            Step::Ready => v.waiting = false,
            Step::Preselect(_) | Step::Toast { .. } => {}
        }
        self.try_answer();
    }

    fn end(&mut self, session: SessionId) {
        let Some(v) = self.visible.as_mut().filter(|v| v.session == session) else {
            return;
        };
        if v.mode != Mode::Dmenu || v.waiting {
            return;
        }
        v.waiting = true;
        let answer = v
            .entries
            .iter()
            .position(|e| e.text.starts_with(Self::MARK))
            .map_or(Answer::Dismissed, |i| Answer::Item(i as u32));
        self.answer(session, answer);
    }

    fn hide(&mut self, session: SessionId) {
        self.visible.take_if(|v| v.session == session);
    }

    fn activation(&mut self, session: SessionId, _app_id: &str) {
        let token = Some(format!("scripted-{session}"));
        let _ = self.events.send(Event::Activation { session, token });
    }
}

/// What a stage that takes typed text gets typed into it.
fn typed(options: &Options) -> Option<String> {
    (!options.only_match)
        .then(|| options.placeholder.strip_prefix(Scripted::MARK))
        .flatten()
        .map(str::to_owned)
}

#[cfg(test)]
mod tests {
    use super::*;
    use logis::list::Decor;
    use std::sync::mpsc::{Receiver, channel};

    fn entries(texts: &[&str]) -> Vec<Entry> {
        texts.iter().map(|t| Entry::from(t.to_string())).collect()
    }

    fn answers(rx: &Receiver<Event>) -> Vec<(SessionId, Answer)> {
        rx.try_iter()
            .map(|e| match e {
                Event::Answered { session, answer } => (session, answer),
                _ => panic!("unexpected event"),
            })
            .collect()
    }

    fn scripted() -> (Scripted, Receiver<Event>) {
        let (tx, rx) = channel();
        (Scripted::new(tx), rx)
    }

    #[test]
    fn a_dmenu_session_is_answered_at_its_end_with_the_first_marked_item() {
        let (mut r, rx) = scripted();
        r.show(4, Mode::Dmenu, Options::default(), entries(&["a"]));
        r.step(4, Step::Append(entries(&["b", ">c", ">d"])));
        assert!(answers(&rx).is_empty(), "the client still streams");
        r.end(4);
        assert_eq!(answers(&rx), [(4, Answer::Item(2))]);
    }

    #[test]
    fn without_a_marked_item_the_dmenu_session_is_dismissed() {
        let (mut r, rx) = scripted();
        r.show(2, Mode::Dmenu, Options::default(), entries(&["a", "b"]));
        r.end(2);
        assert_eq!(answers(&rx), [(2, Answer::Dismissed)]);
    }

    #[test]
    fn a_hidden_session_is_never_answered() {
        let (mut r, rx) = scripted();
        r.show(1, Mode::Dmenu, Options::default(), entries(&[">a"]));
        r.hide(1);
        r.end(1);
        r.step(1, Step::Append(entries(&[">b"])));
        assert!(answers(&rx).is_empty());
    }

    #[test]
    fn servitor_sessions_are_left_open() {
        let (mut r, rx) = scripted();
        r.show(2, Mode::Servitor, Options::default(), entries(&[">a"]));
        r.end(2);
        assert!(answers(&rx).is_empty());
    }

    #[test]
    fn app_sessions_choose_the_marked_app_and_get_a_stand_in_token() {
        let (mut r, rx) = scripted();
        r.show(1, Mode::Apps, Options::default(), entries(&["a", ">b"]));
        assert_eq!(answers(&rx), [(1, Answer::Item(1))]);
        r.activation(1, "forge-b");
        let Ok(Event::Activation { session, token }) = rx.try_recv() else {
            panic!("no token");
        };
        assert_eq!((session, token.as_deref()), (1, Some("scripted-1")));
    }

    #[test]
    fn a_provider_stage_is_answered_once_a_marked_item_settles() {
        let (mut r, rx) = scripted();
        let pending = Entry {
            decor: Some(Box::new(Decor {
                pending: true,
                ..Decor::default()
            })),
            ..Entry::from(">oil".to_owned())
        };
        r.show(3, Mode::Provider, Options::default(), vec![pending]);
        assert!(answers(&rx).is_empty(), "pending is never chosen");
        r.step(3, Step::Update(vec![(0, Entry::from(">oil".to_owned()))]));
        assert_eq!(answers(&rx), [(3, Answer::Item(0))]);
        r.step(3, Step::Append(entries(&[">more"])));
        assert!(answers(&rx).is_empty(), "it waits for the provider");
        let typed = Options {
            only_match: false,
            placeholder: ">holy oil".into(),
            ..Options::default()
        };
        r.step(
            3,
            Step::Stage {
                options: typed,
                entries: Vec::new(),
            },
        );
        assert_eq!(answers(&rx), [(3, Answer::Typed("holy oil".into()))]);
    }

    #[test]
    fn windowless_refuses_every_mode() {
        for mode in [Mode::Dmenu, Mode::Apps, Mode::Servitor, Mode::Provider] {
            assert!(Windowless.accepts(mode).is_err());
        }
    }
}
