//! The config while the daemon runs: the last sound config, what each reload
//! means, and the precedence of `logis daemon --idle-timeout` over the
//! config's `idle-timeout`. An invalid edit never replaces a sound config.

use std::time::Duration;

use super::{Config, Error};

/// The daemon's view of its config.
#[derive(Debug)]
pub struct Live {
    config: Config,
    /// `--idle-timeout` as given; it outranks the config, `Some(0)` included.
    idle_flag: Option<Duration>,
    /// The latest edit was refused.
    rejected: bool,
}

/// What a reload did.
#[derive(Debug, PartialEq, Eq)]
pub enum Reload {
    /// A different sound config is in force now.
    Applied,
    /// The file is sound again after a refused edit, and the same as before.
    Restored,
    /// Nothing changed.
    Unchanged,
    /// The edit was refused (the error's text); the last sound config stays.
    Rejected(String),
}

impl Live {
    pub fn new(config: Config, idle_flag: Option<Duration>) -> Self {
        Self {
            config,
            idle_flag,
            rejected: false,
        }
    }

    /// The config in force: the last sound one.
    pub fn config(&self) -> &Config {
        &self.config
    }

    /// `--idle-timeout` if it was given, else the config's; `None` keeps the
    /// daemon resident.
    pub fn idle_timeout(&self) -> Option<Duration> {
        self.idle_flag
            .or(self.config.idle_timeout)
            .filter(|d| !d.is_zero())
    }

    /// Takes the result of reading the config again.
    pub fn reload(&mut self, loaded: Result<Config, Error>) -> Reload {
        let config = match loaded {
            Ok(config) => config,
            Err(e) => {
                self.rejected = true;
                return Reload::Rejected(e.to_string());
            }
        };
        let was_rejected = std::mem::take(&mut self.rejected);
        if config != self.config {
            self.config = config;
            Reload::Applied
        } else if was_rejected {
            Reload::Restored
        } else {
            Reload::Unchanged
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::{FocusLoss, Invalid, Problem};
    use std::path::PathBuf;

    fn idle(secs: u64) -> Option<Duration> {
        Some(Duration::from_secs(secs))
    }

    fn with_idle(secs: u64) -> Config {
        Config {
            idle_timeout: idle(secs),
            ..Config::default()
        }
    }

    fn invalid() -> Error {
        Error {
            path: PathBuf::from("/cfg/logis.jsonc"),
            problem: Problem::Invalid(Invalid {
                line: 2,
                column: 8,
                message: "unknown field `thme`".into(),
            }),
        }
    }

    #[test]
    fn without_the_flag_the_config_sets_the_idle_timeout() {
        assert_eq!(Live::new(with_idle(10), None).idle_timeout(), idle(10));
        assert_eq!(Live::new(Config::default(), None).idle_timeout(), None);
    }

    #[test]
    fn the_flag_outranks_the_config() {
        assert_eq!(Live::new(with_idle(10), idle(5)).idle_timeout(), idle(5));
        assert_eq!(
            Live::new(Config::default(), idle(5)).idle_timeout(),
            idle(5)
        );
    }

    #[test]
    fn a_flag_of_zero_keeps_the_daemon_resident_whatever_the_config_says() {
        let mut live = Live::new(with_idle(10), Some(Duration::ZERO));
        assert_eq!(live.idle_timeout(), None);
        live.reload(Ok(with_idle(3)));
        assert_eq!(live.idle_timeout(), None);
    }

    #[test]
    fn a_sound_edit_is_applied() {
        let mut live = Live::new(Config::default(), None);
        assert_eq!(live.reload(Ok(with_idle(3))), Reload::Applied);
        assert_eq!(live.config(), &with_idle(3));
        assert_eq!(live.idle_timeout(), idle(3));
    }

    #[test]
    fn the_same_config_again_changes_nothing() {
        let mut live = Live::new(with_idle(3), None);
        assert_eq!(live.reload(Ok(with_idle(3))), Reload::Unchanged);
    }

    #[test]
    fn an_invalid_edit_is_rejected_and_the_last_sound_config_stays() {
        let mut live = Live::new(with_idle(3), None);
        let Reload::Rejected(message) = live.reload(Err(invalid())) else {
            panic!("the edit was not rejected");
        };
        assert!(
            message.contains("/cfg/logis.jsonc:2:8: unknown field `thme`"),
            "{message}"
        );
        assert_eq!(live.config(), &with_idle(3));
        assert_eq!(live.idle_timeout(), idle(3));
    }

    #[test]
    fn after_a_rejection_the_old_config_is_restored_and_a_new_one_applied() {
        let mut live = Live::new(with_idle(3), None);
        live.reload(Err(invalid()));
        assert_eq!(live.reload(Ok(with_idle(3))), Reload::Restored);
        assert_eq!(live.reload(Ok(with_idle(3))), Reload::Unchanged);
        live.reload(Err(invalid()));
        let keep = Config {
            focus_loss: FocusLoss::Keep,
            ..with_idle(3)
        };
        assert_eq!(live.reload(Ok(keep.clone())), Reload::Applied);
        assert_eq!(live.config(), &keep);
    }
}
