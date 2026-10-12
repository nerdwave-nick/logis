//! The rows: each a single line whose tab-separated columns line up with
//! the other rows' (at the widths [`super::Columns`] measured, or else
//! estimated) and take the theme's column colours, matched characters set
//! apart in the match colour, face and underline; an emblem column when
//! some item has one; badges and the pending mark at the end. The selected
//! row lies on the band, which may glide.

use std::ops::Range;

use super::columns::widest;
use super::{Grid, Style, column, icon_size, marks, text};
use crate::icons::IconSource;
use crate::list::view::{Look, Row};
use crate::render::{
    Border, Glow, ImageQuad, NoMeasure, Overflow, Paint, Rgba, RoundedRect, Scene,
};
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

/// Every row in view that fits, icons from `icons`, its columns lined up
/// at the stops all rows share.
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
    let mut left = grid.left + theme.row.padding;
    let mut width = grid.right - theme.row.padding - left;
    if look.glyphs {
        left += emblem;
        width -= emblem;
    }
    let shown = &look.rows[..look.rows.len().min(grid.rows)];
    let stops = Stops::new(&widths(look, shown, theme), width, theme);
    for (i, row) in shown.iter().enumerate() {
        let top = grid.top(i);
        let centre = top + grid.pitch / 2.0;
        let fade = if row.pending {
            theme.pending.opacity
        } else {
            1.0
        };
        if look.glyphs {
            let at = left - emblem;
            let icon = row.icon.as_deref().and_then(|name| icons.image(name, side));
            match (icon, &row.glyph) {
                (Some(image), _) => scene.images.push(ImageQuad {
                    image,
                    origin: [at, centre - side as f32 / 2.0],
                    opacity: fade,
                }),
                (None, Some(glyph)) => {
                    let color = if row.selected {
                        theme.selection.columns[0]
                    } else {
                        theme.row.glyph
                    };
                    let t = text(glyph.clone(), at, centre, &font, faded(color, fade));
                    scene.texts.push(column(t, emblem - 4.0, Overflow::Clip));
                }
                (None, None) => {}
            }
        }
        let ends = marks::marks(row, left + width, centre, style);
        let room = width - ends.width;
        scene
            .texts
            .extend(cells(row, left, centre, room, &stops, theme));
        scene.shapes.extend(ends.shapes);
        scene.texts.extend(ends.texts);
    }
}

fn faded(c: Rgba, fade: f32) -> Rgba {
    c.with_alpha(c.a * fade)
}

/// The widths of the columns: as measured, or else estimated from `shown`.
fn widths(look: &Look, shown: &[Row], theme: &Theme) -> Vec<f32> {
    if look.columns.is_empty() {
        widest(shown, &theme.row.font, &mut NoMeasure).0
    } else {
        look.columns.clone()
    }
}

/// Where the columns start, from the left of the row's text: each after the
/// one before, at its width (at most the theme's share of the row) and the
/// gap.
struct Stops {
    starts: Vec<f32>,
    gap: f32,
}

impl Stops {
    fn new(widths: &[f32], width: f32, theme: &Theme) -> Self {
        let cap = (theme.row.column_max * width).floor();
        let gap = theme.row.column_gap;
        let mut starts = vec![0.0];
        let mut at = 0.0;
        for w in widths {
            at += w.ceil().min(cap) + gap;
            starts.push(at);
        }
        Self { starts, gap }
    }

    /// Column `i`'s start and, unless it is the row's last, its width: up
    /// to half the gap beyond its text, so a slightly wider line (a bolder
    /// match) still fits.
    fn column(&self, i: usize, last: bool) -> (f32, Option<f32>) {
        let at = |i: usize| self.starts.get(i).copied();
        let start = at(i).unwrap_or_else(|| *self.starts.last().unwrap_or(&0.0));
        let width = (!last)
            .then(|| at(i + 1).map(|next| next - start - self.gap / 2.0))
            .flatten();
        (start, width)
    }
}

/// A row's cells: each column of its text in its colour at its stop, its
/// matches set apart (none while it is pending); the last runs to `room`.
fn cells(
    row: &Row,
    left: f32,
    centre: f32,
    room: f32,
    stops: &Stops,
    theme: &Theme,
) -> Vec<crate::render::Text> {
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
    let ranges = cell_ranges(&row.text);
    let n = ranges.len();
    let mut texts = Vec::with_capacity(n);
    for (i, range) in ranges.into_iter().enumerate() {
        let (start, width) = stops.column(i, i + 1 == n);
        let width = width.unwrap_or(room - start).min(room - start);
        if width < 1.0 {
            break;
        }
        let colour = *columns.get(i).or(columns.last()).unwrap_or(&matched);
        let words = &row.text[range.clone()];
        let mut t = column(
            text(words, left + start, centre, &font, colour),
            width,
            Overflow::Ellipsis,
        );
        let mine = within(highlights, &range);
        t.spans = mine.iter().map(|r| (r.clone(), matched)).collect();
        t.emphasis = mine;
        t.emphasis_face = Some(theme.matched.face);
        t.underline_emphasis = theme.matched.underline;
        texts.push(t);
    }
    texts
}

/// The byte ranges of `text`'s tab-separated columns.
pub(super) fn cell_ranges(text: &str) -> Vec<Range<usize>> {
    let mut at = 0;
    let mut ranges = Vec::new();
    for (tab, _) in text.match_indices('\t') {
        ranges.push(at..tab);
        at = tab + 1;
    }
    ranges.push(at..text.len());
    ranges
}

/// The parts of `highlights` inside `cell`, counted from its start.
fn within(highlights: &[Range<usize>], cell: &Range<usize>) -> Vec<Range<usize>> {
    highlights
        .iter()
        .map(|r| {
            r.start.max(cell.start) - cell.start..r.end.min(cell.end).max(cell.start) - cell.start
        })
        .filter(|r| !r.is_empty())
        .collect()
}

#[cfg(test)]
#[allow(clippy::single_range_in_vec_init)] // one highlighted range is a case
mod tests {
    use super::*;

    #[test]
    fn a_line_is_cut_into_columns_at_its_tabs() {
        assert_eq!(cell_ranges("ab\tcd\tef"), [0..2, 3..5, 6..8]);
        assert_eq!(cell_ranges("rite"), [0..4]);
        assert_eq!(cell_ranges(""), [0..0]);
        assert_eq!(cell_ranges("\tx\t"), [0..0, 1..2, 3..3]);
    }

    #[test]
    fn matches_are_cut_to_their_column() {
        let h = [1..2, 3..6, 7..9];
        assert_eq!(within(&h, &(0..2)), [1..2]);
        assert_eq!(within(&h, &(3..5)), [0..2]);
        assert_eq!(within(&h, &(6..8)), [1..2]);
        assert!(within(&h, &(9..9)).is_empty());
    }
}
