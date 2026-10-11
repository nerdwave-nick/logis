//! Text in a scene: single lines in logical pixels, each optionally confined
//! to a column, shaped by cosmic-text (bidi, fallback over the resolved
//! faces) in buffer pixels so hinting snaps to the real pixel grid.

use glyphon::cosmic_text::{Align, Ellipsize, EllipsizeHeightLimit, Hinting};
use glyphon::{Attrs, Buffer, Color, Family, Metrics, Shaping, Wrap};

use super::scale::Scale;
use super::scene::Rgba;
use crate::text::Fonts;

/// What happens to a line wider than its column.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Overflow {
    /// Cut at the column's edges.
    Clip,
    /// End with "…" so that the line fits, then cut at the edges.
    #[default]
    Ellipsis,
}

/// One line of text, painted after the shapes. Lengths are logical pixels.
#[derive(Clone, Debug, PartialEq)]
pub struct Text {
    /// What to show. Line breaks are not interpreted; it stays one line.
    pub text: String,
    /// Left and top of the line box.
    pub origin: [f32; 2],
    /// Font size (the em).
    pub size: f32,
    /// Height of the line box; glyphs are centred in it.
    pub line_height: f32,
    /// Colour of ordinary glyphs (colour emoji keep their own).
    pub color: Rgba,
    /// The column width from `origin.x`, if the line is confined to one.
    pub width: Option<f32>,
    /// What a too-wide line does in its column.
    pub overflow: Overflow,
    /// Byte ranges of `text` painted in another colour (match highlights),
    /// ascending and not overlapping.
    pub spans: Vec<(std::ops::Range<usize>, Rgba)>,
    /// Ends the line at its column's end edge instead of starting it at
    /// the start edge.
    pub align_end: bool,
}

impl Text {
    /// A line at `origin` with a line box of 1.3 × `size`, no column.
    pub fn new(text: impl Into<String>, origin: [f32; 2], size: f32, color: Rgba) -> Self {
        Self {
            text: text.into(),
            origin,
            size,
            line_height: size * 1.3,
            color,
            width: None,
            overflow: Overflow::default(),
            spans: Vec::new(),
            align_end: false,
        }
    }

    /// The same line confined to a column of `width`.
    pub fn in_column(self, width: f32, overflow: Overflow) -> Self {
        Self {
            width: Some(width),
            overflow,
            ..self
        }
    }

    /// The column's left and right edges in buffer pixels, if any.
    pub(crate) fn column(&self, scale: Scale) -> Option<(i32, i32)> {
        let f = scale.factor();
        self.width.map(|w| {
            let left = (self.origin[0] * f).round() as i32;
            (left, left + (w.max(0.0) * f).round() as i32)
        })
    }
}

/// Shapes `text` at `scale` into a single-line buffer in buffer pixels.
pub(crate) fn shape(fonts: &mut Fonts, text: &Text, scale: Scale) -> Buffer {
    let f = scale.factor();
    let size = (text.size * f).max(1.0);
    let line_height = (text.line_height * f).max(1.0);
    fonts.ensure(&text.text);
    if text.width.is_some() && text.overflow == Overflow::Ellipsis {
        fonts.ensure("\u{2026}");
    }
    let mut buffer = Buffer::new_empty(Metrics::new(size, line_height));
    let family = fonts.family().to_owned();
    let attrs = Attrs::new().family(Family::Name(&family));
    let system = fonts.system_mut();
    let mut b = buffer.borrow_with(system);
    b.set_wrap(Wrap::None);
    b.set_hinting(Hinting::Enabled);
    let width = text.column(scale).map(|(l, r)| (r - l) as f32);
    b.set_size(width, None);
    if width.is_some() && text.overflow == Overflow::Ellipsis {
        b.set_ellipsize(Ellipsize::End(EllipsizeHeightLimit::Lines(1)));
    }
    // One line: line breaks become spaces rather than new lines (one byte
    // for one, so the spans' byte ranges still hold).
    let line = text.text.replace(['\n', '\r'], " ");
    let align = text.align_end.then_some(Align::End);
    if text.spans.is_empty() {
        b.set_text(&line, &attrs, Shaping::Advanced, align);
    } else {
        let pieces = spans(&line, &text.spans).map(|(range, color)| {
            let attrs = match color {
                Some(c) => attrs.clone().color(glyph_color(c)),
                None => attrs.clone(),
            };
            (&line[range], attrs)
        });
        b.set_rich_text(pieces, &attrs, Shaping::Advanced, align);
    }
    b.shape_until_scroll(false);
    buffer
}

/// A colour as glyphon takes it: sRGB bytes, straight alpha.
pub(crate) fn glyph_color(c: Rgba) -> Color {
    let [r, g, b, a] = [c.r, c.g, c.b, c.a].map(|v| (v.clamp(0.0, 1.0) * 255.0).round() as u8);
    Color::rgba(r, g, b, a)
}

/// `line` cut into consecutive pieces: the spans with their colour, the
/// rest with none. Spans out of order, overlapping or off a character
/// boundary are skipped.
fn spans(
    line: &str,
    spans: &[(std::ops::Range<usize>, Rgba)],
) -> impl Iterator<Item = (std::ops::Range<usize>, Option<Rgba>)> {
    let mut pieces = Vec::new();
    let mut at = 0;
    for (range, color) in spans {
        let fits = at <= range.start
            && range.start < range.end
            && range.end <= line.len()
            && line.is_char_boundary(range.start)
            && line.is_char_boundary(range.end);
        if !fits {
            continue;
        }
        if at < range.start {
            pieces.push((at..range.start, None));
        }
        pieces.push((range.clone(), Some(*color)));
        at = range.end;
    }
    if at < line.len() || pieces.is_empty() {
        pieces.push((at..line.len(), None));
    }
    pieces.into_iter()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::text::FontConfig;

    #[test]
    fn spans_cut_a_line_into_coloured_and_plain_pieces() {
        let red = Rgba::hex(0xff0000);
        let cut: Vec<_> = spans("abcdef", &[(1..2, red), (4..6, red)]).collect();
        assert_eq!(
            cut,
            [
                (0..1, None),
                (1..2, Some(red)),
                (2..4, None),
                (4..6, Some(red))
            ]
        );
        // Off a character boundary, backwards or past the end: skipped.
        let cut: Vec<_> = spans("öb", &[(1..2, red), (3..9, red), (2..3, red)]).collect();
        assert_eq!(cut, [(0..2, None), (2..3, Some(red))]);
        let cut: Vec<_> = spans("", &[]).collect();
        assert_eq!(cut, [(0..0, None)]);
    }

    pub(crate) fn pinned() -> Fonts {
        let dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fonts");
        let files: Vec<_> = [
            "NotoSans-Regular.ttf",
            "NotoSansHebrew-Regular.ttf",
            "NotoSansArabic-Regular.ttf",
            "NotoSansCJKjp-Regular.otf",
            "NotoColorEmoji.ttf",
        ]
        .map(|f| dir.join(f))
        .to_vec();
        let files: Vec<_> = files.iter().map(|p| p.as_path()).collect();
        let rules = "<fontconfig><alias><family>sans-serif</family>\
                     <prefer><family>Noto Sans</family></prefer></alias></fontconfig>";
        Fonts::new(FontConfig::isolated(rules, &files).unwrap(), "sans-serif").unwrap()
    }

    /// The glyphs' source byte offsets, left to right.
    fn visual_order(text: &str) -> (bool, Vec<usize>) {
        let mut fonts = pinned();
        let line = Text::new(text, [0.0, 0.0], 16.0, Rgba::hex(0xffffff));
        let buffer = shape(&mut fonts, &line, Scale::ONE);
        let run = buffer.layout_runs().next().expect("one line");
        let mut glyphs: Vec<_> = run.glyphs.iter().map(|g| (g.x, g.start)).collect();
        glyphs.sort_by(|a, b| a.0.total_cmp(&b.0));
        (run.rtl, glyphs.into_iter().map(|g| g.1).collect())
    }

    #[test]
    fn right_to_left_runs_are_reordered_within_a_left_to_right_line() {
        // "ab " then four Hebrew letters (two bytes each), then " cd".
        let (rtl, order) = visual_order("ab \u{5E9}\u{5DC}\u{5D5}\u{5DD} cd");
        assert!(!rtl);
        let hebrew: Vec<usize> = order
            .iter()
            .copied()
            .filter(|&i| (3..11).contains(&i))
            .collect();
        assert_eq!(hebrew, [9, 7, 5, 3], "Hebrew is drawn right to left");
        assert_eq!(order[..3], [0, 1, 2]);
    }

    #[test]
    fn a_right_to_left_line_runs_right_to_left() {
        let (rtl, order) = visual_order("\u{5E9}\u{5DC}\u{5D5}\u{5DD}");
        assert!(rtl);
        assert_eq!(order, [6, 4, 2, 0]);
    }

    #[test]
    fn line_breaks_stay_on_one_line() {
        let mut fonts = pinned();
        let line = Text::new("a\nb", [0.0, 0.0], 16.0, Rgba::hex(0xffffff));
        assert_eq!(
            shape(&mut fonts, &line, Scale::ONE).layout_runs().count(),
            1
        );
    }
}
