//! Latency (spec §4.10, §5), measured the research/bench way: the harness
//! reads CLOCK_MONOTONIC just before it starts the client (`T0_NS` in
//! research/bench/launch.py) and the client stamps its milestones with the
//! same clock (the `LOGIS_TEST_TRACE` lines).
//!
//! - **start → first commit:** `logis daemon --debug-panel` from exec to its
//!   first committed frame (connect, wake the GPU, show the panel). This is
//!   the closest thing to "first open after the client starts the daemon"
//!   (≤ 150 ms) until the daemon serves windows; warm open (≤ 15 ms) needs
//!   a window-backed resolver and uses the same API then.
//! - **keystroke → commit:** an injected Down/Up to the frame that shows
//!   the moved selection; budget one frame at the output's refresh rate.
//!
//! Runs only with `LOGIS_E2E=niri|sway`. `LOGIS_LATENCY_RUNS` (default 10)
//! sets the samples, `LOGIS_LATENCY_REPORT=<file>` appends JSON lines, and
//! `LOGIS_LATENCY_BUDGETS=enforce` fails on a median over budget (for the
//! owner's machine; CI records the trend only). Use `--release` for
//! absolute numbers.

mod support;

use std::time::Duration;

use logis::window::trace::monotonic_ns;
use support::e2e::client::Traced;
use support::e2e::keyboard::code;
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

fn debug_panel(compositor: &TestCompositor) -> Traced {
    let mut command = compositor.logis();
    command.args(["daemon", "--debug-panel"]);
    Traced::spawn(command)
}

/// exec → first committed frame, `runs` times, each panel ended before the
/// next starts.
fn start_to_first_commit(compositor: &TestCompositor, runs: usize) -> Vec<f64> {
    (0..runs)
        .map(|_| {
            let t0 = monotonic_ns();
            let mut panel = debug_panel(compositor);
            let commit = panel.wait_for_first_commit();
            drop(panel);
            std::thread::sleep(Duration::from_millis(100));
            latency::ms_between(t0, commit.ns)
        })
        .collect()
}

/// injected key press → the commit of the frame it caused, `runs` times.
fn keystroke_to_commit(
    compositor: &TestCompositor,
    harness: &mut Harness,
    runs: usize,
) -> Vec<f64> {
    let mut kb = harness.virtual_keyboard();
    let mut panel = debug_panel(compositor);
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
    let series = [
        Series::new(
            "debug panel start → first commit",
            latency::FIRST_OPEN_MS,
            start_to_first_commit(&compositor, runs),
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
