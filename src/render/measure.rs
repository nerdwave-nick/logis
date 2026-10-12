//! Measuring lines of text as the renderer sets them, for layouts that
//! depend on how wide text is (the list's columns). Shaping is not free, so
//! callers measure only what they need and remember the answers.

use super::scale::Scale;
use super::text::{Text, shape};
use crate::text::Fonts;

/// How wide lines of text are.
pub trait Measure {
    /// The width `text` takes on one line in logical pixels, ignoring its
    /// column, colours, emphasis and caret; `None` when it cannot be
    /// measured (no fonts).
    fn width(&mut self, text: &Text) -> Option<f32>;
}

/// Measures nothing: every width is unknown.
pub struct NoMeasure;

impl Measure for NoMeasure {
    fn width(&mut self, _: &Text) -> Option<f32> {
        None
    }
}

/// Lines measured with the renderer's fonts at a scale: shaped in buffer
/// pixels as they will be drawn, so hinting is the frame's.
pub struct TextMeasure<'a> {
    pub(super) fonts: Option<&'a mut Fonts>,
    pub(super) scale: Scale,
}

impl Measure for TextMeasure<'_> {
    fn width(&mut self, text: &Text) -> Option<f32> {
        let fonts = self.fonts.as_deref_mut()?;
        let line = Text {
            width: None,
            spans: Vec::new(),
            emphasis: Vec::new(),
            caret: None,
            ..text.clone()
        };
        let buffer = shape(fonts, &line, self.scale);
        let width = buffer
            .layout_runs()
            .map(|run| run.line_w)
            .fold(0.0, f32::max);
        Some(width / self.scale.factor())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::render::Rgba;
    use crate::render::text::tests::pinned;

    fn width(fonts: &mut Fonts, words: &str, scale: Scale) -> f32 {
        let line = Text::new(words, [0.0, 0.0], 16.0, Rgba::hex(0xffffff));
        let mut measure = TextMeasure {
            fonts: Some(fonts),
            scale,
        };
        measure.width(&line).unwrap()
    }

    #[test]
    fn a_line_measures_its_glyphs_advances_in_logical_pixels() {
        let mut fonts = pinned();
        let one = width(&mut fonts, "rite", Scale::ONE);
        let two = width(&mut fonts, "rite of oil", Scale::ONE);
        assert!(one > 16.0 && one < 64.0, "{one}");
        assert!(two > 2.0 * one, "{one} {two}");
        assert_eq!(width(&mut fonts, "", Scale::ONE), 0.0);
        let doubled = width(&mut fonts, "rite of oil", Scale::from_integer(2));
        assert!(
            (doubled - two).abs() <= 1.0,
            "logical at any scale: {two} {doubled}"
        );
    }

    #[test]
    fn a_column_does_not_narrow_the_measure_and_nothing_measures_without_fonts() {
        let mut fonts = pinned();
        let line = Text::new("the long litany", [0.0, 0.0], 16.0, Rgba::hex(0xffffff));
        let free = width(&mut fonts, "the long litany", Scale::ONE);
        let mut measure = TextMeasure {
            fonts: Some(&mut fonts),
            scale: Scale::ONE,
        };
        let narrow = line
            .clone()
            .in_column(10.0, crate::render::Overflow::Ellipsis);
        assert_eq!(measure.width(&narrow), Some(free));
        let mut none = TextMeasure {
            fonts: None,
            scale: Scale::ONE,
        };
        assert_eq!(none.width(&line), None);
        assert_eq!(NoMeasure.width(&line), None);
    }
}
