//! Paste (spec §4.5): which selection, which of its offered MIME types to
//! read, and what of the bytes read reaches the query. The window reads
//! the selection's pipe without blocking its frame loop and hands the bytes
//! here once the owner closes it (or the limit is reached); the text then
//! becomes an ordinary [`super::Edit::Insert`] at the cursor.

/// Which selection a paste reads.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Selection {
    /// The clipboard (Ctrl+V, Shift+Insert): `wl_data_device`.
    Clipboard,
    /// The primary selection (the middle button):
    /// `zwp_primary_selection_device_v1`.
    Primary,
}

impl Selection {
    /// Its name in the test trace.
    pub fn name(self) -> &'static str {
        match self {
            Self::Clipboard => "clipboard",
            Self::Primary => "primary",
        }
    }
}

/// The text MIME types logis reads, best first: UTF-8 declared, then the
/// X11 names for UTF-8, then text without a charset (read as UTF-8 too).
const TEXT: [&str; 5] = [
    "text/plain;charset=utf-8",
    "UTF8_STRING",
    "text/plain",
    "TEXT",
    "STRING",
];

/// The most bytes a paste reads; the query is one line.
pub const LIMIT: usize = 64 * 1024;

/// The offered MIME type to read, as the owner spelled it: the best of
/// [`TEXT`] on offer. `charset` and its value are compared without regard to
/// case or spaces (`text/plain; charset=UTF-8` is UTF-8 too).
pub fn choose(offered: &[String]) -> Option<&str> {
    let normal = |m: &str| m.to_ascii_lowercase().replace(' ', "");
    TEXT.iter().find_map(|want| {
        offered
            .iter()
            .find(|m| normal(m) == want.to_ascii_lowercase())
            .map(String::as_str)
    })
}

/// The bytes of a paste in progress, at most [`LIMIT`].
#[derive(Clone, Debug, Default)]
pub struct Bytes {
    bytes: Vec<u8>,
}

impl Bytes {
    /// Adds what was read; returns whether the limit is reached (stop
    /// reading).
    pub fn push(&mut self, chunk: &[u8]) -> bool {
        let room = LIMIT - self.bytes.len();
        self.bytes
            .extend_from_slice(&chunk[..chunk.len().min(room)]);
        self.bytes.len() >= LIMIT
    }

    /// The text this paste inserts (see [`to_query`]).
    pub fn text(&self) -> String {
        to_query(&self.bytes)
    }
}

/// What pasted bytes insert into the one-line query: the first line that is
/// not empty, decoded as UTF-8 (invalid bytes become U+FFFD, a sequence cut
/// off by the limit is dropped), tabs as spaces and no other control
/// characters. This is dmenu's rule (a paste ends at its first newline),
/// except that leading line breaks are skipped.
pub fn to_query(bytes: &[u8]) -> String {
    let complete = match std::str::from_utf8(bytes) {
        Err(e) if e.error_len().is_none() => &bytes[..e.valid_up_to()],
        _ => bytes,
    };
    let text = String::from_utf8_lossy(complete);
    let line = text
        .trim_start_matches(['\n', '\r'])
        .split(['\n', '\r'])
        .next()
        .unwrap_or_default();
    line.chars()
        .filter_map(|c| match c {
            '\t' => Some(' '),
            c if c.is_control() => None,
            c => Some(c),
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn offered(mimes: &[&str]) -> Vec<String> {
        mimes.iter().map(|m| m.to_string()).collect()
    }

    #[test]
    fn utf8_text_is_preferred_whatever_else_is_offered() {
        let mimes = offered(&[
            "image/png",
            "STRING",
            "text/plain;charset=utf-8",
            "text/plain",
        ]);
        assert_eq!(choose(&mimes), Some("text/plain;charset=utf-8"));
    }

    #[test]
    fn the_owners_spelling_of_the_charset_is_kept() {
        let mimes = offered(&["text/plain; charset=UTF-8"]);
        assert_eq!(choose(&mimes), Some("text/plain; charset=UTF-8"));
    }

    #[test]
    fn x11_names_and_plain_text_follow_in_order() {
        assert_eq!(
            choose(&offered(&["TEXT", "UTF8_STRING"])),
            Some("UTF8_STRING")
        );
        assert_eq!(
            choose(&offered(&["STRING", "text/plain"])),
            Some("text/plain")
        );
        assert_eq!(choose(&offered(&["STRING"])), Some("STRING"));
    }

    #[test]
    fn no_text_on_offer_means_nothing_to_read() {
        assert_eq!(choose(&offered(&["image/png", "text/uri-list"])), None);
        assert_eq!(choose(&[]), None);
    }

    #[test]
    fn a_paste_inserts_its_first_line() {
        assert_eq!(to_query(b"Ave Omnissiah\n"), "Ave Omnissiah");
        assert_eq!(to_query(b"first\r\nsecond"), "first");
        assert_eq!(
            to_query(b"\n\nafter blank lines\nnot this"),
            "after blank lines"
        );
    }

    #[test]
    fn tabs_become_spaces_and_control_characters_go() {
        assert_eq!(to_query(b"a\tb\x07c\x1bd"), "a bcd");
    }

    #[test]
    fn invalid_utf8_is_replaced_and_a_cut_sequence_dropped() {
        assert_eq!(to_query(b"caf\xc3\xa9 \xff!"), "café \u{fffd}!");
        assert_eq!(to_query(b"caf\xc3"), "caf", "cut by the limit");
    }

    #[test]
    fn reading_stops_at_the_limit() {
        let mut bytes = Bytes::default();
        assert!(!bytes.push(&vec![b'a'; LIMIT - 1]));
        assert!(bytes.push(b"bc"), "the limit is reached");
        let text = bytes.text();
        assert_eq!(text.len(), LIMIT);
        assert!(text.ends_with("ab"));
    }
}
