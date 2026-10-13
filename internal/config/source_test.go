package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where the config comes from (ported from Rust src/config/source.rs).

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadErr(t *testing.T, s Source) *Error {
	t.Helper()
	_, err := s.Load()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %v", err)
	}
	return e
}

func TestTheDefaultLivesInXDGConfigHome(t *testing.T) {
	if got := defaultPath("/cfg", "/home/u"); got != "/cfg/logis/logis.jsonc" {
		t.Error(got)
	}
}

func TestWithoutAUsableXDGConfigHomeTheDefaultIsUnderHome(t *testing.T) {
	for _, xdg := range []string{"", "rel/cfg"} {
		if got := defaultPath(xdg, "/home/u"); got != "/home/u/.config/logis/logis.jsonc" {
			t.Errorf("%q: %s", xdg, got)
		}
	}
}

func TestWithoutAnyHomeThereIsNoDefault(t *testing.T) {
	if defaultPath("", "") != "" || defaultPath("rel", "also/rel") != "" {
		t.Error("a default from nothing")
	}
	if c, err := (Source{}).Load(); err != nil || !c.Equal(Default()) {
		t.Errorf("%+v %v", c, err)
	}
}

func TestTheDefaultIsTakenFromTheEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if got := NewSource("").Path(); got != "/cfg/logis/logis.jsonc" {
		t.Error(got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := NewSource("").Path(); got != "/home/u/.config/logis/logis.jsonc" {
		t.Error(got)
	}
}

func TestARelativeConfigFlagIsMadeAbsolute(t *testing.T) {
	path := NewSource("some/logis.jsonc").Path()
	if !filepath.IsAbs(path) || !strings.HasSuffix(path, "/some/logis.jsonc") {
		t.Error(path)
	}
}

func TestAMissingDefaultFileMeansTheDefaults(t *testing.T) {
	s := Source{path: filepath.Join(t.TempDir(), "logis.jsonc")}
	if s.Exists() {
		t.Fatal("exists")
	}
	if c, err := s.Load(); err != nil || !c.Equal(Default()) {
		t.Errorf("%+v %v", c, err)
	}
}

func TestAMissingNamedFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nowhere.jsonc")
	e := loadErr(t, NewSource(path))
	if e.Problem != Missing || e.Error() != path+": no such file" {
		t.Errorf("%v %q", e.Problem, e.Error())
	}
}

func TestAFileIsReadAndParsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	write(t, path, "{\"focus-loss\": \"keep\", // stay\n}")
	c, err := NewSource(path).Load()
	if err != nil || c.FocusLoss != FocusKeep {
		t.Errorf("%+v %v", c, err)
	}
}

func TestAnInvalidFileIsReportedWithPathLineAndColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	write(t, path, "{\n  \"thme\": 1\n}")
	e := loadErr(t, NewSource(path))
	if e.Problem != Invalid || !strings.HasPrefix(e.Error(), path+":2:8: unknown field `thme`") {
		t.Errorf("%v %q", e.Problem, e.Error())
	}
}

func TestAnUnreadableFileIsAnError(t *testing.T) {
	// A directory where the file should be cannot be read as text.
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	e := loadErr(t, Source{path: path})
	if e.Problem != Unreadable || !strings.HasPrefix(e.Error(), path+": ") {
		t.Errorf("%v %q", e.Problem, e.Error())
	}
}

func TestAFileThatIsNotUTF8IsUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	write(t, path, "{\"theme\": \"\xff\"}")
	e := loadErr(t, Source{path: path})
	if e.Problem != Unreadable || e.Error() != path+": stream did not contain valid UTF-8" {
		t.Errorf("%v %q", e.Problem, e.Error())
	}
}

func TestHeresiesAreWordedAsInRust(t *testing.T) {
	e := &Error{Path: "/c.jsonc", Problem: Missing}
	if got := Heresy(e); got != "the config is heretical: /c.jsonc: no such file" {
		t.Error(got)
	}
	if got := ThemeHeresy(errors.New("x: y")); got != "the theme is heretical: x: y" {
		t.Error(got)
	}
}

// --check-config's verdict (Rust src/config/mod.rs check).

func TestCheckNamesTheSoundConfigAndItsTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	write(t, path, `{"theme": "cogitator-phosphor"}`)
	var asked [2]string
	verdict, err := Check(NewSource(path), func(dir, name string) error {
		asked = [2]string{dir, name}
		return nil
	})
	want := "the config at " + path + " is sound; the theme 'cogitator-phosphor' is sound"
	if err != nil || verdict != want {
		t.Errorf("%q %v", verdict, err)
	}
	if asked != [2]string{filepath.Join(filepath.Dir(path), "themes"), "cogitator-phosphor"} {
		t.Errorf("asked %q", asked)
	}
}

func TestCheckWithoutAFileAcceptsTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	verdict, err := Check(Source{path: path}, FindTheme)
	want := "no config at " + path + "; the defaults stand; the theme 'forge-crimson-brass' is sound"
	if err != nil || verdict != want {
		t.Errorf("%q %v", verdict, err)
	}
	verdict, err = Check(Source{}, FindTheme)
	want = "no config to be found (neither XDG_CONFIG_HOME nor HOME is set); the defaults stand; " +
		"the theme 'forge-crimson-brass' is sound"
	if err != nil || verdict != want {
		t.Errorf("%q %v", verdict, err)
	}
}

func TestCheckReportsAHereticalConfigOrTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	write(t, path, `{"thme": 1}`)
	if _, err := Check(NewSource(path), FindTheme); err == nil ||
		!strings.HasPrefix(err.Error(), "the config is heretical: "+path+":1:7: unknown field") {
		t.Errorf("%v", err)
	}
	write(t, path, `{"theme": "purple"}`)
	_, err := Check(NewSource(path), FindTheme)
	themes := filepath.Join(filepath.Dir(path), "themes")
	want := "the theme is heretical: theme 'purple': no such theme is bundled " +
		"(forge-crimson-brass, cogitator-phosphor) nor in " + themes
	if err == nil || err.Error() != want {
		t.Errorf("%v", err)
	}
}

func TestFindThemeLooksBesideTheConfigThenAmongTheBundled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "themes")
	write(t, filepath.Join(dir, "mine.jsonc"), "{}")
	for _, name := range []string{"mine", "forge-crimson-brass", "cogitator-phosphor"} {
		if err := FindTheme(dir, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := FindTheme("", "mine"); err == nil ||
		err.Error() != "theme 'mine': no such theme is bundled (forge-crimson-brass, cogitator-phosphor)" {
		t.Errorf("%v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "odd.jsonc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := FindTheme(dir, "odd"); err == nil || !strings.HasPrefix(err.Error(), filepath.Join(dir, "odd.jsonc")+": ") {
		t.Errorf("an unreadable theme file: %v", err)
	}
}
