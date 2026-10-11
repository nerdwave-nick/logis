//! A session's list as the user works it: the query, what it matches, the
//! selection and the rows in view. Keys arrive as [`Action`]s; the result
//! is a [`Reaction`]: redraw, accept a [`Choice`], or dismiss.
//!
//! Accepting (fuzzel's dmenu rules): the selected match if any; otherwise
//! the typed text, unless `only-match` forbids it (then Return does
//! nothing). A preselection (`--select`, `--select-index`) applies when its
//! item arrives, until the user moves or edits.

use std::ops::Range;

mod look;
mod pointer;

pub use look::{Look, Row};

use super::filter::Filter;
use super::{Decor, Entry, Options, Preselect};
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
    /// The item at this index, asking for its next stage (Shift+Return).
    Descend(u32),
    /// The typed text, which matched nothing.
    Typed(String),
}

/// The list of one session.
pub struct ListView {
    options: Options,
    query: Query,
    filter: Filter,
    texts: Vec<String>,
    /// Per item: whether it is matched by its shown text (highlights apply).
    shown_is_matched: Vec<bool>,
    /// Per item: a provider's additions (`None` for plain items).
    decor: Vec<Option<Box<Decor>>>,
    /// Per item: whether it was removed.
    removed: Vec<bool>,
    /// How many items have something for the icon column.
    emblems: usize,
    selected: usize,
    top: usize,
    preselect: Option<Preselect>,
    /// The item the user moved the selection to; it stays selected while
    /// more items stream in.
    follow: Option<u32>,
    /// Whether the user has moved the selection or edited the query: no
    /// preselection applies after that.
    touched: bool,
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
            decor: Vec::new(),
            removed: Vec::new(),
            emblems: 0,
            selected: 0,
            top: 0,
            follow: None,
            touched: false,
            pointing: pointer::Pointing::default(),
        }
    }

    /// Adds items: each its shown text and, if it differs, the text it is
    /// matched by. Returns whether the rows in view may have changed.
    pub fn push<E: Into<Entry>>(&mut self, items: impl IntoIterator<Item = E>) -> bool {
        let first = self.texts.len();
        let mut matched = Vec::new();
        for entry in items {
            let entry = entry.into();
            self.emblems += usize::from(entry.has_emblem());
            let Entry {
                text,
                match_text,
                decor,
            } = entry;
            self.shown_is_matched
                .push(highlightable(&text, match_text.as_deref()));
            let boost = decor.as_ref().map_or(0, |d| d.boost);
            matched.push((match_text.unwrap_or_else(|| text.clone()), boost));
            self.texts.push(text);
            self.decor.push(decor);
            self.removed.push(false);
        }
        if matched.is_empty() {
            return false;
        }
        self.filter
            .push_boosted(matched.iter().map(|(text, boost)| (text.as_str(), *boost)));
        self.settle(first..self.texts.len());
        true
    }

    /// Replaces item `index` in its place; whether it existed.
    pub fn update(&mut self, index: u32, entry: Entry) -> bool {
        let i = index as usize;
        if !self.exists(index) {
            return false;
        }
        self.emblems -= usize::from(self.emblem(i));
        self.emblems += usize::from(entry.has_emblem());
        let match_text = entry.match_text.as_deref().unwrap_or(&entry.text);
        self.filter.update(index, match_text);
        self.shown_is_matched[i] = highlightable(&entry.text, entry.match_text.as_deref());
        self.texts[i] = entry.text;
        self.decor[i] = entry.decor;
        self.settle(i..i + 1);
        true
    }

    /// Removes item `index` for good; whether it was there.
    pub fn remove(&mut self, index: u32) -> bool {
        let i = index as usize;
        if !self.exists(index) {
            return false;
        }
        self.emblems -= usize::from(self.emblem(i));
        self.removed[i] = true;
        self.decor[i] = None;
        self.filter.remove(index);
        if self.follow == Some(index) {
            self.follow = None;
        }
        self.clamp();
        true
    }

    /// After the items in `changed` arrived or changed: keeps the item the
    /// user moved to selected, or applies the preselection once its item is
    /// there and settled.
    fn settle(&mut self, changed: Range<usize>) {
        if let Some(item) = self.follow {
            self.select_item(item);
        } else if let Some(item) = self.preselected(changed) {
            self.preselect = None;
            self.select_item(item);
        }
        self.clamp();
    }

    fn exists(&self, index: u32) -> bool {
        self.removed.get(index as usize) == Some(&false)
    }

    fn emblem(&self, i: usize) -> bool {
        self.decor[i]
            .as_ref()
            .is_some_and(|d| d.glyph.is_some() || d.icon.is_some())
    }

    fn pending(&self, index: u32) -> bool {
        self.decor
            .get(index as usize)
            .and_then(Option::as_ref)
            .is_some_and(|d| d.pending)
    }

    /// Selects item `index` once it is there and settled, unless the user
    /// has moved the selection or edited the query; whether it is selected
    /// now.
    pub fn preselect(&mut self, index: u32) -> bool {
        if self.touched {
            return false;
        }
        self.preselect = Some(Preselect::Index(u64::from(index)));
        let i = index as usize;
        if i < self.texts.len() {
            self.settle(i..i + 1);
        }
        self.preselect.is_none()
    }

    /// Applies a key's action.
    pub fn apply(&mut self, action: &Action) -> Reaction {
        match action {
            Action::Dismiss => Reaction::Dismiss,
            Action::Accept | Action::Descend => match self.selected() {
                Some(item) if self.pending(item) => Reaction::Unchanged,
                Some(item) if *action == Action::Descend => Reaction::Accept(Choice::Descend(item)),
                Some(item) => Reaction::Accept(Choice::Item(item)),
                None if self.options.only_match => Reaction::Unchanged,
                None if self.options.required && self.query.text().is_empty() => {
                    Reaction::Unchanged
                }
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
                self.touched = true;
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
                    self.touched = true;
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

    fn matched(&self) -> usize {
        self.filter.matches().len()
    }

    /// The preselected item, if it is among the `changed` items, there and
    /// settled (a pending item is never preselected).
    fn preselected(&self, changed: Range<usize>) -> Option<u32> {
        let found = match self.preselect.as_ref()? {
            Preselect::Index(i) => usize::try_from(*i).ok().filter(|i| changed.contains(i)),
            Preselect::Text(want) => {
                let want = want.to_lowercase();
                changed
                    .clone()
                    .find(|&i| !self.removed[i] && self.texts[i].to_lowercase() == want)
            }
        };
        found
            .and_then(|i| u32::try_from(i).ok())
            .filter(|&i| self.exists(i) && !self.pending(i))
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

/// Whether a match shows on the shown text: it is what is matched, or the
/// match text begins with it (then matches beyond it are not shown).
fn highlightable(text: &str, match_text: Option<&str>) -> bool {
    match_text.is_none_or(|m| m.starts_with(text))
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

    fn decorated(text: &str, decor: Decor) -> Entry {
        Entry {
            text: text.into(),
            match_text: None,
            decor: Some(Box::new(decor)),
        }
    }

    fn pending(text: &str) -> Entry {
        decorated(
            text,
            Decor {
                pending: true,
                ..Decor::default()
            },
        )
    }

    #[test]
    fn a_pending_item_is_never_accepted() {
        let mut v = ListView::new(Options::default());
        v.push([pending("awaited"), Entry::from("ready".to_owned())]);
        assert_eq!(v.selected(), Some(0));
        assert_eq!(v.apply(&Action::Accept), Reaction::Unchanged);
        assert!(v.look().rows[0].pending);
        v.update(0, Entry::from("settled".to_owned()));
        assert_eq!(v.apply(&Action::Accept), Reaction::Accept(Choice::Item(0)));
    }

    #[test]
    fn a_pending_item_is_preselected_only_once_it_settles() {
        let options = Options {
            select: Some(Preselect::Index(1)),
            ..Options::default()
        };
        let mut v = ListView::new(options);
        v.push([Entry::from("a".to_owned()), pending("current")]);
        assert_eq!(v.selected(), Some(0), "pending: not preselected");
        v.update(1, Entry::from("current".to_owned()));
        assert_eq!(v.selected(), Some(1), "settled: preselected");
    }

    #[test]
    fn a_late_preselection_applies_until_the_user_moves() {
        let mut v = view(Options::default(), &["a", "b", "c"]);
        assert!(v.preselect(2));
        assert_eq!(v.selected(), Some(2));
        v.push([pending("d")]);
        assert!(!v.preselect(3), "pending");
        v.update(3, Entry::from("d".to_owned()));
        assert_eq!(v.selected(), Some(3), "applied once settled");
        v.apply(&UP);
        assert!(!v.preselect(0), "the user moved");
        assert_eq!(v.selected(), Some(2));
    }

    #[test]
    fn an_update_changes_an_item_in_place_and_keeps_the_users_choice() {
        let mut v = view(Options::default(), &["a", "b", "c"]);
        v.apply(&DOWN);
        assert!(v.update(2, Entry::from("c2".to_owned())));
        assert_eq!(texts(&v.look()), ["a", "b", "c2"]);
        assert_eq!(v.selected(), Some(1));
        typed(&mut v, "c");
        v.update(0, Entry::from("ccc".to_owned()));
        assert_eq!(texts(&v.look()), ["ccc", "c2"]);
        assert!(!v.update(9, Entry::from("x".to_owned())), "no such item");
    }

    #[test]
    fn a_removed_item_leaves_the_list_and_the_selection_stays_on_a_match() {
        let mut v = view(Options::default(), &["a", "b", "c"]);
        v.apply(&DOWN);
        v.apply(&DOWN);
        assert!(v.remove(2));
        assert_eq!(texts(&v.look()), ["a", "b"]);
        assert_eq!(v.selected(), Some(1));
        assert!(!v.remove(2), "already gone");
        v.update(2, Entry::from("back".to_owned()));
        assert_eq!(texts(&v.look()), ["a", "b"], "a removed item stays gone");
    }

    #[test]
    fn required_text_is_never_empty() {
        let options = Options {
            required: true,
            ..Options::default()
        };
        let mut v = view(options, &[]);
        assert_eq!(v.apply(&Action::Accept), Reaction::Unchanged);
        typed(&mut v, "x");
        assert_eq!(
            v.apply(&Action::Accept),
            Reaction::Accept(Choice::Typed("x".into()))
        );
    }

    #[test]
    fn rows_carry_glyphs_badges_and_pending_and_the_list_reserves_a_glyph_column() {
        let mut v = ListView::new(Options::default());
        assert!(!v.look().glyphs, "no glyph column for plain items");
        let decor = Decor {
            glyph: Some("⚙".into()),
            badges: vec!["current".into()],
            ..Decor::default()
        };
        v.push([decorated("oil", decor), Entry::from("plain".to_owned())]);
        let look = v.look();
        assert!(look.glyphs);
        assert_eq!(look.rows[0].glyph.as_deref(), Some("⚙"));
        assert_eq!(look.rows[0].badges, ["current"]);
        assert_eq!(
            (look.rows[1].glyph.as_deref(), look.rows[1].pending),
            (None, false)
        );
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

    #[test]
    fn a_match_text_that_begins_with_the_shown_text_highlights_it() {
        let mut v = ListView::new(Options::default());
        v.push([(
            "Anoint".to_owned(),
            Some("Anoint\tHoly Oil unguent".to_owned()),
        )]);
        typed(&mut v, "ano");
        assert_eq!(v.look().rows[0].highlights, [0..3]);
        typed(&mut v, " ung");
        let look = v.look();
        assert_eq!(texts(&look), ["Anoint"], "matched beyond the shown text");
        assert_eq!(
            look.rows[0].highlights,
            [0..3],
            "only what is shown is highlighted"
        );
    }

    #[test]
    fn a_boost_ranks_an_item_higher_and_rows_carry_icons() {
        let mut v = ListView::new(Options::default());
        let boosted = Decor {
            icon: Some("forge-cog".into()),
            boost: 30,
            ..Decor::default()
        };
        v.push([
            Entry::from("rite of oil".to_owned()),
            decorated("rite of oil!", boosted),
        ]);
        typed(&mut v, "rite");
        let look = v.look();
        assert_eq!(texts(&look), ["rite of oil!", "rite of oil"]);
        assert_eq!(look.rows[0].icon.as_deref(), Some("forge-cog"));
        assert!(look.glyphs, "an icon needs the icon column");
    }

    #[test]
    fn descend_chooses_the_item_for_its_next_stage() {
        let mut v = view(Options::default(), &["oil", "fire"]);
        v.apply(&Action::Select(Step::Next));
        assert_eq!(
            v.apply(&Action::Descend),
            Reaction::Accept(Choice::Descend(1))
        );
        typed(&mut v, "zz");
        assert_eq!(
            v.apply(&Action::Descend),
            Reaction::Accept(Choice::Typed("zz".into())),
            "typed text has no next stage: Shift+Return is Return"
        );
        let mut p = ListView::new(Options::default());
        p.push([pending("oil")]);
        assert_eq!(p.apply(&Action::Descend), Reaction::Unchanged);
    }
}
