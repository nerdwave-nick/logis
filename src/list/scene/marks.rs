//! What ends a row (its badges in their order, then the pending mark,
//! shimmering) and the toast across the bottom of the body.

use super::{Style, advance, column, text};
use crate::list::view::Row;
use crate::list::{Level, Notice};
use crate::render::{Border, Glow, Overflow, Paint, Rgba, RoundedRect, Scene, Text};
use crate::theme::Theme;
use crate::theme::doc::BadgeStyle;

/// What ends a row.
pub(super) struct Marks {
    pub shapes: Vec<RoundedRect>,
    pub texts: Vec<Text>,
    /// The room they take from the row, gaps included.
    pub width: f32,
}

/// The badges of `row` and its pending mark, ending at `right`, centred on
/// `centre`.
pub(super) fn marks(row: &Row, right: f32, centre: f32, style: Style<'_>) -> Marks {
    let theme = style.theme;
    let b = &theme.badge;
    let p = &theme.pending;
    let fade = if row.pending { p.opacity } else { 1.0 };
    // (words, ink, edge, fill)
    let mut pills: Vec<(String, Rgba, Rgba, Rgba)> = row
        .badges
        .iter()
        .map(|t| (t.clone(), b.ink, b.edge, b.fill))
        .map(|(t, ink, edge, fill)| {
            let f = |c: Rgba| c.with_alpha(c.a * fade);
            (t, f(ink), f(edge), f(fill))
        })
        .collect();
    if row.pending {
        let shimmer = 1.0 - p.shimmer * (1.0 - style.wave(p.period));
        let ink = p.color.with_alpha(p.color.a * shimmer);
        let edge = ink.with_alpha(ink.a * 0.6);
        pills.push((p.mark.clone(), ink, edge, Rgba::TRANSPARENT));
    }
    let pad = b.font.size * 0.55;
    let gap = b.font.size * 0.55;
    let height = b.font.size * 1.55;
    let words: Vec<String> = pills
        .iter()
        .map(|(t, ..)| match (b.style, b.uppercase) {
            (BadgeStyle::Bracket, true) => format!("[{}]", t.to_uppercase()),
            (BadgeStyle::Bracket, false) => format!("[{t}]"),
            (_, true) => t.to_uppercase(),
            (_, false) => t.clone(),
        })
        .collect();
    let pad = if b.style == BadgeStyle::Bracket { 0.0 } else { pad };
    let widths: Vec<f32> = words
        .iter()
        .map(|w| w.chars().count() as f32 * advance(&b.font) + 2.0 * pad)
        .collect();
    let width: f32 = widths.iter().map(|w| w + gap).sum();
    let mut marks = Marks {
        shapes: Vec::new(),
        texts: Vec::new(),
        width,
    };
    let mut left = right - width + gap;
    for (((_, ink, edge, fill), words), w) in pills.into_iter().zip(words).zip(widths) {
        if b.style != BadgeStyle::Bracket {
            let (fill, border) = match b.style {
                BadgeStyle::Fill => (edge, None),
                _ => (fill, Some(edge)),
            };
            marks.shapes.push(RoundedRect {
                rect: [left, centre - height / 2.0, w, height],
                radius: height / 2.0,
                fill: Paint::Solid(fill),
                border: border.map(|edge| Border {
                    width: 1.0,
                    paint: Paint::Solid(edge),
                    highlight: None,
                }),
                glow: None,
            });
        }
        let label = text(words, left + pad, centre, &b.font, ink);
        marks.texts.push(column(label, w - 2.0 * pad + 2.0, Overflow::Clip));
        left += w + gap;
    }
    marks
}

/// A toast's strip across the bottom of the body, and its words: its edge
/// in the colour of its level.
pub(super) fn toast(scene: &mut Scene, notice: &Notice, [x, y, w, h]: [f32; 4], theme: &Theme) {
    let font = theme.row.font;
    let height = font.size * 1.3 + 12.0;
    let inset = 8.0;
    let edge = match notice.level {
        Level::Info => theme.prompt.color,
        Level::Success => theme.matched.color,
        Level::Error => theme.error,
    };
    let rect = [x + inset, y + h - height - inset, w - 2.0 * inset, height];
    let base = *theme.panel.fill.last().unwrap_or(&Rgba::hex(0));
    scene.shapes.push(RoundedRect {
        rect,
        radius: 6.0,
        fill: Paint::Solid(base.with_alpha(0.97)),
        border: Some(Border {
            width: 1.5,
            paint: Paint::Solid(edge),
            highlight: None,
        }),
        glow: Some(Glow {
            radius: 8.0,
            color: edge.with_alpha(0.35),
        }),
    });
    let words = text(
        notice.text.clone(),
        rect[0] + 12.0,
        rect[1] + height / 2.0,
        &font,
        theme.row.columns[0],
    );
    scene.texts.push(column(words, rect[2] - 24.0, Overflow::Ellipsis));
}
