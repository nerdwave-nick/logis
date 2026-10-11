//! A session's list as the user works it: the query, what it matches, the
//! selection and the rows in view. Keys arrive as [`Action`]s; the result
//! is a [`Reaction`]: redraw, accept a [`Choice`], or dismiss.
//!
//! Accepting (fuzzel's dmenu rules): the selected match if any; otherwise
//! the typed text, unless `only-match` forbids it (then Return does
//! nothing). A preselection (`--select`, `--select-index`) applies when its
//! item arrives, until the user moves or edits.

use std::ops::Range;

mod pointer;

use super::filter::{Filter, byte_ranges};
use super::{Options, Preselect};
use crate::input::{Action, Edit, Query, Step};

/// What an action did.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Reaction {
    /// Nothing visible changed.
    Unchanged,
    /// The list must be drawn again.
    Changed,
    /// The session ends with this choice.
    Accept(Choice),
    /// The session ends without a choice.
    Dismiss,
}

/// What the user chose.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Choice {
    /// The item at this index (input order).
    Item(u32),
    /// The typed text, which matched nothing.
    Typed(String),
}

/// One row in view.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Row {
    /// The item's text.
    pub text: String,
    /// The bytes of `text` the query matched (empty when the item is
    /// matched by another text, `--match-nth`).
    pub highlights: Vec<Range<usize>>,
    /// Whether the row is selected.
    pub selected: bool,
}

/// Everything the scene shows, taken from the view at one moment.
#[derive(Clone, Debug, PartialEq, Eq)]
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
}

/// The list of one session.
pub struct ListView {
    options: Options,
    query: Query,
    filter: Filter,
    texts: Vec<String>,
    /// Per item: whether it is matched by its shown text (highlights apply).
    shown_is_matched: Vec<bool>,
    selected: usize,
    top: usize,
    preselect: Option<Preselect>,
    /// The item the user moved the selection to; it stays selected while
    /// more items stream in.
    follow: Option<u32>,
    /// The pointer's state (see [`Self::point`]).
    pointing: pointer::Pointing,
}

impl ListView {
    /// An empty list opening with `options.search` as its query.
    pub fn new(options: Options) -> Self {
        let mut query = Query::new();
        query.apply(&Edit::Insert(options.search.clone()));
        let mut filter = Filter::new();
        filter.set_query(query.text());
        Self {
            preselect: options.select.clone(),
            options,
            query,
            filter,
            texts: Vec::new(),
            shown_is_matched: Vec::new(),
            selected: 0,
            top: 0,
            follow: None,
            pointing: pointer::Pointing::default(),
        }
    }

    /// Adds items: each its shown text and, if it differs, the text it is
    /// matched by. Returns whether the rows in view may have changed.
    pub fn push(&mut self, items: impl IntoIterator<Item = (String, Option<String>)>) -> bool {
        let first = self.texts.len();
        let mut matched = Vec::new();
        for (text, match_text) in items {
            self.shown_is_matched.push(match_text.is_none());
            matched.push(match_text.unwrap_or_else(|| text.clone()));
            self.texts.push(text);
        }
        if matched.is_empty() {
            return false;
        }
        self.filter.push(matched.iter().map(String::as_str));
        if let Some(item) = self.follow {
            self.select_item(item);
        } else if let Some(item) = self.preselected(first) {
            self.preselect = None;
            self.select_item(item);
        }
        self.clamp();
        true
    }

    /// Applies a key's action.
    pub fn apply(&mut self, action: &Action) -> Reaction {
        match action {
            Action::Dismiss => Reaction::Dismiss,
            Action::Accept => match self.selected() {
                Some(item) => Reaction::Accept(Choice::Item(item)),
                None if self.options.only_match => Reaction::Unchanged,
                None => Reaction::Accept(Choice::Typed(self.query.text().to_owned())),
            },
            Action::Select(step) => {
                let row = match step {
                    Step::Previous => self.selected.checked_sub(1),
                    Step::Next => Some(self.selected + 1).filter(|&r| r < self.matched()),
                };
                let Some(row) = row else {
                    return Reaction::Unchanged;
                };
                self.preselect = None;
                self.selected = row;
                self.follow = self.selected();
                self.clamp();
                Reaction::Changed
            }
            Action::Edit(edit) => {
                if !self.query.apply(edit) {
                    return Reaction::Unchanged;
                }
                if self.query.text() != self.filter.query() {
                    self.preselect = None;
                    self.follow = None;
                    self.filter.set_query(self.query.text());
                    self.selected = 0;
                    self.top = 0;
                }
                Reaction::Changed
            }
            // The window turns pastes into edits, and a point needs the
            // surface it was made on: see [`Self::point`].
            Action::Paste(_) | Action::Point(_) => Reaction::Unchanged,
        }
    }

    /// The query typed so far (unmasked).
    pub fn query(&self) -> &str {
        self.query.text()
    }

    /// The selected item, if any matches.
    pub fn selected(&self) -> Option<u32> {
        self.filter.matches().get(self.selected).map(|m| m.index)
    }

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
                Row {
                    text,
                    highlights,
                    selected: self.top + i == self.selected,
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

    fn matched(&self) -> usize {
        self.filter.matches().len()
    }

    /// The preselected item, if it is among the items from `first` on.
    fn preselected(&self, first: usize) -> Option<u32> {
        let found = match self.preselect.as_ref()? {
            Preselect::Index(i) => usize::try_from(*i).ok().filter(|&i| i < self.texts.len()),
            Preselect::Text(want) => {
                let want = want.to_lowercase();
                (first..self.texts.len()).find(|&i| self.texts[i].to_lowercase() == want)
            }
        };
        found.and_then(|i| u32::try_from(i).ok())
    }

    /// Selects `item` if it matches.
    fn select_item(&mut self, item: u32) {
        if let Some(row) = self.filter.matches().iter().position(|m| m.index == item) {
            self.selected = row;
        }
    }

    /// Keeps the selection on a match and in view.
    fn clamp(&mut self) {
        let lines = (self.options.lines as usize).max(1);
        self.selected = self.selected.min(self.matched().saturating_sub(1));
        if self.selected < self.top {
            self.top = self.selected;
        } else if self.selected >= self.top + lines {
            self.top = self.selected + 1 - lines;
        }
    }
}

#[cfg(test)]
#[allow(clippy::single_range_in_vec_init)] // one highlighted range is a case
mod tests {
    use super::*;

    fn items(texts: &[&str]) -> Vec<(String, Option<String>)> {
        texts.iter().map(|t| (t.to_string(), None)).collect()
    }

    fn view(options: Options, texts: &[&str]) -> ListView {
        let mut v = ListView::new(options);
        v.push(items(texts));
        v
    }

    fn texts(look: &Look) -> Vec<&str> {
        look.rows.iter().map(|r| r.text.as_str()).collect()
    }

    fn typed(v: &mut ListView, text: &str) {
        v.apply(&Action::Edit(Edit::Insert(text.into())));
    }

    const DOWN: Action = Action::Select(Step::Next);
    const UP: Action = Action::Select(Step::Previous);

    #[test]
    fn rows_show_at_most_lines_with_the_first_selected() {
        let options = Options {
            lines: 2,
            ..Options::default()
        };
        let mut v = view(options, &["a", "b", "c"]);
        let look = v.look();
        assert_eq!(texts(&look), ["a", "b"]);
        assert!(look.rows[0].selected && !look.rows[1].selected);
        assert_eq!(v.selected(), Some(0));
    }

    #[test]
    fn the_selection_moves_and_the_rows_scroll_to_keep_it_in_view() {
        let options = Options {
            lines: 2,
            ..Options::default()
        };
        let mut v = view(options, &["a", "b", "c"]);
        assert_eq!(v.apply(&UP), Reaction::Unchanged);
        assert_eq!(v.apply(&DOWN), Reaction::Changed);
        assert_eq!(v.apply(&DOWN), Reaction::Changed);
        assert_eq!(
            v.apply(&DOWN),
            Reaction::Unchanged,
            "the last item stops it"
        );
        let look = v.look();
        assert_eq!(texts(&look), ["b", "c"]);
        assert!(look.rows[1].selected);
        v.apply(&UP);
        v.apply(&UP);
        assert_eq!(texts(&v.look()), ["a", "b"]);
    }

    #[test]
    fn typing_filters_highlights_and_reselects_the_best() {
        let mut v = view(Options::default(), &["xaxbc", "abc", "zzz"]);
        v.apply(&DOWN);
        typed(&mut v, "abc");
        let look = v.look();
        assert_eq!(texts(&look), ["abc", "xaxbc"]);
        assert!(look.rows[0].selected);
        assert_eq!(look.rows[0].highlights, [0..3]);
        assert_eq!(look.rows[1].highlights, [1..2, 3..5]);
        assert_eq!((look.input.as_str(), look.cursor), ("abc", Some(3)));
    }

    #[test]
    fn return_accepts_the_selected_match() {
        let mut v = view(Options::default(), &["a", "b"]);
        v.apply(&DOWN);
        assert_eq!(v.apply(&Action::Accept), Reaction::Accept(Choice::Item(1)));
        assert_eq!(v.apply(&Action::Dismiss), Reaction::Dismiss);
    }

    #[test]
    fn return_without_a_match_accepts_the_typed_text_unless_only_match() {
        let mut v = view(Options::default(), &["a"]);
        typed(&mut v, "zz top");
        assert_eq!(
            v.apply(&Action::Accept),
            Reaction::Accept(Choice::Typed("zz top".into()))
        );
        let mut strict = view(
            Options {
                only_match: true,
                ..Options::default()
            },
            &["a"],
        );
        typed(&mut strict, "zz");
        assert_eq!(strict.apply(&Action::Accept), Reaction::Unchanged);
    }

    #[test]
    fn an_empty_list_accepts_the_typed_text() {
        let mut v = view(Options::default(), &[]);
        assert_eq!(
            v.apply(&Action::Accept),
            Reaction::Accept(Choice::Typed(String::new()))
        );
    }

    #[test]
    fn items_streaming_in_keep_the_item_the_user_moved_to() {
        let mut v = view(Options::default(), &["xaxb1", "xaxb2"]);
        typed(&mut v, "ab");
        v.apply(&DOWN);
        assert_eq!(v.selected(), Some(1));
        // A better match arrives and sorts first.
        assert!(v.push(items(&["ab"])));
        assert_eq!(v.selected(), Some(1), "still xaxb2");
        assert_eq!(texts(&v.look()), ["ab", "xaxb1", "xaxb2"]);
    }

    #[test]
    fn select_preselects_the_first_equal_item_ignoring_case_when_it_arrives() {
        let options = Options {
            select: Some(Preselect::Text("SECOND".into())),
            ..Options::default()
        };
        let mut v = view(options, &["first"]);
        assert_eq!(v.selected(), Some(0));
        v.push(items(&["second", "Second"]));
        assert_eq!(v.selected(), Some(1));
    }

    #[test]
    fn select_index_preselects_once_it_arrives_unless_the_user_moved_first() {
        let options = Options {
            select: Some(Preselect::Index(2)),
            ..Options::default()
        };
        let mut v = view(options.clone(), &["a", "b"]);
        v.push(items(&["c"]));
        assert_eq!(v.selected(), Some(2));
        let mut moved = view(options, &["a", "b"]);
        moved.apply(&DOWN);
        moved.push(items(&["c"]));
        assert_eq!(moved.selected(), Some(1));
    }

    #[test]
    fn search_opens_with_a_query() {
        let options = Options {
            search: "b".into(),
            ..Options::default()
        };
        let mut v = view(options, &["a", "b"]);
        assert_eq!(v.query(), "b");
        assert_eq!(texts(&v.look()), ["b"]);
    }

    #[test]
    fn a_password_is_masked_per_character() {
        let options = Options {
            password: Some('*'),
            ..Options::default()
        };
        let mut v = view(options, &[]);
        typed(&mut v, "pä");
        v.apply(&Action::Edit(Edit::Left));
        let look = v.look();
        assert_eq!((look.input.as_str(), look.cursor), ("**", Some(1)));
        assert_eq!(v.query(), "pä");
    }

    #[test]
    fn an_empty_query_shows_the_placeholder_without_a_cursor() {
        let options = Options {
            placeholder: "seek the rite".into(),
            ..Options::default()
        };
        let mut v = view(options, &[]);
        let look = v.look();
        assert_eq!((look.input.as_str(), look.cursor), ("seek the rite", None));
        typed(&mut v, "x");
        assert_eq!(v.look().cursor, Some(1));
    }

    #[test]
    fn the_counter_counts_matches_of_all() {
        let options = Options {
            counter: true,
            ..Options::default()
        };
        let mut v = view(options, &["ab", "b", "c"]);
        typed(&mut v, "b");
        assert_eq!(v.look().counter, Some((2, 3)));
        assert_eq!(view(Options::default(), &["a"]).look().counter, None);
    }

    #[test]
    fn items_matched_by_another_text_are_not_highlighted() {
        let mut v = ListView::new(Options::default());
        v.push([("Shown".to_owned(), Some("hidden key".to_owned()))]);
        typed(&mut v, "key");
        let look = v.look();
        assert_eq!(texts(&look), ["Shown"]);
        assert!(look.rows[0].highlights.is_empty());
        typed(&mut v, "x");
        assert!(v.look().rows.is_empty());
    }
}
