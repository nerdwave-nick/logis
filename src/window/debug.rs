//! The debug session: a static panel in the spirit of "forge crimson &
//! brass": SDF shapes and text rows (themes and animation come in later
//! slices). Used to check scale and text on a live compositor and as the
//! client of the E2E and latency harnesses: Up/Down move the selection,
//! Escape closes it.

use std::cell::Cell;
use std::rc::Rc;

use super::{WindowError, Windows};
use crate::render::{BackendChoice, Border, Glow, Overflow, Paint, Rgba, RoundedRect, Scene, Text};

/// The panel body in logical pixels.
const PANEL: (f32, f32) = (640.0, 360.0);
/// Room around the body for its glow.
const MARGIN: f32 = 24.0;
/// The surface the debug panel requests.
pub const DEBUG_SIZE: (u32, u32) = (
    (PANEL.0 + 2.0 * MARGIN) as u32,
    (PANEL.1 + 2.0 * MARGIN) as u32,
);

const BRASS: Rgba = Rgba::new(0.83, 0.63, 0.09, 1.0);
const CRIMSON: Rgba = Rgba::new(0.70, 0.06, 0.05, 1.0);
const SOOT_TOP: Rgba = Rgba::new(0.085, 0.055, 0.045, 0.86);
const SOOT_BOTTOM: Rgba = Rgba::new(0.045, 0.030, 0.026, 0.86);
const ASH: Rgba = Rgba::new(0.54, 0.48, 0.40, 0.28);
const BONE: Rgba = Rgba::hex(0xeadfc4);

/// The debug panel's scene for a surface of `logical` size: the body fills
/// it up to [`PANEL`], less the glow margin, so a smaller configure than
/// requested still shows the whole panel.
pub fn debug_scene(logical: (u32, u32)) -> Scene {
    let fit = |side: u32, max: f32| (side as f32 - 2.0 * MARGIN).clamp(64.0, max);
    let panel = (fit(logical.0, PANEL.0), fit(logical.1, PANEL.1));
    let x = (logical.0 as f32 - panel.0) / 2.0;
    let y = (logical.1 as f32 - panel.1) / 2.0;
    let rect = |dx: f32, dy: f32, w: f32, h: f32, radius: f32, fill: Paint| RoundedRect {
        rect: [x + dx, y + dy, w, h],
        radius,
        fill,
        border: None,
        glow: None,
    };
    let vertical = |from: Rgba, to: Rgba| Paint::Linear {
        start: [0.0, y],
        end: [0.0, y + panel.1],
        from,
        to,
    };

    let mut body = rect(
        0.0,
        0.0,
        panel.0,
        panel.1,
        14.0,
        vertical(SOOT_TOP, SOOT_BOTTOM),
    );
    body.border = Some(Border {
        width: 2.0,
        paint: vertical(BRASS, CRIMSON),
    });
    body.glow = Some(Glow {
        radius: MARGIN - 2.0,
        color: CRIMSON.with_alpha(0.55),
    });
    let rune = rect(20.0, 18.0, 16.0, 16.0, 3.0, Paint::Solid(BRASS));
    let rule = rect(
        18.0,
        52.0,
        panel.0 - 36.0,
        1.5,
        0.75,
        Paint::Solid(BRASS.with_alpha(0.8)),
    );
    let mut selected = rect(
        2.0,
        66.0,
        panel.0 - 4.0,
        26.0,
        2.0,
        Paint::Linear {
            start: [x, 0.0],
            end: [x + panel.0, 0.0],
            from: Rgba::new(0.30, 0.07, 0.05, 0.9),
            to: Rgba::new(0.18, 0.05, 0.04, 0.6),
        },
    );
    selected.border = Some(Border {
        width: 1.0,
        paint: Paint::Solid(CRIMSON.with_alpha(0.6)),
    });
    let mut marker = rect(10.0, 70.0, 3.0, 18.0, 1.5, Paint::Solid(BRASS));
    marker.glow = Some(Glow {
        radius: 6.0,
        color: BRASS.with_alpha(0.7),
    });

    let column = panel.0 - 44.0;
    let line = |dy: f32, text: &str, size: f32, color: Rgba| {
        Text::new(text, [x + 22.0, y + dy], size, color).in_column(column, Overflow::Ellipsis)
    };
    let title = Text::new(
        "COGITATOR // RITE OF TESTING",
        [x + 46.0, y + 12.0],
        18.0,
        BRASS,
    );
    let mut texts = vec![title.in_column(column - 24.0, Overflow::Ellipsis)];
    for (row, text) in ROWS.iter().enumerate() {
        let dy = 68.0 + 26.0 * row as f32;
        if dy + 22.0 > panel.1 {
            break;
        }
        let color = if row == 0 { BONE } else { ASH.with_alpha(0.9) };
        texts.push(line(dy, text, 15.0, color));
    }
    Scene {
        clear: None,
        shapes: vec![body, rune, rule, selected, marker],
        texts,
        ..Scene::default()
    }
}

/// The debug rows: every script and feature the text slice covers, the
/// last one too long for its column.
const ROWS: [&str; 9] = [
    "The machine spirit wakes: 0123456789",
    "Ça déjà vu, naïve Ærø — Œuvre, Šťastný",
    "機械の精霊 万机之神 기계 정령",
    "logis: שָׁלוֹם and مرحبا بالعالم (42) ok",
    "Praise 🔥⚙️💀🤖 the Omnissiah 👍🏽 🧑‍💻",
    "fi fl ffi — ligatures, kerning: AVATAR Tolkien",
    "Ἐν ἀρχῇ ἦν ὁ λόγος · Съешь же ещё",
    "Ellipsis rite: this line is far too long for its column, so the cogitator cuts it short and seals it",
    "Scale is taken from the compositor (1.0, 1.3, 2.0)",
];

/// The evdev codes the debug panel reacts to (raw codes: the keyboard
/// slice brings xkb keysyms; until then this is enough for a debug panel).
pub const KEY_ESC: u32 = 1;
/// Up arrow.
pub const KEY_UP: u32 = 103;
/// Down arrow.
pub const KEY_DOWN: u32 = 108;

/// The number of rows the selection can rest on: the text rows.
pub const SELECTABLE: usize = ROWS.len();
/// The distance between rows in logical pixels.
const ROW_PITCH: f32 = 26.0;
/// Indices of the selection band and its marker in [`debug_scene`].
const SELECTED: usize = 3;
const MARKER: usize = 4;

/// What a key press does to the debug panel.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Reaction {
    /// Nothing.
    Ignore,
    /// Select another row and redraw.
    Select(usize),
    /// Close the panel.
    Close,
}

/// The reaction to evdev key `code` pressed while `row` is selected.
pub fn react(row: usize, code: u32) -> Reaction {
    match code {
        KEY_ESC => Reaction::Close,
        KEY_UP if row > 0 => Reaction::Select(row - 1),
        KEY_DOWN if row + 1 < SELECTABLE => Reaction::Select(row + 1),
        _ => Reaction::Ignore,
    }
}

/// [`debug_scene`] with `row` selected.
/// The band stops at the last row that fits the body.
pub fn debug_scene_at(logical: (u32, u32), row: usize) -> Scene {
    let mut scene = debug_scene(logical);
    let [_, body_y, _, body_h] = scene.shapes[0].rect;
    let band = scene.shapes[SELECTED].rect;
    let room = body_y + body_h - (band[1] + band[3]);
    let fits = (room / ROW_PITCH).floor().max(0.0) as usize;
    let dy = ROW_PITCH * row.min(fits) as f32;
    for i in [SELECTED, MARKER] {
        scene.shapes[i].rect[1] += dy;
    }
    scene
}

/// `logis daemon --debug-panel`: wakes the GPU, shows the static panel and
/// keeps it, re-rendering only on configure, scale or selection changes,
/// until Escape, the compositor closing it, or the process ending. Up and
/// Down move the selection, which also gives harnesses an input → frame
/// path to time.
pub fn run_debug_panel(choice: BackendChoice) -> Result<(), WindowError> {
    let (mut windows, mut queue) = Windows::connect(choice)?;
    let info = windows.gpu().info();
    eprintln!(
        "logis: the machine spirit wakes on {} ({:?}, {})",
        info.name, info.backend, info.driver
    );
    windows.set_verbose(true);
    let row = Rc::new(Cell::new(0));
    let selected = Rc::clone(&row);
    let scene = move |logical| debug_scene_at(logical, selected.get());
    windows.show(&queue.handle(), DEBUG_SIZE, Box::new(scene))?;
    let qh = queue.handle();
    while windows.is_visible() {
        queue
            .blocking_dispatch(&mut windows)
            .map_err(|e| WindowError::Disconnected(e.to_string()))?;
        for code in windows.take_key_presses() {
            match react(row.get(), code) {
                Reaction::Select(next) => {
                    row.set(next);
                    windows.request_redraw();
                }
                Reaction::Close => windows.hide(),
                Reaction::Ignore => {}
            }
        }
        windows.draw_pending(&qh);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn down_and_up_move_the_selection_within_the_rows() {
        assert_eq!(react(0, KEY_DOWN), Reaction::Select(1));
        assert_eq!(react(1, KEY_UP), Reaction::Select(0));
        assert_eq!(react(0, KEY_UP), Reaction::Ignore);
        assert_eq!(react(SELECTABLE - 1, KEY_DOWN), Reaction::Ignore);
    }

    #[test]
    fn escape_closes_and_other_keys_are_ignored() {
        assert_eq!(react(3, KEY_ESC), Reaction::Close);
        assert_eq!(react(3, 30), Reaction::Ignore);
    }

    #[test]
    fn the_selection_and_its_marker_move_by_one_row_pitch_per_row() {
        let top = debug_scene_at(DEBUG_SIZE, 0);
        let third = debug_scene_at(DEBUG_SIZE, 2);
        assert_eq!(top, debug_scene(DEBUG_SIZE));
        for (i, (a, b)) in top.shapes.iter().zip(&third.shapes).enumerate() {
            let moved = if [SELECTED, MARKER].contains(&i) {
                2.0 * ROW_PITCH
            } else {
                0.0
            };
            assert_eq!(b.rect[1] - a.rect[1], moved, "shape {i}");
        }
    }

    #[test]
    fn the_selection_never_leaves_a_smaller_body() {
        let scene = debug_scene_at((466, 300), SELECTABLE - 1);
        let body = scene.shapes[0].rect;
        let selected = scene.shapes[SELECTED].rect;
        assert!(selected[1] + selected[3] <= body[1] + body[3]);
    }

    #[test]
    fn at_the_requested_size_the_body_is_the_full_panel() {
        let scene = debug_scene(DEBUG_SIZE);
        assert_eq!(scene.shapes[0].rect, [MARGIN, MARGIN, PANEL.0, PANEL.1]);
    }

    #[test]
    fn a_smaller_configure_shrinks_the_body_and_keeps_its_glow_inside() {
        let scene = debug_scene((466, 300));
        let [x, y, w, h] = scene.shapes[0].rect;
        assert_eq!(
            (x, y, w, h),
            (MARGIN, MARGIN, 466.0 - 2.0 * MARGIN, 300.0 - 2.0 * MARGIN)
        );
        assert!(scene.shapes.iter().all(|s| s.rect[1] + s.rect[3] <= y + h));
    }
}
