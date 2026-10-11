//! Compose sequences (dead keys and the Compose key) from the locale's
//! Compose table, as libxkbcommon finds it: `XCOMPOSEFILE` if set, else
//! `$XDG_CONFIG_HOME/XCompose`, else `~/.XCompose`, else the system table
//! for the locale.

use std::ffi::{OsStr, OsString};

use xkbcommon::xkb::{self, Keysym, compose};

/// A Compose table and the sequence typed so far.
pub struct Compose {
    state: compose::State,
}

/// What feeding one keysym did.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Fed {
    /// Not part of a sequence: the key means what it always means.
    Pass,
    /// Swallowed: a sequence goes on, or the key cancelled one.
    Swallowed,
    /// A sequence completed with this text.
    Composed(String),
}

/// The locale whose Compose table applies: `LC_ALL`, `LC_CTYPE` or `LANG`,
/// the first that is set and not empty, else `C` (the POSIX order).
pub fn compose_locale(
    lc_all: Option<&OsStr>,
    lc_ctype: Option<&OsStr>,
    lang: Option<&OsStr>,
) -> OsString {
    [lc_all, lc_ctype, lang]
        .into_iter()
        .flatten()
        .find(|v| !v.is_empty())
        .unwrap_or(OsStr::new("C"))
        .to_owned()
}

impl Compose {
    /// The table for the process's locale, or for `C` if that locale has
    /// none; `None` if neither loads (keys then pass through uncomposed).
    pub fn from_env(context: &xkb::Context) -> Option<Self> {
        let var = |name| std::env::var_os(name);
        let locale = compose_locale(
            var("LC_ALL").as_deref(),
            var("LC_CTYPE").as_deref(),
            var("LANG").as_deref(),
        );
        let load = |locale: &OsStr| {
            compose::Table::new_from_locale(context, locale, compose::COMPILE_NO_FLAGS).ok()
        };
        load(&locale)
            .or_else(|| load(OsStr::new("C")))
            .map(|table| Self::new(&table))
    }

    /// Composes with `table`.
    pub fn new(table: &compose::Table) -> Self {
        Self {
            state: compose::State::new(table, compose::STATE_NO_FLAGS),
        }
    }

    /// Feeds a pressed key's keysym. Modifier keysyms are never fed: a
    /// Shift pressed for `"` must not cancel the sequence it belongs to.
    pub fn feed(&mut self, keysym: Keysym) -> Fed {
        if keysym.is_modifier_key() || self.state.feed(keysym) == xkb::FeedResult::Ignored {
            return Fed::Pass;
        }
        match self.state.status() {
            xkb::Status::Nothing => Fed::Pass,
            xkb::Status::Composing => Fed::Swallowed,
            xkb::Status::Composed => {
                let text = self.state.utf8().unwrap_or_default();
                self.state.reset();
                Fed::Composed(text)
            }
            xkb::Status::Cancelled => {
                self.state.reset();
                Fed::Swallowed
            }
        }
    }

    /// Forgets a sequence in progress (focus or keymap changed).
    pub fn reset(&mut self) {
        self.state.reset();
    }
}

#[cfg(test)]
pub(super) mod tests {
    use super::*;

    /// A table of its own, so no test depends on the machine's locale or
    /// on anyone's ~/.XCompose.
    pub const TABLE: &str = "
<Multi_key> <o> <quotedbl> : \"ö\" odiaeresis
<Multi_key> <e> <e> : \"e\u{301}\"
<dead_acute> <a> : \"á\" aacute
";

    pub fn compose() -> Compose {
        let context = xkb::Context::new(xkb::CONTEXT_NO_FLAGS);
        let table = compose::Table::new_from_buffer(
            &context,
            TABLE,
            "C",
            compose::FORMAT_TEXT_V1,
            compose::COMPILE_NO_FLAGS,
        )
        .expect("the test table compiles");
        Compose::new(&table)
    }

    #[test]
    fn a_sequence_composes_and_swallows_its_keys() {
        let mut c = compose();
        assert_eq!(c.feed(Keysym::Multi_key), Fed::Swallowed);
        assert_eq!(c.feed(Keysym::o), Fed::Swallowed);
        assert_eq!(c.feed(Keysym::quotedbl), Fed::Composed("ö".into()));
        assert_eq!(c.feed(Keysym::o), Fed::Pass, "the sequence is over");
    }

    #[test]
    fn shift_pressed_inside_a_sequence_never_cancels_it() {
        let mut c = compose();
        c.feed(Keysym::Multi_key);
        c.feed(Keysym::o);
        for modifier in [
            Keysym::Shift_L,
            Keysym::Shift_R,
            Keysym::Control_L,
            Keysym::ISO_Level3_Shift,
        ] {
            assert_eq!(c.feed(modifier), Fed::Pass, "{modifier:?}");
        }
        assert_eq!(c.feed(Keysym::quotedbl), Fed::Composed("ö".into()));
    }

    #[test]
    fn a_multi_codepoint_result_and_dead_keys_compose() {
        let mut c = compose();
        c.feed(Keysym::Multi_key);
        c.feed(Keysym::e);
        assert_eq!(c.feed(Keysym::e), Fed::Composed("e\u{301}".into()));
        assert_eq!(c.feed(Keysym::dead_acute), Fed::Swallowed);
        assert_eq!(c.feed(Keysym::a), Fed::Composed("á".into()));
    }

    #[test]
    fn a_key_outside_the_table_cancels_and_is_swallowed() {
        let mut c = compose();
        c.feed(Keysym::Multi_key);
        assert_eq!(c.feed(Keysym::x), Fed::Swallowed);
        assert_eq!(c.feed(Keysym::x), Fed::Pass);
    }

    #[test]
    fn reset_forgets_a_sequence_in_progress() {
        let mut c = compose();
        c.feed(Keysym::Multi_key);
        c.feed(Keysym::o);
        c.reset();
        assert_eq!(c.feed(Keysym::quotedbl), Fed::Pass);
    }

    #[test]
    fn the_locale_is_lc_all_then_lc_ctype_then_lang_then_c() {
        let s = |v: &'static str| Some(OsStr::new(v));
        assert_eq!(
            compose_locale(s("de_DE.UTF-8"), s("x"), s("y")),
            "de_DE.UTF-8"
        );
        assert_eq!(
            compose_locale(s(""), s("fr_FR.UTF-8"), s("y")),
            "fr_FR.UTF-8"
        );
        assert_eq!(compose_locale(None, None, s("en_GB.UTF-8")), "en_GB.UTF-8");
        assert_eq!(compose_locale(None, s(""), None), "C");
    }
}
