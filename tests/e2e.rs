//! End to end in a real compositor the harness starts itself (nested niri
//! locally, headless sway on CI; see tests/support/e2e/mod.rs). Runs only
//! with `LOGIS_E2E=niri|sway`; otherwise the compositor tests print
//! `SKIPPED`. The isolation guard and the harness's pure parts always run.
//!
//! The client is `logis daemon --debug-panel` (the only window logis shows
//! yet): it must report keyboard focus, receive injected keys with real
//! modifiers and masks, and show the result on screen, compared with the
//! goldens in tests/goldens/e2e/.

mod support;

use logis::render::Image;
use logis::window::DEBUG_SIZE;
use support::e2e::client::{self, Traced};
use support::e2e::keyboard::{self, Modifier, code};
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
