//! The key-file format of desktop entries (Desktop Entry Specification,
//! "Basic format of the file" and "Possible value types"): `[Group]`
//! headers, `Key=Value` and `Key[locale]=Value` lines, `#` comments.
//!
//! - Space around `=` is ignored; a value keeps its other spaces.
//! - Strings unescape `\s`, `\n`, `\t`, `\r` and `\\`; other backslashes
//!   stay (the Exec key's own quoting needs them).
//! - Lists are separated by `;` (`\;` is a literal one; a final `;` is
//!   optional).
//! - The first of duplicate keys counts.
//!
//! A line that is neither a comment, a group header nor a key, or a key
//! before any group, makes the whole file invalid, as GLib's parser does.

use std::fmt;

use super::locale::Locale;

/// A parsed key file.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct KeyFile {
    groups: Vec<Group>,
}

/// One `[Group]` and its keys, values as written (still escaped).
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Group {
    /// The name between the brackets.
    pub name: String,
    entries: Vec<KeyValue>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
struct KeyValue {
    key: String,
    locale: Option<String>,
    value: String,
}

/// Why a key file was refused, and on which line (from 1).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Invalid {
    pub line: usize,
    pub message: String,
}

impl fmt::Display for Invalid {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "line {}: {}", self.line, self.message)
    }
}

/// Parses a key file.
pub fn parse(text: &str) -> Result<KeyFile, Invalid> {
    let mut groups: Vec<Group> = Vec::new();
    for (n, line) in text.lines().enumerate() {
        let invalid = |message: &str| Invalid {
            line: n + 1,
            message: message.to_owned(),
        };
        let trimmed = line.trim_start();
        if trimmed.is_empty() || trimmed.starts_with('#') {
            continue;
        }
        if let Some(header) = trimmed.strip_prefix('[') {
            let name = header
                .trim_end()
                .strip_suffix(']')
                .filter(|n| !n.is_empty() && !n.contains(['[', ']']))
                .ok_or_else(|| invalid("a group header wants [Name]"))?;
            groups.push(Group {
                name: name.to_owned(),
                entries: Vec::new(),
            });
            continue;
        }
        let (key, value) = trimmed
            .split_once('=')
            .ok_or_else(|| invalid("neither a group, a key nor a comment"))?;
        let (key, locale) = split_key(key.trim_end()).ok_or_else(|| invalid("a malformed key"))?;
        let group = groups
            .last_mut()
            .ok_or_else(|| invalid("a key before any group"))?;
        let duplicate = group
            .entries
            .iter()
            .any(|e| e.key == key && e.locale.as_deref() == locale);
        if !duplicate {
            group.entries.push(KeyValue {
                key: key.to_owned(),
                locale: locale.map(str::to_owned),
                value: value.trim_start().to_owned(),
            });
        }
    }
    Ok(KeyFile { groups })
}

/// `Key` or `Key[locale]`; keys are `A-Za-z0-9-`.
fn split_key(key: &str) -> Option<(&str, Option<&str>)> {
    let (name, locale) = match key.split_once('[') {
        Some((name, rest)) => (name, Some(rest.strip_suffix(']')?)),
        None => (key, None),
    };
    let valid = !name.is_empty() && name.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-');
    (valid && locale.is_none_or(|l| !l.is_empty())).then_some((name, locale))
}

impl KeyFile {
    /// The groups in file order.
    pub fn groups(&self) -> &[Group] {
        &self.groups
    }

    /// The first group of this name.
    pub fn group(&self, name: &str) -> Option<&Group> {
        self.groups.iter().find(|g| g.name == name)
    }
}

impl Group {
    /// The unlocalised value of `key`, as written.
    pub fn raw(&self, key: &str) -> Option<&str> {
        self.entries
            .iter()
            .find(|e| e.key == key && e.locale.is_none())
            .map(|e| e.value.as_str())
    }

    /// The value of `key` best suited to `locale` (see [`Locale`]), as written.
    fn raw_localized(&self, key: &str, locale: &Locale) -> Option<&str> {
        self.entries
            .iter()
            .filter(|e| e.key == key)
            .filter_map(|e| match &e.locale {
                Some(l) => locale.rank(l).map(|rank| (rank, &e.value)),
                None => Some((usize::MAX, &e.value)),
            })
            .min_by_key(|(rank, _)| *rank)
            .map(|(_, v)| v.as_str())
    }

    /// A string value, unescaped.
    pub fn string(&self, key: &str) -> Option<String> {
        self.raw(key).map(unescape)
    }

    /// A localised string value, unescaped.
    pub fn localized(&self, key: &str, locale: &Locale) -> Option<String> {
        self.raw_localized(key, locale).map(unescape)
    }

    /// A boolean: `true` is true, anything else (or nothing) false.
    pub fn boolean(&self, key: &str) -> bool {
        self.raw(key).is_some_and(|v| v.trim_end() == "true")
    }

    /// A list of strings, each unescaped.
    pub fn list(&self, key: &str) -> Vec<String> {
        self.raw(key).map(split_list).unwrap_or_default()
    }

    /// A localised list of strings, each unescaped.
    pub fn localized_list(&self, key: &str, locale: &Locale) -> Vec<String> {
        self.raw_localized(key, locale)
            .map(split_list)
            .unwrap_or_default()
    }
}

/// Unescapes `\s`, `\n`, `\t`, `\r` and `\\`; any other backslash stays.
pub fn unescape(value: &str) -> String {
    let mut out = String::with_capacity(value.len());
    let mut chars = value.chars();
    while let Some(c) = chars.next() {
        if c != '\\' {
            out.push(c);
            continue;
        }
        match chars.next() {
            Some('s') => out.push(' '),
            Some('n') => out.push('\n'),
            Some('t') => out.push('\t'),
            Some('r') => out.push('\r'),
            Some('\\') => out.push('\\'),
            Some(other) => {
                out.push('\\');
                out.push(other);
            }
            None => out.push('\\'),
        }
    }
    out
}

/// Splits a list at each `;` that is not escaped, then unescapes each
/// element (`\;` becomes `;`). Empty elements are dropped.
fn split_list(value: &str) -> Vec<String> {
    let mut items = Vec::new();
    let mut current = String::new();
    let mut chars = value.chars();
    while let Some(c) = chars.next() {
        match c {
            '\\' => match chars.next() {
                Some(';') => current.push(';'),
                Some(other) => {
                    current.push('\\');
                    current.push(other);
                }
                None => current.push('\\'),
            },
            ';' => items.push(std::mem::take(&mut current)),
            c => current.push(c),
        }
    }
    items.push(current);
    items
        .into_iter()
        .map(|item| unescape(item.trim_end_matches(['\n', '\r'])))
        .filter(|item| !item.is_empty())
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    const ENTRY: &str = "\
# a comment before the first group
[Desktop Entry]
Type=Application
Name = Rite of Oil
Name[de]=Ölritus
Name[de_AT]=Ölritual
Comment=anoint\\sthe\\tvessel\\\\now\\;
Exec=sh -c \"echo \\\\\"\\$x\\\\\"\"
Keywords=oil;unguent\\;holy;;
Terminal=true
NoDisplay=false
Name=a duplicate

[Desktop Action anoint]
Name=Anoint
";

    #[test]
    fn groups_keys_and_comments_are_read() {
        let file = parse(ENTRY).unwrap();
        let names: Vec<&str> = file.groups().iter().map(|g| g.name.as_str()).collect();
        assert_eq!(names, ["Desktop Entry", "Desktop Action anoint"]);
        let entry = file.group("Desktop Entry").unwrap();
        assert_eq!(entry.raw("Type"), Some("Application"));
        assert_eq!(
            entry.raw("Name"),
            Some("Rite of Oil"),
            "the first duplicate"
        );
        assert_eq!(
            file.group("Desktop Action anoint").unwrap().raw("Name"),
            Some("Anoint")
        );
        assert_eq!(entry.raw("Missing"), None);
    }

    #[test]
    fn strings_unescape_the_spec_escapes_only() {
        let entry = parse(ENTRY)
            .unwrap()
            .group("Desktop Entry")
            .cloned()
            .unwrap();
        assert_eq!(
            entry.string("Comment").as_deref(),
            Some("anoint the\tvessel\\now\\;")
        );
        // \\ becomes \, the Exec quoting's own escapes stay for it.
        assert_eq!(
            entry.string("Exec").as_deref(),
            Some("sh -c \"echo \\\"\\$x\\\"\"")
        );
    }

    #[test]
    fn lists_split_at_unescaped_semicolons() {
        let entry = parse(ENTRY)
            .unwrap()
            .group("Desktop Entry")
            .cloned()
            .unwrap();
        assert_eq!(entry.list("Keywords"), ["oil", "unguent;holy"]);
        assert!(entry.list("Missing").is_empty());
    }

    #[test]
    fn booleans_are_true_only_when_true() {
        let entry = parse(ENTRY)
            .unwrap()
            .group("Desktop Entry")
            .cloned()
            .unwrap();
        assert!(entry.boolean("Terminal"));
        assert!(!entry.boolean("NoDisplay"));
        assert!(!entry.boolean("Hidden"));
    }

    #[test]
    fn localised_values_follow_the_locale() {
        let entry = parse(ENTRY)
            .unwrap()
            .group("Desktop Entry")
            .cloned()
            .unwrap();
        let name = |l: &str| entry.localized("Name", &Locale::parse(l));
        assert_eq!(name("de_AT.UTF-8").as_deref(), Some("Ölritual"));
        assert_eq!(name("de_CH").as_deref(), Some("Ölritus"));
        assert_eq!(name("fr_FR").as_deref(), Some("Rite of Oil"));
        assert_eq!(name("C").as_deref(), Some("Rite of Oil"));
    }

    #[test]
    fn malformed_lines_make_the_file_invalid() {
        for (text, line) in [
            ("[Desktop Entry]\nno equals sign\n", 2),
            ("Name=before any group\n", 1),
            ("[Desktop Entry]\nName[=x\n", 2),
            ("[Desktop Entry\n", 1),
            ("[Desktop Entry]\nNa me=x\n", 2),
        ] {
            assert_eq!(parse(text).map_err(|e| e.line), Err(line), "{text:?}");
        }
    }
}
