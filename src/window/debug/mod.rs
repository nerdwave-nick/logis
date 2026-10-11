//! The debug session: a static panel in the spirit of "forge crimson &
//! brass": SDF shapes and text rows (themes and animation come in later
//! slices). Used to check scale and text on a live compositor and as the
//! client of the E2E and latency harnesses. It takes the whole keyboard
//! model ([`crate::input`]): a query line at the bottom shows what is typed,
//! the selection moves, and the dismissal and accept keys close it
//! ([`session`]).

mod session;

pub use session::run_debug_panel;

use crate::input::Query;
use crate::render::{Border, Glow, Overflow, Paint, Rgba, RoundedRect, Scene, Text};

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
        highlight: None,
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
        highlight: None,
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
    let query_top = panel.1 - QUERY_BLOCK;
    for (row, text) in ROWS.iter().enumerate() {
        let dy = 68.0 + ROW_PITCH * row as f32;
        if dy + 22.0 > query_top {
            break;
        }
        let color = if row == 0 { BONE } else { ASH.with_alpha(0.9) };
        texts.push(line(dy, text, 15.0, color));
    }
    let query_rule = rect(
        18.0,
        query_top,
        panel.0 - 36.0,
        1.5,
        0.75,
        Paint::Solid(BRASS.with_alpha(0.8)),
    );
    texts.push(line(
        query_top + 10.0,
        &format!("{PROMPT}{CURSOR}"),
        15.0,
        BRASS,
    ));
    Scene {
        clear: None,
        shapes: vec![body, rune, rule, selected, marker, query_rule],
        texts,
        ..Scene::default()
    }
}

/// Room at the bottom of the body for the query rule and line.
const QUERY_BLOCK: f32 = 44.0;
/// What the query line starts with; the query follows it.
const PROMPT: &str = "> ";
/// Marks the cursor in the query line.
const CURSOR: &str = "|";

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

/// The number of rows the selection can rest on: the text rows.
pub const SELECTABLE: usize = ROWS.len();
/// The distance between rows in logical pixels.
const ROW_PITCH: f32 = 26.0;
/// Indices of the selection band and its marker in [`debug_scene`].
const SELECTED: usize = 3;
const MARKER: usize = 4;

/// [`debug_scene`] with `row` selected and `query` in the query line (the
/// cursor marked). The band stops at the last row above the query block.
pub fn debug_scene_at(logical: (u32, u32), row: usize, query: &Query) -> Scene {
    let mut scene = debug_scene(logical);
    let [_, body_y, _, body_h] = scene.shapes[0].rect;
    let band = scene.shapes[SELECTED].rect;
    let room = body_y + body_h - QUERY_BLOCK - (band[1] + band[3]);
    let fits = (room / ROW_PITCH).floor().max(0.0) as usize;
    let dy = ROW_PITCH * row.min(fits) as f32;
    for i in [SELECTED, MARKER] {
        scene.shapes[i].rect[1] += dy;
    }
    let (before, after) = query.text().split_at(query.cursor());
    if let Some(line) = scene.texts.last_mut() {
        line.text = format!("{PROMPT}{before}{CURSOR}{after}");
    }
    scene
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_selection_and_its_marker_move_by_one_row_pitch_per_row() {
        let top = debug_scene_at(DEBUG_SIZE, 0, &Query::new());
        let third = debug_scene_at(DEBUG_SIZE, 2, &Query::new());
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
        let scene = debug_scene_at((466, 300), SELECTABLE - 1, &Query::new());
        let body = scene.shapes[0].rect;
        let selected = scene.shapes[SELECTED].rect;
        assert!(selected[1] + selected[3] <= body[1] + body[3] - QUERY_BLOCK);
    }

    #[test]
    fn the_query_line_shows_the_query_with_its_cursor_below_every_row() {
        use crate::input::Edit;
        let mut query = Query::new();
        query.apply(&Edit::Insert("öλ".into()));
        query.apply(&Edit::Left);
        let scene = debug_scene_at(DEBUG_SIZE, 0, &query);
        let line = scene.texts.last().unwrap();
        assert_eq!(line.text, "> ö|λ");
        let rows = &scene.texts[1..scene.texts.len() - 1];
        assert_eq!(rows.len(), SELECTABLE, "every row still fits");
        let rule = scene.shapes.last().unwrap().rect;
        assert!(rows.iter().all(|t| t.origin[1] + t.line_height <= rule[1]));
        assert!(line.origin[1] > rule[1]);
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
