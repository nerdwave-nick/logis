//! Motion and blur end to end (spec §4.2, §4.3): a daemon in the test
//! compositor (see tests/support/e2e/mod.rs) opens, moves and closes its
//! window paced by frame callbacks, draws nothing once settled, answers a
//! dmenu caller before its close transition ends, shows a waiting session
//! without waiting for it, and blurs behind the body where the compositor
//! offers ext-background-effect. Runs only with `LOGIS_E2E=niri|sway`;
//! otherwise each test prints `SKIPPED`.
//!
//! `LOGIS_MOTION=4` slows every transition fourfold where a test must see
//! one in progress, so that machine load cannot hide it. The tests take
//! turns ([`turn`]): they count frames, and a nested niri whose window the
//! session hides behind another test's gets no frames from its parent.

mod support;

use std::sync::{Mutex, MutexGuard};
use std::time::{Duration, Instant};

use logis::window::trace::monotonic_ns;
use support::e2e::Kind;
use support::e2e::client::Event;
use support::e2e::cogitator::{Cogitator, Ended};
use support::e2e::keyboard::code;

const ITEMS: &[u8] = b"alpha\nbeta\ngamma\n";
/// The default close transition, fourfold.
const SLOW_CLOSE_MS: u64 = 4 * 110;

/// One test at a time: see the module docs.
fn turn() -> MutexGuard<'static, ()> {
    static TURN: Mutex<()> = Mutex::new(());
    TURN.lock().unwrap_or_else(|e| e.into_inner())
}

fn commits_between(events: &[Event], from: u64, to: u64) -> usize {
    events
        .iter()
        .filter(|e| e.what == "commit" && e.ns >= from && e.ns <= to)
        .count()
}

/// Waits `span` and returns the trace events that arrived meanwhile.
fn quiet_for(c: &mut Cogitator, span: Duration) -> Vec<Event> {
    let since = monotonic_ns();
    std::thread::sleep(span);
    c.daemon.stderr();
    c.daemon
        .events()
        .iter()
        .filter(|e| e.ns >= since)
        .cloned()
        .collect()
}

#[test]
fn the_window_opens_over_several_frames_then_draws_nothing() {
    let _turn = turn();
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    c.shown(3);
    let first = c.daemon.wait_for_first_commit();
    let settled = c
        .daemon
        .wait_for("the open settles", |e| e.what == "settled");
    // A still window settles with its first frame. How many frames the
    // open gets depends on the parent session pacing the nested niri.
    let frames = commits_between(c.daemon.events(), first.ns, settled.ns);
    assert!(frames >= 2, "the open transition drew {frames} frames");
    let opened_ms = (settled.ns - first.ns) as f64 / 1e6;
    // The first frame already shows the open one frame in; a slow first
    // frame (the swapchain is configured) may take some more of it.
    assert!(
        (75.0..1000.0).contains(&opened_ms),
        "the 150 ms open took {opened_ms:.1} ms from the first commit"
    );
    let after = quiet_for(&mut c, Duration::from_millis(400));
    assert!(
        after.iter().all(|e| e.what != "commit"),
        "an idle window drew: {after:?}"
    );
    drop(client);
}

#[test]
fn moving_the_selection_glides_over_frames_and_settles() {
    let _turn = turn();
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    let focused = c.shown(3);
    c.daemon
        .wait_for("the open settles", |e| e.what == "settled");
    c.tap(&focused, code::DOWN);
    let select = c.daemon.wait_for("select", |e| e.what == "select 1");
    let settled = c
        .daemon
        .wait_for("the glide settles", |e| e.what == "settled");
    let frames = commits_between(c.daemon.events(), select.ns, settled.ns);
    assert!(frames >= 2, "the band moved in {frames} frames");
    let after = quiet_for(&mut c, Duration::from_millis(300));
    assert!(after.iter().all(|e| e.what != "commit"), "{after:?}");
    drop(client);
}

#[test]
fn the_answer_is_sent_at_accept_while_the_window_still_fades() {
    let _turn = turn();
    let Some(mut c) = Cogitator::start_with(&[("LOGIS_MOTION", "4")], None) else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    let focused = c.shown(3);
    c.tap(&focused, code::ENTER);
    let accept = c.daemon.wait_for("accept", |e| e.what == "accept 0");
    let ended = client.ended();
    let answered = monotonic_ns();
    assert_eq!(
        ended,
        Ended {
            code: Some(0),
            stdout: b"alpha\n".to_vec()
        }
    );
    let faded = c.daemon.wait_for("the close to end", |e| e.what == "faded");
    let fading = c
        .daemon
        .events()
        .iter()
        .filter(|e| e.what == "commit closing")
        .count();
    assert!(fading >= 1, "the close drew {fading} frames");
    assert!(
        answered < faded.ns,
        "the caller had its answer {:.1} ms after accept, the window faded at {:.1} ms",
        (answered - accept.ns) as f64 / 1e6,
        (faded.ns - accept.ns) as f64 / 1e6,
    );
    assert!(
        faded.ns - accept.ns >= (SLOW_CLOSE_MS - 40) * 1_000_000,
        "the close ran its course"
    );
    let after = quiet_for(&mut c, Duration::from_millis(300));
    assert!(
        after.iter().all(|e| !e.what.starts_with("commit")),
        "{after:?}"
    );
}

#[test]
fn a_waiting_session_is_shown_without_waiting_for_the_close() {
    let _turn = turn();
    let Some(mut c) = Cogitator::start_with(&[("LOGIS_MOTION", "4")], None) else {
        return;
    };
    let first = c.dmenu_all(&[], b"one\ntwo\n");
    let focused = c.shown(2);
    let second = c.dmenu_all(&[], b"three\n");
    c.tap(&focused, code::ENTER);
    let accept = c.daemon.wait_for("accept", |e| e.what == "accept 0");
    let started = Instant::now();
    let focused = c.shown(1);
    let shown = c.daemon.wait_seen("the second show", |e| {
        e.what.starts_with("show ") && e.ns > accept.ns
    });
    assert!(
        shown.ns - accept.ns < SLOW_CLOSE_MS * 1_000_000 / 2,
        "the next session waited {:.1} ms for a {SLOW_CLOSE_MS} ms close",
        (shown.ns - accept.ns) as f64 / 1e6
    );
    assert!(started.elapsed() < Duration::from_secs(5));
    assert!(
        !c.daemon
            .events()
            .iter()
            .any(|e| e.what == "faded" && e.ns < shown.ns),
        "the closing window was cut short, not waited for"
    );
    assert_eq!(first.ended().stdout, b"one\n");
    c.tap(&focused, code::ENTER);
    assert_eq!(second.ended().stdout, b"three\n");
}

#[test]
fn blur_follows_the_body_where_the_compositor_offers_it() {
    let _turn = turn();
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    c.shown(3);
    c.daemon
        .wait_for("the open settles", |e| e.what == "settled");
    let blurs: Vec<String> = c
        .daemon
        .events()
        .iter()
        .filter(|e| e.what.starts_with("blur "))
        .map(|e| e.what.clone())
        .collect();
    match c.compositor.kind() {
        // niri offers ext_background_effect_manager_v1: the body's rounded
        // rectangle, stepped at its corners (14 rows each, and the middle).
        Kind::NestedNiri => {
            assert!(!blurs.is_empty(), "no blur region was set");
            assert!(blurs.iter().all(|b| b == "blur 29"), "{blurs:?}");
        }
        // sway 1.9 does not: no blur, and no error either.
        Kind::HeadlessSway => assert!(blurs.is_empty(), "{blurs:?}"),
    }
    drop(client);
}

#[test]
fn blur_off_in_the_config_sets_no_region() {
    let _turn = turn();
    let Some(mut c) = Cogitator::start_with(&[], Some(r#"{ "blur": false }"#)) else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    c.shown(3);
    c.daemon
        .wait_for("the open settles", |e| e.what == "settled");
    assert!(
        !c.daemon
            .events()
            .iter()
            .any(|e| e.what.starts_with("blur ")),
        "blur was set although the config forbids it"
    );
    drop(client);
}

#[test]
fn an_idle_window_wakes_no_thread_of_the_daemon() {
    let _turn = turn();
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    c.shown(3);
    c.daemon
        .wait_for("the open settles", |e| e.what == "settled");
    // Let the last frame's callback and presentation feedback come back.
    std::thread::sleep(Duration::from_millis(200));
    let pid = c.daemon.pid();
    let before = switches(pid);
    std::thread::sleep(Duration::from_secs(1));
    let after = switches(pid);
    assert_eq!(
        before, after,
        "context switches per thread moved while idle"
    );
    drop(client);
}

/// Voluntary and involuntary context switches of every thread of `pid`.
fn switches(pid: u32) -> Vec<(String, u64, u64)> {
    let mut out = Vec::new();
    let tasks = std::fs::read_dir(format!("/proc/{pid}/task")).unwrap();
    for task in tasks.flatten() {
        let status = std::fs::read_to_string(task.path().join("status")).unwrap_or_default();
        let field = |name: &str| {
            status
                .lines()
                .find_map(|l| l.strip_prefix(name))
                .and_then(|v| v.trim().parse().ok())
                .unwrap_or(0)
        };
        let name = status
            .lines()
            .find_map(|l| l.strip_prefix("Name:"))
            .unwrap_or("")
            .trim()
            .to_owned();
        out.push((
            name,
            field("voluntary_ctxt_switches:"),
            field("nonvoluntary_ctxt_switches:"),
        ));
    }
    out.sort();
    out
}
