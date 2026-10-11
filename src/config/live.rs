//! The config while the daemon runs: the last sound config, what each reload
//! means, and the precedence of `logis daemon --idle-timeout` over the
//! config's `idle-timeout`. An invalid edit never replaces a sound config.

use std::sync::Arc;
use std::time::Duration;

use super::{Config, Error};
use crate::theme::{Theme, Themes};

/// The daemon's view of its config.
#[derive(Debug)]
pub struct Live {
    config: Config,
    /// `--idle-timeout` as given; it outranks the config, `Some(0)` included.
    idle_flag: Option<Duration>,
    /// The latest edit was refused.
    rejected: bool,
    /// The theme the config names, as worn.
    theme: Worn,
}

/// The theme in force: the last sound one the config named, stilled when
/// the config reduces motion.
#[derive(Debug)]
struct Worn {
    themes: Themes,
    /// As loaded; `theme` is what is worn.
    sound: Arc<Theme>,
    theme: Arc<Theme>,
    rejected: bool,
    /// Why the theme named at the start could not be worn.
    error: Option<String>,
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
    /// The config in force, its theme the bundled default until
    /// [`Self::with_themes`] says where themes are.
    pub fn new(config: Config, idle_flag: Option<Duration>) -> Self {
        let default = Arc::new(Theme::bundled_default());
        Self {
            theme: Worn {
                themes: Themes::default(),
                theme: worn(&default, &config),
                sound: default,
                rejected: false,
                error: None,
            },
            config,
            idle_flag,
            rejected: false,
        }
    }

    /// Wears the theme the config names from `themes`; one that cannot be
    /// worn leaves the bundled default in force, and
    /// [`Self::take_theme_error`] says why.
    pub fn with_themes(mut self, themes: Themes) -> Self {
        match themes.load(&self.config.theme) {
            Ok(theme) => self.theme.sound = Arc::new(theme),
            Err(e) => self.theme.error = Some(e.to_string()),
        }
        self.theme.theme = worn(&self.theme.sound, &self.config);
        self.theme.themes = themes;
        self
    }

    /// Why the theme named at the start could not be worn, once.
    pub fn take_theme_error(&mut self) -> Option<String> {
        self.theme.error.take()
    }

    /// The theme worn now.
    pub fn theme(&self) -> &Arc<Theme> {
        &self.theme.theme
    }

    /// The themes directory, if any.
    pub fn themes(&self) -> &Themes {
        &self.theme.themes
    }

    /// Reads the theme the config names again (after a theme file or the
    /// config changed). Like [`Self::reload`], a theme that cannot be worn
    /// leaves the last sound one in force.
    pub fn reload_theme(&mut self) -> Reload {
        let worn = &mut self.theme;
        let theme = match worn.themes.load(&self.config.theme) {
            Ok(theme) => Arc::new(theme),
            Err(e) => {
                worn.rejected = true;
                return Reload::Rejected(e.to_string());
            }
        };
        let was_rejected = std::mem::take(&mut worn.rejected);
        let now = self::worn(&theme, &self.config);
        worn.sound = theme;
        if now != worn.theme {
            worn.theme = now;
            Reload::Applied
        } else if was_rejected {
            Reload::Restored
        } else {
            Reload::Unchanged
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

/// `theme` as the config would have it worn.
fn worn(theme: &Arc<Theme>, config: &Config) -> Arc<Theme> {
    if config.reduce_motion {
        Arc::new(Theme::clone(theme).stilled())
    } else {
        Arc::clone(theme)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::{FocusLoss, Invalid, Problem};
    use crate::theme::Themes;
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

    /// A scratch themes directory, removed on drop.
    struct Themed(PathBuf);

    impl Themed {
        fn new(name: &str) -> Self {
            let dir =
                std::env::temp_dir().join(format!("logis-live-{}-{name}", std::process::id()));
            let _ = std::fs::remove_dir_all(&dir);
            std::fs::create_dir_all(&dir).unwrap();
            Self(dir)
        }

        fn write(&self, name: &str, text: &str) {
            std::fs::write(self.0.join(format!("{name}.jsonc")), text).unwrap();
        }

        fn live(&self, config: Config) -> Live {
            Live::new(config, None).with_themes(Themes::new(Some(self.0.clone())))
        }
    }

    impl Drop for Themed {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn named(theme: &str) -> Config {
        Config {
            theme: theme.into(),
            ..Config::default()
        }
    }

    #[test]
    fn the_theme_the_config_names_is_worn_from_the_start() {
        let dir = Themed::new("start");
        let live = dir.live(named("cogitator-phosphor"));
        assert_eq!(live.theme().name, "cogitator-phosphor");
        let broken = Themed::new("start-broken");
        broken.write("mine", "{\"glow\": 1}");
        let mut live = broken.live(named("mine"));
        assert_eq!(live.theme().name, "forge-crimson-brass", "the default instead");
        let error = live.take_theme_error().expect("the broken theme is reported");
        assert!(error.contains("mine.jsonc:1:"), "{error}");
        assert_eq!(live.take_theme_error(), None, "once");
    }

    #[test]
    fn an_edited_theme_is_applied_and_a_broken_one_keeps_the_last_sound() {
        let dir = Themed::new("edit");
        dir.write("mine", "{\"window\": {\"rows\": 4}}");
        let mut live = dir.live(named("mine"));
        assert_eq!(live.reload_theme(), Reload::Unchanged);
        dir.write("mine", "{\"window\": {\"rows\": 5}}");
        assert_eq!(live.reload_theme(), Reload::Applied);
        assert_eq!(live.theme().window.rows, 5);
        dir.write("mine", "{\"window\": {\"rows\": \"many\"}}");
        let Reload::Rejected(e) = live.reload_theme() else {
            panic!("a broken theme was taken")
        };
        assert!(e.contains("mine.jsonc:1:"), "{e}");
        assert_eq!(live.theme().window.rows, 5, "the last sound theme stands");
        dir.write("mine", "{\"window\": {\"rows\": 5}}");
        assert_eq!(live.reload_theme(), Reload::Restored);
    }

    #[test]
    fn a_config_naming_another_theme_or_reducing_motion_changes_what_is_worn() {
        let dir = Themed::new("switch");
        let mut live = dir.live(named("forge-crimson-brass"));
        assert!(live.theme().is_continuous());
        live.reload(Ok(Config {
            reduce_motion: true,
            ..named("forge-crimson-brass")
        }));
        assert_eq!(live.reload_theme(), Reload::Applied);
        assert!(!live.theme().is_continuous(), "stilled");
        live.reload(Ok(named("cogitator-phosphor")));
        assert_eq!(live.reload_theme(), Reload::Applied);
        assert_eq!(live.theme().name, "cogitator-phosphor");
        live.reload(Ok(named("purple")));
        assert!(matches!(live.reload_theme(), Reload::Rejected(e) if e.contains("theme 'purple'")));
        assert_eq!(live.theme().name, "cogitator-phosphor");
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
