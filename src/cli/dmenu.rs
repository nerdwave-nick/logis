//! The dmenu runes (spec §4.6), fuzzel-compatible: what the list shows
//! and how the choice is printed. A value follows its flag as `--flag=V`
//! or `--flag V` (`--password` takes its character only as `--password=C`).

use std::ffi::OsString;

use logis::list::{Options, Preselect};

use super::ParseError;
use crate::nth::Nth;

/// What a dmenu session reads, shows and prints.
#[derive(Debug, PartialEq)]
pub struct Dmenu {
    /// What ends an input line: `\n`, or NUL for `--dmenu0`.
    pub separator: u8,
    /// Whether stdin is read at all (`--prompt-only` reads none).
    pub read_stdin: bool,
    /// Exit at once, without a window, when stdin is empty.
    pub no_run_if_empty: bool,
    /// How the list looks and behaves.
    pub view: Options,
    /// Print the chosen item's index instead of its text.
    pub index: bool,
    /// The columns shown (`--with-nth`).
    pub with_nth: Option<Nth>,
    /// The columns printed (`--accept-nth`).
    pub accept_nth: Option<Nth>,
    /// The columns matched (`--match-nth`).
    pub match_nth: Option<Nth>,
    /// The column delimiter (`--nth-delimiter`), tab by default.
    pub delimiter: u8,
}

impl Default for Dmenu {
    fn default() -> Self {
        Self {
            separator: b'\n',
            read_stdin: true,
            no_run_if_empty: false,
            view: Options::default(),
            index: false,
            with_nth: None,
            accept_nth: None,
            match_nth: None,
            delimiter: b'\t',
        }
    }
}

/// Collects the dmenu runes of one command line.
#[derive(Default)]
pub struct Runes {
    dmenu: Dmenu,
    /// The first dmenu rune seen, for the error when there is no `--dmenu`.
    first: Option<String>,
    select: bool,
    select_index: bool,
}

impl Runes {
    /// Takes `arg` (and its value from `rest`) if it is a dmenu rune.
    pub fn take(
        &mut self,
        arg: &str,
        rest: &mut impl Iterator<Item = OsString>,
    ) -> Result<bool, ParseError> {
        let (name, inline) = match arg.split_once('=') {
            Some((name, value)) if name.starts_with("--") => (name, Some(value.to_owned())),
            _ => (arg, None),
        };
        let d = &mut self.dmenu;
        let mut value = |flag: &'static str, expected: &'static str| {
            inline
                .clone()
                .or_else(|| rest.next().map(|v| v.to_string_lossy().into_owned()))
                .ok_or(ParseError::BadValue {
                    flag,
                    value: None,
                    expected,
                })
        };
        match name {
            "--prompt" | "-p" => d.view.prompt = value("--prompt", "the prompt's text")?,
            "--prompt-only" => {
                d.view.prompt = value("--prompt-only", "the prompt's text")?;
                d.view.lines = 0;
                d.read_stdin = false;
            }
            "--placeholder" => {
                d.view.placeholder = value("--placeholder", "the placeholder's text")?
            }
            "--password" => d.view.password = Some(password(inline.as_deref())?),
            "--lines" | "-l" => d.view.lines = number("--lines", &value("--lines", LINES)?, LINES)?,
            "--width" | "-w" => {
                d.view.width = number("--width", &value("--width", WIDTH)?, WIDTH)?;
                if d.view.width == 0 {
                    return Err(bad("--width", "0", WIDTH));
                }
            }
            "--counter" => d.view.counter = true,
            "--search" => d.view.search = value("--search", "the opening query")?,
            "--select" => {
                d.view.select = Some(Preselect::Text(value("--select", "an item's text")?));
                self.select = true;
            }
            "--select-index" => {
                let v = value("--select-index", INDEX)?;
                d.view.select = Some(Preselect::Index(number("--select-index", &v, INDEX)?));
                self.select_index = true;
            }
            "--only-match" => d.view.only_match = true,
            "--no-run-if-empty" | "-R" => d.no_run_if_empty = true,
            "--index" => d.index = true,
            "--with-nth" => d.with_nth = nth("--with-nth", &value("--with-nth", NTH)?)?,
            "--accept-nth" => d.accept_nth = nth("--accept-nth", &value("--accept-nth", NTH)?)?,
            "--match-nth" => d.match_nth = nth("--match-nth", &value("--match-nth", NTH)?)?,
            "--nth-delimiter" => d.delimiter = delimiter(&value("--nth-delimiter", DELIMITER)?)?,
            _ => return Ok(false),
        }
        self.first.get_or_insert_with(|| name.to_owned());
        Ok(true)
    }

    /// The runes for a `--dmenu` (NUL-separated with `nul`) session.
    pub fn finish(mut self, nul: bool) -> Result<Dmenu, ParseError> {
        if self.select && self.select_index {
            return Err(ParseError::Conflict("--select", "--select-index"));
        }
        if nul {
            self.dmenu.separator = 0;
        }
        Ok(self.dmenu)
    }

    /// Refuses dmenu runes outside a dmenu session.
    pub fn none_given(&self) -> Result<(), ParseError> {
        match &self.first {
            Some(rune) => Err(ParseError::DmenuOnly(rune.clone())),
            None => Ok(()),
        }
    }
}

const LINES: &str = "a number of rows (0 or more)";
const WIDTH: &str = "a width in characters (1 or more)";
const INDEX: &str = "an item's index (0 or more)";
const NTH: &str = "a column number or a format with {N}, {N..M} or {N..}";
const DELIMITER: &str = "one ASCII character";

fn bad(flag: &'static str, value: &str, expected: &'static str) -> ParseError {
    ParseError::BadValue {
        flag,
        value: Some(value.to_owned()),
        expected,
    }
}

fn number<T: std::str::FromStr>(
    flag: &'static str,
    text: &str,
    expected: &'static str,
) -> Result<T, ParseError> {
    text.parse().map_err(|_| bad(flag, text, expected))
}

fn nth(flag: &'static str, text: &str) -> Result<Option<Nth>, ParseError> {
    Nth::parse(text).map_err(|e| ParseError::Refused {
        flag,
        reason: e.to_string(),
    })
}

fn delimiter(text: &str) -> Result<u8, ParseError> {
    match text.as_bytes() {
        [b] if b.is_ascii() => Ok(*b),
        _ => Err(bad("--nth-delimiter", text, DELIMITER)),
    }
}

fn password(text: Option<&str>) -> Result<char, ParseError> {
    let Some(text) = text else { return Ok('*') };
    let mut chars = text.chars();
    match (chars.next(), chars.next()) {
        (Some(c), None) => Ok(c),
        _ => Err(bad(
            "--password",
            text,
            "one character to show in place of each typed one",
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(args: &[&str]) -> Result<Dmenu, ParseError> {
        let mut runes = Runes::default();
        let mut rest = args.iter().map(OsString::from);
        while let Some(arg) = rest.next() {
            let arg = arg.to_string_lossy().into_owned();
            assert!(runes.take(&arg, &mut rest)?, "not a dmenu rune: {arg}");
        }
        runes.finish(false)
    }

    #[test]
    fn values_follow_their_flag_or_an_equals_sign() {
        let d = parse(&[
            "--prompt",
            "rite? ",
            "--lines=4",
            "-w",
            "50",
            "-p",
            "again> ",
        ])
        .unwrap();
        assert_eq!(d.view.prompt, "again> ");
        assert_eq!((d.view.lines, d.view.width), (4, 50));
        let d = parse(&["--placeholder=seek", "--search", "ab"]).unwrap();
        assert_eq!(
            (d.view.placeholder.as_str(), d.view.search.as_str()),
            ("seek", "ab")
        );
    }

    #[test]
    fn the_defaults_are_fuzzels() {
        let d = parse(&[]).unwrap();
        assert_eq!(d, Dmenu::default());
        assert_eq!(
            (d.view.prompt.as_str(), d.view.lines, d.view.width),
            ("> ", 15, 30)
        );
        assert_eq!(
            (d.separator, d.delimiter, d.read_stdin),
            (b'\n', b'\t', true)
        );
    }

    #[test]
    fn prompt_only_reads_nothing_and_shows_no_rows() {
        let d = parse(&["--prompt-only", "pin: "]).unwrap();
        assert_eq!(
            (d.view.prompt.as_str(), d.view.lines, d.read_stdin),
            ("pin: ", 0, false)
        );
    }

    #[test]
    fn password_takes_an_optional_character() {
        assert_eq!(parse(&["--password"]).unwrap().view.password, Some('*'));
        assert_eq!(parse(&["--password=•"]).unwrap().view.password, Some('•'));
        assert!(parse(&["--password=ab"]).is_err());
    }

    #[test]
    fn switches_need_no_value() {
        let d = parse(&["--counter", "--only-match", "--index", "-R"]).unwrap();
        assert!(d.view.counter && d.view.only_match && d.index && d.no_run_if_empty);
    }

    #[test]
    fn columns_are_selected_with_nth_runes() {
        let d = parse(&[
            "--with-nth=2",
            "--accept-nth",
            "{1}",
            "--match-nth=3",
            "--nth-delimiter=,",
        ])
        .unwrap();
        let line = b"a,b,c";
        assert_eq!(d.with_nth.unwrap().apply(line, d.delimiter), b"b");
        assert_eq!(d.accept_nth.unwrap().apply(line, d.delimiter), b"a");
        assert_eq!(d.match_nth.unwrap().apply(line, d.delimiter), b"c");
        assert_eq!(parse(&["--with-nth=0"]).unwrap().with_nth, None);
    }

    #[test]
    fn preselection_is_by_text_or_by_index_not_both() {
        let d = parse(&["--select", "Second"]).unwrap();
        assert_eq!(d.view.select, Some(Preselect::Text("Second".into())));
        let d = parse(&["--select-index=2"]).unwrap();
        assert_eq!(d.view.select, Some(Preselect::Index(2)));
        let e = parse(&["--select=a", "--select-index=1"]).unwrap_err();
        assert!(e.to_string().contains("--select-index"), "{e}");
    }

    #[test]
    fn dmenu0_separates_lines_with_nul() {
        assert_eq!(Runes::default().finish(true).unwrap().separator, 0);
    }

    #[test]
    fn bad_values_are_named_with_their_flag() {
        for (args, flag) in [
            (&["--lines", "many"][..], "--lines"),
            (&["--lines=-1"], "--lines"),
            (&["--width=0"], "--width"),
            (&["--select-index=x"], "--select-index"),
            (&["--with-nth={0}"], "--with-nth"),
            (&["--nth-delimiter=ab"], "--nth-delimiter"),
            (&["--nth-delimiter=é"], "--nth-delimiter"),
            (&["--prompt"], "--prompt"),
        ] {
            let e = parse(args).unwrap_err();
            assert!(e.to_string().contains(flag), "{args:?}: {e}");
        }
    }

    #[test]
    fn other_arguments_are_not_taken() {
        let mut runes = Runes::default();
        let mut rest = std::iter::empty();
        assert_eq!(runes.take("--dmenu", &mut rest), Ok(false));
        assert_eq!(runes.take("--config", &mut rest), Ok(false));
        assert!(runes.none_given().is_ok());
        assert_eq!(runes.take("--index", &mut rest), Ok(true));
        assert_eq!(
            runes.none_given(),
            Err(ParseError::DmenuOnly("--index".into()))
        );
    }
}
