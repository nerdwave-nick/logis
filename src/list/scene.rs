//! The list drawn as a cogitator panel in the default forge colours (crimson
//! and brass; themes come later): the prompt line with the query, its
//! cursor and the counter, a brass rule, and the rows below it, the
//! selected one on a crimson band with a brass marker and every matched
//! character in brass. Rows are single lines whose tabs advance to tab
//! stops, so tab-separated columns line up.

use super::view::{Look, Row};
use super::{Level, Notice, Options};
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
/// The icon column's width, when some item has a glyph or icon.
pub const GLYPH_COLUMN: f32 = 24.0;
/// What a pending row says at its end.
pub const PENDING_MARK: &str = "divining…";

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
    let body_w = (logical.0 as f32 - 2.0 * MARGIN).max(2.0 * PAD_X + CHAR);
    let body_h = (logical.1 as f32 - 2.0 * MARGIN).max(PROMPT_BLOCK);
    let (x, y) = (MARGIN, MARGIN);
    let mut shapes = vec![body([x, y, body_w, body_h])];
    let mut texts = prompt_line(look, x + PAD_X, y, body_w - 2.0 * PAD_X);
    if look.lines > 0 {
        shapes.push(solid(
            [x + PAD_X, y + PROMPT_BLOCK, body_w - 2.0 * PAD_X, 1.5],
            0.75,
            BRASS.with_alpha(0.8),
        ));
    }
    let first = y + PROMPT_BLOCK + LIST_PAD;
    let room = ((y + body_h - LIST_PAD - first) / ROW_PITCH)
        .floor()
        .max(0.0) as usize;
    for (i, row) in look.rows.iter().take(room).enumerate() {
        let top = first + i as f32 * ROW_PITCH;
        if row.selected {
            shapes.extend(band([x + 2.0, top, body_w - 4.0, ROW_PITCH], x, body_w));
        }
        let mut left = x + PAD_X + 8.0;
        let mut width = body_w - 2.0 * PAD_X - 8.0;
        if look.glyphs {
            if let Some(glyph) = &row.glyph {
                texts.push(glyph_text(glyph, row.selected, left, top));
            }
            left += GLYPH_COLUMN;
            width -= GLYPH_COLUMN;
        }
        let ends = marks(row, left + width, top);
        width -= ends.width;
        texts.push(row_text(row, left, top, width.max(CHAR)));
        shapes.extend(ends.shapes);
        texts.extend(ends.texts);
    }
    if let Some(notice) = &look.toast {
        let (strip, words) = toast(notice, [x, y, body_w, body_h]);
        shapes.push(strip);
        texts.push(words);
    }
    Scene {
        clear: None,
        shapes,
        texts,
        ..Scene::default()
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
    let (color, mark) = match (row.pending, row.selected) {
        (true, _) => (ASH, ASH),
        (false, true) => (BONE, GOLD),
        (false, false) => (PARCHMENT, BRASS),
    };
    let line_top = top + (ROW_PITCH - FONT * 1.3) / 2.0;
    let mut text = Text::new(row.text.clone(), [left, line_top], FONT, color)
        .in_column(width, Overflow::Ellipsis);
    if !row.pending {
        text.spans = row.highlights.iter().map(|r| (r.clone(), mark)).collect();
    }
    text
}

/// A row's emblem in the icon column.
fn glyph_text(glyph: &str, selected: bool, left: f32, top: f32) -> Text {
    let color = if selected { GOLD } else { BRASS };
    let line_top = top + (ROW_PITCH - FONT * 1.3) / 2.0;
    Text::new(glyph, [left, line_top], FONT, color).in_column(GLYPH_COLUMN - 4.0, Overflow::Clip)
}

/// The badge text size.
const BADGE_FONT: f32 = 11.0;
/// Inside a badge, left and right of its text.
const BADGE_PAD: f32 = 6.0;
/// Between badges, and between the row text and the first one.
const BADGE_GAP: f32 = 6.0;
const BADGE_HEIGHT: f32 = 17.0;

/// What ends a row: its badges in their order, then the pending mark.
struct Marks {
    shapes: Vec<RoundedRect>,
    texts: Vec<Text>,
    /// The room they take from the row, gaps included.
    width: f32,
}

fn marks(row: &Row, right: f32, top: f32) -> Marks {
    let mut pills: Vec<(&str, Rgba, Rgba)> = row
        .badges
        .iter()
        .map(|b| (b.as_str(), BRASS, BRASS.with_alpha(0.7)))
        .collect();
    if row.pending {
        pills.push((PENDING_MARK, ASH, CRIMSON.with_alpha(0.8)));
    }
    let widths: Vec<f32> = pills
        .iter()
        .map(|(t, ..)| t.chars().count() as f32 * BADGE_FONT * 0.6 + 2.0 * BADGE_PAD)
        .collect();
    let width: f32 = widths.iter().map(|w| w + BADGE_GAP).sum();
    let mut marks = Marks {
        shapes: Vec::new(),
        texts: Vec::new(),
        width,
    };
    let mut left = right - width + BADGE_GAP;
    let pill_top = top + (ROW_PITCH - BADGE_HEIGHT) / 2.0;
    let text_top = top + (ROW_PITCH - BADGE_FONT * 1.3) / 2.0;
    for ((text, ink, edge), w) in pills.into_iter().zip(widths) {
        let mut pill = solid(
            [left, pill_top, w, BADGE_HEIGHT],
            BADGE_HEIGHT / 2.0,
            SOOT_BOTTOM,
        );
        pill.border = Some(Border {
            width: 1.0,
            paint: Paint::Solid(edge),
        });
        marks.shapes.push(pill);
        let label = Text::new(text, [left + BADGE_PAD, text_top], BADGE_FONT, ink)
            .in_column(w - 2.0 * BADGE_PAD + 2.0, Overflow::Clip);
        marks.texts.push(label);
        left += w + BADGE_GAP;
    }
    marks
}

/// A toast's strip across the bottom of the body, and its words.
fn toast(notice: &Notice, [x, y, w, h]: [f32; 4]) -> (RoundedRect, Text) {
    const HEIGHT: f32 = 28.0;
    const INSET: f32 = 8.0;
    let edge = match notice.level {
        Level::Info => BRASS,
        Level::Success => GOLD,
        Level::Error => Rgba::new(0.92, 0.16, 0.10, 1.0),
    };
    let rect = [x + INSET, y + h - HEIGHT - INSET, w - 2.0 * INSET, HEIGHT];
    let mut strip = solid(rect, 6.0, SOOT_TOP.with_alpha(0.97));
    strip.border = Some(Border {
        width: 1.5,
        paint: Paint::Solid(edge),
    });
    strip.glow = Some(Glow {
        radius: 8.0,
        color: edge.with_alpha(0.35),
    });
    let line_top = rect[1] + (HEIGHT - FONT * 1.3) / 2.0;
    let words = Text::new(notice.text.clone(), [rect[0] + 12.0, line_top], FONT, BONE)
        .in_column(rect[2] - 24.0, Overflow::Ellipsis);
    (strip, words)
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

fn body(rect: [f32; 4]) -> RoundedRect {
    let [_, y, _, h] = rect;
    let vertical = |from, to| Paint::Linear {
        start: [0.0, y],
        end: [0.0, y + h],
        from,
        to,
    };
    RoundedRect {
        rect,
        radius: 14.0,
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
                    ..Row::default()
                })
                .collect(),
            lines: 5,
            counter: Some((rows, 9)),
            ..Look::default()
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

    fn decorated() -> Look {
        let mut l = look(3, 0);
        l.glyphs = true;
        l.rows[0].glyph = Some("⚙".into());
        l.rows[0].badges = vec!["current".into(), "edict".into()];
        l.rows[1].pending = true;
        l
    }

    #[test]
    fn an_icon_column_moves_every_row_text_and_holds_the_glyphs() {
        let logical = size(&options(5, 30));
        let plain = scene(&look(3, 0), logical);
        let rich = scene(&decorated(), logical);
        let x = |s: &Scene, text: &str| s.texts.iter().find(|t| t.text == text).unwrap().origin[0];
        assert_eq!(x(&rich, "row 2") - x(&plain, "row 2"), GLYPH_COLUMN);
        assert!(x(&rich, "⚙") < x(&rich, "row 0"));
    }

    #[test]
    fn badges_end_the_row_and_narrow_its_text() {
        let logical = size(&options(5, 30));
        let s = scene(&decorated(), logical);
        let text = |t: &str| s.texts.iter().find(|x| x.text == t).unwrap().clone();
        let (row, current, edict) = (text("row 0"), text("current"), text("edict"));
        assert!(row.origin[0] + row.width.unwrap() <= current.origin[0]);
        assert!(current.origin[0] < edict.origin[0], "in the given order");
        let other = text("row 2");
        assert!(row.width.unwrap() < other.width.unwrap());
    }

    #[test]
    fn a_pending_row_is_dimmed_and_marked() {
        let s = scene(&decorated(), size(&options(5, 30)));
        let row = s.texts.iter().find(|t| t.text == "row 1").unwrap();
        assert_eq!(row.color, ASH);
        assert!(row.spans.is_empty(), "no highlights while pending");
        assert!(s.texts.iter().any(|t| t.text == PENDING_MARK));
    }

    #[test]
    fn a_toast_lies_over_the_bottom_of_the_body() {
        let logical = size(&options(5, 30));
        let mut l = look(3, 0);
        let without = scene(&l, logical);
        l.toast = Some(Notice {
            text: "the rite is begun".into(),
            level: Level::Success,
        });
        let with = scene(&l, logical);
        assert_eq!(with.shapes.len(), without.shapes.len() + 1);
        let strip = with.shapes.last().unwrap().rect;
        let body = with.shapes[0].rect;
        assert!(strip[1] + strip[3] <= body[1] + body[3]);
        let text = with.texts.last().unwrap();
        assert_eq!(text.text, "the rite is begun");
        assert!(text.origin[1] >= strip[1]);
    }

    #[test]
    fn no_lines_means_no_rule() {
        let mut l = look(0, 0);
        l.lines = 0;
        let scene = scene(&l, size(&options(0, 30)));
        assert_eq!(scene.shapes.len(), 1, "the body alone");
    }
}
