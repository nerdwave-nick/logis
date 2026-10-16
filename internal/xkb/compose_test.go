package xkb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// table is the tests' own table (as in internal/input): no test depends
// on the machine's locale or anyone's ~/.XCompose.
const table = `
<Multi_key> <o> <quotedbl> : "ö" odiaeresis
<Multi_key> <e> <e> : "e` + "́" + `"
<dead_acute> <a> : "á" aacute
`

func feed(s *ComposeState, syms ...Keysym) (ComposeStatus, string) {
	for _, ks := range syms {
		s.Feed(ks)
	}
	return s.Status(), s.UTF8()
}

func TestASequenceComposes(t *testing.T) {
	ct, err := ComposeFromString(ComposeEnv{}, table, "C")
	if err != nil {
		t.Fatal(err)
	}
	s := ct.NewState()
	if st, _ := feed(s, KeyMultiKey, 'o'); st != ComposeComposing {
		t.Fatalf("Multi_key o: %v", st)
	}
	if st, text := feed(s, '"'); st != ComposeComposed || text != "ö" || s.Keysym() != KeysymFromName("odiaeresis") {
		t.Fatalf("Multi_key o \": %v %q %v", st, text, s.Keysym())
	}
	s.Reset()
	if st, text := feed(s, KeyMultiKey, 'e', 'e'); st != ComposeComposed || text != "é" {
		t.Errorf("a multi-code-point result: %v %q", st, text)
	}
	s.Reset()
	if st, text := feed(s, KeyDeadAcute, 'a'); st != ComposeComposed || text != "á" {
		t.Errorf("a dead key: %v %q", st, text)
	}
	if ct.Len() != 3 {
		t.Errorf("%d sequences", ct.Len())
	}
}

func TestModifierKeysymsNeverCancelASequence(t *testing.T) {
	ct, _ := ComposeFromString(ComposeEnv{}, table, "C")
	s := ct.NewState()
	feed(s, KeyMultiKey, 'o')
	for _, m := range []string{"Shift_L", "Shift_R", "Control_L", "ISO_Level3_Shift", "Num_Lock", "Caps_Lock"} {
		if s.Feed(KeysymFromName(m)) || s.Status() != ComposeComposing {
			t.Errorf("%s was fed or changed the sequence: %v", m, s.Status())
		}
	}
	if st, text := feed(s, '"'); st != ComposeComposed || text != "ö" {
		t.Errorf("after modifiers: %v %q", st, text)
	}
}

func TestAKeyOutsideTheTableCancels(t *testing.T) {
	ct, _ := ComposeFromString(ComposeEnv{}, table, "C")
	s := ct.NewState()
	if st, _ := feed(s, KeyMultiKey, 'x'); st != ComposeCancelled {
		t.Errorf("Multi_key x: %v", st)
	}
	s.Reset()
	if st, _ := feed(s, 'x'); st != ComposeNothing {
		t.Errorf("x alone: %v", st)
	}
}

func TestTheLocaleIsLcAllThenLcCtypeThenLangThenC(t *testing.T) {
	for _, c := range []struct{ all, ctype, lang, want string }{
		{"de_DE.UTF-8", "x", "y", "de_DE.UTF-8"},
		{"", "fr_FR.UTF-8", "y", "fr_FR.UTF-8"},
		{"", "", "en_GB.UTF-8", "en_GB.UTF-8"},
		{"", "", "", "C"},
	} {
		if got := ComposeLocale(c.all, c.ctype, c.lang); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

// localeDir is a system Compose directory: compose.dir, locale.alias and
// one table for en_US.UTF-8 and one for de_DE.UTF-8.
func localeDir(t *testing.T) string {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "compose.dir"), "# comment\nen_US.UTF-8/Compose:\t\ten_US.UTF-8\nde_DE.UTF-8/Compose:  de_DE.UTF-8\n")
	write(t, filepath.Join(dir, "locale.alias"), "german:  de_DE.UTF-8\n")
	write(t, filepath.Join(dir, "en_US.UTF-8", "Compose"), "<Multi_key> <a> <a> : \"system-en\"\n")
	write(t, filepath.Join(dir, "de_DE.UTF-8", "Compose"), "<Multi_key> <a> <a> : \"system-de\"\n")
	return dir
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func composed(t *testing.T, env ComposeEnv, locale string) string {
	t.Helper()
	ct, err := ComposeFromLocale(env, locale)
	if err != nil {
		t.Fatal(err)
	}
	_, text := feed(ct.NewState(), KeyMultiKey, 'a', 'a')
	return text
}

// libxkbcommon's lookup: XCOMPOSEFILE, $XDG_CONFIG_HOME/XCompose (else
// ~/.config/XCompose), ~/.XCompose, then the locale's system table; a
// file that cannot be read is passed over.
func TestComposeFilesAreLookedUpInLibxkbcommonsOrder(t *testing.T) {
	sys, home, xdg := localeDir(t), t.TempDir(), t.TempDir()
	env := ComposeEnv{XComposeFile: filepath.Join(home, "xcf"), XDGConfigHome: xdg, Home: home, XLocaleDir: sys}
	files := map[string]string{
		env.XComposeFile:                        "xcomposefile",
		filepath.Join(xdg, "XCompose"):          "xdg",
		filepath.Join(home, ".XCompose"):        "home",
		filepath.Join(home, ".config/XCompose"): "dot-config",
	}
	for path, name := range files {
		write(t, path, "<Multi_key> <a> <a> : \""+name+"\"\n")
	}
	steps := []struct {
		remove string
		want   string
	}{
		{"", "xcomposefile"},
		{env.XComposeFile, "xdg"},
		{filepath.Join(xdg, "XCompose"), "home"},
		{filepath.Join(home, ".XCompose"), "system-en"},
	}
	for _, s := range steps {
		if s.remove != "" {
			if err := os.Remove(s.remove); err != nil {
				t.Fatal(err)
			}
		}
		if got := composed(t, env, "en_US.UTF-8"); got != s.want {
			t.Errorf("without %s: %q, want %q", s.remove, got, s.want)
		}
	}
	relative := env
	relative.XDGConfigHome = "relative/dir"
	if got := composed(t, relative, "en_US.UTF-8"); got != "dot-config" {
		t.Errorf("a relative XDG_CONFIG_HOME: %q, want ~/.config/XCompose", got)
	}
}

func TestTheSystemTableFollowsTheLocale(t *testing.T) {
	env := ComposeEnv{XLocaleDir: localeDir(t), Home: t.TempDir()}
	for locale, want := range map[string]string{
		"de_DE.UTF-8": "system-de",
		"german":      "system-de", // locale.alias
		"C":           "system-en", // C is en_US.UTF-8
		"fi_FI.UTF-8": "system-en", // no table: en_US.UTF-8
	} {
		if got := composed(t, env, locale); got != want {
			t.Errorf("%s: %q, want %q", locale, got, want)
		}
	}
	if _, err := ComposeFromLocale(ComposeEnv{XLocaleDir: t.TempDir()}, "C"); err == nil {
		t.Error("a table without any Compose file")
	}
}

func TestIncludesExpandHomeLocaleAndSystem(t *testing.T) {
	sys, home := localeDir(t), t.TempDir()
	write(t, filepath.Join(home, "mine"), "<Multi_key> <b> <b> : \"mine\"\n")
	env := ComposeEnv{XLocaleDir: sys, Home: home}
	text := "include \"%L\"\ninclude \"%H/mine\"\ninclude \"%S/de_DE.UTF-8/Compose\"\n<Multi_key> <c> <c> : \"own\"\n"
	ct, err := ComposeFromString(env, text, "en_US.UTF-8")
	if err != nil {
		t.Fatal(err)
	}
	for seq, want := range map[string]string{"aa": "system-de", "bb": "mine", "cc": "own"} {
		if _, got := feed(ct.NewState(), KeyMultiKey, Keysym(seq[0]), Keysym(seq[1])); got != want {
			t.Errorf("%s: %q, want %q", seq, got, want)
		}
	}
}

func TestBrokenTablesAreRefusedOrSkippedAsLibxkbcommonDoes(t *testing.T) {
	bad := strings.Repeat("<Multi_key> <nosuchkeysym> : \"x\"\n", composeMaxErrors+1)
	if _, err := ComposeFromString(ComposeEnv{}, bad, "C"); err == nil {
		t.Error("more than 10 bad lines were accepted")
	}
	some := strings.Repeat("<Multi_key> <nosuchkeysym> : \"x\"\n", composeMaxErrors) + "<Multi_key> <v> : \"ok\"\n"
	ct, err := ComposeFromString(ComposeEnv{}, some, "C")
	if err != nil || ct.Len() != 1 {
		t.Errorf("10 bad lines and a good one: %v, %v", ct, err)
	}
	dir := t.TempDir()
	loop := filepath.Join(dir, "loop")
	write(t, loop, "include \""+loop+"\"\n<Multi_key> <l> : \"l\"\n")
	if ct, err := ComposeFromString(ComposeEnv{}, "include \""+loop+"\"\n", "C"); err != nil || ct.Len() != 1 {
		t.Errorf("an include loop: %v, %v (the depth limit ends it)", ct, err)
	}
}
