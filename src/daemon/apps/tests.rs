use std::path::Path;
use std::sync::{Arc, Mutex};

use logis::apps::index::{Context, Dirs, Index};
use logis::apps::locale::Locale;
use logis::list::{Entry, Level, Options};

use super::*;
use crate::daemon::core::{Event, Peer};
use crate::protocol::{self, Reply, Request};
use crate::provider::Step;

/// What the resolver was asked, as text.
#[derive(Clone, Default)]
struct Recorder(Arc<Mutex<Vec<String>>>);

impl Recorder {
    fn take(&self) -> Vec<String> {
        std::mem::take(&mut self.0.lock().unwrap())
    }
    fn note(&self, line: String) {
        self.0.lock().unwrap().push(line);
    }
}

fn names(entries: &[Entry]) -> String {
    entries
        .iter()
        .map(|e| e.text.as_str())
        .collect::<Vec<_>>()
        .join(",")
}

impl Resolver for Recorder {
    fn accepts(&self, _: Mode) -> Result<(), String> {
        Ok(())
    }
    fn show(&mut self, s: SessionId, mode: Mode, o: Options, entries: Vec<Entry>) {
        self.note(format!(
            "show {s} {mode:?} '{}' [{}]",
            o.placeholder,
            names(&entries)
        ));
    }
    fn step(&mut self, s: SessionId, step: Step) {
        self.note(match step {
            Step::Stage { options, entries } => {
                format!("stage {s} '{}' [{}]", options.prompt, names(&entries))
            }
            Step::Toast { notice, .. } => format!("toast {s} {:?} {}", notice.level, notice.text),
            Step::Ready => format!("ready {s}"),
            other => format!("step {s} {other:?}"),
        });
    }
    fn end(&mut self, _: SessionId) {}
    fn hide(&mut self, s: SessionId) {
        self.note(format!("hide {s}"));
    }
    fn activation(&mut self, s: SessionId, app_id: &str) {
        self.note(format!("activation {s} {app_id}"));
    }
}

/// What a client was sent.
#[derive(Clone, Default)]
struct Wire(Arc<Mutex<Vec<Reply>>>);

impl Peer for Wire {
    fn send(&mut self, reply: &Reply) {
        self.0.lock().unwrap().push(reply.clone());
    }
    fn close(&mut self) {}
}

fn fixtures() -> std::path::PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/apps")
}

fn index() -> Arc<Index> {
    let cx = Context {
        locale: Locale::default(),
        desktops: vec!["niri".into()],
        path: vec![fixtures().join("bin")],
    };
    Arc::new(Index::scan(
        &Dirs(vec![fixtures().join("system/applications")]),
        &cx,
    ))
}

fn core() -> (Core<Recorder>, Recorder) {
    let recorder = Recorder::default();
    (Core::new(recorder.clone(), 1), recorder)
}

/// Opens app session `conn` with the environment `env`.
fn open(core: &mut Core<Recorder>, conn: SessionId, env: &[&str]) -> Wire {
    let wire = Wire::default();
    core.connect(conn, Box::new(wire.clone()));
    core.receive(
        conn,
        Ok(Request::Hello {
            version: protocol::VERSION,
        }),
    );
    let env = env.iter().map(|e| Bytes(e.as_bytes().to_vec())).collect();
    core.receive(
        conn,
        Ok(Request::Open {
            mode: Mode::Apps,
            options: Options::default(),
            provider: None,
            env,
        }),
    );
    wire
}

fn anoint_row(core: &Core<Recorder>) -> u32 {
    core.apps
        .index
        .apps()
        .iter()
        .position(|a| a.id == "forge-anoint.desktop")
        .unwrap() as u32
}

#[test]
fn a_session_before_the_index_shows_a_placeholder_then_the_apps() {
    let (mut core, log) = core();
    open(&mut core, 1, &[]);
    assert_eq!(
        log.take(),
        ["show 1 Apps 'the census of applications is taken…' []"]
    );
    core.indexed(index());
    let seen = log.take();
    assert_eq!(seen.len(), 1);
    assert!(
        seen[0].starts_with("stage 1 '› ' [Anoint,Broken in home"),
        "{seen:?}"
    );
    open(&mut core, 2, &[]);
    let seen = log.take();
    assert!(
        seen.last()
            .unwrap()
            .starts_with("show 2 Apps 'summon an application' [Anoint,"),
        "{seen:?}"
    );
}

#[test]
fn return_asks_for_a_token_and_waits_for_it() {
    let (mut core, log) = core();
    core.indexed(index());
    open(&mut core, 1, &[]);
    log.take();
    let row = anoint_row(&core);
    core.answered(1, Answer::Item(row));
    assert_eq!(log.take(), ["activation 1 forge-anoint"]);
    core.answered(1, Answer::Item(row));
    assert!(log.take().is_empty(), "one launch at a time");
    assert!(core.deadline().is_some(), "it does not wait for ever");
}

#[test]
fn a_launch_that_fails_is_toasted_and_the_stage_takes_return_again() {
    let (mut core, log) = core();
    core.indexed(index());
    // No PATH: the program is nowhere.
    let wire = open(&mut core, 1, &["HOME=/"]);
    log.take();
    core.answered(1, Answer::Item(anoint_row(&core)));
    log.take();
    core.activated(1, Some("tok".into()));
    let seen = log.take();
    assert!(
        seen[0].starts_with("toast 1 Error Anoint cannot be summoned"),
        "{seen:?}"
    );
    assert_eq!(seen[1], "ready 1");
    assert!(
        !wire
            .0
            .lock()
            .unwrap()
            .iter()
            .any(|r| matches!(r, Reply::Result { .. }))
    );
    core.answered(1, Answer::Item(anoint_row(&core)));
    assert_eq!(
        log.take(),
        ["activation 1 forge-anoint"],
        "Return is taken again"
    );
}

#[test]
fn without_a_token_in_time_the_launch_goes_ahead() {
    let (mut core, log) = core();
    core.indexed(index());
    open(&mut core, 1, &["HOME=/"]);
    core.answered(1, Answer::Item(anoint_row(&core)));
    log.take();
    core.tick(Instant::now() + TOKEN_PATIENCE);
    let seen = log.take();
    assert!(
        seen[0].starts_with("toast 1 Error"),
        "it launched (and failed): {seen:?}"
    );
    let _ = Level::Error;
}

#[test]
fn a_launch_closes_the_session_and_counts_for_frecency() {
    let scratch = std::env::temp_dir().join(format!("logis-daemon-apps-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&scratch);
    std::fs::create_dir_all(&scratch).unwrap();
    let record = scratch.join("record");
    let (mut core, log) = core();
    core.apps.frecency_path = Some(scratch.join("state/frecency.json"));
    core.indexed(index());
    let path = format!("PATH={}:/usr/bin:/bin", fixtures().join("bin").display());
    let record_var = format!("ANOINT_RECORD={}", record.display());
    let wire = open(&mut core, 1, &[&path, &record_var, "HOME=/"]);
    core.answered(1, Answer::Item(anoint_row(&core)));
    core.activated(1, Some("tok".into()));
    assert!(log.take().contains(&"hide 1".to_owned()));
    let replies = wire.0.lock().unwrap().clone();
    assert_eq!(
        replies.last(),
        Some(&Reply::Result {
            outcome: Outcome::Closed
        })
    );
    assert!(
        core.apps
            .frecency
            .boost("forge-anoint.desktop", frecency::now())
            > 0
    );
    let deadline = Instant::now() + Duration::from_secs(5);
    while !(record.exists() && scratch.join("state/frecency.json").exists()) {
        assert!(
            Instant::now() < deadline,
            "the app never ran or frecency was never kept"
        );
        std::thread::sleep(Duration::from_millis(5));
    }
    let _ = Event::Indexed;
    std::fs::remove_dir_all(scratch).unwrap();
}
