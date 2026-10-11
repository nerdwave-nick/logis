//! The query editor: the committed text and a cursor between graphemes.
//!
//! Every change arrives as an [`Edit`], whatever produced it (a key, a
//! repeat, later a paste or an input method), so the editor never knows
//! about keys. Positions are byte offsets that always sit on grapheme
//! boundaries (UAX #29 extended grapheme clusters).

use unicode_segmentation::UnicodeSegmentation;

/// One change to a [`Query`].
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Edit {
    /// Insert text at the cursor and move the cursor after it.
    Insert(String),
    /// Delete the grapheme before the cursor (Backspace).
    DeleteBack,
    /// Delete the grapheme after the cursor (Delete).
    DeleteForward,
    /// Delete the word before the cursor (Ctrl+W): first the whitespace
    /// right before it, then the run of non-whitespace before that, as in
    /// dmenu and readline's `unix-word-rubout`.
    DeleteWordBack,
    /// Delete everything (Ctrl+U).
    Clear,
    /// Move the cursor one grapheme left.
    Left,
    /// Move the cursor one grapheme right.
    Right,
    /// Move the cursor to the start.
    Home,
    /// Move the cursor to the end.
    End,
}

/// The query being typed: its text and the cursor's byte offset.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Query {
    text: String,
    cursor: usize,
}

impl Query {
    /// An empty query.
    pub fn new() -> Self {
        Self::default()
    }

    /// The committed text.
    pub fn text(&self) -> &str {
        &self.text
    }

    /// The cursor as a byte offset into [`Self::text`], on a grapheme
    /// boundary.
    pub fn cursor(&self) -> usize {
        self.cursor
    }

    /// Applies `edit`; returns whether the text or the cursor changed.
    pub fn apply(&mut self, edit: &Edit) -> bool {
        let before = (self.text.len(), self.cursor);
        let text_changed = match edit {
            Edit::Insert(s) => {
                self.text.insert_str(self.cursor, s);
                self.cursor += s.len();
                !s.is_empty()
            }
            Edit::DeleteBack => self.delete_to(self.previous_boundary()),
            Edit::DeleteForward => self.delete_to(self.next_boundary()),
            Edit::DeleteWordBack => self.delete_to(self.word_start()),
            Edit::Clear => {
                let had = !self.text.is_empty();
                self.text.clear();
                self.cursor = 0;
                had
            }
            Edit::Left => self.move_to(self.previous_boundary()),
            Edit::Right => self.move_to(self.next_boundary()),
            Edit::Home => self.move_to(0),
            Edit::End => self.move_to(self.text.len()),
        };
        text_changed || before != (self.text.len(), self.cursor)
    }

    /// Deletes between the cursor and `to` (either side), leaving the
    /// cursor at the start of the deleted range.
    fn delete_to(&mut self, to: usize) -> bool {
        let (start, end) = if to < self.cursor {
            (to, self.cursor)
        } else {
            (self.cursor, to)
        };
        self.text.replace_range(start..end, "");
        self.cursor = start;
        start != end
    }

    fn move_to(&mut self, to: usize) -> bool {
        let moved = to != self.cursor;
        self.cursor = to;
        moved
    }

    /// The grapheme boundary before the cursor (the cursor at the start).
    fn previous_boundary(&self) -> usize {
        self.text[..self.cursor]
            .grapheme_indices(true)
            .next_back()
            .map_or(0, |(i, _)| i)
    }

    /// The grapheme boundary after the cursor (the cursor at the end).
    fn next_boundary(&self) -> usize {
        self.text[self.cursor..]
            .graphemes(true)
            .next()
            .map_or(self.cursor, |g| self.cursor + g.len())
    }

    /// Where Ctrl+W stops: skip whitespace back, then non-whitespace.
    fn word_start(&self) -> usize {
        let head = &self.text[..self.cursor];
        let words = head.trim_end_matches(char::is_whitespace);
        words.trim_end_matches(|c: char| !c.is_whitespace()).len()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn typed(text: &str) -> Query {
        let mut q = Query::new();
        q.apply(&Edit::Insert(text.to_owned()));
        q
    }

    #[test]
    fn insert_puts_text_at_the_cursor() {
        let mut q = typed("ac");
        assert!(q.apply(&Edit::Left));
        assert!(q.apply(&Edit::Insert("b".into())));
        assert_eq!((q.text(), q.cursor()), ("abc", 2));
        assert!(!q.apply(&Edit::Insert(String::new())));
    }

    #[test]
    fn backspace_deletes_one_whole_grapheme() {
        // e + combining acute, a flag (two regional indicators), and a
        // family emoji joined by ZWJs: each is one grapheme.
        for grapheme in ["e\u{301}", "🇩🇪", "👨‍👩‍👧", "ö"] {
            let mut q = typed(&format!("x{grapheme}"));
            assert!(q.apply(&Edit::DeleteBack), "{grapheme}");
            assert_eq!((q.text(), q.cursor()), ("x", 1), "{grapheme}");
        }
        let mut empty = Query::new();
        assert!(!empty.apply(&Edit::DeleteBack));
    }

    #[test]
    fn delete_forward_removes_the_grapheme_after_the_cursor() {
        let mut q = typed("e\u{301}x");
        q.apply(&Edit::Home);
        assert!(q.apply(&Edit::DeleteForward));
        assert_eq!((q.text(), q.cursor()), ("x", 0));
        q.apply(&Edit::End);
        assert!(!q.apply(&Edit::DeleteForward));
    }

    #[test]
    fn cursor_moves_by_graphemes_and_stops_at_the_ends() {
        let mut q = typed("a🇩🇪b");
        assert!(q.apply(&Edit::Left));
        assert!(q.apply(&Edit::Left));
        assert_eq!(q.cursor(), 1);
        assert!(q.apply(&Edit::Right));
        assert_eq!(q.cursor(), 1 + "🇩🇪".len());
        assert!(q.apply(&Edit::Home));
        assert!(!q.apply(&Edit::Left));
        assert!(q.apply(&Edit::End));
        assert!(!q.apply(&Edit::Right));
        assert_eq!(q.cursor(), q.text().len());
    }

    #[test]
    fn ctrl_w_deletes_trailing_whitespace_and_then_one_word() {
        let mut q = typed("firefox --private  ");
        assert!(q.apply(&Edit::DeleteWordBack));
        assert_eq!(q.text(), "firefox ");
        assert!(q.apply(&Edit::DeleteWordBack));
        assert_eq!(q.text(), "");
        assert!(!q.apply(&Edit::DeleteWordBack));
    }

    #[test]
    fn ctrl_w_deletes_only_before_the_cursor() {
        let mut q = typed("über alles");
        for _ in 0.." alles".len() {
            q.apply(&Edit::Left);
        }
        assert!(q.apply(&Edit::DeleteWordBack));
        assert_eq!((q.text(), q.cursor()), (" alles", 0));
    }

    #[test]
    fn ctrl_u_clears_text_and_cursor() {
        let mut q = typed("Ave Omnissiah");
        assert!(q.apply(&Edit::Clear));
        assert_eq!(q, Query::new());
        assert!(!q.apply(&Edit::Clear));
    }
}
