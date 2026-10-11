//! What ends a row (its badges, then the pending mark) and the toast strip.

use super::{
    ASH, BONE, BRASS, CRIMSON, FONT, GOLD, PENDING_MARK, ROW_PITCH, SOOT_BOTTOM, SOOT_TOP, solid,
};
use crate::list::view::Row;
use crate::list::{Level, Notice};
use crate::render::{Border, Glow, Overflow, Paint, Rgba, RoundedRect, Text};

/// The badge text size.
const BADGE_FONT: f32 = 11.0;
/// Inside a badge, left and right of its text.
const BADGE_PAD: f32 = 6.0;
/// Between badges, and between the row text and the first one.
const BADGE_GAP: f32 = 6.0;
const BADGE_HEIGHT: f32 = 17.0;

/// What ends a row: its badges in their order, then the pending mark.
pub(super) struct Marks {
    pub(super) shapes: Vec<RoundedRect>,
    pub(super) texts: Vec<Text>,
    /// The room they take from the row, gaps included.
    pub(super) width: f32,
}

pub(super) fn marks(row: &Row, right: f32, top: f32) -> Marks {
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
pub(super) fn toast(notice: &Notice, [x, y, w, h]: [f32; 4]) -> (RoundedRect, Text) {
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
