//! Latency (spec §4.10, §5), measured the research/bench way: the harness
//! reads CLOCK_MONOTONIC just before it starts the client (`T0_NS` in
//! research/bench/launch.py) and the client stamps its milestones with the
//! same clock (the `LOGIS_TEST_TRACE` lines).
//!
//! - **start → first commit:** `logis daemon --debug-panel` from exec to its
//!   first committed frame (connect, wake the GPU, resolve the fonts, show
//!   the panel). This is the closest thing to "first open after the client
//!   starts the daemon" (≤ 150 ms) until the daemon serves windows; warm
//!   open (≤ 15 ms) needs a window-backed resolver and uses the same API
//!   then. It is measured twice, alternating run by run: with the pinned
//!   test fonts, and with the user's own fontconfig (system fonts: on the
//!   owner's machine, thousands of faces and the CJK and emoji fallbacks).
//! - **keystroke → commit:** an injected Down/Up to the frame that shows
//!   the moved selection; budget one frame at the output's refresh rate.
//! - **dmenu warm open:** with the daemon running (and warmed by one
//!   session), `logis --dmenu` from exec to the daemon's first committed
//!   frame of its window (≤ 15 ms).
//! - **dmenu keystroke → commit:** typing into the query and deleting it
//!   again, each key to the frame showing the new matches; with three
//!   items, and with 100 000 (every keystroke re-filters them all, or the
//!   last matches when the query grows).
//!
//! Runs only with `LOGIS_E2E=niri|sway`. `LOGIS_LATENCY_RUNS` (default 10)
//! sets the samples, `LOGIS_LATENCY_REPORT=<file>` appends JSON lines, and
//! `LOGIS_LATENCY_BUDGETS=enforce` fails on a median over budget (for the
//! owner's machine; CI records the trend only). Use `--release` for
//! absolute numbers.

mod support;

use std::time::Duration;

use logis::window::trace::monotonic_ns;
use support::e2e::client::{Focused, Traced};
use support::e2e::cogitator::Cogitator;
use support::e2e::keyboard::{code, us_key};
use support::e2e::wire::Harness;
use support::e2e::{Kind, TestCompositor};
use support::latency::{self, Series};

// ------------------------------------------------------- statistics

#[test]
fn statistics_use_the_nearest_rank() {
    let s = Series::new("x", 10.0, vec![4.0, 1.0, 3.0, 2.0, 5.0]);
    assert_eq!((s.min(), s.median(), s.max()), (1.0, 3.0, 5.0));
    assert_eq!(s.percentile(90.0), 5.0);
    let even = Series::new("y", 10.0, vec![1.0, 2.0, 3.0, 4.0]);
    assert_eq!(even.median(), 2.5);
}

#[test]
fn milliseconds_between_monotonic_stamps() {
    assert_eq!(latency::ms_between(1_000_000, 8_500_000), 7.5);
}

#[test]
fn the_summary_shows_each_series_against_its_budget() {
    let s = Series::new("start → first commit", 150.0, vec![30.0, 60.0, 45.0]);
    let table = latency::summary("niri-nested", std::slice::from_ref(&s));
    assert!(
        table.contains("| start → first commit | 3 | 45.00 |"),
        "{table}"
    );
    assert!(table.contains("| 150.00 | 30%"), "{table}");
    assert!(s.within_budget());
    let json = latency::json_line("niri-nested", &s);
    let value: serde_json::Value = serde_json::from_str(&json).unwrap();
    assert_eq!(value["metric"], "start → first commit");
    assert_eq!(value["median_ms"], 45.0);
    assert_eq!(value["budget_ms"], 150.0);
    assert_eq!(value["compositor"], "niri-nested");
}

#[test]
fn a_median_over_budget_is_out_of_budget() {
    assert!(!Series::new("x", 10.0, vec![9.0, 11.0, 12.0]).within_budget());
}

// ------------------------------------------------------- measurements

/// The fonts a measured client resolves.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Fonts {
    /// The test compositor's pinned fonts (tests/fonts), the same everywhere.
    Pinned,
    /// The user's own fontconfig, as the daemon sees it in a session.
    System,
}

fn debug_panel(compositor: &TestCompositor, fonts: Fonts) -> Traced {
    let mut command = compositor.logis();
    command.args(["daemon", "--debug-panel"]);
    if fonts == Fonts::System {
        // Back to the harness's own fontconfig: its FONTCONFIG_FILE, if any,
        // else the default configuration (HOME and the caches are inherited).
        // XDG_CONFIG_HOME too: fontconfig reads the user's fonts.conf there.
        for var in ["FONTCONFIG_FILE", "XDG_CONFIG_HOME"] {
            match std::env::var_os(var) {
                Some(value) => command.env(var, value),
                None => command.env_remove(var),
            };
        }
    }
    Traced::spawn(command)
}

/// exec → first committed frame, `runs` times for each of `fonts`,
/// alternating so that every variant sees the same machine load; each panel
/// ends before the next starts. One series of samples per variant.
fn start_to_first_commit(
    compositor: &TestCompositor,
    fonts: &[Fonts],
    runs: usize,
) -> Vec<Vec<f64>> {
    let mut samples = vec![Vec::with_capacity(runs); fonts.len()];
    for _ in 0..runs {
        for (variant, &f) in fonts.iter().enumerate() {
            let t0 = monotonic_ns();
            let mut panel = debug_panel(compositor, f);
            let commit = panel.wait_for_first_commit();
            drop(panel);
            std::thread::sleep(Duration::from_millis(100));
            samples[variant].push(latency::ms_between(t0, commit.ns));
        }
    }
    samples
}

/// injected key press → the commit of the frame it caused, `runs` times.
fn keystroke_to_commit(
    compositor: &TestCompositor,
    harness: &mut Harness,
    runs: usize,
) -> Vec<f64> {
    let mut kb = harness.virtual_keyboard();
    let mut panel = debug_panel(compositor, Fonts::Pinned);
    let focused = panel.wait_for_focus();
    panel.wait_for_first_commit();
    (0..runs)
        .map(|i| {
            // Idle first: the previous frame's callback has come back.
            std::thread::sleep(Duration::from_millis(100));
            let key = if i % 2 == 0 { code::DOWN } else { code::UP };
            let t = monotonic_ns();
            harness.tap(&mut kb, &focused, key);
            let commit = panel.wait_for_commit_after(t);
            latency::ms_between(t, commit.ns)
        })
        .collect()
}

#[test]
fn debug_panel_latency() {
    let Some(kind) = Kind::requested() else {
        return;
    };
    let runs = latency::runs();
    let compositor = TestCompositor::start(kind);
    let mut harness = Harness::connect(&compositor);
    let frame_ms = 1000.0 / harness.refresh_hz();
    let [pinned, system] =
        start_to_first_commit(&compositor, &[Fonts::Pinned, Fonts::System], runs)
            .try_into()
            .unwrap();
    let series = [
        Series::new(
            "debug panel start → first commit, pinned fonts",
            latency::FIRST_OPEN_MS,
            pinned,
        ),
        Series::new(
            "debug panel start → first commit, system fonts",
            latency::FIRST_OPEN_MS,
            system,
        ),
        Series::new(
            "keystroke → commit",
            frame_ms,
            keystroke_to_commit(&compositor, &mut harness, runs),
        ),
    ];
    latency::record(kind.name(), &series);
    if latency::enforce() {
        for s in &series {
            assert!(
                s.within_budget(),
                "{}: median {:.2} ms over its {:.2} ms budget",
                s.name,
                s.median(),
                s.budget_ms
            );
        }
    }
}

// ------------------------------------------------------- dmenu sessions

/// exec of `logis --dmenu` → the daemon's first commit for its window,
/// `runs` times after one warm-up session; each session is cancelled (its
/// client killed) and gone before the next starts.
fn dmenu_warm_open(c: &mut Cogitator, runs: usize) -> Vec<f64> {
    let mut samples = Vec::with_capacity(runs);
    for run in 0..=runs {
        std::thread::sleep(Duration::from_millis(100));
        let t0 = monotonic_ns();
        let client = c.dmenu_all(&[], b"alpha\nbeta\ngamma\n");
        c.daemon.wait_for("show", |e| e.what.starts_with("show "));
        let commit = c
            .daemon
            .wait_for("a first frame", |e| e.what == "commit" && e.ns >= t0);
        if run > 0 {
            samples.push(latency::ms_between(t0, commit.ns));
        }
        drop(client);
        c.daemon.wait_for("hide", |e| e.what.starts_with("hide "));
    }
    samples
}

/// Each key of `keys` (cycled) → the commit of the frame it caused.
fn dmenu_keystrokes(c: &mut Cogitator, focused: &Focused, keys: &[u32], runs: usize) -> Vec<f64> {
    (0..runs)
        .map(|i| {
            // Idle first: the previous frame's callback has come back.
            std::thread::sleep(Duration::from_millis(100));
            let t = monotonic_ns();
            c.harness.tap(&mut c.kb, focused, keys[i % keys.len()]);
            let commit = c
                .daemon
                .wait_for("a frame commit", |e| e.what == "commit" && e.ns >= t);
            latency::ms_between(t, commit.ns)
        })
        .collect()
}

/// 100 000 distinct lines, path-like, as a big dmenu input might be.
fn many_lines() -> Vec<u8> {
    let words = [
        "rite",
        "aspect",
        "cogitator",
        "noosphere",
        "spirit",
        "litany",
        "forge",
    ];
    let mut out = Vec::new();
    for i in 0..100_000usize {
        let line = format!(
            "/{}/{}/{}-{i}\n",
            words[i % 7],
            words[(i / 7) % 7],
            words[(i / 49) % 7]
        );
        out.extend_from_slice(line.as_bytes());
    }
    out
}

#[test]
fn dmenu_latency() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let runs = latency::runs();
    let frame_ms = 1000.0 / c.harness.refresh_hz();
    let open = dmenu_warm_open(&mut c, runs);

    let letter = |ch| us_key(ch).expect("on the US layout").0;
    let client = c.dmenu_all(&[], b"alpha\nbeta\ngamma\n");
    let focused = c.shown(3);
    let few = dmenu_keystrokes(&mut c, &focused, &[letter('a'), code::BACKSPACE], runs);
    drop(client);
    c.daemon.wait_for("hide", |e| e.what.starts_with("hide "));

    let mut client = c.dmenu(&[], b"");
    let mut stdin = client.stdin.take().unwrap();
    let feeder = std::thread::spawn(move || {
        use std::io::Write;
        stdin.write_all(&many_lines()).unwrap();
    });
    let focused = c.shown(100_000);
    feeder.join().unwrap();
    let keys = [
        letter('r'),
        letter('i'),
        letter('t'),
        letter('e'),
        code::BACKSPACE,
        code::BACKSPACE,
        code::BACKSPACE,
        code::BACKSPACE,
    ];
    let many = dmenu_keystrokes(&mut c, &focused, &keys, runs.max(keys.len()));
    drop(client);

    let series = [
        Series::new(
            "dmenu warm open: start → first commit",
            latency::WARM_OPEN_MS,
            open,
        ),
        Series::new("dmenu keystroke → commit, 3 items", frame_ms, few),
        Series::new("dmenu keystroke → commit, 100 000 items", frame_ms, many),
    ];
    latency::record(c.compositor.kind().name(), &series);
    if latency::enforce() {
        for s in &series {
            assert!(
                s.within_budget(),
                "{}: median {:.2} ms over its {:.2} ms budget",
                s.name,
                s.median(),
                s.budget_ms
            );
        }
    }
}
