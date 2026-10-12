//! Themes end to end (spec §4.9): a daemon in the test compositor (see
//! tests/support/e2e/mod.rs) wears the theme its config names, wears an
//! edited theme at once on a visible window, keeps the last sound theme
//! when an edit is broken and says so over the list, and keeps a window
//! drawing only while the theme's continuous effects move (reduce-motion
//! stills them, and then an idle window draws nothing). Runs only with
//! `LOGIS_E2E=niri|sway`; otherwise each test prints `SKIPPED`.
//!
//! The tests take turns, like tests/motion.rs: they count frames, and a
//! nested niri hidden behind another test's gets no frames.

mod support;

use std::path::PathBuf;
use std::sync::{Mutex, MutexGuard};
use std::time::Duration;

use logis::window::trace::monotonic_ns;
use support::e2e::cogitator::Cogitator;

const ITEMS: &[u8] = b"alpha\nbeta\ngamma\n";

fn turn() -> MutexGuard<'static, ()> {
    static TURN: Mutex<()> = Mutex::new(());
    TURN.lock().unwrap_or_else(|e| e.into_inner())
}

fn themes_dir(c: &Cogitator) -> PathBuf {
    let dir = c.compositor.config_home().join("logis/themes");
    std::fs::create_dir_all(&dir).unwrap();
    dir
}

/// Commits from now on for `span`.
fn commits_for(c: &mut Cogitator, span: Duration) -> usize {
    let since = monotonic_ns();
    std::thread::sleep(span);
    c.daemon.stderr();
    c.daemon
        .events()
        .iter()
        .filter(|e| e.ns >= since && e.what == "commit")
        .count()
}

/// Context switches of every thread of `pid`, summed.
fn switches(pid: u32) -> u64 {
    let tasks = std::fs::read_dir(format!("/proc/{pid}/task")).unwrap();
    tasks
        .flatten()
        .map(|task| {
            let status = std::fs::read_to_string(task.path().join("status")).unwrap_or_default();
            status
                .lines()
                .filter_map(|l| {
                    l.strip_prefix("voluntary_ctxt_switches:")
                        .or_else(|| l.strip_prefix("nonvoluntary_ctxt_switches:"))
                })
                .filter_map(|v| v.trim().parse::<u64>().ok())
                .sum::<u64>()
        })
        .sum()
}

/// CPU time of `pid` so far, in clock ticks (utime + stime).
fn cpu_ticks(pid: u32) -> u64 {
    let stat = std::fs::read_to_string(format!("/proc/{pid}/stat")).unwrap();
    // The fields after the command's closing parenthesis; utime and stime
    // are the 14th and 15th of the whole line.
    let rest = &stat[stat.rfind(')').unwrap() + 2..];
    let fields: Vec<&str> = rest.split_whitespace().collect();
    fields[11].parse::<u64>().unwrap() + fields[12].parse::<u64>().unwrap()
}

/// Wake-ups per second and CPU (percent of one core, at 100 ticks a
/// second) of the daemon over `span`.
fn wakeups(c: &Cogitator, span: Duration) -> (f64, f64) {
    let pid = c.daemon.pid();
    let (before, ticks) = (switches(pid), cpu_ticks(pid));
    std::thread::sleep(span);
    let seconds = span.as_secs_f64();
    let cpu = (cpu_ticks(pid) - ticks) as f64 / seconds;
    ((switches(pid) - before) as f64 / seconds, cpu)
}

#[test]
fn an_edited_theme_reaches_the_visible_window_and_a_broken_one_is_reported() {
    let _turn = turn();
    let config = r#"{ "theme": "mine" }"#;
    let Some(mut c) = Cogitator::start_with(&[], Some(config)) else {
        return;
    };
    let dir = themes_dir(&c);
    std::fs::write(
        dir.join("mine.jsonc"),
        r#"{ "extends": "cogitator-phosphor" }"#,
    )
    .unwrap();
    c.daemon
        .wait_for("the theme the config names", |e| e.what == "theme mine");
    let client = c.dmenu_all(&["--lines=3"], ITEMS);
    c.shown(3);
    c.daemon
        .wait_for("the open settles", |e| e.what == "settled");

    // An edit: worn at once, and the visible window draws again.
    std::fs::write(
        dir.join("mine.jsonc"),
        r##"{ "extends": "cogitator-phosphor", "palette": { "phosphor": "#ffb000" } }"##,
    )
    .unwrap();
    c.daemon
        .wait_for("the edited theme", |e| e.what == "theme mine");
    c.daemon.wait_for_commit();

    // A broken edit: the last sound theme stays, a notice says where.
    let broken = monotonic_ns();
    std::fs::write(
        dir.join("mine.jsonc"),
        "{\n  \"glow\": { \"radius\": \"wide\" }\n}",
    )
    .unwrap();
    let toast = c
        .daemon
        .wait_for("the theme's notice", |e| e.what.starts_with("toast "));
    assert!(
        toast.what.contains("the theme is heretical"),
        "{}",
        toast.what
    );
    assert!(toast.what.contains("mine.jsonc:2:"), "{}", toast.what);
    c.daemon.wait_for_commit();
    c.daemon
        .wait_for("the notice goes", |e| e.what == "untoast");
    let worn: Vec<_> = c
        .daemon
        .events()
        .iter()
        .filter(|e| e.ns >= broken && e.what.starts_with("theme "))
        .map(|e| e.what.clone())
        .collect();
    assert!(
        worn.is_empty(),
        "a broken theme changed what is worn: {worn:?}"
    );
    drop(client);
}

#[test]
fn continuous_effects_keep_a_window_drawing_until_motion_is_reduced() {
    let _turn = turn();
    let config = r#"{ "reduce-motion": false }"#;
    let Some(mut c) = Cogitator::start_with(&[], Some(config)) else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    c.shown(3);
    std::thread::sleep(Duration::from_millis(300));
    let span = Duration::from_secs(1);
    let moving = commits_for(&mut c, span);
    // Any steady drawing will do: a software renderer on a CI runner is slow.
    assert!(
        moving >= 5,
        "only {moving} frames in a second of pulse and highlight"
    );
    let (busy, busy_cpu) = wakeups(&c, Duration::from_secs(2));

    let file = c.compositor.config_home().join("logis/logis.jsonc");
    std::fs::write(file, r#"{ "reduce-motion": true }"#).unwrap();
    c.daemon.wait_for("the stilled theme", |e| {
        e.what == "theme forge-crimson-brass"
    });
    c.daemon
        .wait_for("the window settles", |e| e.what == "settled");
    std::thread::sleep(Duration::from_millis(200));
    let still = commits_for(&mut c, span);
    assert_eq!(still, 0, "a stilled window kept drawing");
    let (idle, idle_cpu) = wakeups(&c, Duration::from_secs(2));
    eprintln!(
        "REPORT idle visible window: {moving} frames/s, {busy:.0} daemon wake-ups/s, \
         {busy_cpu:.1}% CPU with continuous effects; {still} frames/s, {idle:.1} wake-ups/s, \
         {idle_cpu:.1}% CPU with reduce-motion"
    );
    drop(client);
}
