package apps

import "strings"

// Locale is which localised value a key file offers for the user's locale
// (Desktop Entry Specification, "Localized values for keys"): for
// LC_MESSAGES of the form
// lang_COUNTRY.ENCODING@MODIFIER, the keys Key[lang_COUNTRY@MODIFIER],
// Key[lang_COUNTRY], Key[lang@MODIFIER] and Key[lang] are tried in that
// order, then the plain Key. The encoding is ignored.
type Locale struct {
	// candidates are the locale names a localised key may carry, best
	// first.
	candidates []string
}

// ParseLocale reads an LC_MESSAGES value such as de_AT.UTF-8@euro. C, POSIX
// and an empty value have no localised keys.
func ParseLocale(value string) Locale {
	value = strings.TrimSpace(value)
	if value == "" || value == "C" || value == "POSIX" || strings.HasPrefix(value, "C.") {
		return Locale{}
	}
	rest, modifier, hasModifier := strings.Cut(value, "@")
	rest, _, _ = strings.Cut(rest, ".")
	lang, country, hasCountry := strings.Cut(rest, "_")
	var c []string
	if hasCountry && hasModifier {
		c = append(c, lang+"_"+country+"@"+modifier)
	}
	if hasCountry {
		c = append(c, lang+"_"+country)
	}
	if hasModifier {
		c = append(c, lang+"@"+modifier)
	}
	if lang != "" {
		c = append(c, lang)
	}
	return Locale{candidates: c}
}

// LocaleFromVars is the locale of messages in an environment: LC_ALL, then
// LC_MESSAGES, then LANG, the first one set and not empty.
func LocaleFromVars(get func(string) string) Locale {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := get(name); v != "" {
			return ParseLocale(v)
		}
	}
	return Locale{}
}

// Rank is how well a key's locale suits this one: 0 is the best; false if
// it does not suit at all.
func (l Locale) Rank(keyLocale string) (int, bool) {
	for i, c := range l.candidates {
		if c == keyLocale {
			return i, true
		}
	}
	return 0, false
}
