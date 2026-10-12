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
//!   frame of its window (≤ 15 ms). The daemon moves as for users: that
//!   first commit is the first frame of the open transition (the panel
//!   already shown, at the transition's start pose), not its end. The end
//!   is a series of its own, "open settled": exec to the frame after which
//!   nothing moves (budget: the warm open, the 150 ms transition and two
//!   frames).
//! - **apps warm open / open settled:** the same for `logis --apps`, the
//!   index resident (the system's own applications and icons; nothing is
//!   launched).
//! - **dmenu warm open, continuous effects:** the same first commit with the
//!   default theme's glow pulse, travelling highlight and flicker moving
//!   (the harness otherwise runs with `reduce-motion`, so that windows
//!   settle); such a window never settles, so there is no settled series.
//! - **dmenu keystroke → commit:** typing into the query and deleting it
//!   again, each key to the frame showing the new matches; with three
//!   items, and with 100 000 (every keystroke re-filters them all, or the
//!   last matches when the query grows): plain lines, and lines of three
//!   tab-separated columns, whose rows coming into view are measured.
//!
//! - **no round trip before a warm open is shown:** counted in the daemon's
//!   `WAYLAND_DEBUG` log (always checked).
//!
//! Runs only with `LOGIS_E2E=niri|sway`, one measurement at a time
//! ([`latency::alone`]): a nested niri is measured only as the newest
//! window of the session, the one the session draws. `LOGIS_LATENCY_RUNS`
//! (default 10) sets the samples, `LOGIS_LATENCY_REPORT=<file>` appends
//! JSON lines, and `LOGIS_LATENCY_BUDGETS=enforce` fails on a median over
//! budget and on any warm open over [`latency::SLOW_OPEN_MS`] (for the
//! owner's machine; CI records the trend only). A slow open names whose it
//! was: a watchdog on the compositor tells whether the compositor itself
//! stopped answering meanwhile. Use `--release` for absolute numbers.

mod support;

use std::time::Duration;

use logis::window::trace::monotonic_ns;
use support::e2e::client::{Focused, Traced, parse};
use support::e2e::cogitator::Cogitator;
use support::e2e::keyboard::{code, us_key};
use support::e2e::watchdog::Watchdog;
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
fn round_trips_are_the_sync_requests_of_a_wayland_debug_log() {
    let log = [
        "[ 1.0] {mesa formats query} \u{1b}[31m\u{1b}[0m -> \u{1b}[34mwl_display\u{1b}[35m#1\u{1b}[36m.sync\u{1b}[0m(new id wl_callback#29)",
        "[ 1.1] -> wl_display#1.get_registry(new id wl_registry#28)",
        "[ 1.2] wl_callback#29.done(1234)",
        "[ 1.3]  -> wl_display#1.sync(new id wl_callback#30)",
        "logis-trace: 99 show 2",
    ];
    assert_eq!(latency::round_trips(log), 2);
}

#[test]
fn a_slow_open_names_the_compositor_only_when_its_stall_made_it_slow() {
    let theirs = latency::slow_open(1902.9, Some(1996.9));
    assert!(theirs.contains("1902.9 ms"), "{theirs}");
    assert!(theirs.contains("answered nobody for 1996.9 ms"), "{theirs}");
    // Without the stall, this open would still have been slow.
    let busy = latency::slow_open(666.7, Some(70.0));
    assert!(busy.contains("stalled only 70.0 ms"), "{busy}");
    assert!(
        busy.contains("logis, or the machine under load, was slow"),
        "{busy}"
    );
    let ours = latency::slow_open(120.0, None);
    assert!(ours.contains("120.0 ms"), "{ours}");
    assert!(ours.contains("answered throughout"), "{ours}");
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
    let _alone = latency::alone();
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

/// exec of `logis --dmenu` → the daemon's first commit for its window, and
/// → the frame that settles its open transition, `runs` times after one
/// warm-up session; each session is cancelled (its client killed) and gone
/// (its close transition over, `faded`) 100 ms before the next starts, as
/// a warm open finds nothing on screen. A closing window's frames (`commit
/// closing`) are not the new window's.
fn dmenu_warm_open(c: &mut Cogitator, runs: usize) -> WarmOpens {
    warm_open(c, runs, true, |c| c.dmenu_all(&[], b"alpha\nbeta\ngamma\n"))
}

/// The samples of [`warm_open`].
struct WarmOpens {
    /// exec → first commit, ms.
    first: Vec<f64>,
    /// exec → open settled, ms (none for a window that never settles).
    settled: Vec<f64>,
    /// What to say of each open over [`latency::SLOW_OPEN_MS`].
    slow: Vec<String>,
}

impl WarmOpens {
    /// With the budgets enforced, no single open may be slow.
    fn enforce_none_slow(&self) {
        if latency::enforce() {
            assert!(self.slow.is_empty(), "{}", self.slow.join("\n"));
        }
    }
}

/// Warm opens of the client `open` starts: exec → first commit, and exec →
/// the open settled (if `settles`; a window whose theme moves on its own
/// never does); the first (warming) run is not counted. A [`Watchdog`] on
/// the compositor tells whose a slow open was.
fn warm_open(
    c: &mut Cogitator,
    runs: usize,
    settles: bool,
    open: impl Fn(&Cogitator) -> support::e2e::cogitator::Client,
) -> WarmOpens {
    let watchdog = Watchdog::start(&c.compositor);
    let mut first = Vec::with_capacity(runs);
    let mut settled = Vec::with_capacity(runs);
    let mut slow = Vec::new();
    for run in 0..=runs {
        std::thread::sleep(Duration::from_millis(100));
        let t0 = monotonic_ns();
        let client = open(c);
        c.daemon.wait_for("show", |e| e.what.starts_with("show "));
        let commit = c
            .daemon
            .wait_for("a first frame", |e| e.what == "commit" && e.ns >= t0);
        if run > 0 {
            let ms = latency::ms_between(t0, commit.ns);
            if ms > latency::SLOW_OPEN_MS {
                // What the window did meanwhile: the trace of a slow open.
                let trace: Vec<String> = c
                    .daemon
                    .events()
                    .iter()
                    .filter(|e| e.ns >= t0 && e.ns <= commit.ns)
                    .map(|e| format!("+{:.1} {}", latency::ms_between(t0, e.ns), e.what))
                    .collect();
                let stalled = watchdog.stalled_within(t0, commit.ns);
                let verdict = latency::slow_open(ms, stalled);
                eprintln!("SLOW OPEN {ms:.1} ms: {trace:?}\n  {verdict}");
                slow.push(verdict);
            }
            first.push(ms);
        }
        if settles {
            let rest = c
                .daemon
                .wait_for("the open settles", |e| e.what == "settled");
            if run > 0 {
                settled.push(latency::ms_between(t0, rest.ns));
            }
        }
        drop(client);
        c.daemon.wait_for("hide", |e| e.what.starts_with("hide "));
        // Gone, not only hidden: its close transition has run (`faded`).
        c.daemon
            .wait_seen("the window gone", |e| e.what == "faded" && e.ns >= t0);
    }
    WarmOpens {
        first,
        settled,
        slow,
    }
}

/// A warm open shows its session without a single round trip to the
/// compositor: nothing the window needs to show it waits for an answer.
/// Each round trip waits out whatever the compositor is doing, and a nested
/// niri whose window the session does not draw answers nobody for up to two
/// seconds (ticket d49bceee: the surface's capabilities, the same for every
/// window, were asked again at every open, 30 round trips). Counted in the
/// daemon's own `WAYLAND_DEBUG` log, from the last window gone (`faded`)
/// to the next `show`; the first window of the daemon may ask.
#[test]
fn a_warm_open_waits_on_no_round_trip_before_it_is_shown() {
    let _alone = latency::alone();
    let Some(mut c) = Cogitator::start_with(&[("WAYLAND_DEBUG", "client")], None) else {
        return;
    };
    for run in 0..3 {
        let client = c.dmenu_all(&[], b"alpha\n");
        let show = c.daemon.wait_for("show", |e| e.what.starts_with("show "));
        c.daemon
            .wait_for("a first frame", |e| e.what == "commit" && e.ns >= show.ns);
        if run > 0 {
            let log = c.daemon.stderr();
            let lines: Vec<&str> = log.lines().collect();
            let is = |l: &str, what: &str| parse(l).is_some_and(|e| e.what == what);
            let shown = lines.iter().position(|l| is(l, &show.what)).unwrap();
            let gone = lines[..shown]
                .iter()
                .rposition(|l| is(l, "faded"))
                .expect("the last window was gone before the next was shown");
            let n = latency::round_trips(lines[gone..shown].iter().copied());
            assert_eq!(
                n, 0,
                "warm open {run} waited on {n} round trips before it was shown"
            );
        }
        drop(client);
        c.daemon.wait_for("hide", |e| e.what.starts_with("hide "));
        c.daemon
            .wait_seen("the window gone", |e| e.what == "faded" && e.ns >= show.ns);
    }
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

/// 100 000 distinct lines, path-like, as a big dmenu input might be; with
/// `columns`, each a path, a word and a number, separated by tabs, so every
/// keystroke brings rows whose columns are measured into view.
fn many_lines(columns: bool) -> Vec<u8> {
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
        let path = format!(
            "/{}/{}/{}-{i}",
            words[i % 7],
            words[(i / 7) % 7],
            words[(i / 49) % 7]
        );
        let line = if columns {
            format!("{path}\t{}\t{}\n", words[(i / 343) % 7], i * 7919 % 100_003)
        } else {
            format!("{path}\n")
        };
        out.extend_from_slice(line.as_bytes());
    }
    out
}

#[test]
fn dmenu_latency() {
    let _alone = latency::alone();
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let runs = latency::runs();
    let frame_ms = 1000.0 / c.harness.refresh_hz();
    let warm = dmenu_warm_open(&mut c, runs);

    let letter = |ch| us_key(ch).expect("on the US layout").0;
    let client = c.dmenu_all(&[], b"alpha\nbeta\ngamma\n");
    let focused = c.shown(3);
    let few = dmenu_keystrokes(&mut c, &focused, &[letter('a'), code::BACKSPACE], runs);
    drop(client);
    c.daemon.wait_for("hide", |e| e.what.starts_with("hide "));

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
    let mut many_keystrokes = |columns| {
        let mut client = c.dmenu(&[], b"");
        let mut stdin = client.stdin.take().unwrap();
        let feeder = std::thread::spawn(move || {
            use std::io::Write;
            stdin.write_all(&many_lines(columns)).unwrap();
        });
        let focused = c.shown(100_000);
        feeder.join().unwrap();
        let times = dmenu_keystrokes(&mut c, &focused, &keys, runs.max(keys.len()));
        drop(client);
        c.daemon.wait_for("hide", |e| e.what.starts_with("hide "));
        times
    };
    let many = many_keystrokes(false);
    let columned = many_keystrokes(true);

    let series = [
        Series::new(
            "dmenu warm open: start → first commit",
            latency::WARM_OPEN_MS,
            warm.first.clone(),
        ),
        Series::new(
            "dmenu warm open: start → open settled",
            latency::WARM_OPEN_MS + 150.0 + 2.0 * frame_ms,
            warm.settled.clone(),
        ),
        Series::new("dmenu keystroke → commit, 3 items", frame_ms, few),
        Series::new("dmenu keystroke → commit, 100 000 items", frame_ms, many),
        Series::new(
            "dmenu keystroke → commit, 100 000 items in columns",
            frame_ms,
            columned,
        ),
    ];
    latency::record(c.compositor.kind().name(), &series);
    warm.enforce_none_slow();
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

/// The app launcher's warm open (story 5, ≤ 15 ms): with the daemon running
/// and its index resident (the applications of the system this runs on,
/// and their icons, rasterised by a first session), `logis --apps` from
/// exec to the first committed frame. Each session is dismissed by its
/// client leaving: nothing is ever launched.
#[test]
fn apps_latency() {
    let _alone = latency::alone();
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let runs = latency::runs();
    let frame_ms = 1000.0 / c.harness.refresh_hz();
    // The index is read on a thread of its own at start; let it finish.
    std::thread::sleep(Duration::from_millis(500));
    let warm = warm_open(&mut c, runs, true, |c| c.apps(&[]));
    let series = [
        Series::new(
            "apps warm open: start → first commit",
            latency::WARM_OPEN_MS,
            warm.first.clone(),
        ),
        Series::new(
            "apps warm open: start → open settled",
            latency::WARM_OPEN_MS + 150.0 + 2.0 * frame_ms,
            warm.settled.clone(),
        ),
    ];
    latency::record(c.compositor.kind().name(), &series);
    warm.enforce_none_slow();
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

/// The warm open with the default theme moving on its own: its first
/// frame costs what a still one does, and the window never settles.
#[test]
fn dmenu_latency_with_continuous_effects() {
    let _alone = latency::alone();
    let config = r#"{ "reduce-motion": false }"#;
    let Some(mut c) = Cogitator::start_with(&[], Some(config)) else {
        return;
    };
    let runs = latency::runs();
    let items = b"alpha\nbeta\ngamma\n";
    let warm = warm_open(&mut c, runs, false, |c| c.dmenu_all(&[], items));
    let series = [Series::new(
        "dmenu warm open, continuous effects: start → first commit",
        latency::WARM_OPEN_MS,
        warm.first.clone(),
    )];
    latency::record(c.compositor.kind().name(), &series);
    warm.enforce_none_slow();
    if latency::enforce() {
        let s = &series[0];
        assert!(
            s.within_budget(),
            "{}: median {:.2} ms over its {:.2} ms budget",
            s.name,
            s.median(),
            s.budget_ms
        );
    }
}
