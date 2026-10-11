//! The rows: each a single line whose tab-separated columns take the
//! theme's column colours, matched characters set apart in the match
//! colour, face and underline; an emblem column when some item has one;
//! badges and the pending mark at the end. The selected row lies on the
//! band, which may glide.

use std::ops::Range;

use super::{Grid, Style, column, icon_size, marks, text};
use crate::icons::IconSource;
use crate::list::view::{Look, Row};
use crate::render::{Border, Glow, ImageQuad, Overflow, Paint, Rgba, RoundedRect, Scene};
use crate::theme::Theme;

/// The selection band on the row whose top is `top`, with its edge bar.
pub(super) fn band(scene: &mut Scene, body: [f32; 4], top: f32, theme: &Theme) {
    let s = &theme.selection;
    let [x, _, w, _] = body;
    let pitch = theme.row.height;
    let left = x + s.inset;
    let width = (w - 2.0 * s.inset).max(0.0);
    scene.shapes.push(RoundedRect {
        rect: [left, top + 1.0, width, pitch - 2.0],
        radius: 2.0,
        fill: Paint::ramp([x, 0.0], [x + w, 0.0], &s.band),
        border: (s.outline.a > 0.0).then_some(Border {
            width: 1.0,
            paint: Paint::Solid(s.outline),
            highlight: None,
        }),
        glow: (s.glow.a > 0.0).then_some(Glow {
            radius: 14.0,
            color: s.glow,
        }),
    });
    if s.edge.a > 0.0 {
        scene.shapes.push(RoundedRect {
            rect: [left + 6.0, top + 7.0, 3.0, (pitch - 14.0).max(1.0)],
            radius: 1.5,
            fill: Paint::Solid(s.edge),
            border: None,
            glow: Some(Glow {
                radius: 8.0,
                color: s.edge.with_alpha(s.edge.a * 0.9),
            }),
        });
    }
}

/// Every row in view that fits, icons from `icons`.
pub(super) fn rows(
    scene: &mut Scene,
    look: &Look,
    grid: &Grid,
    style: Style<'_>,
    icons: &mut dyn IconSource,
) {
    let theme = style.theme;
    let font = theme.row.font;
    let side = icon_size(&font);
    let emblem = side as f32 + font.size * 0.4;
    for (i, row) in look.rows.iter().take(grid.rows).enumerate() {
        let top = grid.top(i);
        let centre = top + grid.pitch / 2.0;
        let fade = if row.pending {
            theme.pending.opacity
        } else {
            1.0
        };
        let mut left = grid.left + theme.row.padding;
        let mut width = grid.right - theme.row.padding - left;
        if look.glyphs {
            let icon = row.icon.as_deref().and_then(|name| icons.image(name, side));
            match (icon, &row.glyph) {
                (Some(image), _) => scene.images.push(ImageQuad {
                    image,
                    origin: [left, centre - side as f32 / 2.0],
                    opacity: fade,
                }),
                (None, Some(glyph)) => {
                    let color = if row.selected {
                        theme.selection.columns[0]
                    } else {
                        theme.row.glyph
                    };
                    let t = text(glyph.clone(), left, centre, &font, faded(color, fade));
                    scene.texts.push(column(t, emblem - 4.0, Overflow::Clip));
                }
                (None, None) => {}
            }
            left += emblem;
            width -= emblem;
        }
        let ends = marks::marks(row, left + width, centre, style);
        width -= ends.width;
        scene.texts.push(row_text(row, left, centre, width, theme));
        scene.shapes.extend(ends.shapes);
        scene.texts.extend(ends.texts);
    }
}

fn faded(c: Rgba, fade: f32) -> Rgba {
    c.with_alpha(c.a * fade)
}

/// A row's line: its columns coloured, its matches set apart (none while
/// it is pending).
fn row_text(row: &Row, left: f32, centre: f32, width: f32, theme: &Theme) -> crate::render::Text {
    let font = theme.row.font;
    let (columns, matched) = match (row.pending, row.selected) {
        (true, _) => {
            let c = faded(theme.pending.color, theme.pending.opacity);
            (vec![c], c)
        }
        (false, true) => (theme.selection.columns.clone(), theme.matched.selected),
        (false, false) => (theme.row.columns.clone(), theme.matched.color),
    };
    let highlights: &[Range<usize>] = if row.pending { &[] } else { &row.highlights };
    let mut t = column(
        text(row.text.clone(), left, centre, &font, columns[0]),
        width,
        Overflow::Ellipsis,
    );
    t.spans = spans(&row.text, &columns, highlights, matched);
    t.emphasis = highlights.to_vec();
    t.emphasis_face = Some(theme.matched.face);
    t.underline_emphasis = theme.matched.underline;
    t
}

/// The colour spans of a row: each tab-separated column in its colour (the
/// last colour for further columns), cut where a match begins or ends so
/// that matches take the match colour.
fn spans(
    text: &str,
    columns: &[Rgba],
    highlights: &[Range<usize>],
    matched: Rgba,
) -> Vec<(Range<usize>, Rgba)> {
    let mut cuts: Vec<usize> = vec![0, text.len()];
    cuts.extend(text.match_indices('\t').map(|(i, _)| i + 1));
    cuts.extend(highlights.iter().flat_map(|r| [r.start, r.end]));
    cuts.retain(|&c| c <= text.len() && text.is_char_boundary(c));
    cuts.sort_unstable();
    cuts.dedup();
    let colour_at = |at: usize| {
        if highlights.iter().any(|r| r.contains(&at)) {
            return matched;
        }
        let n = text[..at].matches('\t').count();
        *columns.get(n).or(columns.last()).unwrap_or(&matched)
    };
    cuts.windows(2)
        .map(|w| (w[0]..w[1], colour_at(w[0])))
        .filter(|(r, _)| !r.is_empty())
        .collect()
}

#[cfg(test)]
#[allow(clippy::single_range_in_vec_init)] // one highlighted range is a case
mod tests {
    use super::*;

    const A: Rgba = Rgba::hex(0x111111);
    const B: Rgba = Rgba::hex(0x222222);
    const M: Rgba = Rgba::hex(0xffffff);

    #[test]
    fn columns_take_their_colours_and_the_last_repeats() {
        let s = spans("ab\tcd\tef", &[A, B], &[], M);
        assert_eq!(s, [(0..3, A), (3..6, B), (6..8, B)]);
    }

    #[test]
    fn matches_cut_their_columns() {
        let s = spans("ab\tcd", &[A, B], &[1..2, 3..4], M);
        assert_eq!(s, [(0..1, A), (1..2, M), (2..3, A), (3..4, M), (4..5, B)]);
    }

    #[test]
    fn a_line_without_tabs_is_one_column() {
        assert_eq!(spans("rite", &[A, B], &[], M), [(0..4, A)]);
        assert_eq!(spans("", &[A], &[], M), []);
    }
}
