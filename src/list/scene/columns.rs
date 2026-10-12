//! How wide a list's columns are: measured with the renderer's fonts from
//! the rows the list shows, so tab-separated columns line up the way the
//! design canvas draws them.
//!
//! **Policy (stable widths).** A column is as wide as the widest text that
//! has stood in it in any row shown since the list opened (each column of a
//! row but its last, which takes the rest of the row). Widths only grow:
//! filtering or scrolling to narrower rows never pulls a column in, so the
//! layout moves only when a row wider than any before comes into view, and
//! never on a keystroke that only narrows the list. The theme caps each
//! column at a share of the row (`row.column-max`); wider text ends in an
//! ellipsis. A new list (a new session or a provider's next stage) starts
//! afresh, as does a change of the row's font.
//!
//! **Cost.** Only rows in view are measured, each once (remembered by its
//! text), so 100 000 items cost no more than the nine rows shown: shaping
//! stays off the paths that see every item (arrival, filtering).

use std::collections::HashSet;

use super::text;
use crate::list::view::{Look, Row};
use crate::render::{Measure, Rgba};
use crate::theme::{Face, Font, Theme};

/// How many measured rows are remembered before the memory starts again
/// (the widths stay; a row forgotten is only measured again).
const REMEMBERED: usize = 4096;

/// The widths of one list's columns, as they grew.
#[derive(Clone, Debug, Default)]
pub struct Columns {
    /// Per column but the last: the widest text measured in it.
    widths: Vec<f32>,
    /// The rows already measured, by their text.
    seen: HashSet<String>,
    /// The row font (and its face) the widths are in.
    font: Option<(Font, Option<Face>)>,
}

impl Columns {
    /// Measures the rows of `look` not measured before and gives `look` the
    /// columns' widths. A row that cannot be measured (no fonts yet) is
    /// estimated for this frame and measured on a later one.
    pub fn fit(&mut self, look: &mut Look, theme: &Theme, measure: &mut dyn Measure) {
        let font = theme.row.font;
        let key = (font, theme.faces.get(font.face).cloned());
        if self.font.as_ref() != Some(&key) {
            *self = Self {
                font: Some(key),
                ..Self::default()
            };
        }
        let fresh: Vec<Row> = look
            .rows
            .iter()
            .filter(|r| !self.seen.contains(&r.text))
            .cloned()
            .collect();
        let (widths, measured) = widest(&fresh, &font, measure);
        let mut shown = self.widths.clone();
        grow(&mut shown, &widths);
        if measured {
            self.widths = shown.clone();
            if self.seen.len() + fresh.len() > REMEMBERED {
                self.seen.clear();
            }
            self.seen.extend(fresh.into_iter().map(|r| r.text));
        }
        look.columns = shown;
    }
}

/// Per column but each row's last, the widest text of `rows` in `font`,
/// and whether every one was measured (else some were estimated).
pub(super) fn widest(rows: &[Row], font: &Font, measure: &mut dyn Measure) -> (Vec<f32>, bool) {
    let mut widths = Vec::new();
    let mut measured = true;
    for row in rows {
        let mut cells: Vec<&str> = row.text.split('\t').collect();
        cells.pop();
        for (i, cell) in cells.into_iter().enumerate() {
            let line = text(cell, 0.0, 0.0, font, Rgba::TRANSPARENT);
            let width = measure.width(&line).unwrap_or_else(|| {
                measured = false;
                estimate(cell, font)
            });
            if widths.len() <= i {
                widths.resize(i + 1, 0.0);
            }
            widths[i] = f32::max(widths[i], width);
        }
    }
    (widths, measured)
}

/// Each of `widths` at least as wide as the same one of `other`.
fn grow(widths: &mut Vec<f32>, other: &[f32]) {
    if widths.len() < other.len() {
        widths.resize(other.len(), 0.0);
    }
    for (w, o) in widths.iter_mut().zip(other) {
        *w = w.max(*o);
    }
}

/// An unmeasured line's width: its characters at an average advance.
fn estimate(words: &str, font: &Font) -> f32 {
    words.chars().count() as f32 * super::advance(font)
}

#[cfg(test)]
mod tests;
