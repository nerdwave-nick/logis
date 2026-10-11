//! JSONC as VS Code and servitor (hujson) read it: JSON plus `//` and
//! `/* */` comments and trailing commas. [`strip`] blanks those out and
//! leaves standard JSON for serde_json.
//!
//! Every byte keeps its offset: comments and trailing commas become spaces
//! and newlines stay where they were, so serde_json's line and column point
//! into the file the user wrote. A small own scanner instead of a JSONC crate
//! (spec §4.11): serde_json is already here, and the scanner is a few dozen
//! lines that keep the positions exact.

/// Where an unterminated block comment begins (1-based line and column, in
/// bytes like serde_json's).
#[derive(Debug, PartialEq, Eq)]
pub struct Unterminated {
    pub line: usize,
    pub column: usize,
}

/// `text` with comments and trailing commas replaced by spaces.
pub fn strip(text: &str) -> Result<String, Unterminated> {
    let bytes = text.as_bytes();
    let mut out = bytes.to_vec();
    let mut i = 0;
    if text.starts_with('\u{feff}') {
        i = '\u{feff}'.len_utf8();
        blank(&mut out[..i]);
    }
    // A comma seen since the last value; blanked if a closing bracket follows.
    let mut comma = None;
    while i < bytes.len() {
        match bytes[i] {
            b'"' => {
                comma = None;
                i += 1;
                while i < bytes.len() {
                    match bytes[i] {
                        b'\\' => i += 2,
                        b'"' => break,
                        _ => i += 1,
                    }
                }
                i += 1;
            }
            b'/' if bytes.get(i + 1) == Some(&b'/') => {
                let end = text[i..].find('\n').map_or(bytes.len(), |n| i + n);
                blank(&mut out[i..end]);
                i = end;
            }
            b'/' if bytes.get(i + 1) == Some(&b'*') => {
                let Some(n) = text[i + 2..].find("*/") else {
                    return Err(locate(text, i));
                };
                let end = i + 2 + n + 2;
                blank(&mut out[i..end]);
                i = end;
            }
            b',' => {
                comma = Some(i);
                i += 1;
            }
            b'}' | b']' => {
                if let Some(c) = comma.take() {
                    out[c] = b' ';
                }
                i += 1;
            }
            b if b.is_ascii_whitespace() => i += 1,
            _ => {
                comma = None;
                i += 1;
            }
        }
    }
    // Only whole UTF-8 sequences (inside comments, or the BOM) were blanked.
    Ok(String::from_utf8_lossy(&out).into_owned())
}

/// Spaces for everything but newlines.
fn blank(bytes: &mut [u8]) {
    for b in bytes.iter_mut().filter(|b| **b != b'\n') {
        *b = b' ';
    }
}

fn locate(text: &str, offset: usize) -> Unterminated {
    let before = &text[..offset];
    let line_start = before.rfind('\n').map_or(0, |n| n + 1);
    Unterminated {
        line: before.matches('\n').count() + 1,
        column: offset - line_start + 1,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn json(text: &str) -> serde_json::Value {
        let stripped = strip(text).unwrap();
        assert_eq!(stripped.len(), text.len(), "offsets moved: {stripped:?}");
        serde_json::from_str(&stripped).unwrap()
    }

    #[test]
    fn plain_json_passes_unchanged() {
        let text = r#"{"a": [1, 2.5, "x"], "b": {"c": null}}"#;
        assert_eq!(strip(text).unwrap(), text);
    }

    #[test]
    fn line_and_block_comments_become_spaces() {
        let text = "// head\n{ /* a */ \"a\": 1 // tail\n}";
        assert_eq!(
            strip(text).unwrap(),
            "       \n{         \"a\": 1        \n}"
        );
    }

    #[test]
    fn newlines_inside_block_comments_are_kept() {
        let text = "{/* one\ntwo */\"a\": 1}";
        assert_eq!(strip(text).unwrap(), "{      \n      \"a\": 1}");
    }

    #[test]
    fn comment_markers_inside_strings_are_text() {
        let text = r#"{"url": "http://x/*y*/", "q": "say \"// hi\""}"#;
        assert_eq!(strip(text).unwrap(), text);
        assert_eq!(json(text)["url"], "http://x/*y*/");
    }

    #[test]
    fn an_escaped_backslash_ends_before_the_closing_quote() {
        let text = r#"{"a": "\\", "b": 1, /* c */}"#;
        assert_eq!(json(text), serde_json::json!({"a": "\\", "b": 1}));
    }

    #[test]
    fn trailing_commas_before_closing_brackets_are_removed() {
        let text = "{\"a\": [1, 2,], \"b\": {\"c\": 3,},}";
        assert_eq!(json(text), serde_json::json!({"a": [1, 2], "b": {"c": 3}}));
    }

    #[test]
    fn a_trailing_comma_followed_by_comments_is_removed() {
        let text = "{\"a\": 1, // last\n /* really */ }";
        assert_eq!(json(text), serde_json::json!({"a": 1}));
    }

    #[test]
    fn a_comma_between_values_is_kept() {
        let text = "{\"a\": 1, /* b */ \"b\": 2}";
        assert_eq!(json(text), serde_json::json!({"a": 1, "b": 2}));
    }

    #[test]
    fn a_double_comma_stays_an_error_for_serde_json() {
        let stripped = strip("[1,,]").unwrap();
        assert!(serde_json::from_str::<serde_json::Value>(&stripped).is_err());
    }

    #[test]
    fn multibyte_text_in_comments_keeps_byte_offsets() {
        let text = "{\"a\": 1 /* ⚙ Omnissiah */, \"b\": \"⚙\" // ⚙\n}";
        let stripped = strip(text).unwrap();
        assert_eq!(stripped.len(), text.len());
        assert_eq!(json(text), serde_json::json!({"a": 1, "b": "⚙"}));
    }

    #[test]
    fn a_byte_order_mark_is_blanked() {
        assert_eq!(json("\u{feff}{\"a\": 1}"), serde_json::json!({"a": 1}));
    }

    #[test]
    fn a_lone_slash_is_left_for_serde_json_to_refuse() {
        assert_eq!(strip("{/}").unwrap(), "{/}");
    }

    #[test]
    fn an_unterminated_block_comment_is_located() {
        let err = strip("{\n  \"a\": 1 /* never\n closed").unwrap_err();
        assert_eq!(
            err,
            Unterminated {
                line: 2,
                column: 10
            }
        );
    }
}
