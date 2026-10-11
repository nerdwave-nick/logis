//! An item as a list shows it: its text, the text it is matched by, and
//! what a provider may add to it (a glyph or icon, badges, a pending state).

/// One item of a list.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Entry {
    /// The text shown; tabs advance to tab stops, so columns line up.
    pub text: String,
    /// The text matched instead of `text` (`--match-nth`, or a provider's
    /// `match`); `None` matches `text`, and only then are matches highlighted.
    pub match_text: Option<String>,
    /// What a provider adds; `None` for a plain dmenu line, which keeps
    /// 100 000 lines small.
    pub decor: Option<Box<Decor>>,
}

/// A provider's additions to an item.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Decor {
    /// A short text drawn in the icon column (an emblem such as `⚙`).
    pub glyph: Option<String>,
    /// A freedesktop icon name or an absolute path, drawn in the icon column
    /// in place of the glyph once the list draws icons.
    pub icon: Option<String>,
    /// Short labels drawn at the row's end, such as "current".
    pub badges: Vec<String>,
    /// The item's data has not settled: it is shown as pending, never
    /// accepted and never preselected.
    pub pending: bool,
}

impl Entry {
    /// Whether the item is pending.
    pub fn is_pending(&self) -> bool {
        self.decor.as_ref().is_some_and(|d| d.pending)
    }

    /// Whether the item has something for the icon column.
    pub fn has_emblem(&self) -> bool {
        self.decor
            .as_ref()
            .is_some_and(|d| d.glyph.is_some() || d.icon.is_some())
    }
}

/// A dmenu line: its shown text and, if it differs, its match text.
impl From<(String, Option<String>)> for Entry {
    fn from((text, match_text): (String, Option<String>)) -> Self {
        Self {
            text,
            match_text,
            decor: None,
        }
    }
}

/// A plain item matched by its text.
impl From<String> for Entry {
    fn from(text: String) -> Self {
        Self {
            text,
            match_text: None,
            decor: None,
        }
    }
}
