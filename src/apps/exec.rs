//! The `Exec` key (Desktop Entry Specification, "The Exec key"): its
//! arguments and their field codes.
//!
//! The key-file escapes are undone first ([`super::keyfile`]); then the value
//! is split into arguments the way a shell splits words, which is what the
//! spec's quoting rules amount to and what GLib accepts:
//! - arguments are separated by spaces;
//! - in double quotes, a backslash escapes `"`, `` ` ``, `$` and `\` (and
//!   stays before any other character);
//! - single quotes quote everything up to the next one (GLib's leniency);
//! - outside quotes, a backslash escapes the next character.
//!
//! Field codes, as a launcher without files to open expands them:
//! - `%f`, `%F`, `%u`, `%U`: no file, so an argument that is only the code
//!   disappears, and inside an argument the code becomes nothing;
//! - `%i`: `--icon <Icon>` (two arguments) when the entry has an icon;
//! - `%c`: the translated Name; `%k`: the desktop file's location;
//! - `%%`: a `%`;
//! - the deprecated `%d %D %n %N %v %m`: removed.
//!
//! Any other field code makes the Exec key (and its entry) invalid.

use std::fmt;

/// Why an Exec value is refused.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ExecError {
    /// No program to run.
    Empty,
    /// A quote that is never closed.
    Unterminated,
    /// A field code the spec does not define.
    UnknownCode(char),
    /// A `%` that ends an argument.
    LonePercent,
}

impl fmt::Display for ExecError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Empty => write!(f, "Exec names no program"),
            Self::Unterminated => write!(f, "Exec has a quote that never closes"),
            Self::UnknownCode(c) => write!(f, "Exec has an unknown field code %{c}"),
            Self::LonePercent => write!(f, "Exec has a lone % (a literal one is %%)"),
        }
    }
}

/// The arguments of an Exec key, unquoted, field codes still in them.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Exec {
    args: Vec<String>,
}

/// What the field codes expand to.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Fields<'a> {
    /// The entry's Icon, for `%i`.
    pub icon: Option<&'a str>,
    /// The entry's translated Name, for `%c`.
    pub name: &'a str,
    /// The desktop file's location, for `%k`.
    pub location: &'a str,
}

impl Exec {
    /// Splits an (unescaped) Exec value into arguments and checks its field
    /// codes.
    pub fn parse(value: &str) -> Result<Self, ExecError> {
        let args = split(value)?;
        if args.first().is_none_or(String::is_empty) {
            return Err(ExecError::Empty);
        }
        for arg in &args {
            let mut chars = arg.chars();
            while let Some(c) = chars.next() {
                if c != '%' {
                    continue;
                }
                match chars.next() {
                    Some(code) if KNOWN.contains(code) => {}
                    Some(code) => return Err(ExecError::UnknownCode(code)),
                    None => return Err(ExecError::LonePercent),
                }
            }
        }
        Ok(Self { args })
    }

    /// The program: the first argument, as written.
    pub fn program(&self) -> &str {
        self.args.first().map_or("", String::as_str)
    }

    /// The command line, field codes expanded for a launch without files.
    pub fn expand(&self, fields: &Fields<'_>) -> Vec<String> {
        let mut out = Vec::with_capacity(self.args.len());
        for arg in &self.args {
            match arg.as_str() {
                "%f" | "%F" | "%u" | "%U" | "%d" | "%D" | "%n" | "%N" | "%v" | "%m" => {}
                "%i" => {
                    if let Some(icon) = fields.icon.filter(|i| !i.is_empty()) {
                        out.extend(["--icon".to_owned(), icon.to_owned()]);
                    }
                }
                _ => out.push(inline(arg, fields)),
            }
        }
        out
    }
}

/// Every field code: the spec's, `%%` and the deprecated ones.
const KNOWN: &str = "fFuUickdDnNvm%";

/// Field codes inside an argument (checked by [`Exec::parse`]).
fn inline(arg: &str, fields: &Fields<'_>) -> String {
    let mut out = String::with_capacity(arg.len());
    let mut chars = arg.chars();
    while let Some(c) = chars.next() {
        if c != '%' {
            out.push(c);
            continue;
        }
        match chars.next() {
            Some('%') => out.push('%'),
            Some('c') => out.push_str(fields.name),
            Some('k') => out.push_str(fields.location),
            _ => {}
        }
    }
    out
}

/// Splits a value into words, as described in the module docs.
fn split(value: &str) -> Result<Vec<String>, ExecError> {
    let mut words = Vec::new();
    let mut word: Option<String> = None;
    let mut chars = value.chars();
    while let Some(c) = chars.next() {
        match c {
            ' ' | '\t' | '\n' => words.extend(word.take()),
            '"' => {
                let w = word.get_or_insert_with(String::new);
                loop {
                    match chars.next().ok_or(ExecError::Unterminated)? {
                        '"' => break,
                        '\\' => match chars.next().ok_or(ExecError::Unterminated)? {
                            e @ ('"' | '`' | '$' | '\\') => w.push(e),
                            other => {
                                w.push('\\');
                                w.push(other);
                            }
                        },
                        other => w.push(other),
                    }
                }
            }
            '\'' => {
                let w = word.get_or_insert_with(String::new);
                loop {
                    match chars.next().ok_or(ExecError::Unterminated)? {
                        '\'' => break,
                        other => w.push(other),
                    }
                }
            }
            '\\' => {
                let w = word.get_or_insert_with(String::new);
                w.push(chars.next().unwrap_or('\\'));
            }
            other => word.get_or_insert_with(String::new).push(other),
        }
    }
    words.extend(word);
    Ok(words)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn args(value: &str) -> Vec<String> {
        Exec::parse(value).unwrap().args
    }

    #[test]
    fn arguments_split_at_spaces() {
        assert_eq!(args("foot  --server   -e"), ["foot", "--server", "-e"]);
    }

    #[test]
    fn double_quotes_keep_spaces_and_take_four_escapes() {
        assert_eq!(
            args(r#"sh -c "echo \"a b\" \$HOME \`x\` \\ \n""#),
            ["sh", "-c", r#"echo "a b" $HOME `x` \ \n"#]
        );
        assert_eq!(args(r#"run """#), ["run", ""], "an empty argument");
        assert_eq!(args(r#"a"b c"d"#), ["ab cd"], "quotes inside a word");
    }

    #[test]
    fn single_quotes_and_bare_backslashes_follow_the_shell() {
        assert_eq!(args(r"sh -c 'a \b'"), ["sh", "-c", r"a \b"]);
        assert_eq!(args(r"a\ b c\\d"), ["a b", r"c\d"]);
    }

    #[test]
    fn unclosed_quotes_and_no_program_are_refused() {
        assert_eq!(Exec::parse(r#"sh -c "x"#), Err(ExecError::Unterminated));
        assert_eq!(Exec::parse("sh 'x"), Err(ExecError::Unterminated));
        assert_eq!(Exec::parse("   "), Err(ExecError::Empty));
        assert_eq!(Exec::parse(r#""""#), Err(ExecError::Empty));
    }

    #[test]
    fn unknown_field_codes_are_refused() {
        assert_eq!(Exec::parse("app %x"), Err(ExecError::UnknownCode('x')));
        assert_eq!(Exec::parse("app --a=%Z"), Err(ExecError::UnknownCode('Z')));
        assert_eq!(Exec::parse("app 100%"), Err(ExecError::LonePercent));
    }

    const FIELDS: Fields<'static> = Fields {
        icon: Some("forge-cog"),
        name: "Rite of Oil",
        location: "/apps/oil.desktop",
    };

    fn expanded(value: &str, fields: &Fields<'_>) -> Vec<String> {
        Exec::parse(value).unwrap().expand(fields)
    }

    #[test]
    fn file_and_url_codes_vanish_without_files() {
        assert_eq!(expanded("app %f %F %u %U", &FIELDS), ["app"]);
        assert_eq!(
            expanded("app --open=%u x", &FIELDS),
            ["app", "--open=", "x"]
        );
        assert_eq!(expanded("app -- %U", &FIELDS), ["app", "--"]);
    }

    #[test]
    fn icon_name_and_location_expand() {
        assert_eq!(
            expanded("app %i --title=%c %k", &FIELDS),
            [
                "app",
                "--icon",
                "forge-cog",
                "--title=Rite of Oil",
                "/apps/oil.desktop"
            ]
        );
        let iconless = Fields {
            icon: None,
            ..FIELDS
        };
        assert_eq!(expanded("app %i x", &iconless), ["app", "x"]);
        assert_eq!(
            expanded(r#"sh -c "echo %c""#, &FIELDS),
            ["sh", "-c", "echo Rite of Oil"],
            "a name with spaces stays one argument"
        );
    }

    #[test]
    fn a_double_percent_is_a_percent_and_deprecated_codes_vanish() {
        assert_eq!(
            expanded("app 50%% %d %D %n %N %v %m x%dy", &FIELDS),
            ["app", "50%", "xy"]
        );
    }

    #[test]
    fn the_program_is_the_first_argument() {
        let exec = Exec::parse(r#""/opt/the forge/bin" %U"#).unwrap();
        assert_eq!(exec.program(), "/opt/the forge/bin");
    }
}
