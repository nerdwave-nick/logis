//! Column selection for dmenu input (`--with-nth`, `--accept-nth`,
//! `--match-nth`): a line is split into columns at the nth delimiter (tab
//! by default), and a selection is either a column number (1-based) or a
//! format string in which `{N}`, `{N..M}` and `{N..}` expand to columns.
//! Ranges are joined by the delimiter. Everything works on bytes, so the
//! output keeps the input's bytes exactly.

use std::fmt;

/// One part of a selection.
#[derive(Clone, Debug, PartialEq, Eq)]
enum Piece {
    /// Text copied as written.
    Literal(Vec<u8>),
    /// Columns `from` through `to` (inclusive, 1-based); `None` = the last.
    Columns { from: usize, to: Option<usize> },
}

/// A parsed `--*-nth` selection.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Nth(Vec<Piece>);

/// Why a selection was refused.
#[derive(Debug, PartialEq, Eq)]
pub struct NthError(String);

impl fmt::Display for NthError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl Nth {
    /// Parses a selection. `0` means none (`Ok(None)`), as if the flag
    /// were not given.
    pub fn parse(text: &str) -> Result<Option<Self>, NthError> {
        if text == "0" {
            return Ok(None);
        }
        if let Ok(n) = text.parse::<usize>() {
            return Ok(Some(Self(vec![column(n, Some(n), text)?])));
        }
        let mut pieces = Vec::new();
        let mut literal = Vec::new();
        let mut rest = text;
        while let Some(open) = rest.find('{') {
            literal.extend_from_slice(&rest.as_bytes()[..open]);
            let after = &rest[open + 1..];
            match after
                .find('}')
                .and_then(|close| reference(&after[..close], text).transpose())
            {
                Some(piece) => {
                    pieces.extend(
                        (!literal.is_empty()).then(|| Piece::Literal(literal.split_off(0))),
                    );
                    pieces.push(piece?);
                    rest = &after[after.find('}').unwrap_or(0) + 1..];
                }
                None => {
                    literal.push(b'{');
                    rest = after;
                }
            }
        }
        literal.extend_from_slice(rest.as_bytes());
        pieces.extend((!literal.is_empty()).then_some(Piece::Literal(literal)));
        if !pieces.iter().any(|p| matches!(p, Piece::Columns { .. })) {
            return Err(NthError(format!(
                "'{text}' names no column: it wants a column number or a format with {{N}}, {{N..M}} or {{N..}}"
            )));
        }
        Ok(Some(Self(pieces)))
    }

    /// The selection applied to `line`, its columns split at `delimiter`.
    /// Columns past the line's last are empty.
    pub fn apply(&self, line: &[u8], delimiter: u8) -> Vec<u8> {
        let columns: Vec<&[u8]> = line.split(|&b| b == delimiter).collect();
        let mut out = Vec::new();
        for piece in &self.0 {
            match piece {
                Piece::Literal(bytes) => out.extend_from_slice(bytes),
                Piece::Columns { from, to } => {
                    let last = to.unwrap_or(usize::MAX).min(columns.len());
                    let chosen = columns.get(from - 1..last).unwrap_or_default();
                    out.extend_from_slice(&chosen.join(&delimiter));
                }
            }
        }
        out
    }
}

/// `N`, `N..M` or `N..` inside braces as columns; `Ok(None)` if `inner` is
/// not a column reference at all (the braces are then literal text).
fn reference(inner: &str, text: &str) -> Result<Option<Piece>, NthError> {
    let number = |s: &str| {
        (!s.is_empty() && s.bytes().all(|b| b.is_ascii_digit()))
            .then(|| s.parse().ok())
            .flatten()
    };
    match inner.split_once("..") {
        None => match number(inner) {
            Some(n) => column(n, Some(n), text).map(Some),
            None => Ok(None),
        },
        Some((from, "")) => match number(from) {
            Some(n) => column(n, None, text).map(Some),
            None => Ok(None),
        },
        Some((from, to)) => match (number(from), number(to)) {
            (Some(n), Some(m)) => column(n, Some(m), text).map(Some),
            _ => Ok(None),
        },
    }
}

/// Columns `from..=to`, refused when they count from 0 or run backwards.
fn column(from: usize, to: Option<usize>, text: &str) -> Result<Piece, NthError> {
    if from == 0 || to.is_some_and(|to| to < from) {
        return Err(NthError(format!(
            "'{text}' is no column: columns are counted from 1, and a range runs forwards"
        )));
    }
    Ok(Piece::Columns { from, to })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn nth(text: &str) -> Nth {
        Nth::parse(text).unwrap().expect("a selection")
    }

    fn apply(text: &str, line: &str) -> String {
        String::from_utf8(nth(text).apply(line.as_bytes(), b'\t')).unwrap()
    }

    #[test]
    fn a_number_selects_that_column_counted_from_one() {
        assert_eq!(apply("1", "a\tb\tc"), "a");
        assert_eq!(apply("2", "a\tb\tc"), "b");
        assert_eq!(apply("3", "a\tb\tc"), "c");
    }

    #[test]
    fn zero_means_no_selection() {
        assert_eq!(Nth::parse("0"), Ok(None));
    }

    #[test]
    fn a_column_past_the_last_is_empty() {
        assert_eq!(apply("4", "a\tb\tc"), "");
        assert_eq!(apply("2", "no columns"), "");
    }

    #[test]
    fn a_format_expands_columns_inside_free_text() {
        assert_eq!(apply("{2} ({1})", "id7\tFirst"), "First (id7)");
        assert_eq!(apply("[{3}]", "a\tb"), "[]");
    }

    #[test]
    fn ranges_join_their_columns_with_the_delimiter() {
        assert_eq!(apply("{2..3}", "a\tb\tc\td"), "b\tc");
        assert_eq!(apply("{2..}", "a\tb\tc\td"), "b\tc\td");
        assert_eq!(apply("{3..9}", "a\tb\tc\td"), "c\td");
        assert_eq!(apply("{5..}", "a\tb"), "");
    }

    #[test]
    fn another_delimiter_splits_and_joins() {
        let n = nth("{1..2}:{3}");
        assert_eq!(n.apply(b"a,b,c", b','), b"a,b:c");
    }

    #[test]
    fn bytes_are_kept_exactly() {
        let n = nth("2");
        assert_eq!(n.apply(b"\xff\t\xfe\r", b'\t'), b"\xfe\r");
    }

    #[test]
    fn text_that_is_not_a_column_reference_stays_literal() {
        assert_eq!(apply("{x} {} {1}{1", "a"), "{x} {} a{1");
        assert_eq!(apply("{{1}}", "a"), "{a}");
    }

    #[test]
    fn column_zero_and_backward_ranges_are_refused() {
        for bad in ["{0}", "{0..2}", "{3..2}", "-1", "", "{x}"] {
            let e = Nth::parse(bad).unwrap_err();
            assert!(!e.to_string().is_empty(), "{bad}");
        }
    }
}
