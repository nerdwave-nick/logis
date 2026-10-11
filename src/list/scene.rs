//! The list drawn as a cogitator panel in the default forge colours (crimson
//! and brass; themes come later): the prompt line with the query, its
//! cursor and the counter, a brass rule, and the rows below it, the
//! selected one on a crimson band with a brass marker and every matched
//! character in brass. Rows are single lines whose tabs advance to tab
//! stops, so tab-separated columns line up.

use super::Options;
use super::view::{Look, Row};
use crate::render::{Border, Glow, Overflow, Paint, Rgba, RoundedRect, Scene, Text};

/// Room around the body for its glow.
pub const MARGIN: f32 = 24.0;
/// The text size.
pub const FONT: f32 = 15.0;
/// The distance between rows.
pub const ROW_PITCH: f32 = 26.0;
/// An average character's advance, for `--width` in characters.
const CHAR: f32 = FONT * 0.6;
/// Inside the body: left and right padding.
const PAD_X: f32 = 18.0;
/// The prompt line's height, from the body's top to the rule.
const PROMPT_BLOCK: f32 = 40.0;
/// From the rule to the first row, and below the last.
const LIST_PAD: f32 = 8.0;
/// The body's corner radius.
const RADIUS: f32 = 14.0;

const BRASS: Rgba = Rgba::new(0.83, 0.63, 0.09, 1.0);
const GOLD: Rgba = Rgba::hex(0xf2c14e);
const CRIMSON: Rgba = Rgba::new(0.70, 0.06, 0.05, 1.0);
const SOOT_TOP: Rgba = Rgba::new(0.085, 0.055, 0.045, 0.86);
const SOOT_BOTTOM: Rgba = Rgba::new(0.045, 0.030, 0.026, 0.86);
const ASH: Rgba = Rgba::new(0.54, 0.48, 0.40, 0.9);
const PARCHMENT: Rgba = Rgba::new(0.78, 0.72, 0.62, 1.0);
const BONE: Rgba = Rgba::hex(0xeadfc4);

/// The surface a list with these options asks for, in logical pixels.
pub fn size(options: &Options) -> (u32, u32) {
    let width = options.width.max(1) as f32 * CHAR + 2.0 * (PAD_X + MARGIN);
    let list = match options.lines {
        0 => 0.0,
        n => 2.0 * LIST_PAD + n as f32 * ROW_PITCH,
    };
    let height = PROMPT_BLOCK + list + 2.0 * MARGIN;
    (width.ceil() as u32, height.ceil() as u32)
}

/// The scene for `look` on a surface of `logical` size: the body fills it,
/// less the glow margin, so a smaller configure still shows a whole panel
/// (with fewer rows).
pub fn scene(look: &Look, logical: (u32, u32)) -> Scene {
    scene_at(look, logical, band_top(look, logical))
}

/// The panel's body (left, top, width, height) and its corner radius on a
/// surface of `logical` size: what blur goes behind.
pub fn body(logical: (u32, u32)) -> ([f32; 4], f32) {
    let w = (logical.0 as f32 - 2.0 * MARGIN).max(2.0 * PAD_X + CHAR);
    let h = (logical.1 as f32 - 2.0 * MARGIN).max(PROMPT_BLOCK);
    ([MARGIN, MARGIN, w, h], RADIUS)
}

/// Where the selection band rests: the top of the selected row in view,
/// or `None` when no row in view is selected.
pub fn band_top(look: &Look, logical: (u32, u32)) -> Option<f32> {
    let rows = Rows::of(logical);
    look.rows
        .iter()
        .take(rows.room)
        .position(|row| row.selected)
        .map(|i| rows.top(i))
}

/// [`scene`] with the selection band at `band_top` (its glide toward the
/// selected row) instead of on the selected row; `None` draws no band.
pub fn scene_at(look: &Look, logical: (u32, u32), band_top: Option<f32>) -> Scene {
    let ([x, y, body_w, body_h], _) = body(logical);
    let mut shapes = vec![panel([x, y, body_w, body_h])];
    let mut texts = prompt_line(look, x + PAD_X, y, body_w - 2.0 * PAD_X);
    if look.lines > 0 {
        shapes.push(solid(
            [x + PAD_X, y + PROMPT_BLOCK, body_w - 2.0 * PAD_X, 1.5],
            0.75,
            BRASS.with_alpha(0.8),
        ));
    }
    if let Some(top) = band_top {
        shapes.extend(band([x + 2.0, top, body_w - 4.0, ROW_PITCH], x, body_w));
    }
    let rows = Rows::of(logical);
    for (i, row) in look.rows.iter().take(rows.room).enumerate() {
        texts.push(row_text(
            row,
            x + PAD_X + 8.0,
            rows.top(i),
            body_w - 2.0 * PAD_X - 8.0,
        ));
    }
    Scene {
        clear: None,
        shapes,
        texts,
        ..Scene::default()
    }
}

/// Where the rows go on a surface: the first row's top and how many fit.
struct Rows {
    first: f32,
    room: usize,
}

impl Rows {
    fn of(logical: (u32, u32)) -> Self {
        let ([_, y, _, body_h], _) = body(logical);
        let first = y + PROMPT_BLOCK + LIST_PAD;
        let room = ((y + body_h - LIST_PAD - first) / ROW_PITCH)
            .floor()
            .max(0.0) as usize;
        Self { first, room }
    }

    fn top(&self, i: usize) -> f32 {
        self.first + i as f32 * ROW_PITCH
    }
}

/// The prompt, the query (or placeholder) with its cursor, and the counter.
fn prompt_line(look: &Look, left: f32, top: f32, width: f32) -> Vec<Text> {
    let line_top = top + (PROMPT_BLOCK - FONT * 1.3) / 2.0;
    let mut texts = Vec::new();
    let mut room = width;
    if let Some((matched, total)) = look.counter {
        let counter = format!("{matched}/{total}");
        room -= (counter.chars().count() as f32 + 2.0) * CHAR;
        let mut text =
            Text::new(counter, [left, line_top], FONT, ASH).in_column(width, Overflow::Clip);
        text.align_end = true;
        texts.push(text);
    }
    let mut line = look.prompt.clone();
    let mut spans = vec![(0..line.len(), BRASS)];
    let input_color = match look.cursor {
        None => ASH,
        Some(_) => BONE,
    };
    let start = line.len();
    match look.cursor {
        Some(cursor) => {
            let cursor = cursor.min(look.input.len());
            line.push_str(&look.input[..cursor]);
            spans.push((start..line.len(), input_color));
            let bar = line.len();
            line.push('|');
            spans.push((bar..line.len(), GOLD));
            let after = line.len();
            line.push_str(&look.input[cursor..]);
            spans.push((after..line.len(), input_color));
        }
        None => {
            line.push_str(&look.input);
            spans.push((start..line.len(), input_color));
        }
    }
    spans.retain(|(r, _)| !r.is_empty());
    let mut text =
        Text::new(line, [left, line_top], FONT, BONE).in_column(room.max(CHAR), Overflow::Ellipsis);
    text.spans = spans;
    texts.insert(0, text);
    texts
}

fn row_text(row: &Row, left: f32, top: f32, width: f32) -> Text {
    let (color, mark) = if row.selected {
        (BONE, GOLD)
    } else {
        (PARCHMENT, BRASS)
    };
    let line_top = top + (ROW_PITCH - FONT * 1.3) / 2.0;
    let mut text = Text::new(row.text.clone(), [left, line_top], FONT, color)
        .in_column(width, Overflow::Ellipsis);
    text.spans = row.highlights.iter().map(|r| (r.clone(), mark)).collect();
    text
}

fn solid(rect: [f32; 4], radius: f32, color: Rgba) -> RoundedRect {
    RoundedRect {
        rect,
        radius,
        fill: Paint::Solid(color),
        border: None,
        glow: None,
    }
}

fn panel(rect: [f32; 4]) -> RoundedRect {
    let [_, y, _, h] = rect;
    let vertical = |from, to| Paint::Linear {
        start: [0.0, y],
        end: [0.0, y + h],
        from,
        to,
    };
    RoundedRect {
        rect,
        radius: RADIUS,
        fill: vertical(SOOT_TOP, SOOT_BOTTOM),
        border: Some(Border {
            width: 2.0,
            paint: vertical(BRASS, CRIMSON),
        }),
        glow: Some(Glow {
            radius: MARGIN - 2.0,
            color: CRIMSON.with_alpha(0.55),
        }),
    }
}

/// The selection band across the body and its brass marker.
fn band(rect: [f32; 4], body_x: f32, body_w: f32) -> [RoundedRect; 2] {
    let [x, y, w, h] = rect;
    let mut band = RoundedRect {
        rect: [x, y + 1.0, w, h - 2.0],
        radius: 2.0,
        fill: Paint::Linear {
            start: [body_x, 0.0],
            end: [body_x + body_w, 0.0],
            from: Rgba::new(0.30, 0.07, 0.05, 0.9),
            to: Rgba::new(0.18, 0.05, 0.04, 0.6),
        },
        border: None,
        glow: None,
    };
    band.border = Some(Border {
        width: 1.0,
        paint: Paint::Solid(CRIMSON.with_alpha(0.6)),
    });
    let mut marker = solid([body_x + 10.0, y + 5.0, 3.0, h - 10.0], 1.5, BRASS);
    marker.glow = Some(Glow {
        radius: 6.0,
        color: BRASS.with_alpha(0.7),
    });
    [band, marker]
}

#[cfg(test)]
#[allow(clippy::single_range_in_vec_init)] // one highlighted range is a case
mod tests {
    use super::*;

    fn look(rows: usize, selected: usize) -> Look {
        Look {
            prompt: "> ".into(),
            input: "rite".into(),
            cursor: Some(2),
            rows: (0..rows)
                .map(|i| Row {
                    text: format!("row {i}"),
                    highlights: vec![0..1],
                    selected: i == selected,
                })
                .collect(),
            lines: 5,
            counter: Some((rows, 9)),
        }
    }

    fn options(lines: u32, width: u32) -> Options {
        Options {
            lines,
            width,
            ..Options::default()
        }
    }

    #[test]
    fn the_size_follows_lines_and_width() {
        let (w30, h15) = size(&options(15, 30));
        let (w60, h5) = size(&options(5, 60));
        assert_eq!(w60 - w30, (30.0 * CHAR) as u32);
        assert_eq!(h15 - h5, (10.0 * ROW_PITCH) as u32);
        let (_, h0) = size(&options(0, 30));
        assert_eq!(
            h0,
            (PROMPT_BLOCK + 2.0 * MARGIN) as u32,
            "only the prompt line"
        );
    }

    #[test]
    fn the_prompt_line_shows_prompt_query_cursor_and_counter() {
        let scene = scene(&look(3, 0), size(&options(5, 30)));
        let line = &scene.texts[0];
        assert_eq!(line.text, "> ri|te");
        assert_eq!(line.spans[0], (0..2, BRASS));
        assert_eq!(line.spans[2], (4..5, GOLD));
        let counter = &scene.texts[1];
        assert_eq!(counter.text, "3/9");
        assert!(counter.align_end);
        assert!(
            line.width.unwrap() < counter.width.unwrap(),
            "the query leaves room"
        );
    }

    #[test]
    fn a_placeholder_has_no_cursor() {
        let mut l = look(0, 0);
        l.cursor = None;
        l.input = "seek".into();
        let scene = scene(&l, size(&options(5, 30)));
        assert_eq!(scene.texts[0].text, "> seek");
        assert_eq!(scene.texts[0].spans[1], (2..6, ASH));
    }

    #[test]
    fn rows_stack_below_the_rule_with_the_band_on_the_selected_one() {
        let logical = size(&options(5, 30));
        let scene = scene(&look(3, 1), logical);
        let rows = &scene.texts[2..];
        assert_eq!(rows.len(), 3);
        let rule = scene.shapes[1].rect;
        assert!(rows.iter().all(|t| t.origin[1] > rule[1]));
        assert_eq!(rows[1].origin[1] - rows[0].origin[1], ROW_PITCH);
        let band = scene.shapes[2].rect;
        assert!(
            band[1] <= rows[1].origin[1]
                && rows[1].origin[1] + rows[1].line_height <= band[1] + band[3]
        );
        assert_eq!(rows[1].color, BONE);
        assert_eq!(rows[0].spans, [(0..1, BRASS)]);
        assert_eq!(rows[1].spans, [(0..1, GOLD)]);
    }

    #[test]
    fn a_smaller_surface_shows_fewer_rows_inside_the_body() {
        let (w, h) = size(&options(5, 30));
        let scene = scene(&look(5, 4), (w, h - 2 * ROW_PITCH as u32));
        let body = scene.shapes[0].rect;
        let rows = &scene.texts[2..];
        assert_eq!(rows.len(), 3);
        assert!(
            rows.iter()
                .all(|t| t.origin[1] + t.line_height <= body[1] + body[3])
        );
    }

    #[test]
    fn the_band_rests_on_the_selected_row_in_view() {
        let logical = size(&options(5, 30));
        let l = look(3, 2);
        let top = band_top(&l, logical).unwrap();
        let scene = scene(&l, logical);
        assert_eq!(scene.shapes[2].rect[1], top + 1.0, "the band's inset top");
        assert_eq!(
            top,
            scene.texts[4].origin[1] - (ROW_PITCH - FONT * 1.3) / 2.0
        );
        assert_eq!(band_top(&look(3, 7), logical), None, "no row selected");
    }

    #[test]
    fn a_gliding_band_moves_alone() {
        let logical = size(&options(5, 30));
        let l = look(3, 2);
        let rest = scene(&l, logical);
        let top = band_top(&l, logical).unwrap();
        let gliding = scene_at(&l, logical, Some(top - 10.0));
        assert_eq!(gliding.texts, rest.texts);
        assert_eq!(gliding.shapes.len(), 4, "body, rule, band and marker");
        assert_eq!(gliding.shapes[..2], rest.shapes[..2]);
        for (g, r) in gliding.shapes[2..].iter().zip(&rest.shapes[2..]) {
            assert_eq!(g.rect[1], r.rect[1] - 10.0);
        }
        assert_eq!(scene_at(&l, logical, None).shapes.len(), 2, "no band");
        assert_eq!(scene_at(&l, logical, Some(top)), rest, "at rest");
    }

    #[test]
    fn the_body_is_the_first_shape() {
        let logical = size(&options(5, 30));
        let (rect, radius) = body(logical);
        let shape = scene(&look(3, 0), logical).shapes[0];
        assert_eq!((shape.rect, shape.radius), (rect, radius));
    }

    #[test]
    fn no_lines_means_no_rule() {
        let mut l = look(0, 0);
        l.lines = 0;
        let scene = scene(&l, size(&options(0, 30)));
        assert_eq!(scene.shapes.len(), 1, "the body alone");
    }
}
