//! End to end in a real compositor the harness starts itself (nested niri
//! locally, headless sway on CI; see tests/support/e2e/mod.rs). Runs only
//! with `LOGIS_E2E=niri|sway`; otherwise the compositor tests print
//! `SKIPPED`. The isolation guard and the harness's pure parts always run.
//!
//! The client is `logis daemon --debug-panel` (the only window logis shows
//! yet): it must report keyboard focus, receive injected keys with real
//! modifiers and masks, and show the result on screen, compared with the
//! goldens in tests/goldens/e2e/. Its trace reports what each key did
//! (`query <JSON string>`, `select <row>`, `accept <row>`, `dismiss`), so the
//! keyboard model is checked end to end: Compose with a real Shift press,
//! Caps Lock, the Num Lock keypad, key repeat and every shortcut.

mod support;

use logis::render::Image;
use logis::window::DEBUG_SIZE;
use std::time::Duration;
use support::e2e::client::{self, Focused, Traced};

use support::e2e::keyboard::{self, Lock, Modifier, VirtualKeyboard, code};
use support::e2e::screencopy::{self, crop_centred};
use support::e2e::wire::{Harness, verify_peer};
use support::e2e::{Kind, TestCompositor};
use support::golden::{Goldens, Tolerance};
use support::{ScratchDir, fake_compositor};

// ------------------------------------------------------- isolation guard

#[test]
fn the_injector_refuses_a_socket_served_by_any_other_process() {
    let dir = ScratchDir::new();
    // A compositor this process serves: its peer is us, not a compositor
    // the harness spawned, exactly like the live session's socket would be.
    let _compositor = fake_compositor::serve(dir.path(), "wayland-0", &[]);
    let stream = std::os::unix::net::UnixStream::connect(dir.path().join("wayland-0")).unwrap();
    let mut spawned = std::process::Command::new("true").spawn().unwrap();
    let e = verify_peer(&stream, spawned.id()).unwrap_err();
    spawned.wait().unwrap();
    assert!(e.contains("refusing to inject"), "{e}");
    assert!(verify_peer(&stream, std::process::id()).is_ok());
}

#[test]
fn the_compositor_choice_is_niri_sway_or_none() {
    assert_eq!(Kind::parse(None), Ok(None));
    assert_eq!(Kind::parse(Some("")), Ok(None));
    assert_eq!(Kind::parse(Some("niri")), Ok(Some(Kind::NestedNiri)));
    assert_eq!(Kind::parse(Some("sway")), Ok(Some(Kind::HeadlessSway)));
    assert!(Kind::parse(Some("gnome")).is_err());
}

// ------------------------------------------------------- pure helpers

#[test]
fn trace_lines_parse_into_time_and_event() {
    let e = client::parse("logis-trace: 42 key 30 pressed").unwrap();
    assert_eq!((e.ns, e.what.as_str()), (42, "key 30 pressed"));
    assert!(client::parse("logis: the machine spirit wakes").is_none());
}

#[test]
fn the_us_layout_types_shifted_characters_with_shift() {
    assert_eq!(keyboard::us_key('a'), Some((30, false)));
    assert_eq!(keyboard::us_key('A'), Some((30, true)));
    assert_eq!(keyboard::us_key('?'), Some((53, true)));
    assert_eq!(keyboard::us_key(' '), Some((code::SPACE, false)));
    assert_eq!(keyboard::us_key('é'), None);
}

#[test]
fn captures_convert_from_both_byte_orders_and_flip_when_inverted() {
    use smithay_client_toolkit::reexports::client::protocol::wl_shm::Format;
    // Two rows of one pixel, stride 4: red on top, blue below (XRGB: B G R X).
    let xrgb = [0, 0, 255, 0, 255, 0, 0, 0];
    let image = screencopy::to_rgba(&xrgb, Format::Xrgb8888, 1, 2, 4, false);
    assert_eq!(image.pixels, [255, 0, 0, 255, 0, 0, 255, 255]);
    let flipped = screencopy::to_rgba(&xrgb, Format::Xrgb8888, 1, 2, 4, true);
    assert_eq!(flipped.pixels, [0, 0, 255, 255, 255, 0, 0, 255]);
    let xbgr = [255, 0, 0, 0, 0, 0, 255, 0];
    let image = screencopy::to_rgba(&xbgr, Format::Xbgr8888, 1, 2, 4, false);
    assert_eq!(image.pixels, [255, 0, 0, 255, 0, 0, 255, 255]);
}

#[test]
fn output_transforms_turn_captures_upright() {
    use smithay_client_toolkit::reexports::client::protocol::wl_output::Transform;
    // 2x2: a b / c d
    let px = |v: u8| [v, v, v, 255];
    let image = Image {
        width: 2,
        height: 2,
        pixels: [px(1), px(2), px(3), px(4)].concat(),
    };
    let order = |t| {
        let i = screencopy::upright(image.clone(), t);
        i.pixels.chunks(4).map(|p| p[0]).collect::<Vec<_>>()
    };
    assert_eq!(order(Transform::Normal), [1, 2, 3, 4]);
    assert_eq!(order(Transform::Flipped180), [3, 4, 1, 2]);
    assert_eq!(order(Transform::Flipped), [2, 1, 4, 3]);
    assert_eq!(order(Transform::_180), [4, 3, 2, 1]);
}

#[test]
fn a_centred_crop_settles_an_odd_remainder_by_the_reference() {
    // A 5x3 capture with a 2x1 marker at x 2..4, y 1: centring rounds down
    // to x 1, the reference finds x 2.
    let mut capture = Image {
        width: 5,
        height: 3,
        pixels: [0, 0, 0, 255].repeat(15),
    };
    for x in 2..4 {
        let i = (5 + x) * 4;
        capture.pixels[i..i + 4].copy_from_slice(&[255, 255, 255, 255]);
    }
    let reference = Image {
        width: 2,
        height: 1,
        pixels: [255, 255, 255, 255].repeat(2),
    };
    assert_eq!(crop_centred(&capture, (2, 1), Some(&reference)), reference);
    assert_ne!(crop_centred(&capture, (2, 1), None), reference);
}

// ------------------------------------------------------- in a compositor

/// How far a capture may stray from its golden: the compositor composites
/// the translucent panel itself (GLES on niri, pixman on sway), and the
/// client renders on whatever GPU is there.
const CAPTURE: Tolerance = Tolerance {
    channel: 4,
    outliers: 0.001,
};

/// Captures until two consecutive captures agree, so a frame the compositor
/// has not shown yet is never compared.
fn settled_capture(harness: &mut Harness) -> Image {
    let mut last = harness.capture();
    for _ in 0..50 {
        let next = harness.capture();
        if next == last {
            return next;
        }
        last = next;
    }
    panic!("the output never settled");
}

/// The panel cropped from the output, checked against golden `e2e/<name>`.
#[track_caller]
fn check_panel(harness: &mut Harness, name: &str) {
    let goldens = Goldens::checked_in();
    let reference = (!goldens.updating())
        .then(|| goldens.load("e2e", name).ok())
        .flatten();
    let capture = settled_capture(harness);
    for (x, y) in [(0, 0), (capture.width - 1, capture.height - 1)] {
        assert_eq!(
            capture.pixel(x, y),
            [0, 0, 0, 255],
            "the harness's backdrop is opaque black around the panel"
        );
    }
    let panel = crop_centred(&capture, DEBUG_SIZE, reference.as_ref());
    let verdict = goldens.assert("e2e", name, &panel, CAPTURE);
    eprintln!("e2e/{name}: {verdict:?}");
}

#[test]
fn the_debug_panel_takes_focus_receives_keys_and_shows_them() {
    let Some(kind) = Kind::requested() else {
        return;
    };
    let compositor = TestCompositor::start(kind);
    let mut harness = Harness::connect(&compositor);
    let mut kb = harness.virtual_keyboard();
    let mut panel = Traced::spawn({
        let mut c = compositor.logis();
        c.args(["daemon", "--debug-panel"]);
        c
    });

    let focused = panel.wait_for_focus();
    panel.wait_for_first_commit();
    check_panel(&mut harness, "debug-panel");

    // A real Shift press plus its mask around the letter, as a user types.
    harness.type_text(&mut kb, &focused, "A");
    for step in [
        "key 42 pressed",
        "modifiers 1 0 0 0",
        "key 30 pressed",
        "key 30 released",
        "key 42 released",
        "modifiers 0 0 0 0",
    ] {
        panel.wait_for(step, |e| e.what == step);
    }
    harness.hold(&mut kb, &focused, Modifier::Control);
    panel.wait_for("Control's mask", |e| e.what == "modifiers 4 0 0 0");
    harness.release(&mut kb, &focused, Modifier::Control);

    for _ in 0..2 {
        harness.tap(&mut kb, &focused, code::DOWN);
        panel.wait_for_commit();
    }
    check_panel(&mut harness, "debug-panel-row-2");

    harness.tap(&mut kb, &focused, code::ESC);
    let status = panel.wait_exit();
    assert!(status.success(), "Escape closes the panel: {status}");
    drop(compositor);
}

// ------------------------------------------------------- the keyboard model

/// A debug panel in `compositor`, focused and drawn, with a virtual
/// keyboard made before it (a headless seat gains its keyboard from it).
struct Typing {
    harness: Harness,
    kb: VirtualKeyboard,
    panel: Traced,
    focused: Focused,
}

impl Typing {
    fn start(compositor: &TestCompositor) -> Self {
        Self::start_with(compositor, |_| {})
    }

    fn start_with(
        compositor: &TestCompositor,
        adjust: impl FnOnce(&mut std::process::Command),
    ) -> Self {
        let mut harness = Harness::connect(compositor);
        let kb = harness.virtual_keyboard();
        let mut command = compositor.logis();
        command.args(["daemon", "--debug-panel"]);
        adjust(&mut command);
        let mut panel = Traced::spawn(command);
        let focused = panel.wait_for_focus();
        panel.wait_for_first_commit();
        Self {
            harness,
            kb,
            panel,
            focused,
        }
    }

    fn tap(&mut self, code: u32) {
        self.harness.tap(&mut self.kb, &self.focused, code);
    }

    fn type_text(&mut self, text: &str) {
        self.harness.type_text(&mut self.kb, &self.focused, text);
    }

    /// `code` with a real `modifier` press around it.
    fn chord(&mut self, modifier: Modifier, code: u32) {
        self.harness.hold(&mut self.kb, &self.focused, modifier);
        self.tap(code);
        self.harness.release(&mut self.kb, &self.focused, modifier);
    }

    fn toggle(&mut self, lock: Lock) {
        self.harness.toggle(&mut self.kb, &self.focused, lock);
    }

    /// The next report starting with `word ` (the sequence cursor moves
    /// past it), without its word: what the last key did.
    fn next(&mut self, word: &str) -> String {
        let prefix = format!("{word} ");
        let event = self.panel.wait_for(word, |e| e.what.starts_with(&prefix));
        event.what[prefix.len()..].to_owned()
    }

    /// The next query report, unquoted.
    fn query(&mut self) -> String {
        let quoted = self.next("query");
        serde_json::from_str(&quoted).unwrap_or_else(|_| panic!("a quoted query: {quoted}"))
    }

    /// Waits until the panel reported `what` and exited successfully.
    fn ends_with(mut self, what: &str) {
        self.panel.wait_for(what, |e| e.what == what);
        let status = self.panel.wait_exit();
        assert!(status.success(), "{what} closes the panel: {status}");
    }
}

#[test]
fn compose_locks_and_the_keypad_type_what_the_layout_says() {
    let Some(kind) = Kind::requested() else {
        return;
    };
    let compositor = TestCompositor::start(kind);
    let mut t = Typing::start(&compositor);

    // Menu o " with a real Shift press for the quote, as a user types it.
    t.tap(code::COMPOSE);
    t.type_text("o\"");
    for step in [
        "key 127 pressed",
        "key 24 pressed",
        "key 42 pressed",
        "modifiers 1 0 0 0",
        "key 40 pressed",
    ] {
        t.panel.wait_for(step, |e| e.what == step);
    }
    assert_eq!(
        t.query(),
        "ö",
        "Shift inside the sequence did not cancel it"
    );

    // A sequence only the harness's XCOMPOSEFILE knows.
    t.tap(code::COMPOSE);
    t.type_text("lg");
    assert_eq!(t.query(), "öλ");
    check_panel(&mut t.harness, "debug-panel-query");

    // One grapheme of two code points; Backspace deletes it whole.
    t.tap(code::COMPOSE);
    t.type_text("ee");
    assert_eq!(t.query(), "öλe\u{301}");
    t.tap(code::BACKSPACE);
    assert_eq!(t.query(), "öλ");

    // Caps Lock: a letter typed without Shift is upper case, until it is off.
    t.toggle(Lock::Caps);
    t.type_text("a");
    assert_eq!(t.query(), "öλA");
    t.toggle(Lock::Caps);
    t.type_text("a");
    assert_eq!(t.query(), "öλAa");

    // Num Lock: the keypad types digits; without it KP_3 is Page Down
    // (nothing) and KP_1 is End (the cursor is there already).
    t.toggle(Lock::Num);
    t.tap(code::KP_1);
    assert_eq!(t.query(), "öλAa1");
    t.tap(code::KP_2);
    assert_eq!(t.query(), "öλAa12");
    t.toggle(Lock::Num);
    t.tap(code::KP_3);
    t.tap(code::KP_1);
    t.type_text("b");
    assert_eq!(
        t.query(),
        "öλAa12b",
        "the keypad typed nothing without Num Lock"
    );

    drop(t);
    drop(compositor);
}

#[test]
fn every_editing_and_selection_key_does_its_work() {
    let Some(kind) = Kind::requested() else {
        return;
    };
    let compositor = TestCompositor::start(kind);
    let mut t = Typing::start(&compositor);

    t.type_text("rite of");
    assert_eq!(t.query(), "r");
    let typed = (0.."rite of".len() - 1).map(|_| t.query()).last();
    assert_eq!(typed.as_deref(), Some("rite of"));
    t.type_text("  ");
    t.query();
    assert_eq!(t.query(), "rite of  ");
    t.chord(Modifier::Control, code::W);
    assert_eq!(t.query(), "rite ", "Ctrl+W: the spaces, then the word");
    t.tap(code::BACKSPACE);
    assert_eq!(t.query(), "rite");
    t.chord(Modifier::Control, code::U);
    assert_eq!(t.query(), "", "Ctrl+U clears");

    // Down, Tab, Ctrl+N move on; Up, Shift+Tab, Ctrl+P move back.
    t.tap(code::DOWN);
    assert_eq!(t.next("select"), "1");
    t.tap(code::TAB);
    assert_eq!(t.next("select"), "2");
    t.chord(Modifier::Control, code::N);
    assert_eq!(t.next("select"), "3");
    t.tap(code::UP);
    assert_eq!(t.next("select"), "2");
    t.chord(Modifier::Shift, code::TAB);
    assert_eq!(t.next("select"), "1");
    t.chord(Modifier::Control, code::P);
    assert_eq!(t.next("select"), "0");
    // Caps Lock changes no shortcut.
    t.toggle(Lock::Caps);
    t.chord(Modifier::Control, code::N);
    assert_eq!(t.next("select"), "1");
    t.toggle(Lock::Caps);

    t.tap(code::ENTER);
    t.ends_with("accept 1");
    drop(compositor);
}

#[test]
fn escape_ctrl_g_and_ctrl_bracket_each_dismiss() {
    let Some(kind) = Kind::requested() else {
        return;
    };
    let compositor = TestCompositor::start(kind);
    for (name, modifier, key) in [
        ("Escape", None, code::ESC),
        ("Ctrl+G", Some(Modifier::Control), code::G),
        ("Ctrl+[", Some(Modifier::Control), code::LEFT_BRACE),
    ] {
        let mut t = Typing::start(&compositor);
        match modifier {
            Some(m) => t.chord(m, key),
            None => t.tap(key),
        }
        eprintln!("{name}");
        t.ends_with("dismiss");
    }
    drop(compositor);
}

#[test]
fn a_held_key_repeats_at_the_compositors_rate_and_stops_on_release() {
    let Some(kind) = Kind::requested() else {
        return;
    };
    let compositor = TestCompositor::start(kind);
    let mut t = Typing::start(&compositor);
    let info = t
        .panel
        .wait_seen("repeat info", |e| e.what.starts_with("repeat-info "));
    let mut info = info
        .what
        .split(' ')
        .skip(1)
        .map(|v| v.parse::<u64>().unwrap());
    let (rate, delay) = (info.next().unwrap(), info.next().unwrap());
    eprintln!(
        "repeat-info from the {}: rate {rate}, delay {delay} ms",
        kind.name()
    );
    assert!(rate > 0, "the test compositors enable repeat");

    t.harness.key(&mut t.kb, &t.focused, code::X, true);
    let pressed = t
        .panel
        .wait_for("x pressed", |e| e.what == "key 45 pressed");
    assert_eq!(t.query(), "x");
    let first = t
        .panel
        .wait_for("a repeat", |e| e.what == "key 45 repeated");
    assert!(
        first.ns - pressed.ns >= delay * 1_000_000,
        "the first repeat waits the delay"
    );
    for n in 2..=3 {
        assert_eq!(t.query(), "x".repeat(n));
    }
    t.harness.key(&mut t.kb, &t.focused, code::X, false);
    t.panel
        .wait_for("x released", |e| e.what == "key 45 released");
    let interval = 1000 / rate;
    std::thread::sleep(Duration::from_millis(delay + 5 * interval));
    t.panel.stderr();
    let events = t.panel.events();
    let released = events
        .iter()
        .position(|e| e.what == "key 45 released")
        .unwrap();
    let repeats = events[..released]
        .iter()
        .filter(|e| e.what == "key 45 repeated")
        .count();
    assert!(
        !events[released..]
            .iter()
            .any(|e| e.what == "key 45 repeated"),
        "no repeat after the release"
    );
    let last = events
        .iter()
        .rev()
        .find_map(|e| e.what.strip_prefix("query "))
        .unwrap();
    let last: String = serde_json::from_str(last).unwrap();
    assert_eq!(last, "x".repeat(repeats + 1), "one x per press and repeat");
    drop(t);
    drop(compositor);
}

#[test]
fn home_xcompose_is_honoured_when_xcomposefile_is_unset() {
    let Some(kind) = Kind::requested() else {
        return;
    };
    let compositor = TestCompositor::start(kind);
    let home = compositor.runtime_dir().join("home");
    std::fs::create_dir_all(&home).unwrap();
    std::fs::write(home.join(".XCompose"), "<Multi_key> <l> <g> : \"Ψ\"\n").unwrap();
    let mut t = Typing::start_with(&compositor, |c| {
        c.env_remove("XCOMPOSEFILE").env("HOME", &home);
    });
    t.tap(code::COMPOSE);
    t.type_text("lg");
    assert_eq!(t.query(), "Ψ");
    drop(t);
    drop(compositor);
}
