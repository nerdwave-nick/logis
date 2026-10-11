//! Command-line parsing. Flag names stay conventional (spec §4.6).

pub mod dmenu;

use std::ffi::OsString;
use std::fmt;
use std::path::PathBuf;
use std::time::Duration;

pub use dmenu::Dmenu;

/// What one invocation of `logis` was asked to do. `config` is `--config
/// PATH`; a client passes it on to a daemon it starts.
#[derive(Debug, PartialEq)]
pub enum Command {
    /// Print the name and version.
    Version,
    /// Rouse the daemon (starting it if none answers) without opening a session.
    Run { config: Option<PathBuf> },
    /// A dmenu session: lines from stdin, the chosen line on stdout.
    Dmenu {
        config: Option<PathBuf>,
        dmenu: Box<Dmenu>,
    },
    /// A provider session: the daemon runs this program and shows what it
    /// says (spec §4.8).
    Provider {
        config: Option<PathBuf>,
        argv: Vec<OsString>,
    },
    /// The app launcher: desktop applications, launched by the daemon.
    Apps { config: Option<PathBuf> },
    /// Validate the config: exit 0 if it is sound, 2 with its errors if not.
    CheckConfig { config: Option<PathBuf> },
    /// Be the daemon.
    Daemon(DaemonOptions),
}

/// Options of `logis daemon`.
#[derive(Debug, Default, PartialEq)]
pub struct DaemonOptions {
    /// Exit after this long without any client connected. Given, it outranks
    /// the config's `idle-timeout`, and `Some(0)` keeps the daemon resident.
    pub idle_timeout: Option<Duration>,
    /// The config file instead of `$XDG_CONFIG_HOME/logis/logis.jsonc`.
    pub config: Option<PathBuf>,
    /// Once listening, stop writing to the starter's stdout and stderr;
    /// later diagnostics go to `$XDG_RUNTIME_DIR/logis.log`.
    pub detach: bool,
    /// Instead of serving sessions, show the renderer's static debug panel
    /// until the compositor closes it; the other options do not apply then.
    pub debug_panel: bool,
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
    /// A flag's value was refused for the reason given.
    Refused { flag: &'static str, reason: String },
    /// A list rune given without `--dmenu`.
    DmenuOnly(String),
    /// Two flags that exclude each other.
    Conflict(&'static str, &'static str),
}

impl fmt::Display for ParseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Unknown(arg) => write!(
                f,
                "the rune '{arg}' is unknown to this cogitator. \
                 It knows only --version, --dmenu or --dmenu0 (with fuzzel's list runes), \
                 --apps, --provider PROGRAM [ARG]..., --check-config, --config PATH and \
                 daemon [--idle-timeout SECONDS] [--config PATH] [--detach] [--debug-panel]."
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
            Self::Refused { flag, reason } => write!(f, "the rune {flag} is refused: {reason}"),
            Self::DmenuOnly(rune) => write!(
                f,
                "the rune {rune} serves only --dmenu; this cogitator has no other list yet"
            ),
            Self::Conflict(a, b) => write!(f, "the runes {a} and {b} cannot be borne together"),
        }
    }
}

const CONFIG: &str = "--config";

/// Parses the arguments after the program name.
pub fn parse(args: impl IntoIterator<Item = OsString>) -> Result<Command, ParseError> {
    let mut args = args.into_iter().peekable();
    if args.peek().and_then(|a| a.to_str()) == Some("daemon") {
        args.next();
        return parse_daemon(args).map(Command::Daemon);
    }
    let mut config = None;
    let mut command = None;
    let mut runes = dmenu::Runes::default();
    while let Some(arg) = args.next() {
        if config_flag(&arg, &mut args, &mut config)? {
            continue;
        }
        if let Some(a) = arg.to_str()
            && runes.take(a, &mut args)?
        {
            continue;
        }
        let this = match arg.to_str() {
            Some("--version" | "-V") => Started::Version,
            Some("--dmenu" | "-d") => Started::Dmenu { nul: false },
            Some("--dmenu0") => Started::Dmenu { nul: true },
            Some("--check-config") => Started::CheckConfig,
            Some("--apps") => Started::Apps,
            Some("--provider") => {
                let argv: Vec<OsString> = args.by_ref().collect();
                if argv.is_empty() {
                    return Err(ParseError::BadValue {
                        flag: "--provider",
                        value: None,
                        expected: "a program to run, then its arguments",
                    });
                }
                Started::Provider(argv)
            }
            // dmenu's case-insensitivity switch: logis always ignores case.
            Some("-i") => continue,
            _ => return Err(unknown(&arg)),
        };
        if command.replace(this).is_some() {
            return Err(unknown(&arg));
        }
    }
    if let Some(Started::Dmenu { nul }) = command {
        let dmenu = Box::new(runes.finish(nul)?);
        return Ok(Command::Dmenu { config, dmenu });
    }
    runes.none_given()?;
    Ok(match command {
        None => Command::Run { config },
        Some(Started::Version) => Command::Version,
        Some(Started::CheckConfig) => Command::CheckConfig { config },
        Some(Started::Apps) => Command::Apps { config },
        Some(Started::Provider(argv)) => Command::Provider { config, argv },
        Some(Started::Dmenu { .. }) => unreachable!("handled above"),
    })
}

/// The command a flag named, before its options are complete.
enum Started {
    Version,
    Dmenu {
        nul: bool,
    },
    CheckConfig,
    Apps,
    /// The program and its arguments: everything after `--provider`.
    Provider(Vec<OsString>),
}

/// Takes `--config PATH` or `--config=PATH` into `config`; `false` if `arg`
/// is another flag.
fn config_flag(
    arg: &OsString,
    rest: &mut impl Iterator<Item = OsString>,
    config: &mut Option<PathBuf>,
) -> Result<bool, ParseError> {
    let bad = |value: Option<&OsString>| ParseError::BadValue {
        flag: CONFIG,
        value: value.map(|v| v.to_string_lossy().into_owned()),
        expected: "one path to a config file",
    };
    let path = match arg.to_str() {
        Some(CONFIG) => rest.next().ok_or_else(|| bad(None))?,
        Some(a) if a.starts_with("--config=") => OsString::from(&a[CONFIG.len() + 1..]),
        _ => return Ok(false),
    };
    if path.is_empty() || config.is_some() {
        return Err(bad(Some(&path)));
    }
    *config = Some(PathBuf::from(path));
    Ok(true)
}

fn parse_daemon(mut args: impl Iterator<Item = OsString>) -> Result<DaemonOptions, ParseError> {
    const IDLE: &str = "--idle-timeout";
    const SECONDS: &str = "a number of seconds (0 keeps the daemon resident)";
    let mut options = DaemonOptions::default();
    while let Some(arg) = args.next() {
        if config_flag(&arg, &mut args, &mut options.config)? {
            continue;
        }
        let value = match arg.to_str() {
            Some("--detach") => {
                options.detach = true;
                continue;
            }
            Some("--debug-panel") => {
                options.debug_panel = true;
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
        options.idle_timeout = Some(seconds);
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
            ..DaemonOptions::default()
        }))
    }

    #[test]
    fn no_arguments_runs() {
        assert_eq!(parse_strs(&[]), Ok(Command::Run { config: None }));
    }

    #[test]
    fn version_flags_ask_for_the_version() {
        assert_eq!(parse_strs(&["--version"]), Ok(Command::Version));
        assert_eq!(parse_strs(&["-V"]), Ok(Command::Version));
    }

    fn dmenu(config: Option<PathBuf>, dmenu: Dmenu) -> Result<Command, ParseError> {
        Ok(Command::Dmenu {
            config,
            dmenu: Box::new(dmenu),
        })
    }

    #[test]
    fn dmenu_asks_for_a_dmenu_session() {
        assert_eq!(parse_strs(&["--dmenu"]), dmenu(None, Dmenu::default()));
        assert_eq!(parse_strs(&["-d"]), dmenu(None, Dmenu::default()));
        let nul = Dmenu {
            separator: 0,
            ..Dmenu::default()
        };
        assert_eq!(parse_strs(&["--dmenu0"]), dmenu(None, nul));
    }

    #[test]
    fn list_runes_go_with_dmenu_in_any_order() {
        let Ok(Command::Dmenu { dmenu, .. }) =
            parse_strs(&["--index", "--dmenu", "-i", "--lines", "3"])
        else {
            panic!("a dmenu session");
        };
        assert!(dmenu.index);
        assert_eq!(dmenu.view.lines, 3);
    }

    #[test]
    fn list_runes_without_dmenu_are_refused() {
        assert_eq!(
            parse_strs(&["--prompt", "x"]),
            Err(ParseError::DmenuOnly("--prompt".into()))
        );
        assert!(parse_strs(&["--check-config", "--index"]).is_err());
    }

    #[test]
    fn provider_takes_every_argument_after_it_as_the_program_and_its_arguments() {
        let argv = |a: &[&str]| a.iter().map(OsString::from).collect::<Vec<_>>();
        assert_eq!(
            parse_strs(&["--config", "/c", "--provider", "rite", "--dmenu", "-x"]),
            Ok(Command::Provider {
                config: Some(PathBuf::from("/c")),
                argv: argv(&["rite", "--dmenu", "-x"]),
            })
        );
        let err = parse_strs(&["--provider"]).unwrap_err();
        assert!(err.to_string().contains("--provider"), "{err}");
        assert!(parse_strs(&["--dmenu", "--provider", "x"]).is_err());
        assert!(parse_strs(&["--prompt", "p", "--provider", "x"]).is_err());
    }

    #[test]
    fn apps_asks_for_the_app_launcher() {
        assert_eq!(parse_strs(&["--apps"]), Ok(Command::Apps { config: None }));
        assert_eq!(
            parse_strs(&["--apps", "--config", "/c"]),
            Ok(Command::Apps {
                config: Some(PathBuf::from("/c"))
            })
        );
        assert!(parse_strs(&["--apps", "--dmenu"]).is_err());
        assert!(parse_strs(&["--apps", "--prompt", "x"]).is_err());
    }

    #[test]
    fn check_config_asks_to_validate_the_config() {
        assert_eq!(
            parse_strs(&["--check-config"]),
            Ok(Command::CheckConfig { config: None })
        );
    }

    #[test]
    fn config_names_the_config_file_before_or_after_the_command() {
        let path = || Some(PathBuf::from("/x/logis.jsonc"));
        assert_eq!(
            parse_strs(&["--config", "/x/logis.jsonc", "--dmenu"]),
            dmenu(path(), Dmenu::default())
        );
        assert_eq!(
            parse_strs(&["--check-config", "--config=/x/logis.jsonc"]),
            Ok(Command::CheckConfig { config: path() })
        );
        assert_eq!(
            parse_strs(&["--config", "/x/logis.jsonc"]),
            Ok(Command::Run { config: path() })
        );
        let options = DaemonOptions {
            config: path(),
            ..DaemonOptions::default()
        };
        assert_eq!(
            parse_strs(&["daemon", "--config", "/x/logis.jsonc"]),
            Ok(Command::Daemon(options))
        );
    }

    #[test]
    fn config_wants_one_path() {
        for args in [
            &["--config"][..],
            &["--config="],
            &["--dmenu", "--config"],
            &["daemon", "--config"],
            &["--config", "a", "--config", "b"],
        ] {
            let err = parse_strs(args).unwrap_err();
            assert!(err.to_string().contains("--config"), "{args:?}: {err}");
        }
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
        // An explicit 0 is kept: it outranks the config's idle-timeout.
        assert_eq!(
            parse_strs(&["daemon", "--idle-timeout", "0"]),
            daemon(Some(Duration::ZERO))
        );
    }

    #[test]
    fn daemon_can_detach_from_its_starter() {
        let options = DaemonOptions {
            idle_timeout: Some(Duration::from_secs(5)),
            detach: true,
            ..DaemonOptions::default()
        };
        let args = ["daemon", "--detach", "--idle-timeout", "5"];
        assert_eq!(parse_strs(&args), Ok(Command::Daemon(options)));
    }

    #[test]
    fn daemon_debug_panel_shows_the_debug_panel() {
        let options = DaemonOptions {
            debug_panel: true,
            ..DaemonOptions::default()
        };
        assert_eq!(
            parse_strs(&["daemon", "--debug-panel"]),
            Ok(Command::Daemon(options))
        );
    }

    #[test]
    fn debug_panel_is_a_daemon_flag_only() {
        let unknown = Err(ParseError::Unknown("--debug-panel".to_owned()));
        assert_eq!(parse_strs(&["--debug-panel"]), unknown);
        assert_eq!(parse_strs(&["--dmenu", "--debug-panel"]), unknown);
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
        assert_eq!(parse_strs(&["--dmenu", "--dmenu0"]), unknown("--dmenu0"));
        assert_eq!(
            parse_strs(&["--dmenu", "--check-config"]),
            unknown("--check-config")
        );
        assert_eq!(parse_strs(&["daemon", "--debug"]), unknown("--debug"));
    }
}
