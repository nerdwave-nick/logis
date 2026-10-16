package apps

import (
	"reflect"
	"testing"
)

// Ported from Rust src/apps/locale.rs.

func ranks(locale string, keys ...string) []int {
	l := ParseLocale(locale)
	out := make([]int, len(keys))
	for i, k := range keys {
		if r, ok := l.Rank(k); ok {
			out[i] = r
		} else {
			out[i] = -1
		}
	}
	return out
}

func TestAFullLocaleTriesCountryAndModifierFirst(t *testing.T) {
	got := ranks("sr_YU.UTF-8@Latn", "sr_YU@Latn", "sr_YU", "sr@Latn", "sr", "de")
	if want := []int{0, 1, 2, 3, -1}; !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

func TestALocaleWithoutCountryOrModifierTriesWhatItHas(t *testing.T) {
	for _, tc := range []struct {
		locale string
		keys   []string
		want   []int
	}{
		{"de_DE.UTF-8", []string{"de_DE", "de", "de@euro", "de_AT"}, []int{0, 1, -1, -1}},
		{"fr", []string{"fr", "fr_FR"}, []int{0, -1}},
		{"de@euro", []string{"de@euro", "de"}, []int{0, 1}},
	} {
		if got := ranks(tc.locale, tc.keys...); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.locale, got, tc.want)
		}
	}
}

func TestCAndPOSIXHaveNoLocalisedKeys(t *testing.T) {
	for _, l := range []string{"C", "POSIX", "C.UTF-8", ""} {
		if got := ParseLocale(l); !reflect.DeepEqual(got, Locale{}) {
			t.Errorf("%q: %+v", l, got)
		}
	}
}

func vars(pairs ...string) func(string) string {
	return func(name string) string {
		for i := 0; i+1 < len(pairs); i += 2 {
			if pairs[i] == name {
				return pairs[i+1]
			}
		}
		return ""
	}
}

func TestLCAllOutranksLCMessagesOutranksLang(t *testing.T) {
	for _, tc := range []struct {
		env  func(string) string
		want Locale
	}{
		{vars("LC_ALL", "fi_FI", "LC_MESSAGES", "de", "LANG", "fr"), ParseLocale("fi_FI")},
		{vars("LC_ALL", "", "LC_MESSAGES", "de", "LANG", "fr"), ParseLocale("de")},
		{vars("LANG", "fr"), ParseLocale("fr")},
		{vars(), Locale{}},
	} {
		if got := LocaleFromVars(tc.env); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%+v, want %+v", got, tc.want)
		}
	}
}
