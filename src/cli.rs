//! Command-line parsing. Flag names stay conventional (spec §4.6).

use std::ffi::OsString;
use std::fmt;
use std::time::Duration;

/// What one invocation of `logis` was asked to do.
#[derive(Debug, PartialEq)]
pub enum Command {
    /// Print the name and version.
    Version,
    /// Rouse the daemon (starting it if none answers) without opening a session.
    Run,
    /// A dmenu session: lines from stdin, the chosen line on stdout.
    Dmenu,
    /// Be the daemon.
    Daemon(DaemonOptions),
}

/// Options of `logis daemon`.
#[derive(Debug, Default, PartialEq)]
pub struct DaemonOptions {
    /// Exit after this long without any client connected (off by default).
    pub idle_timeout: Option<Duration>,
    /// Once listening, stop writing to the starter's stdout and stderr;
    /// later diagnostics go to `$XDG_RUNTIME_DIR/logis.log`.
    pub detach: bool,
}

/// Why the arguments were refused.
#[derive(Debug, PartialEq)]
pub enum ParseError {
    /// An argument no part of logis knows.
    Unknown(String),
    /// A flag's value is missing or invalid.
    BadValue {
        flag: &'static str,
        value: Option<String>,
        expected: &'static str,
    },
}

impl fmt::Display for ParseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Unknown(arg) => write!(
                f,
                "the rune '{arg}' is unknown to this cogitator. \
                 It knows only --version, --dmenu and daemon [--idle-timeout SECONDS] [--detach]."
            ),
            Self::BadValue {
                flag,
                value: Some(value),
                expected,
            } => {
                write!(
                    f,
                    "the rune {flag} cannot bear '{value}': it wants {expected}"
                )
            }
            Self::BadValue {
                flag,
                value: None,
                expected,
            } => {
                write!(f, "the rune {flag} stands empty: it wants {expected}")
            }
        }
    }
}

/// Parses the arguments after the program name.
pub fn parse(args: impl IntoIterator<Item = OsString>) -> Result<Command, ParseError> {
    let mut args = args.into_iter();
    let Some(first) = args.next() else {
        return Ok(Command::Run);
    };
    let command = match first.to_str() {
        Some("--version" | "-V") => Command::Version,
        Some("--dmenu") => Command::Dmenu,
        Some("daemon") => return parse_daemon(args).map(Command::Daemon),
        _ => return Err(unknown(&first)),
    };
    match args.next() {
        Some(extra) => Err(unknown(&extra)),
        None => Ok(command),
    }
}

fn parse_daemon(mut args: impl Iterator<Item = OsString>) -> Result<DaemonOptions, ParseError> {
    const IDLE: &str = "--idle-timeout";
    const SECONDS: &str = "a number of seconds (0 keeps the daemon resident)";
    let mut options = DaemonOptions::default();
    while let Some(arg) = args.next() {
        let value = match arg.to_str() {
            Some("--detach") => {
                options.detach = true;
                continue;
            }
            Some(IDLE) => args.next().map(|v| v.to_string_lossy().into_owned()),
            Some(a) if a.starts_with("--idle-timeout=") => Some(a[IDLE.len() + 1..].to_owned()),
            _ => return Err(unknown(&arg)),
        };
        let bad = |value| ParseError::BadValue {
            flag: IDLE,
            value,
            expected: SECONDS,
        };
        let text = value.ok_or_else(|| bad(None))?;
        let seconds = text
            .parse::<f64>()
            .ok()
            .and_then(|s| Duration::try_from_secs_f64(s).ok())
            .ok_or_else(|| bad(Some(text.clone())))?;
        options.idle_timeout = Some(seconds).filter(|d| !d.is_zero());
    }
    Ok(options)
}

fn unknown(arg: &OsString) -> ParseError {
    ParseError::Unknown(arg.to_string_lossy().into_owned())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse_strs(args: &[&str]) -> Result<Command, ParseError> {
        parse(args.iter().map(OsString::from))
    }

    fn daemon(idle_timeout: Option<Duration>) -> Result<Command, ParseError> {
        Ok(Command::Daemon(DaemonOptions {
            idle_timeout,
            detach: false,
        }))
    }

    #[test]
    fn no_arguments_runs() {
        assert_eq!(parse_strs(&[]), Ok(Command::Run));
    }

    #[test]
    fn version_flags_ask_for_the_version() {
        assert_eq!(parse_strs(&["--version"]), Ok(Command::Version));
        assert_eq!(parse_strs(&["-V"]), Ok(Command::Version));
    }

    #[test]
    fn dmenu_asks_for_a_dmenu_session() {
        assert_eq!(parse_strs(&["--dmenu"]), Ok(Command::Dmenu));
    }

    #[test]
    fn daemon_takes_an_optional_idle_timeout_in_seconds() {
        assert_eq!(parse_strs(&["daemon"]), daemon(None));
        let quarter = Some(Duration::from_millis(250));
        assert_eq!(
            parse_strs(&["daemon", "--idle-timeout", "0.25"]),
            daemon(quarter)
        );
        assert_eq!(
            parse_strs(&["daemon", "--idle-timeout=600"]),
            daemon(Some(Duration::from_secs(600)))
        );
        assert_eq!(parse_strs(&["daemon", "--idle-timeout", "0"]), daemon(None));
    }

    #[test]
    fn daemon_can_detach_from_its_starter() {
        let options = DaemonOptions {
            idle_timeout: Some(Duration::from_secs(5)),
            detach: true,
        };
        let args = ["daemon", "--detach", "--idle-timeout", "5"];
        assert_eq!(parse_strs(&args), Ok(Command::Daemon(options)));
    }

    #[test]
    fn an_invalid_idle_timeout_is_refused() {
        for bad in ["soon", "-1", "NaN", "inf", ""] {
            let err = parse_strs(&["daemon", "--idle-timeout", bad]).unwrap_err();
            assert!(err.to_string().contains(&format!("'{bad}'")), "{err}");
        }
        let err = parse_strs(&["daemon", "--idle-timeout"]).unwrap_err();
        assert!(err.to_string().contains("--idle-timeout"), "{err}");
    }

    #[test]
    fn an_unknown_argument_is_named_in_the_error() {
        let unknown = |a: &str| Err(ParseError::Unknown(a.to_owned()));
        assert_eq!(parse_strs(&["--heresy"]), unknown("--heresy"));
        assert_eq!(parse_strs(&["--version", "extra"]), unknown("extra"));
        assert_eq!(parse_strs(&["--dmenu", "--dmenu"]), unknown("--dmenu"));
        assert_eq!(parse_strs(&["daemon", "--debug"]), unknown("--debug"));
    }
}
