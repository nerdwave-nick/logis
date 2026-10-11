//! What the scene draws of a list at one moment: the prompt line and the
//! rows in view with their highlights and decorations.

use std::ops::Range;

use super::ListView;
use crate::list::Notice;
use crate::list::highlight::byte_ranges;

/// One row in view.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Row {
    /// The item's text.
    pub text: String,
    /// The bytes of `text` the query matched (empty when the item is
    /// matched by another text, `--match-nth`).
    pub highlights: Vec<Range<usize>>,
    /// Whether the row is selected.
    pub selected: bool,
    /// The emblem for the icon column.
    pub glyph: Option<String>,
    /// The icon for the icon column (drawn instead of the glyph when found).
    pub icon: Option<String>,
    /// Labels for the row's end.
    pub badges: Vec<String>,
    /// Whether the item is still pending.
    pub pending: bool,
}

/// Everything the scene shows, taken from the view at one moment.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Look {
    /// Shown before the query.
    pub prompt: String,
    /// The query as shown (masked for a password), or the placeholder.
    pub input: String,
    /// The cursor's byte offset in `input`; `None` while the placeholder shows.
    pub cursor: Option<usize>,
    /// The rows in view, top first.
    pub rows: Vec<Row>,
    /// How many rows the list has room for.
    pub lines: u32,
    /// Matching and total items, when the counter is on.
    pub counter: Option<(usize, usize)>,
    /// Whether rows keep an icon column (some item has an emblem).
    pub glyphs: bool,
    /// A notice shown over the list; the view leaves it to its owner.
    pub toast: Option<Notice>,
}

impl ListView {
    /// What to draw now.
    pub fn look(&mut self) -> Look {
        let lines = self.options.lines as usize;
        let shown: Vec<u32> = self.filter.matches()[self.top..]
            .iter()
            .take(lines)
            .map(|m| m.index)
            .collect();
        let rows = shown
            .into_iter()
            .enumerate()
            .map(|(i, item)| {
                let text = self.texts[item as usize].clone();
                let highlights = if self.shown_is_matched[item as usize] {
                    byte_ranges(&text, &self.filter.positions(item))
                } else {
                    Vec::new()
                };
                let decor = self.decor[item as usize].as_deref();
                Row {
                    text,
                    highlights,
                    selected: self.top + i == self.selected,
                    glyph: decor.and_then(|d| d.glyph.clone()),
                    icon: decor.and_then(|d| d.icon.clone()),
                    badges: decor.map(|d| d.badges.clone()).unwrap_or_default(),
                    pending: decor.is_some_and(|d| d.pending),
                }
            })
            .collect();
        let (input, cursor) = self.input();
        Look {
            prompt: self.options.prompt.clone(),
            input,
            cursor,
            rows,
            lines: self.options.lines,
            counter: self
                .options
                .counter
                .then(|| (self.matched(), self.filter.len())),
            glyphs: self.emblems > 0,
            toast: None,
        }
    }

    /// The query as shown and its cursor, or the placeholder.
    fn input(&self) -> (String, Option<usize>) {
        use unicode_segmentation::UnicodeSegmentation;
        let text = self.query.text();
        if text.is_empty() && !self.options.placeholder.is_empty() {
            return (self.options.placeholder.clone(), None);
        }
        match self.options.password {
            None => (text.to_owned(), Some(self.query.cursor())),
            Some(mask) => {
                let before = text[..self.query.cursor()].graphemes(true).count();
                let all = text.graphemes(true).count();
                let shown: String = std::iter::repeat_n(mask, all).collect();
                (shown, Some(before * mask.len_utf8()))
            }
        }
    }
}
