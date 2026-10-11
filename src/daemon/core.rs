//! The daemon's protocol state machine: connections, their sessions, and the
//! resolver. It does no I/O of its own; the event loop feeds it [`Event`]s and
//! it answers through each connection's [`Peer`].

use std::collections::HashMap;

use super::resolver::Resolver;
use super::sessions::{Effect, SessionId, Sessions};
use crate::protocol::{self, Item, Mode, Outcome, Reply, Request};

/// Everything the daemon's event loop reacts to.
pub enum Event {
    /// A client connected; its connection number is also its session's id.
    Connected {
        conn: SessionId,
        peer: Box<dyn Peer>,
    },
    /// A frame arrived, or could not be decoded (the error says why).
    Received {
        conn: SessionId,
        request: Result<Request, String>,
    },
    /// The client hung up.
    Disconnected { conn: SessionId },
    /// The resolver's answer for a session.
    Resolved {
        session: SessionId,
        outcome: Outcome,
    },
    /// The compositor connection is gone (spec §4.2: exit non-zero).
    WaylandLost { reason: String },
}

/// The sending half of one client connection.
pub trait Peer: Send {
    /// Sends one reply; a failure is treated like a hang-up later.
    fn send(&mut self, reply: &Reply);
    /// Ends the connection; the client sees end of stream.
    fn close(&mut self);
}

enum Phase {
    /// Waiting for `hello`.
    Greeting,
    /// Greeted; waiting for `open`.
    Ready,
    /// A session is open. `pending` holds items while it waits in the queue.
    Open {
        mode: Mode,
        pending: Vec<Item>,
        ended: bool,
    },
}

struct Conn {
    peer: Box<dyn Peer>,
    phase: Phase,
}

/// Connections, the session policy and the resolver.
pub struct Core<R> {
    resolver: R,
    sessions: Sessions,
    conns: HashMap<SessionId, Conn>,
    pid: u32,
}

impl<R: Resolver> Core<R> {
    pub fn new(resolver: R, pid: u32) -> Self {
        Self {
            resolver,
            sessions: Sessions::default(),
            conns: HashMap::new(),
            pid,
        }
    }

    /// No client is connected.
    pub fn is_idle(&self) -> bool {
        self.conns.is_empty()
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
            (Phase::Ready, Request::Open { mode }) => match self.resolver.accepts(mode) {
                Ok(()) => {
                    c.phase = Phase::Open {
                        mode,
                        pending: Vec::new(),
                        ended: false,
                    };
                    let effects = self.sessions.open(conn, mode.is_interactive());
                    self.apply(effects);
                }
                Err(message) => self.fail(conn, message),
            },
            (
                Phase::Open {
                    mode,
                    pending,
                    ended,
                },
                Request::Items { items },
            ) => {
                if *mode != Mode::Dmenu {
                    return self.fail(conn, "only dmenu sessions take items".to_owned());
                }
                if *ended {
                    return self.fail(conn, "items arrived after end".to_owned());
                }
                if self.sessions.visible() == Some(conn) {
                    self.resolver.append(conn, items);
                } else {
                    pending.extend(items);
                }
            }
            (Phase::Open { ended, .. }, Request::End) if !*ended => {
                *ended = true;
                if self.sessions.visible() == Some(conn) {
                    self.resolver.end(conn);
                }
            }
            (Phase::Open { .. }, Request::Cancel) => self.finish(conn, Outcome::Cancelled),
            (phase, request) => {
                let name = serde_json::to_value(&request)
                    .ok()
                    .and_then(|v| v["type"].as_str().map(str::to_owned))
                    .unwrap_or_default();
                let expected = match phase {
                    Phase::Greeting => "the first word must be 'hello'",
                    Phase::Ready => "a session begins with 'open'",
                    Phase::Open { .. } => "the session is already open",
                };
                self.fail(conn, format!("'{name}' is out of place: {expected}"));
            }
        }
    }

    pub fn disconnect(&mut self, conn: SessionId) {
        if self.conns.remove(&conn).is_some() {
            self.close_session(conn);
        }
    }

    pub fn resolve(&mut self, session: SessionId, outcome: Outcome) {
        if self.sessions.visible() == Some(session) {
            self.finish(session, outcome);
        }
    }

    /// Ends a session with its result and closes its connection.
    fn finish(&mut self, conn: SessionId, outcome: Outcome) {
        if let Some(mut c) = self.conns.remove(&conn) {
            c.peer.send(&Reply::Result { outcome });
            c.peer.close();
            self.close_session(conn);
        }
    }

    /// Ends a connection with an error, cancelling any session it had.
    fn fail(&mut self, conn: SessionId, message: String) {
        if let Some(mut c) = self.conns.remove(&conn) {
            c.peer.send(&Reply::Error { message });
            c.peer.close();
            self.close_session(conn);
        }
    }

    fn close_session(&mut self, conn: SessionId) {
        let effects = self.sessions.close(conn);
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
        if let Phase::Open {
            mode,
            pending,
            ended,
        } = &mut c.phase
        {
            c.peer.send(&Reply::Visible);
            let items = std::mem::take(pending);
            self.resolver.show(id, *mode, items, *ended);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
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

    fn texts(items: &[Item]) -> String {
        items
            .iter()
            .map(|i| i.text.as_str())
            .collect::<Vec<_>>()
            .join(",")
    }

    impl Resolver for Recorder {
        fn accepts(&self, _: Mode) -> Result<(), String> {
            self.refuse.map_or(Ok(()), |m| Err(m.to_owned()))
        }
        fn show(&mut self, s: SessionId, mode: Mode, items: Vec<Item>, complete: bool) {
            self.note(format!("show {s} {mode:?} [{}] {complete}", texts(&items)));
        }
        fn append(&mut self, s: SessionId, items: Vec<Item>) {
            self.note(format!("append {s} [{}]", texts(&items)));
        }
        fn end(&mut self, s: SessionId) {
            self.note(format!("end {s}"));
        }
        fn hide(&mut self, s: SessionId) {
            self.note(format!("hide {s}"));
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
        core.receive(conn, Ok(Request::Open { mode }));
        wire
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
        assert_eq!(
            log.take(),
            ["show 1 Dmenu [] false", "append 1 [a,b]", "end 1"]
        );
    }

    #[test]
    fn a_resolved_session_gets_its_result_and_the_next_queued_one_is_shown() {
        let (mut core, log) = core();
        let first = open(&mut core, 1, Mode::Dmenu);
        let second = open(&mut core, 2, Mode::Dmenu);
        core.receive(2, Ok(items(&["x"])));
        core.receive(2, Ok(Request::End));
        assert_eq!(second.take(), [Reply::Queued]);
        assert_eq!(log.take(), ["show 1 Dmenu [] false"]);

        core.resolve(1, Outcome::Dismissed);
        assert_eq!(first.take(), [Reply::Visible, result(Outcome::Dismissed)]);
        assert!(first.closed());
        assert_eq!(second.take(), [Reply::Visible]);
        assert_eq!(log.take(), ["hide 1", "show 2 Dmenu [x] true"]);
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
        assert_eq!(
            log.take(),
            ["show 1 Dmenu [] false", "hide 1", "show 2 Apps [] false"]
        );
    }

    #[test]
    fn a_disconnect_cancels_the_session_and_shows_the_next() {
        let (mut core, log) = core();
        open(&mut core, 1, Mode::Dmenu);
        let second = open(&mut core, 2, Mode::Dmenu);
        core.disconnect(1);
        assert_eq!(second.take(), [Reply::Queued, Reply::Visible]);
        assert_eq!(
            log.take(),
            ["show 1 Dmenu [] false", "hide 1", "show 2 Dmenu [] false"]
        );
        core.resolve(1, Outcome::Dismissed);
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
        assert_eq!(log.take(), ["show 1 Servitor [] false", "hide 1"]);
        assert!(core.is_idle());
    }

    #[test]
    fn the_result_goes_only_to_the_session_that_was_answered() {
        let (mut core, _) = core();
        let first = open(&mut core, 1, Mode::Dmenu);
        let second = open(&mut core, 2, Mode::Dmenu);
        let chosen = Item::from_raw(b"b".to_vec());
        core.resolve(
            1,
            Outcome::Selected {
                index: 1,
                item: chosen.clone(),
            },
        );
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
            (
                &[Request::Open { mode: Mode::Apps }, items(&["a"])],
                "dmenu",
            ),
            (
                &[
                    Request::Open { mode: Mode::Dmenu },
                    Request::End,
                    items(&["a"]),
                ],
                "end",
            ),
            (
                &[
                    Request::Open { mode: Mode::Dmenu },
                    Request::Hello { version: 1 },
                ],
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
