//! Where the config comes from: `--config PATH`, else
//! `$XDG_CONFIG_HOME/logis/logis.jsonc` (`~/.config` when unset or relative).
//! A missing default file means the defaults; a missing `--config` file is an
//! error, since the user named it.

use std::ffi::OsStr;
use std::fmt;
use std::io;
use std::path::{Path, PathBuf};

use super::{Config, Invalid, parse};

/// The config file a process reads (and the daemon watches).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Source {
    /// `None` when neither `--config`, `XDG_CONFIG_HOME` nor `HOME` names one.
    path: Option<PathBuf>,
    /// Named with `--config`: then it must exist.
    explicit: bool,
}

/// Why a config could not be loaded.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error {
    pub path: PathBuf,
    pub problem: Problem,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Problem {
    /// `--config` names a file that does not exist.
    Missing,
    /// The file exists but cannot be read (the I/O error's text).
    Unreadable(String),
    /// The text is not a valid config.
    Invalid(Invalid),
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let path = self.path.display();
        match &self.problem {
            Problem::Missing => write!(f, "{path}: no such file"),
            Problem::Unreadable(e) => write!(f, "{path}: {e}"),
            Problem::Invalid(invalid) => write!(f, "{path}:{invalid}"),
        }
    }
}

impl Source {
    /// `--config PATH` if given, else the default location from the
    /// environment. A relative `PATH` is taken from the current directory.
    pub fn new(flag: Option<&Path>) -> Self {
        match flag {
            Some(path) => Self {
                path: Some(std::path::absolute(path).unwrap_or_else(|_| path.to_owned())),
                explicit: true,
            },
            None => Self {
                path: default_path(
                    std::env::var_os("XDG_CONFIG_HOME").as_deref(),
                    std::env::var_os("HOME").as_deref(),
                ),
                explicit: false,
            },
        }
    }

    /// The file, if there is any to read.
    pub fn path(&self) -> Option<&Path> {
        self.path.as_deref()
    }

    /// Whether the file exists (for messages; [`Source::load`] decides).
    pub fn exists(&self) -> bool {
        self.path.as_deref().is_some_and(Path::exists)
    }

    /// Reads and parses the file; a missing default file means the defaults.
    pub fn load(&self) -> Result<Config, Error> {
        let Some(path) = &self.path else {
            return Ok(Config::default());
        };
        let error = |problem| Error {
            path: path.clone(),
            problem,
        };
        match std::fs::read_to_string(path) {
            Ok(text) => parse(&text).map_err(|e| error(Problem::Invalid(e))),
            Err(e) if e.kind() == io::ErrorKind::NotFound && !self.explicit => {
                Ok(Config::default())
            }
            Err(e) if e.kind() == io::ErrorKind::NotFound => Err(error(Problem::Missing)),
            Err(e) => Err(error(Problem::Unreadable(e.to_string()))),
        }
    }
}

/// `$XDG_CONFIG_HOME/logis/logis.jsonc`; the XDG base directory spec says
/// to ignore an empty or relative value and use `$HOME/.config` instead.
fn default_path(xdg_config_home: Option<&OsStr>, home: Option<&OsStr>) -> Option<PathBuf> {
    let absolute = |dir: &OsStr| Some(PathBuf::from(dir)).filter(|d| d.is_absolute());
    let base = xdg_config_home
        .and_then(absolute)
        .or_else(|| home.and_then(absolute).map(|h| h.join(".config")))?;
    Some(base.join("logis").join("logis.jsonc"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::FocusLoss;

    /// A scratch directory under the system temp dir, removed on drop.
    struct Scratch(PathBuf);

    impl Scratch {
        fn new(name: &str) -> Self {
            let dir =
                std::env::temp_dir().join(format!("logis-config-{}-{name}", std::process::id()));
            std::fs::create_dir_all(&dir).unwrap();
            Self(dir)
        }
    }

    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn os(s: &str) -> Option<&OsStr> {
        Some(OsStr::new(s))
    }

    #[test]
    fn the_default_lives_in_xdg_config_home() {
        assert_eq!(
            default_path(os("/cfg"), os("/home/u")),
            Some(PathBuf::from("/cfg/logis/logis.jsonc"))
        );
    }

    #[test]
    fn without_a_usable_xdg_config_home_the_default_is_under_home() {
        let fallback = Some(PathBuf::from("/home/u/.config/logis/logis.jsonc"));
        assert_eq!(default_path(None, os("/home/u")), fallback);
        assert_eq!(default_path(os(""), os("/home/u")), fallback);
        assert_eq!(default_path(os("rel/cfg"), os("/home/u")), fallback);
    }

    #[test]
    fn without_any_home_there_is_no_default() {
        assert_eq!(default_path(None, None), None);
        assert_eq!(default_path(os("rel"), os("also/rel")), None);
        assert_eq!(
            Source {
                path: None,
                explicit: false
            }
            .load(),
            Ok(Config::default())
        );
    }

    #[test]
    fn a_relative_config_flag_is_made_absolute() {
        let source = Source::new(Some(Path::new("some/logis.jsonc")));
        let path = source.path().unwrap();
        assert!(path.is_absolute(), "{path:?}");
        assert!(path.ends_with("some/logis.jsonc"), "{path:?}");
    }

    #[test]
    fn a_missing_default_file_means_the_defaults() {
        let dir = Scratch::new("missing-default");
        let source = Source {
            path: Some(dir.0.join("logis.jsonc")),
            explicit: false,
        };
        assert!(!source.exists());
        assert_eq!(source.load(), Ok(Config::default()));
    }

    #[test]
    fn a_missing_named_file_is_an_error() {
        let dir = Scratch::new("missing-named");
        let path = dir.0.join("nowhere.jsonc");
        let err = Source::new(Some(&path)).load().unwrap_err();
        assert_eq!(err.problem, Problem::Missing);
        assert_eq!(err.to_string(), format!("{}: no such file", path.display()));
    }

    #[test]
    fn a_file_is_read_and_parsed() {
        let dir = Scratch::new("read");
        let path = dir.0.join("logis.jsonc");
        std::fs::write(&path, "{\"focus-loss\": \"keep\", // stay\n}").unwrap();
        let config = Source::new(Some(&path)).load().unwrap();
        assert_eq!(config.focus_loss, FocusLoss::Keep);
    }

    #[test]
    fn an_invalid_file_is_reported_with_path_line_and_column() {
        let dir = Scratch::new("invalid");
        let path = dir.0.join("logis.jsonc");
        std::fs::write(&path, "{\n  \"thme\": 1\n}").unwrap();
        let err = Source::new(Some(&path)).load().unwrap_err();
        let text = err.to_string();
        let located = format!("{}:2:8: unknown field `thme`", path.display());
        assert!(text.starts_with(&located), "{text}");
    }

    #[test]
    fn an_unreadable_file_is_an_error() {
        let dir = Scratch::new("unreadable");
        // A directory where the file should be cannot be read as text.
        let path = dir.0.join("logis.jsonc");
        std::fs::create_dir(&path).unwrap();
        let source = Source {
            path: Some(path),
            explicit: false,
        };
        let err = source.load().unwrap_err();
        assert!(matches!(err.problem, Problem::Unreadable(_)), "{err}");
    }
}
