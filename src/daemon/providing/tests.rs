//! Provider sessions through the daemon's core, with a fake process.

use super::*;
use crate::daemon::core::{Event, Peer};
use crate::protocol::{Bytes, Item as Line_, Options, Reply, Request};
use crate::provider::Answer;
use crate::provider::fake::Fake;
use crate::provider::wire::{FromProvider, Item, Line, Stage};
use logis::list::{Entry, Level, Notice};
use std::sync::{Arc, Mutex};

#[derive(Clone, Default)]
struct Wire(Arc<Mutex<(Vec<Reply>, bool)>>);

impl Wire {
    fn take(&self) -> Vec<Reply> {
        std::mem::take(&mut self.0.lock().unwrap().0)
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

#[derive(Clone, Default)]
struct Seen(Arc<Mutex<Vec<String>>>);

impl Seen {
    fn take(&self) -> Vec<String> {
        std::mem::take(&mut self.0.lock().unwrap())
    }
    fn note(&self, line: String) {
        self.0.lock().unwrap().push(line);
    }
}

fn texts(entries: &[Entry]) -> String {
    entries
        .iter()
        .map(|e| e.text.as_str())
        .collect::<Vec<_>>()
        .join(",")
}

impl Resolver for Seen {
    fn accepts(&self, _: Mode) -> Result<(), String> {
        Ok(())
    }
    fn show(&mut self, s: SessionId, mode: Mode, _: Options, entries: Vec<Entry>) {
        self.note(format!("show {s} {mode:?} [{}]", texts(&entries)));
    }
    fn step(&mut self, s: SessionId, step: Step) {
        self.note(match step {
            Step::Stage { entries, .. } => format!("stage {s} [{}]", texts(&entries)),
            Step::Toast(n) => format!("toast {s} {}", n.text),
            Step::Update(c) => format!("update {s} {}", c[0].0),
            other => format!("{other:?}"),
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

struct Rig {
    core: Core<Seen>,
    seen: Seen,
    /// Each started provider, by session.
    fakes: Arc<Mutex<Vec<(SessionId, Fake)>>>,
}

impl Rig {
    fn new() -> Self {
        let seen = Seen::default();
        let fakes: Arc<Mutex<Vec<(SessionId, Fake)>>> = Arc::default();
        let started = Arc::clone(&fakes);
        let spawn: Spawn = Box::new(move |session, launch| {
            if launch.argv[0].0 == b"/nowhere" {
                return Err("'/nowhere' cannot be started: no such file".into());
            }
            let fake = Fake::default();
            started.lock().unwrap().push((session, fake.clone()));
            Ok(Box::new(fake))
        });
        let core = Core::new(seen.clone(), 7).with_spawn(spawn);
        Self { core, seen, fakes }
    }

    fn fake(&self, session: SessionId) -> Fake {
        let fakes = self.fakes.lock().unwrap();
        fakes.iter().find(|(s, _)| *s == session).unwrap().1.clone()
    }

    /// Opens provider session `conn` running `program`; returns its wire.
    fn open(&mut self, conn: SessionId, program: &str) -> Wire {
        let wire = Wire::default();
        self.core.connect(conn, Box::new(wire.clone()));
        let hello = Request::Hello {
            version: crate::protocol::VERSION,
        };
        self.core.receive(conn, Ok(hello));
        wire.take();
        let launch = Launch {
            argv: vec![Bytes(program.as_bytes().to_vec())],
            cwd: Bytes(b"/".to_vec()),
            env: Vec::new(),
        };
        let open = Request::Open {
            mode: Mode::Provider,
            options: Options::default(),
            provider: Some(launch),
        };
        self.core.receive(conn, Ok(open));
        wire
    }

    fn say(&mut self, conn: SessionId, message: FromProvider) {
        let heard = Heard::Line(Ok(Line::Message(message)));
        self.core.heard(conn, heard);
    }

    /// A provider session past its greeting with one stage of `ids`.
    fn staged(&mut self, conn: SessionId, ids: &[&str]) -> (Wire, Fake) {
        let wire = self.open(conn, "rite");
        assert_eq!(wire.take(), [Reply::Visible]);
        let fake = self.fake(conn);
        assert_eq!(fake.take(), [r#"{"type":"hello","protocol":1}"#]);
        self.say(conn, FromProvider::Hello { protocol: 1 });
        let items = ids.iter().map(|id| item(id)).collect();
        let stage = Stage {
            id: "rite".into(),
            items,
            ..Stage::default()
        };
        self.say(conn, FromProvider::Stage(stage));
        (wire, fake)
    }
}

fn item(id: &str) -> Item {
    Item {
        id: id.into(),
        columns: vec![id.to_uppercase()],
        ..Item::default()
    }
}

fn result(wire: &Wire) -> Outcome {
    match wire.take().as_slice() {
        [Reply::Result { outcome }] => outcome.clone(),
        other => panic!("{other:?}"),
    }
}

fn error(wire: &Wire) -> String {
    match wire.take().as_slice() {
        [Reply::Error { message }] => message.clone(),
        other => panic!("{other:?}"),
    }
}

#[test]
fn a_provider_starts_when_its_session_is_shown_and_its_first_stage_shows_the_window() {
    let mut rig = Rig::new();
    let (wire, _) = rig.staged(1, &["oil", "fire"]);
    assert_eq!(rig.seen.take(), ["show 1 Provider [OIL,FIRE]"]);
    assert!(wire.take().is_empty(), "the session stays open");
    let next = Stage {
        id: "aspect".into(),
        items: vec![item("on")],
        ..Stage::default()
    };
    rig.say(1, FromProvider::Stage(next));
    assert_eq!(rig.seen.take(), ["stage 1 [ON]"]);
}

#[test]
fn a_provider_that_cannot_start_ends_its_session_with_the_reason() {
    let mut rig = Rig::new();
    let wire = rig.open(1, "/nowhere");
    let replies = wire.take();
    let [Reply::Visible, Reply::Error { message }] = &replies[..] else {
        panic!("{replies:?}");
    };
    assert!(
        message.contains("'/nowhere' cannot be started"),
        "{message}"
    );
    assert!(rig.core.is_idle());
}

#[test]
fn an_answer_activates_and_the_providers_close_ends_the_session_with_closed() {
    let mut rig = Rig::new();
    let (wire, fake) = rig.staged(1, &["oil"]);
    rig.core.answered(1, Answer::Item(0));
    assert_eq!(
        fake.take(),
        [r#"{"type":"activate","stage":"rite","id":"oil"}"#]
    );
    rig.say(1, FromProvider::Close);
    assert_eq!(result(&wire), Outcome::Closed);
    assert_eq!(fake.take(), ["eof"], "no cancel after its own close");
    assert_eq!(rig.seen.take().last().unwrap(), "hide 1");
    assert_eq!(
        rig.core.deadline(),
        None,
        "an acting provider is never signalled"
    );
}

#[test]
fn a_dismissed_provider_that_never_acted_is_cancelled_then_ended_if_it_lingers() {
    let mut rig = Rig::new();
    let (wire, fake) = rig.staged(1, &["oil"]);
    rig.core.answered(1, Answer::Dismissed);
    assert_eq!(result(&wire), Outcome::Dismissed);
    assert_eq!(fake.take(), [r#"{"type":"cancel"}"#, "eof"]);
    assert!(!rig.core.is_idle(), "it is still to be ended");
    let term = rig.core.deadline().unwrap();
    rig.core.tick(term);
    assert_eq!(fake.take(), ["signal 15"]);
    let kill = rig.core.deadline().unwrap();
    assert_eq!(kill - term, GRACE);
    rig.core.tick(kill);
    assert_eq!(fake.take(), ["signal 9"]);
    assert!(rig.core.is_idle());
}

#[test]
fn a_retired_provider_that_exits_is_forgotten() {
    let mut rig = Rig::new();
    let (_wire, fake) = rig.staged(1, &["oil"]);
    rig.core.disconnect(1);
    assert_eq!(fake.take(), [r#"{"type":"cancel"}"#, "eof"]);
    rig.core.heard(1, Heard::Exited("exit status: 0".into()));
    assert!(rig.core.is_idle());
    assert_eq!(rig.core.deadline(), None);
}

#[test]
fn dismissal_never_signals_a_provider_that_was_sent_an_answer() {
    let mut rig = Rig::new();
    let (wire, fake) = rig.staged(1, &["oil"]);
    rig.core.answered(1, Answer::Item(0));
    fake.take();
    rig.core.answered(1, Answer::Dismissed);
    assert_eq!(result(&wire), Outcome::Dismissed);
    assert_eq!(fake.take(), [r#"{"type":"cancel"}"#, "eof"]);
    assert_eq!(rig.core.deadline(), None);
    assert!(rig.core.is_idle());
}

#[test]
fn a_replaced_provider_session_is_cancelled() {
    let mut rig = Rig::new();
    let (first, fake) = rig.staged(1, &["oil"]);
    let _second = rig.open(2, "rite");
    assert_eq!(result(&first), Outcome::Replaced);
    assert_eq!(fake.take(), [r#"{"type":"cancel"}"#, "eof"]);
}

#[test]
fn the_first_stage_is_due_within_the_patience() {
    let mut rig = Rig::new();
    let wire = rig.open(1, "rite");
    wire.take();
    rig.say(1, FromProvider::Hello { protocol: 1 });
    let due = rig.core.deadline().unwrap();
    rig.core.tick(due - Duration::from_millis(1));
    assert!(wire.take().is_empty());
    rig.core.tick(due);
    assert!(error(&wire).contains("showed nothing within 5 s"));
}

#[test]
fn a_toast_shows_for_its_span_and_after_close_the_window_lingers_on_it() {
    let mut rig = Rig::new();
    let (wire, _) = rig.staged(1, &["oil"]);
    rig.seen.take();
    let toast = FromProvider::Toast {
        text: "the rite is begun".into(),
        level: Level::Success,
    };
    rig.say(1, toast.clone());
    assert_eq!(rig.seen.take(), ["toast 1 the rite is begun"]);
    let over = rig.core.deadline().unwrap();
    rig.core.tick(over);
    assert_eq!(rig.seen.take(), ["untoast 1"]);
    rig.say(1, toast);
    rig.say(1, FromProvider::Close);
    assert_eq!(
        result(&wire),
        Outcome::Closed,
        "the caller is answered at once"
    );
    assert_eq!(rig.seen.take(), ["toast 1 the rite is begun", "linger 1"]);
    rig.core.tick(Instant::now() + TOAST_SPAN);
    assert_eq!(rig.seen.take(), ["hide 1"]);
}

#[test]
fn a_provider_that_departs_or_breaks_the_protocol_fails_its_session() {
    let mut rig = Rig::new();
    let (wire, _) = rig.staged(1, &["oil"]);
    rig.core.heard(1, Heard::Exited("exit status: 3".into()));
    let message = error(&wire);
    assert!(message.contains("exit status: 3"), "{message}");

    let (wire, fake) = rig.staged(2, &["oil"]);
    rig.core
        .heard(2, Heard::Line(Err("not a JSON line".into())));
    assert!(error(&wire).contains("not a JSON line"));
    assert_eq!(fake.take(), [r#"{"type":"cancel"}"#, "eof"]);
}

#[test]
fn updates_reach_the_window_and_toasts_before_it_are_only_logged() {
    let mut rig = Rig::new();
    let _wire = rig.open(1, "rite");
    rig.say(1, FromProvider::Hello { protocol: 1 });
    let early = FromProvider::Toast {
        text: "early".into(),
        level: Level::Info,
    };
    rig.say(1, early);
    assert!(rig.seen.take().is_empty());
    let stage = Stage {
        id: "s".into(),
        items: vec![Item {
            pending: true,
            ..item("oil")
        }],
        ..Stage::default()
    };
    rig.say(1, FromProvider::Stage(stage));
    rig.say(
        1,
        FromProvider::Items {
            items: vec![item("oil")],
        },
    );
    assert_eq!(rig.seen.take(), ["show 1 Provider [OIL]", "update 1 0"]);
}

#[test]
fn dmenu_answers_end_the_session_with_the_chosen_line() {
    let mut rig = Rig::new();
    let wire = Wire::default();
    rig.core.connect(1, Box::new(wire.clone()));
    let hello = Request::Hello {
        version: crate::protocol::VERSION,
    };
    rig.core.receive(1, Ok(hello));
    let open = Request::Open {
        mode: Mode::Dmenu,
        options: Options::default(),
        provider: None,
    };
    rig.core.receive(1, Ok(open));
    let lines = vec![
        Line_::from_raw(b"a".to_vec()),
        Line_::from_raw(b"\xffb".to_vec()),
    ];
    rig.core.receive(1, Ok(Request::Items { items: lines }));
    wire.take();
    rig.core.answered(1, Answer::Item(1));
    assert_eq!(
        result(&wire),
        Outcome::Selected {
            index: 1,
            item: Line_::from_raw(b"\xffb".to_vec())
        }
    );
    let _ = Notice {
        text: String::new(),
        level: Level::Info,
    };
    let _: Option<Event> = None;
}
