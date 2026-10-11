//! The daemon's protocol state machine: connections, their sessions, their
//! providers and the resolver. It does no I/O of its own; the event loop
//! feeds it [`Event`]s, it answers through each connection's [`Peer`], and
//! its timers ([`Core::deadline`], [`Core::tick`]) run on the loop's clock.

use std::collections::HashMap;
use std::time::Instant;

use logis::config::Config;

pub use super::events::{Event, Peer};

use super::apps::AppsState;
use super::providing::{Feed, Providers, Spawn};
use super::resolver::{Resolver, Toast};
use super::sessions::{Effect, SessionId, Sessions};
use crate::protocol::{self, Mode, Outcome, Reply, Request};
use crate::provider::process::Heard;
use crate::provider::{Answer, Step, dmenu::Dmenu};

pub(super) enum Phase {
    /// Waiting for `hello`.
    Greeting,
    /// Greeted; waiting for `open`.
    Ready,
    /// A session is open, fed by its provider.
    Open { mode: Mode, feed: Feed },
}

pub(super) struct Conn {
    peer: Box<dyn Peer>,
    pub(super) phase: Phase,
}

/// Connections, the session policy, their providers and the resolver.
pub struct Core<R> {
    pub(super) resolver: R,
    pub(super) sessions: Sessions,
    pub(super) conns: HashMap<SessionId, Conn>,
    pid: u32,
    pub(super) providers: Providers,
    pub(super) apps: AppsState,
}

impl<R: Resolver> Core<R> {
    /// A core whose provider sessions cannot start (see [`Self::with_spawn`]).
    pub fn new(resolver: R, pid: u32) -> Self {
        Self {
            resolver,
            sessions: Sessions::default(),
            conns: HashMap::new(),
            pid,
            providers: Providers::default(),
            apps: AppsState::default(),
        }
    }

    /// Starts provider processes with `spawn`.
    pub fn with_spawn(mut self, spawn: Spawn) -> Self {
        self.providers.spawn = spawn;
        self
    }

    /// Starts apps with this state (index, frecency, bus).
    pub fn with_apps(mut self, apps: AppsState) -> Self {
        self.apps = apps;
        self
    }

    /// Hands the config in force to the resolver (and so to the window).
    pub fn configure(&mut self, config: &Config) {
        self.apps.terminal = config.terminal.clone();
        self.resolver.configure(config);
    }

    /// The app index was (re)read.
    pub fn indexed(&mut self, index: std::sync::Arc<logis::apps::index::Index>) {
        super::apps::indexed(self, index);
    }

    /// The compositor's activation token for a session's launch.
    pub fn activated(&mut self, session: SessionId, token: Option<String>) {
        super::apps::activated(self, session, token);
    }

    /// Shows the user a notice.
    pub fn toast(&mut self, toast: Toast) {
        self.resolver.toast(toast);
    }

    /// Hands the theme to wear to the resolver (and so to the window).
    pub fn retheme(&mut self, theme: std::sync::Arc<logis::theme::Theme>) {
        self.resolver.retheme(theme);
    }

    /// No client is connected and no provider is being ended.
    pub fn is_idle(&self) -> bool {
        self.conns.is_empty() && self.providers.is_idle()
    }

    pub fn connect(&mut self, conn: SessionId, peer: Box<dyn Peer>) {
        self.conns.insert(
            conn,
            Conn {
                peer,
                phase: Phase::Greeting,
            },
        );
    }

    pub fn receive(&mut self, conn: SessionId, request: Result<Request, String>) {
        let Some(c) = self.conns.get_mut(&conn) else {
            return;
        };
        let request = match request {
            Ok(request) => request,
            Err(message) => return self.fail(conn, message),
        };
        match (&mut c.phase, request) {
            (Phase::Greeting, Request::Hello { version }) if version == protocol::VERSION => {
                c.phase = Phase::Ready;
                c.peer.send(&Reply::Welcome {
                    version,
                    pid: self.pid,
                });
            }
            (Phase::Greeting, Request::Hello { version }) => self.fail(
                conn,
                format!(
                    "the client speaks protocol {version}, this machine spirit protocol {}; \
                     restart the daemon so both speak the same",
                    protocol::VERSION
                ),
            ),
            (
                Phase::Ready,
                Request::Open {
                    mode,
                    options,
                    provider,
                    env,
                },
            ) => {
                let feed = self
                    .resolver
                    .accepts(mode)
                    .and_then(|()| Feed::new(mode, options, provider, env));
                match feed {
                    Ok(feed) => {
                        c.phase = Phase::Open { mode, feed };
                        let effects = self.sessions.open(conn, mode.is_interactive());
                        self.apply(effects);
                    }
                    Err(message) => self.fail(conn, message),
                }
            }
            (Phase::Open { mode, feed }, Request::Items { items }) => {
                let (Mode::Dmenu, Feed::Dmenu { lines, ended }) = (*mode, feed) else {
                    return self.fail(conn, "only dmenu sessions take items".to_owned());
                };
                if *ended {
                    return self.fail(conn, "items arrived after end".to_owned());
                }
                if let Some(step) = lines.push(items) {
                    self.resolver.step(conn, step);
                }
            }
            (Phase::Open { feed, .. }, Request::End) => match feed {
                Feed::Dmenu { ended, .. } if !*ended => {
                    *ended = true;
                    if self.sessions.visible() == Some(conn) {
                        self.resolver.end(conn);
                    }
                }
                _ => self.out_of_place(conn, "end"),
            },
            (Phase::Open { .. }, Request::Cancel) => self.finish(conn, Outcome::Cancelled),
            (_, request) => {
                let name = serde_json::to_value(&request)
                    .ok()
                    .and_then(|v| v["type"].as_str().map(str::to_owned))
                    .unwrap_or_default();
                self.out_of_place(conn, &name);
            }
        }
    }

    fn out_of_place(&mut self, conn: SessionId, name: &str) {
        let expected = match self.conns.get(&conn).map(|c| &c.phase) {
            Some(Phase::Greeting) => "the first word must be 'hello'",
            Some(Phase::Ready) => "a session begins with 'open'",
            _ => "the session is already open",
        };
        self.fail(conn, format!("'{name}' is out of place: {expected}"));
    }

    pub fn disconnect(&mut self, conn: SessionId) {
        if let Some(c) = self.conns.remove(&conn) {
            self.close_session(conn, c, false);
        }
    }

    /// The user answered the visible session.
    pub fn answered(&mut self, session: SessionId, answer: Answer) {
        if self.sessions.visible() != Some(session) {
            return;
        }
        let Some(Conn {
            phase: Phase::Open { feed, .. },
            ..
        }) = self.conns.get_mut(&session)
        else {
            return;
        };
        if matches!(feed, Feed::Apps(_)) && answer != Answer::Dismissed {
            return super::apps::answered(self, session, answer);
        }
        let ending = match (feed, answer) {
            (Feed::Dmenu { lines, .. }, answer) => Ok(Some(lines.outcome(answer))),
            (_, Answer::Dismissed) => Ok(Some(Outcome::Dismissed)),
            (Feed::Provider(live), answer) => live.external.answer(answer).map(|()| None),
            (Feed::Waiting(_) | Feed::Apps(_), _) => Ok(None),
        };
        match ending {
            Ok(Some(outcome)) => self.finish(session, outcome),
            Ok(None) => {}
            Err(e) => self.fail(session, format!("the provider was lost: {e}")),
        }
    }

    /// The resolver could not show `session`: its client gets `message`
    /// (and exits 2), and the next session is shown.
    pub fn failed(&mut self, session: SessionId, message: String) {
        if self.sessions.visible() == Some(session) {
            self.fail(session, message);
        }
    }

    /// A provider process spoke or exited.
    pub fn heard(&mut self, session: SessionId, heard: Heard) {
        super::providing::heard(self, session, heard);
    }

    /// When the next timer is due, if any.
    pub fn deadline(&self) -> Option<Instant> {
        let providing = super::providing::deadline(self);
        let apps = super::apps::deadline(self);
        providing.into_iter().chain(apps).min()
    }

    /// Runs the timers due at `now`.
    pub fn tick(&mut self, now: Instant) {
        super::providing::tick(self, now);
        super::apps::tick(self, now);
    }

    /// Ends a session with its result and closes its connection.
    pub(super) fn finish(&mut self, conn: SessionId, outcome: Outcome) {
        if let Some(mut c) = self.conns.remove(&conn) {
            let closed = outcome == Outcome::Closed;
            c.peer.send(&Reply::Result { outcome });
            c.peer.close();
            self.close_session(conn, c, closed);
        }
    }

    /// Ends a connection with an error, cancelling any session it had.
    pub(super) fn fail(&mut self, conn: SessionId, message: String) {
        if let Some(mut c) = self.conns.remove(&conn) {
            c.peer.send(&Reply::Error { message });
            c.peer.close();
            self.close_session(conn, c, false);
        }
    }

    /// The session of `c` is over: its provider retires, and the window
    /// hides (or lingers on a toast after the provider's own `close`).
    fn close_session(&mut self, conn: SessionId, c: Conn, closed: bool) {
        let linger = match c.phase {
            Phase::Open {
                feed: Feed::Provider(live),
                ..
            } => self.providers.retire(conn, *live, closed),
            _ => false,
        };
        let mut effects = self.sessions.close(conn);
        if linger && effects.contains(&Effect::Hide(conn)) {
            effects.retain(|e| *e != Effect::Hide(conn));
            self.resolver.linger(conn);
        }
        self.apply(effects);
    }

    fn apply(&mut self, effects: Vec<Effect>) {
        for effect in effects {
            match effect {
                Effect::Show(id) => self.show(id),
                Effect::Queue(id) => {
                    if let Some(c) = self.conns.get_mut(&id) {
                        c.peer.send(&Reply::Queued);
                    }
                }
                Effect::Hide(id) => self.resolver.hide(id),
                Effect::Replaced(id) => self.finish(id, Outcome::Replaced),
            }
        }
    }

    fn show(&mut self, id: SessionId) {
        let Some(c) = self.conns.get_mut(&id) else {
            return;
        };
        let Phase::Open { mode, feed } = &mut c.phase else {
            return;
        };
        c.peer.send(&Reply::Visible);
        let mode = *mode;
        match feed {
            Feed::Dmenu { lines, ended } => {
                let ended = *ended;
                if let Step::Stage { options, entries } = lines.show() {
                    self.resolver.show(id, mode, options, entries);
                }
                if ended {
                    self.resolver.end(id);
                }
            }
            Feed::Waiting(_) => super::providing::start(self, id),
            Feed::Apps(_) => super::apps::show(self, id),
            Feed::Provider(_) => {}
        }
    }
}

impl Feed {
    /// What feeds a new session of `mode`: an external provider (named by
    /// a provider session alone), or the client's own lines.
    fn new(
        mode: Mode,
        options: crate::protocol::Options,
        provider: Option<crate::protocol::Launch>,
        env: Vec<crate::protocol::Bytes>,
    ) -> Result<Self, String> {
        match (mode, provider) {
            (Mode::Apps, None) => Ok(Self::Apps(Box::new(super::apps::AppsFeed::new(env)))),
            (Mode::Provider, Some(launch)) => Ok(Self::Waiting(launch)),
            (Mode::Provider, None) => Err("a provider session must name its provider".to_owned()),
            (_, Some(_)) => Err("only a provider session names a provider".to_owned()),
            (_, None) => Ok(Self::Dmenu {
                lines: Dmenu::new(options),
                ended: false,
            }),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::{Item, Options};
    use logis::list::Entry;
    use std::sync::{Arc, Mutex};

    /// What a peer was sent, and whether it was closed.
    #[derive(Clone, Default)]
    struct Wire(Arc<Mutex<(Vec<Reply>, bool)>>);

    impl Wire {
        fn take(&self) -> Vec<Reply> {
            std::mem::take(&mut self.0.lock().unwrap().0)
        }
        fn closed(&self) -> bool {
            self.0.lock().unwrap().1
        }
    }

    impl Peer for Wire {
        fn send(&mut self, reply: &Reply) {
            self.0.lock().unwrap().0.push(reply.clone());
        }
        fn close(&mut self) {
            self.0.lock().unwrap().1 = true;
        }
    }

    /// Records resolver calls as text.
    #[derive(Clone, Default)]
    struct Recorder {
        log: Arc<Mutex<Vec<String>>>,
        refuse: Option<&'static str>,
    }

    impl Recorder {
        fn take(&self) -> Vec<String> {
            std::mem::take(&mut self.log.lock().unwrap())
        }
        fn note(&self, line: String) {
            self.log.lock().unwrap().push(line);
        }
    }

    fn texts(entries: &[Entry]) -> String {
        entries
            .iter()
            .map(|e| e.text.as_str())
            .collect::<Vec<_>>()
            .join(",")
    }

    impl Resolver for Recorder {
        fn accepts(&self, _: Mode) -> Result<(), String> {
            self.refuse.map_or(Ok(()), |m| Err(m.to_owned()))
        }
        fn show(&mut self, s: SessionId, mode: Mode, o: Options, entries: Vec<Entry>) {
            let prompt = if o.prompt == Options::default().prompt {
                String::new()
            } else {
                format!(" '{}'", o.prompt)
            };
            self.note(format!("show {s} {mode:?}{prompt} [{}]", texts(&entries)));
        }
        fn step(&mut self, s: SessionId, step: Step) {
            self.note(match step {
                Step::Stage { entries, .. } => format!("stage {s} [{}]", texts(&entries)),
                Step::Append(entries) => format!("append {s} [{}]", texts(&entries)),
                Step::Update(changed) => format!(
                    "update {s} {:?}",
                    changed.iter().map(|c| c.0).collect::<Vec<_>>()
                ),
                Step::Remove(gone) => format!("remove {s} {gone:?}"),
                Step::Preselect(i) => format!("preselect {s} {i}"),
                Step::Toast { notice, .. } => format!("toast {s} {}", notice.text),
                Step::Ready => format!("ready {s}"),
            });
        }
        fn end(&mut self, s: SessionId) {
            self.note(format!("end {s}"));
        }
        fn hide(&mut self, s: SessionId) {
            self.note(format!("hide {s}"));
        }
        fn linger(&mut self, s: SessionId) {
            self.note(format!("linger {s}"));
        }
        fn untoast(&mut self, s: SessionId) {
            self.note(format!("untoast {s}"));
        }
    }

    const PID: u32 = 4242;

    fn core() -> (Core<Recorder>, Recorder) {
        let recorder = Recorder::default();
        (Core::new(recorder.clone(), PID), recorder)
    }

    fn items(texts: &[&str]) -> Request {
        let items = texts
            .iter()
            .map(|t| Item::from_raw(t.as_bytes().to_vec()))
            .collect();
        Request::Items { items }
    }

    /// Connects `conn` and greets it; returns its wire with the welcome taken.
    fn greet<R: Resolver>(core: &mut Core<R>, conn: SessionId) -> Wire {
        let wire = Wire::default();
        core.connect(conn, Box::new(wire.clone()));
        core.receive(
            conn,
            Ok(Request::Hello {
                version: protocol::VERSION,
            }),
        );
        assert_eq!(
            wire.take(),
            [Reply::Welcome {
                version: protocol::VERSION,
                pid: PID
            }]
        );
        wire
    }

    fn open<R: Resolver>(core: &mut Core<R>, conn: SessionId, mode: Mode) -> Wire {
        let wire = greet(core, conn);
        core.receive(conn, Ok(open_request(mode)));
        wire
    }

    fn open_request(mode: Mode) -> Request {
        Request::Open {
            mode,
            options: Options::default(),
            provider: None,
            env: Vec::new(),
        }
    }

    #[test]
    fn a_session_the_resolver_could_not_show_ends_with_its_error() {
        let (mut core, recorder) = core();
        let failing = open(&mut core, 1, Mode::Dmenu);
        let waiting = open(&mut core, 2, Mode::Dmenu);
        core.failed(2, "not visible yet".into());
        assert!(failing.take().contains(&Reply::Visible));
        core.failed(1, "the glass cracked".into());
        assert_eq!(
            failing.take(),
            [Reply::Error {
                message: "the glass cracked".into()
            }]
        );
        assert!(failing.closed());
        assert!(
            waiting.take().contains(&Reply::Visible),
            "the next is shown"
        );
        assert_eq!(recorder.take()[..2], ["show 1 Dmenu []", "hide 1"]);
    }

    #[test]
    fn the_options_a_client_opened_with_reach_the_resolver_when_shown() {
        let (mut core, recorder) = core();
        open(&mut core, 1, Mode::Dmenu);
        let _queued = greet(&mut core, 2);
        let options = Options {
            prompt: "rite? ".into(),
            ..Options::default()
        };
        core.receive(
            2,
            Ok(Request::Open {
                mode: Mode::Dmenu,
                options,
                provider: None,
                env: Vec::new(),
            }),
        );
        core.receive(2, Ok(items(&["b"])));
        assert_eq!(recorder.take(), ["show 1 Dmenu []"]);
        core.receive(1, Ok(Request::Cancel));
        assert_eq!(recorder.take(), ["hide 1", "show 2 Dmenu 'rite? ' [b]"]);
    }

    fn result(outcome: Outcome) -> Reply {
        Reply::Result { outcome }
    }

    fn is_error(replies: &[Reply], needle: &str) -> bool {
        matches!(replies, [Reply::Error { message }] if message.contains(needle))
    }

    #[test]
    fn a_greeting_of_another_version_is_refused() {
        let (mut core, _) = core();
        let wire = Wire::default();
        core.connect(1, Box::new(wire.clone()));
        core.receive(1, Ok(Request::Hello { version: 99 }));
        assert!(is_error(&wire.take(), "99"));
        assert!(wire.closed());
        assert!(core.is_idle());
    }

    #[test]
    fn the_first_session_is_shown_and_its_items_streamed_to_the_resolver() {
        let (mut core, log) = core();
        let wire = open(&mut core, 1, Mode::Dmenu);
        core.receive(1, Ok(items(&["a", "b"])));
        core.receive(1, Ok(Request::End));
        assert_eq!(wire.take(), [Reply::Visible]);
        assert_eq!(log.take(), ["show 1 Dmenu []", "append 1 [a,b]", "end 1"]);
    }

    #[test]
    fn a_resolved_session_gets_its_result_and_the_next_queued_one_is_shown() {
        let (mut core, log) = core();
        let first = open(&mut core, 1, Mode::Dmenu);
        let second = open(&mut core, 2, Mode::Dmenu);
        core.receive(2, Ok(items(&["x"])));
        core.receive(2, Ok(Request::End));
        assert_eq!(second.take(), [Reply::Queued]);
        assert_eq!(log.take(), ["show 1 Dmenu []"]);

        core.answered(1, Answer::Dismissed);
        assert_eq!(first.take(), [Reply::Visible, result(Outcome::Dismissed)]);
        assert!(first.closed());
        assert_eq!(second.take(), [Reply::Visible]);
        assert_eq!(log.take(), ["hide 1", "show 2 Dmenu [x]", "end 2"]);
        assert!(!second.closed());
    }

    #[test]
    fn an_interactive_session_replaces_the_visible_one() {
        let (mut core, log) = core();
        let first = open(&mut core, 1, Mode::Dmenu);
        let second = open(&mut core, 2, Mode::Apps);
        assert_eq!(first.take(), [Reply::Visible, result(Outcome::Replaced)]);
        assert!(first.closed());
        assert_eq!(second.take(), [Reply::Visible]);
        assert_eq!(log.take(), ["show 1 Dmenu []", "hide 1", "show 2 Apps []"]);
    }

    #[test]
    fn a_disconnect_cancels_the_session_and_shows_the_next() {
        let (mut core, log) = core();
        open(&mut core, 1, Mode::Dmenu);
        let second = open(&mut core, 2, Mode::Dmenu);
        core.disconnect(1);
        assert_eq!(second.take(), [Reply::Queued, Reply::Visible]);
        assert_eq!(log.take(), ["show 1 Dmenu []", "hide 1", "show 2 Dmenu []"]);
        core.answered(1, Answer::Dismissed);
        assert!(
            log.take().is_empty(),
            "a late answer for a gone session is ignored"
        );
    }

    #[test]
    fn a_cancel_ends_the_session_with_cancelled() {
        let (mut core, log) = core();
        let wire = open(&mut core, 1, Mode::Servitor);
        core.receive(1, Ok(Request::Cancel));
        assert_eq!(wire.take(), [Reply::Visible, result(Outcome::Cancelled)]);
        assert!(wire.closed());
        assert_eq!(log.take(), ["show 1 Servitor []", "hide 1"]);
        assert!(core.is_idle());
    }

    #[test]
    fn the_result_goes_only_to_the_session_that_was_answered() {
        let (mut core, _) = core();
        let first = open(&mut core, 1, Mode::Dmenu);
        let second = open(&mut core, 2, Mode::Dmenu);
        core.receive(1, Ok(items(&["a", "b"])));
        core.answered(1, Answer::Item(1));
        let chosen = Item::from_raw(b"b".to_vec());
        assert_eq!(
            first.take(),
            [
                Reply::Visible,
                result(Outcome::Selected {
                    index: 1,
                    item: chosen
                })
            ]
        );
        assert_eq!(second.take(), [Reply::Queued, Reply::Visible]);
    }

    #[test]
    fn protocol_violations_end_the_connection_with_an_error() {
        let cases: [(&[Request], &str); 4] = [
            (&[items(&["a"])], "open"),
            (&[open_request(Mode::Apps), items(&["a"])], "dmenu"),
            (
                &[open_request(Mode::Dmenu), Request::End, items(&["a"])],
                "end",
            ),
            (
                &[open_request(Mode::Dmenu), Request::Hello { version: 1 }],
                "hello",
            ),
        ];
        for (requests, needle) in cases {
            let (mut core, _) = core();
            let wire = greet(&mut core, 1);
            for r in requests {
                core.receive(1, Ok(r.clone()));
            }
            let replies: Vec<_> = wire
                .take()
                .into_iter()
                .filter(|r| r != &Reply::Visible)
                .collect();
            assert!(is_error(&replies, needle), "{needle}: {replies:?}");
            assert!(wire.closed(), "{needle}");
            assert!(core.is_idle(), "{needle}");
        }
    }

    #[test]
    fn an_undecodable_frame_ends_the_connection_with_an_error() {
        let (mut core, _) = core();
        let wire = greet(&mut core, 1);
        core.receive(1, Err("malformed message: oops".to_owned()));
        assert!(is_error(&wire.take(), "oops"));
        assert!(wire.closed());
    }

    #[test]
    fn a_mode_the_resolver_refuses_is_an_error() {
        let recorder = Recorder {
            refuse: Some("no window"),
            ..Recorder::default()
        };
        let mut core = Core::new(recorder.clone(), PID);
        let wire = open(&mut core, 1, Mode::Dmenu);
        assert!(is_error(&wire.take(), "no window"));
        assert!(wire.closed());
        assert!(recorder.take().is_empty());
    }

    #[test]
    fn the_daemon_is_idle_only_without_connections() {
        let (mut core, _) = core();
        assert!(core.is_idle());
        core.connect(1, Box::new(Wire::default()));
        assert!(!core.is_idle());
        core.disconnect(1);
        assert!(core.is_idle());
    }
}
