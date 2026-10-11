//! The pointer and paste end to end (spec §3 story 16): a real daemon in
//! the test compositor (nested niri locally, headless sway on CI; see
//! tests/support/e2e/mod.rs), `logis --dmenu` clients, the harness's
//! virtual pointer (zwlr_virtual_pointer_v1) and a test-only selection
//! owner (wlr-data-control) in place of `wl-copy`. Runs only with
//! `LOGIS_E2E=niri|sway`; otherwise each compositor test prints `SKIPPED`.
//! The isolation guard's test always runs.
//!
//! The daemon's trace says what the pointer did (`pointer-enter`, `cursor`,
//! `button`, `scroll`) and what the list made of it (`select`, `accept`,
//! `query`, `paste`, `pasted`), so the tests wait for those, never sleep.

mod support;

use logis::list::{Options, scene};
use logis::window::trace::monotonic_ns;
use support::e2e::cogitator::{Cogitator, Ended};
use support::e2e::keyboard::{Modifier, code};
use support::e2e::pointer::{BTN_LEFT, BTN_MIDDLE, Panel, VirtualPointer};
use support::e2e::wire::connect_verified;
use support::{ScratchDir, fake_compositor};

fn chose(stdout: &[u8]) -> Ended {
    Ended {
        code: Some(0),
        stdout: stdout.to_vec(),
    }
}

const ITEMS: &[u8] = b"alpha\nbeta\ngamma\n";
const LINES: u32 = 5;

/// The panel of a list with [`LINES`] rows and the default width.
fn panel_size() -> (u32, u32) {
    scene::size(&Options {
        lines: LINES,
        ..Options::default()
    })
}

/// The middle of row `row` in view, in surface coordinates.
fn row(row: usize) -> (f64, f64) {
    let size = panel_size();
    let top = f64::from(scene::MARGIN + scene::PROMPT_BLOCK + scene::LIST_PAD);
    let point = (
        f64::from(size.0) / 2.0,
        top + (row as f64 + 0.5) * f64::from(scene::ROW_PITCH),
    );
    assert_eq!(scene::row_at(size, LINES, point), Some(row));
    point
}

/// `n` numbered lines.
fn numbered(n: usize) -> Vec<u8> {
    (0..n)
        .map(|i| format!("item {i}\n"))
        .collect::<String>()
        .into_bytes()
}

/// Learns how output points map onto the shown panel: the pointer enters
/// it twice from a corner, at the output's centre (on the panel, whatever
/// the output's size) and a little beside it, and the panel's own
/// `pointer-enter` says where in it the pointer was each time. Entering
/// never selects, so the list is as it was. Checks the cursor was set, too.
fn aim(c: &mut Cogitator, vp: &VirtualPointer) -> Panel {
    let (w, h) = c.harness.output_size();
    let centre = (f64::from(w / 2), f64::from(h / 2));
    let sent = [centre, (centre.0 + 8.0, centre.1 + 8.0)];
    let seen = sent.map(|point| {
        c.harness.move_to(vp, (1.0, 1.0));
        let since = monotonic_ns();
        c.harness.move_to(vp, point);
        let entered = surface_point(c, since, "pointer-enter");
        c.daemon
            .wait_for("the cursor set", |e| e.what == "cursor default");
        entered
    });
    Panel::calibrate(sent, seen)
}

/// The surface point of the next `word <x> <y>` trace event since `since`.
fn surface_point(c: &mut Cogitator, since: u64, word: &str) -> (f64, f64) {
    let prefix = format!("{word} ");
    let event = c
        .daemon
        .wait_for(word, |e| e.ns >= since && e.what.starts_with(&prefix));
    let mut at = event.what[prefix.len()..]
        .split(' ')
        .map(|v| v.parse::<f64>().unwrap());
    (at.next().unwrap(), at.next().unwrap())
}

// ------------------------------------------------------- isolation guard

#[test]
fn the_pointer_and_the_selection_owner_refuse_a_socket_served_by_any_other_process() {
    let dir = ScratchDir::new();
    // Served by this process: its peer is not a compositor the harness
    // spawned, exactly as the live session's socket would not be.
    let _compositor = fake_compositor::serve(dir.path(), "wayland-0", &[]);
    let mut spawned = std::process::Command::new("true").spawn().unwrap();
    let socket = dir.path().join("wayland-0");
    let refused = connect_verified(&socket, spawned.id())
        .map(|_| ())
        .unwrap_err();
    spawned.wait().unwrap();
    assert!(refused.contains("refusing to inject"), "{refused}");
    assert!(connect_verified(&socket, std::process::id()).is_ok());
}

// ------------------------------------------------------- pointer

#[test]
fn hovering_selects_a_row_and_a_click_on_it_accepts_it() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let vp = c.harness.virtual_pointer();
    let lines = format!("--lines={LINES}");
    let client = c.dmenu_all(&[&lines], b"alpha\nbeta\ngamma\ndelta\nepsilon\n");
    c.shown(5);
    let panel = aim(&mut c, &vp);
    c.harness.move_to(&vp, panel.at(row(3)));
    assert_eq!(c.next("select"), "3", "hover highlights the row");
    // A press dragged off its row chooses nothing; the hover follows.
    c.harness.button(&vp, BTN_LEFT, true);
    c.harness.move_to(&vp, panel.at(row(1)));
    assert_eq!(c.next("select"), "1");
    c.harness.button(&vp, BTN_LEFT, false);
    c.harness.click(&vp, BTN_LEFT);
    assert_eq!(c.next("accept"), "1", "only the click on one row accepts");
    assert_eq!(client.ended(), chose(b"beta\n"));
}

#[test]
fn the_wheel_scrolls_the_list_three_rows_a_notch() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let vp = c.harness.virtual_pointer();
    let lines = format!("--lines={LINES}");
    let client = c.dmenu_all(&[&lines], &numbered(30));
    c.shown(30);
    let panel = aim(&mut c, &vp);
    c.harness.move_to(&vp, panel.at(row(0)));
    c.harness.wheel(&vp, 2);
    assert_eq!(c.next("scroll"), "wheel 240");
    assert_eq!(c.next("select"), "6", "row 0 now shows item 6");
    c.harness.wheel(&vp, -1);
    assert_eq!(c.next("scroll"), "wheel -120");
    assert_eq!(c.next("select"), "3");
    c.harness.move_to(&vp, panel.at(row(1)));
    assert_eq!(c.next("select"), "4");
    c.harness.click(&vp, BTN_LEFT);
    assert_eq!(client.ended(), chose(b"item 4\n"));
}

#[test]
fn a_finger_scrolls_a_row_per_row_height_and_carries_the_rest() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let vp = c.harness.virtual_pointer();
    let lines = format!("--lines={LINES}");
    let client = c.dmenu_all(&[&lines], &numbered(30));
    c.shown(30);
    let panel = aim(&mut c, &vp);
    c.harness.move_to(&vp, panel.at(row(0)));
    let pitch = f64::from(scene::ROW_PITCH);
    for item in 1..=3 {
        c.harness.finger(&vp, pitch);
        assert_eq!(c.next("scroll"), format!("pixels {pitch}"));
        assert_eq!(c.next("select"), item.to_string());
    }
    // Half a row moves nothing; the next half completes the row. (Lifting
    // the finger drops a half row: unit-tested only, since nested niri's
    // virtual pointer does not forward axis_stop.)
    c.harness.finger(&vp, pitch / 2.0);
    assert_eq!(c.next("scroll"), format!("pixels {}", pitch / 2.0));
    c.harness.finger(&vp, pitch / 2.0);
    c.next("scroll");
    assert_eq!(c.next("select"), "4");
    c.harness.click(&vp, BTN_LEFT);
    assert_eq!(c.next("accept"), "4");
    assert_eq!(client.ended(), chose(b"item 4\n"));
}

// ------------------------------------------------------- paste

#[test]
fn ctrl_v_and_shift_insert_paste_the_clipboard_at_the_cursor() {
    use support::e2e::selection::SelectionOwner;
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let owner = SelectionOwner::start(&c.compositor, Some("gam\nnot this line"), None);
    let client = c.dmenu_all(&[], ITEMS);
    let focused = c.shown(3);
    c.type_text(&focused, "ma");
    c.tap(&focused, code::HOME);
    c.harness.hold(&mut c.kb, &focused, Modifier::Control);
    c.tap(&focused, code::V);
    c.harness.release(&mut c.kb, &focused, Modifier::Control);
    assert_eq!(c.next("paste"), "clipboard text/plain;charset=utf-8");
    assert_eq!(c.next("pasted"), "clipboard 3");
    assert_eq!(c.next("query"), "\"gamma\"", "inserted at the cursor");
    assert_eq!(c.next("select"), "2");
    assert_eq!(owner.requests(), ["clipboard text/plain;charset=utf-8"]);

    c.harness.hold(&mut c.kb, &focused, Modifier::Control);
    c.tap(&focused, code::U);
    c.harness.release(&mut c.kb, &focused, Modifier::Control);
    assert_eq!(c.next("query"), "\"\"");
    c.harness.hold(&mut c.kb, &focused, Modifier::Shift);
    c.tap(&focused, code::INSERT);
    c.harness.release(&mut c.kb, &focused, Modifier::Shift);
    assert_eq!(c.next("pasted"), "clipboard 3");
    assert_eq!(c.next("query"), "\"gam\"");
    c.tap(&focused, code::ENTER);
    assert_eq!(client.ended(), chose(b"gamma\n"));
}

#[test]
fn the_middle_button_pastes_the_primary_selection() {
    use support::e2e::selection::SelectionOwner;
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let vp = c.harness.virtual_pointer();
    let owner = SelectionOwner::start(&c.compositor, Some("not the clipboard"), Some("bet"));
    let client = c.dmenu_all(&[], ITEMS);
    let focused = c.shown(3);
    aim(&mut c, &vp);
    c.harness.click(&vp, BTN_MIDDLE);
    assert_eq!(c.next("paste"), "primary text/plain;charset=utf-8");
    assert_eq!(c.next("query"), "\"bet\"");
    assert_eq!(c.next("select"), "1");
    assert_eq!(owner.requests(), ["primary text/plain;charset=utf-8"]);
    c.tap(&focused, code::ENTER);
    assert_eq!(client.ended(), chose(b"beta\n"));
}
