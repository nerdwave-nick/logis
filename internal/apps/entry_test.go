package apps

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// Ported from Rust src/apps/entry.rs.

// fixtures is tests/fixtures/apps in the repository.
func fixtures(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "tests", "fixtures", "apps")
}

func forgeAnoint(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures(t), "system/applications/forge-anoint.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func de() Locale { return ParseLocale("de_DE.UTF-8") }

func TestAnApplicationEntryIsReadWithItsLocalisedNames(t *testing.T) {
	e, err := ParseEntry(forgeAnoint(t), de())
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "Salbung" || e.GenericName != "Weihöl" || e.Icon != "forge-cog" {
		t.Errorf("%q %q %q", e.Name, e.GenericName, e.Icon)
	}
	wantArgs(t, e.Keywords, "öl", "salbe")
	if e.Exec == nil || e.Exec.Program() != "anoint" {
		t.Errorf("exec %+v", e.Exec)
	}
	if e.Terminal || e.NoDisplay || e.Hidden || e.DBusActivatable {
		t.Errorf("%+v", e)
	}
	c, err := ParseEntry(forgeAnoint(t), Locale{})
	if err != nil || c.Name != "Anoint" || len(c.Keywords) != 3 {
		t.Errorf("%q %q %v", c.Name, c.Keywords, err)
	}
}

func TestActionsComeInTheirDeclaredOrderAndBadOnesAreSkipped(t *testing.T) {
	e, err := ParseEntry(forgeAnoint(t), de())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range e.Actions {
		got = append(got, a.ID+"="+a.Name)
	}
	// nameless has no Name, broken an invalid Exec, absent no group.
	wantArgs(t, got, "purge=Läutern", "bless=Segnen")
	if e.Actions[0].Icon != "forge-purge" || e.Actions[1].Exec.Program() != "anoint" {
		t.Errorf("%+v", e.Actions)
	}
}

func entry(body string) (DesktopEntry, error) {
	return ParseEntry("[Desktop Entry]\n"+body, Locale{})
}

func TestWhatMakesAnEntryUnusable(t *testing.T) {
	var notApp *NotAnApplicationError
	if _, err := entry("Type=Link\nName=x\nURL=y"); !errors.As(err, &notApp) || notApp.Type != "Link" {
		t.Errorf("a link: %v", err)
	}
	if _, err := entry("Name=x\nExec=y"); !errors.As(err, &notApp) || notApp.Type != "" {
		t.Errorf("no Type: %v", err)
	}
	if _, err := entry("Type=Application\nExec=y"); !errors.Is(err, ErrNoName) {
		t.Errorf("no Name: %v", err)
	}
	if _, err := entry("Type=Application\nName=x"); !errors.Is(err, ErrNoExec) {
		t.Errorf("no Exec: %v", err)
	}
	var bad *ExecError
	if _, err := entry("Type=Application\nName=x\nExec=y %q"); !errors.As(err, &bad) || bad.Code != 'q' {
		t.Errorf("bad Exec: %v", err)
	}
	if _, err := ParseEntry("[Other]\nName=x", Locale{}); !errors.Is(err, ErrNoDesktopEntry) {
		t.Errorf("no group: %v", err)
	}
	var invalid *InvalidError
	if _, err := ParseEntry("garbage", Locale{}); !errors.As(err, &invalid) {
		t.Errorf("garbage: %v", err)
	}
}

func TestAHiddenEntryNeedsNothingElseAndDBusNeedsNoExec(t *testing.T) {
	hidden, err := entry("Hidden=true")
	if err != nil || !hidden.Hidden {
		t.Errorf("%+v %v", hidden, err)
	}
	dbus, err := entry("Type=Application\nName=Bus\nDBusActivatable=true\nActions=a\n\n[Desktop Action a]\nName=A")
	if err != nil || !dbus.DBusActivatable || dbus.Exec != nil {
		t.Errorf("%+v %v", dbus, err)
	}
	if len(dbus.Actions) != 1 {
		t.Errorf("a D-Bus action needs no Exec: %+v", dbus.Actions)
	}
}

func TestVisibilityAndLaunchKeysAreRead(t *testing.T) {
	e, err := entry("Type=Application\nName=x\nExec=y\nTryExec=/bin/forge\nPath=/srv\nTerminal=true\n" +
		"NoDisplay=true\nOnlyShowIn=niri;sway;\nNotShowIn=GNOME;")
	if err != nil {
		t.Fatal(err)
	}
	if e.TryExec != "/bin/forge" || e.Path != "/srv" || !e.Terminal || !e.NoDisplay {
		t.Errorf("%+v", e)
	}
	if !slices.Equal(e.OnlyShowIn, []string{"niri", "sway"}) || !slices.Equal(e.NotShowIn, []string{"GNOME"}) {
		t.Errorf("%q %q", e.OnlyShowIn, e.NotShowIn)
	}
}
