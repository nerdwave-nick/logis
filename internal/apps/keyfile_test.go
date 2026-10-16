package apps

import (
	"errors"
	"slices"
	"testing"
)

// Parsing key files: groups, keys, localised keys and escapes.

const keyFileText = "# a comment before the first group\n" +
	"[Desktop Entry]\n" +
	"Type=Application\n" +
	"Name = Rite of Oil\n" +
	"Name[de]=Ölritus\n" +
	"Name[de_AT]=Ölritual\n" +
	`Comment=anoint\sthe\tvessel\\now\;` + "\n" +
	`Exec=sh -c "echo \\"\$x\\""` + "\n" +
	`Keywords=oil;unguent\;holy;;` + "\n" +
	"Terminal=true\n" +
	"NoDisplay=false\n" +
	"Name=a duplicate\n" +
	"\n" +
	"[Desktop Action anoint]\n" +
	"Name=Anoint\n"

func desktopEntryGroup(t *testing.T) *Group {
	t.Helper()
	f, err := ParseKeyFile(keyFileText)
	if err != nil {
		t.Fatal(err)
	}
	g, ok := f.Group("Desktop Entry")
	if !ok {
		t.Fatal("no [Desktop Entry]")
	}
	return g
}

func TestGroupsKeysAndCommentsAreRead(t *testing.T) {
	f, err := ParseKeyFile(keyFileText)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range f.Groups() {
		names = append(names, g.Name)
	}
	if want := []string{"Desktop Entry", "Desktop Action anoint"}; !slices.Equal(names, want) {
		t.Errorf("groups %q, want %q", names, want)
	}
	entry := desktopEntryGroup(t)
	if v, _ := entry.Raw("Type"); v != "Application" {
		t.Errorf("Type %q", v)
	}
	if v, _ := entry.Raw("Name"); v != "Rite of Oil" {
		t.Errorf("Name %q, want the first duplicate", v)
	}
	action, _ := f.Group("Desktop Action anoint")
	if v, _ := action.Raw("Name"); v != "Anoint" {
		t.Errorf("action Name %q", v)
	}
	if _, ok := entry.Raw("Missing"); ok {
		t.Error("a missing key was found")
	}
}

func TestStringsUnescapeTheSpecEscapesOnly(t *testing.T) {
	entry := desktopEntryGroup(t)
	if v, _ := entry.String("Comment"); v != "anoint the\tvessel\\now\\;" {
		t.Errorf("Comment %q", v)
	}
	// \\ becomes \, the Exec quoting's own escapes stay for it.
	if v, _ := entry.String("Exec"); v != `sh -c "echo \"\$x\""` {
		t.Errorf("Exec %q", v)
	}
}

func TestListsSplitAtUnescapedSemicolons(t *testing.T) {
	entry := desktopEntryGroup(t)
	if got, want := entry.List("Keywords"), []string{"oil", "unguent;holy"}; !slices.Equal(got, want) {
		t.Errorf("Keywords %q, want %q", got, want)
	}
	if got := entry.List("Missing"); len(got) != 0 {
		t.Errorf("Missing %q", got)
	}
}

func TestBooleansAreTrueOnlyWhenTrue(t *testing.T) {
	entry := desktopEntryGroup(t)
	if !entry.Bool("Terminal") || entry.Bool("NoDisplay") || entry.Bool("Hidden") {
		t.Error("Terminal true, NoDisplay and Hidden false expected")
	}
}

func TestLocalisedValuesFollowTheLocale(t *testing.T) {
	entry := desktopEntryGroup(t)
	for locale, want := range map[string]string{
		"de_AT.UTF-8": "Ölritual",
		"de_CH":       "Ölritus",
		"fr_FR":       "Rite of Oil",
		"C":           "Rite of Oil",
	} {
		if got, _ := entry.Localized("Name", ParseLocale(locale)); got != want {
			t.Errorf("%s: %q, want %q", locale, got, want)
		}
	}
}

func TestMalformedLinesMakeTheFileInvalid(t *testing.T) {
	for _, tc := range []struct {
		text string
		line int
	}{
		{"[Desktop Entry]\nno equals sign\n", 2},
		{"Name=before any group\n", 1},
		{"[Desktop Entry]\nName[=x\n", 2},
		{"[Desktop Entry\n", 1},
		{"[Desktop Entry]\nNa me=x\n", 2},
	} {
		_, err := ParseKeyFile(tc.text)
		var invalid *InvalidError
		if !errors.As(err, &invalid) || invalid.Line != tc.line {
			t.Errorf("%q: %v, want line %d", tc.text, err, tc.line)
		}
	}
}
