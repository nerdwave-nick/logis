//! The columns' widths: measured from the rows in view, each row once,
//! growing and never shrinking, afresh for another font.

use super::*;
use crate::list::view::Row;
use crate::render::Text;

/// Ten pixels a character, counting what it measured; `None` while off.
#[derive(Default)]
struct Ruler {
    lines: Vec<String>,
    off: bool,
}

impl Measure for Ruler {
    fn width(&mut self, text: &Text) -> Option<f32> {
        if self.off {
            return None;
        }
        self.lines.push(text.text.clone());
        Some(10.0 * text.text.chars().count() as f32)
    }
}

fn look(rows: &[&str]) -> Look {
    Look {
        rows: rows
            .iter()
            .map(|t| Row {
                text: (*t).into(),
                ..Row::default()
            })
            .collect(),
        lines: 9,
        ..Look::default()
    }
}

fn fit(columns: &mut Columns, rows: &[&str], theme: &Theme, ruler: &mut Ruler) -> Vec<f32> {
    let mut l = look(rows);
    columns.fit(&mut l, theme, ruler);
    l.columns
}

#[test]
fn each_column_is_its_widest_text_but_the_last_is_never_measured() {
    let theme = Theme::bundled_default();
    let mut ruler = Ruler::default();
    let widths = fit(
        &mut Columns::default(),
        &[
            "do-not-disturb\ton\tSilence the vox",
            "vox\tauto\tWho hears",
            "lone",
        ],
        &theme,
        &mut ruler,
    );
    assert_eq!(widths, [140.0, 40.0]);
    assert!(
        !ruler
            .lines
            .iter()
            .any(|l| l.contains("vox ") || l == "lone"),
        "last columns are not measured: {:?}",
        ruler.lines
    );
}

#[test]
fn a_row_is_measured_once() {
    let theme = Theme::bundled_default();
    let mut columns = Columns::default();
    let mut ruler = Ruler::default();
    fit(&mut columns, &["a\tb", "cc\td"], &theme, &mut ruler);
    let first = ruler.lines.len();
    fit(&mut columns, &["a\tb", "cc\td"], &theme, &mut ruler);
    fit(&mut columns, &["cc\td", "eee\tf"], &theme, &mut ruler);
    assert_eq!(ruler.lines.len(), first + 1, "only eee is new");
}

#[test]
fn widths_grow_with_wider_rows_and_never_shrink_while_filtering() {
    let theme = Theme::bundled_default();
    let mut columns = Columns::default();
    let mut ruler = Ruler::default();
    assert_eq!(fit(&mut columns, &["abcd\tx"], &theme, &mut ruler), [40.0]);
    // A keystroke narrows the list to a narrower row: nothing moves.
    assert_eq!(fit(&mut columns, &["ab\tx"], &theme, &mut ruler), [40.0]);
    // A wider row comes into view, with a further column.
    assert_eq!(
        fit(&mut columns, &["abcdef\txy\tz"], &theme, &mut ruler),
        [60.0, 20.0]
    );
    assert_eq!(
        fit(&mut columns, &["a\tb"], &theme, &mut ruler),
        [60.0, 20.0]
    );
}

#[test]
fn another_row_font_starts_afresh() {
    let forge = Theme::bundled_default();
    let mut columns = Columns::default();
    let mut ruler = Ruler::default();
    fit(&mut columns, &["abcdef\tx"], &forge, &mut ruler);
    let mut bigger = forge.clone();
    bigger.row.font.size += 4.0;
    assert_eq!(fit(&mut columns, &["ab\tx"], &bigger, &mut ruler), [20.0]);
}

#[test]
fn without_fonts_rows_are_estimated_and_measured_once_fonts_come() {
    let theme = Theme::bundled_default();
    let mut columns = Columns::default();
    let mut ruler = Ruler {
        off: true,
        ..Ruler::default()
    };
    let estimated = fit(&mut columns, &["abcd\tx"], &theme, &mut ruler);
    assert_eq!(estimated, [4.0 * super::super::advance(&theme.row.font)]);
    ruler.off = false;
    assert_eq!(fit(&mut columns, &["abcd\tx"], &theme, &mut ruler), [40.0]);
    assert_eq!(ruler.lines, ["abcd"], "measured once fonts came");
}
