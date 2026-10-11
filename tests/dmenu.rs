//! `logis --dmenu` end to end (spec §3 stories 1–4): a real daemon in the
//! test compositor (nested niri locally, headless sway on CI; see
//! tests/support/e2e/mod.rs), real `logis --dmenu` clients with piped stdin
//! and stdout, and keys from the harness's virtual keyboard. Runs only with
//! `LOGIS_E2E=niri|sway`; otherwise each test prints `SKIPPED`.
//!
//! The daemon runs with the test trace on, so the tests wait for what the
//! window did (`show`, `items`, `query`, `select`, `accept`, `typed`,
//! `dismiss`, and the window's focus and commits) instead of sleeping. It
//! moves as for users (open, close and selection transitions); captures
//! wait for `settled`, so goldens show the end state.

mod support;

use std::io::Write;

use logis::list::{Options, scene};
use logis::render::Image;
use logis::theme::Theme;
use support::e2e::cogitator::{Cogitator, Ended};
use support::e2e::keyboard::{Modifier, code};
use support::e2e::screencopy::crop_centred;
use support::e2e::wire::Harness;
use support::golden::{Goldens, Tolerance};

fn chose(stdout: &[u8]) -> Ended {
    Ended {
        code: Some(0),
        stdout: stdout.to_vec(),
    }
}

const ITEMS: &[u8] = b"alpha\nbeta\ngamma\n";

#[test]
fn typing_filters_and_return_prints_the_chosen_line() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    let focused = c.shown(3);
    c.type_text(&focused, "gam");
    assert_eq!(c.next("select"), "2");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "2");
    assert_eq!(client.ended(), chose(b"gamma\n"));
}

#[test]
fn escape_dismisses_with_empty_stdout_and_exit_1() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    let focused = c.shown(3);
    c.tap(&focused, code::ESC);
    c.next("dismiss");
    let ended = client.ended();
    assert_eq!((ended.code, ended.stdout.as_slice()), (Some(1), &b""[..]));
}

#[test]
fn index_and_columns_print_what_was_asked() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let input = b"id-1\tFirst\nid-2\tSecond\n";
    let client = c.dmenu_all(&["--with-nth=2", "--accept-nth=1"], input);
    let focused = c.shown(2);
    c.type_text(&focused, "sec");
    c.tap(&focused, code::ENTER);
    assert_eq!(client.ended(), chose(b"id-2\n"));

    let client = c.dmenu_all(&["--index"], input);
    let focused = c.shown(2);
    c.tap(&focused, code::DOWN);
    c.next("select");
    c.tap(&focused, code::ENTER);
    assert_eq!(client.ended(), chose(b"1\n"));
}

#[test]
fn return_without_a_match_prints_the_typed_text() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let client = c.dmenu_all(&[], ITEMS);
    let focused = c.shown(3);
    c.type_text(&focused, "Free Rite");
    assert_eq!(c.next("select"), "none");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("typed"), "\"Free Rite\"");
    assert_eq!(client.ended(), chose(b"Free Rite\n"));
}

#[test]
fn only_match_refuses_free_text_until_a_match_is_chosen() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let client = c.dmenu_all(&["--only-match"], ITEMS);
    let focused = c.shown(3);
    c.type_text(&focused, "zz");
    c.tap(&focused, code::ENTER);
    // Return did nothing; clearing the query (Ctrl+U) brings every item back.
    c.harness.hold(&mut c.kb, &focused, Modifier::Control);
    c.tap(&focused, code::U);
    c.harness.release(&mut c.kb, &focused, Modifier::Control);
    assert_eq!(c.next("query"), "\"\"");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "0");
    assert!(
        !c.daemon
            .events()
            .iter()
            .any(|e| e.what.starts_with("typed")),
        "no free text was accepted"
    );
    assert_eq!(client.ended(), chose(b"alpha\n"));
}

#[test]
fn queued_callers_each_get_their_own_answer_in_turn() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let first = c.dmenu_all(&[], b"one\ntwo\n");
    let focused = c.shown(2);
    let second = c.dmenu_all(&[], b"three\nfour\nfive\n");
    // The second caller waits while the first is answered.
    c.tap(&focused, code::DOWN);
    c.next("select");
    c.tap(&focused, code::ENTER);
    assert_eq!(first.ended(), chose(b"two\n"));
    let focused = c.shown(3);
    c.type_text(&focused, "five");
    c.tap(&focused, code::ENTER);
    assert_eq!(second.ended(), chose(b"five\n"));
}

#[test]
fn the_window_opens_before_the_input_ends_and_takes_streamed_lines() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let mut client = c.dmenu(&[], b"first\n");
    let focused = c.shown(1);
    // Still open: more lines arrive while the window is shown.
    let stdin = client.stdin.as_mut().unwrap();
    stdin.write_all(b"second\nthird\n").unwrap();
    stdin.flush().unwrap();
    c.daemon.wait_for("items 3", |e| e.what == "items 3");
    c.tap(&focused, code::DOWN);
    c.tap(&focused, code::DOWN);
    assert_eq!(c.next("select"), "1");
    assert_eq!(c.next("select"), "2");
    c.tap(&focused, code::ENTER);
    assert_eq!(client.ended(), chose(b"third\n"));
}

#[test]
fn the_list_is_drawn_with_its_matches_highlighted() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let input = "rite of oil\tunguent\nrite of ignition\tspark\ncafé noir\tdrink\nlitany\n";
    let client = c.dmenu_all(&["--counter", "--lines=4", "--width=40"], input.as_bytes());
    let focused = c.shown(4);
    c.type_text(&focused, "rite");
    c.tap(&focused, code::DOWN);
    c.next("select");
    // The band glides to the row; its end state is the golden.
    c.daemon
        .wait_for("the band at rest", |e| e.what == "settled");
    let options = Options {
        lines: Some(4),
        width: Some(40),
        ..Options::default()
    };
    let size = scene::fit(&options, &Theme::bundled_default()).0;
    check_window(&mut c.harness, "dmenu-filtered", size);
    c.tap(&focused, code::ESC);
    assert_eq!(client.ended().code, Some(1));
}

/// Captures until two consecutive captures agree, then compares the window
/// cropped from the output with golden `e2e/<name>`.
fn check_window(harness: &mut Harness, name: &str, size: (u32, u32)) {
    let mut last = harness.capture();
    let capture = loop {
        let next = harness.capture();
        if next == last {
            break next;
        }
        last = next;
    };
    let goldens = Goldens::checked_in();
    let reference: Option<Image> = (!goldens.updating())
        .then(|| goldens.load("e2e", name).ok())
        .flatten();
    let window = crop_centred(&capture, size, reference.as_ref());
    let tolerance = Tolerance {
        channel: 4,
        outliers: 0.001,
    };
    let verdict = goldens.assert("e2e", name, &window, tolerance);
    eprintln!("e2e/{name}: {verdict:?}");
}

#[test]
fn losing_the_compositor_ends_the_daemon_with_exit_2() {
    let Some(c) = Cogitator::start() else { return };
    let Cogitator {
        kb,
        harness,
        mut daemon,
        compositor,
    } = c;
    // The window owns the daemon's only compositor connection; its loss,
    // seen by the window thread, must end the daemon.
    drop((kb, harness));
    drop(compositor);
    let status = daemon.wait_exit();
    assert_eq!(status.code(), Some(2), "stderr: {}", daemon.stderr());
    assert!(
        daemon.stderr().contains("noosphere fell silent"),
        "{}",
        daemon.stderr()
    );
}
