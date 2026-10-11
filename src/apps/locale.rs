//! Which localised value a key file offers for the user's locale (Desktop
//! Entry Specification, "Localized values for keys"): for `LC_MESSAGES` of
//! the form `lang_COUNTRY.ENCODING@MODIFIER`, the keys `Key[lang_COUNTRY@MODIFIER]`,
//! `Key[lang_COUNTRY]`, `Key[lang@MODIFIER]` and `Key[lang]` are tried in
//! that order, then the plain `Key`. The encoding is ignored.

/// The locale names a localised key may carry, best first.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Locale {
    candidates: Vec<String>,
}

impl Locale {
    /// From a `LC_MESSAGES` value such as `de_AT.UTF-8@euro`. `C`, `POSIX`
    /// and an empty value have no localised keys.
    pub fn parse(value: &str) -> Self {
        let value = value.trim();
        if value.is_empty() || value == "C" || value == "POSIX" || value.starts_with("C.") {
            return Self::default();
        }
        let (rest, modifier) = match value.split_once('@') {
            Some((rest, modifier)) => (rest, Some(modifier)),
            None => (value, None),
        };
        let rest = rest.split_once('.').map_or(rest, |(r, _)| r);
        let (lang, country) = match rest.split_once('_') {
            Some((lang, country)) => (lang, Some(country)),
            None => (rest, None),
        };
        let mut candidates = Vec::new();
        if let (Some(c), Some(m)) = (country, modifier) {
            candidates.push(format!("{lang}_{c}@{m}"));
        }
        if let Some(c) = country {
            candidates.push(format!("{lang}_{c}"));
        }
        if let Some(m) = modifier {
            candidates.push(format!("{lang}@{m}"));
        }
        if !lang.is_empty() {
            candidates.push(lang.to_owned());
        }
        Self { candidates }
    }

    /// The locale of messages in an environment: `LC_ALL`, then
    /// `LC_MESSAGES`, then `LANG`, the first one set and not empty.
    pub fn from_vars(get: impl Fn(&str) -> Option<String>) -> Self {
        ["LC_ALL", "LC_MESSAGES", "LANG"]
            .into_iter()
            .filter_map(|name| get(name).filter(|v| !v.is_empty()))
            .map(|v| Self::parse(&v))
            .next()
            .unwrap_or_default()
    }

    /// How well a key's locale suits this one: `Some(0)` is the best;
    /// `None` if it does not suit at all.
    pub fn rank(&self, key_locale: &str) -> Option<usize> {
        self.candidates.iter().position(|c| c == key_locale)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ranks(locale: &str, keys: &[&str]) -> Vec<Option<usize>> {
        let l = Locale::parse(locale);
        keys.iter().map(|k| l.rank(k)).collect()
    }

    #[test]
    fn a_full_locale_tries_country_and_modifier_first() {
        let keys = ["sr_YU@Latn", "sr_YU", "sr@Latn", "sr", "de"];
        assert_eq!(
            ranks("sr_YU.UTF-8@Latn", &keys),
            [Some(0), Some(1), Some(2), Some(3), None]
        );
    }

    #[test]
    fn a_locale_without_country_or_modifier_tries_what_it_has() {
        assert_eq!(
            ranks("de_DE.UTF-8", &["de_DE", "de", "de@euro", "de_AT"]),
            [Some(0), Some(1), None, None]
        );
        assert_eq!(ranks("fr", &["fr", "fr_FR"]), [Some(0), None]);
        assert_eq!(ranks("de@euro", &["de@euro", "de"]), [Some(0), Some(1)]);
    }

    #[test]
    fn c_and_posix_have_no_localised_keys() {
        for locale in ["C", "POSIX", "C.UTF-8", ""] {
            assert_eq!(Locale::parse(locale), Locale::default(), "{locale}");
        }
    }

    #[test]
    fn lc_all_outranks_lc_messages_outranks_lang() {
        let env = |pairs: &'static [(&'static str, &'static str)]| {
            move |name: &str| {
                pairs
                    .iter()
                    .find(|(n, _)| *n == name)
                    .map(|(_, v)| (*v).to_owned())
            }
        };
        const ALL: &[(&str, &str)] = &[("LC_ALL", "fi_FI"), ("LC_MESSAGES", "de"), ("LANG", "fr")];
        assert_eq!(Locale::from_vars(env(ALL)), Locale::parse("fi_FI"));
        const MESSAGES: &[(&str, &str)] = &[("LC_ALL", ""), ("LC_MESSAGES", "de"), ("LANG", "fr")];
        assert_eq!(Locale::from_vars(env(MESSAGES)), Locale::parse("de"));
        assert_eq!(
            Locale::from_vars(env(&[("LANG", "fr")])),
            Locale::parse("fr")
        );
        assert_eq!(Locale::from_vars(env(&[])), Locale::default());
    }
}
