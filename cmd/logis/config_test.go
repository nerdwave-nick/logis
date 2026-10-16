package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --check-config and --config at the command line. Every test uses a
// scratch XDG_CONFIG_HOME, never the real one.

func scratchConfigHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "config")
	t.Setenv("XDG_CONFIG_HOME", home)
	return home
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runLogis(args ...string) (code int, stdout, stderr string) {
	var out, errs bytes.Buffer
	code = run(args, &out, &errs)
	return code, out.String(), errs.String()
}

func TestCheckConfigAcceptsASoundConfigAndExits0(t *testing.T) {
	scratchConfigHome(t)
	path := filepath.Join(t.TempDir(), "mine.jsonc")
	writeFile(t, path, "{\n  // brass and fire\n  \"theme\": \"cogitator-phosphor\",\n  \"terminal\": [\"foot\", \"-e\"],\n}\n")
	code, stdout, stderr := runLogis("--check-config", "--config", path)
	want := "logis: the config at " + path + " is sound; the theme 'cogitator-phosphor' is sound\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestCheckConfigWithoutAConfigFileAcceptsTheDefaults(t *testing.T) {
	home := scratchConfigHome(t)
	code, stdout, stderr := runLogis("--check-config")
	expected := filepath.Join(home, "logis", "logis.jsonc")
	if code != 0 || !strings.Contains(stdout, expected) || !strings.Contains(stdout, "defaults") {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestCheckConfigReadsXDGConfigHomeAndLocatesAnUnknownKey(t *testing.T) {
	home := scratchConfigHome(t)
	path := filepath.Join(home, "logis", "logis.jsonc")
	writeFile(t, path, "{\n  \"thme\": \"x\"\n}\n")
	code, stdout, stderr := runLogis("--check-config")
	if code != 2 || stdout != "" {
		t.Errorf("exit %d stdout %q", code, stdout)
	}
	if !strings.HasPrefix(stderr, "logis: the config is heretical") ||
		!strings.Contains(stderr, path+":2:8: unknown field `thme`") {
		t.Errorf("stderr %q", stderr)
	}
}

func TestCheckConfigNamesTheKeyOfAWrongValue(t *testing.T) {
	scratchConfigHome(t)
	path := filepath.Join(t.TempDir(), "logis.jsonc")
	writeFile(t, path, `{"focus-loss": "linger"}`)
	code, _, stderr := runLogis("--config", path, "--check-config")
	if code != 2 || !strings.Contains(stderr, path+":1:") ||
		!strings.Contains(stderr, "`focus-loss`") || !strings.Contains(stderr, "linger") {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
}

// The theme is looked up beside the config or among the bundled ones, and
// read with what it extends: its contents are judged too.
func TestCheckConfigJudgesTheThemesContents(t *testing.T) {
	home := scratchConfigHome(t)
	writeFile(t, filepath.Join(home, "logis", "logis.jsonc"), `{"theme": "mine"}`)
	mine := filepath.Join(home, "logis", "themes", "mine.jsonc")
	writeFile(t, mine, "{\n  \"glow\": 1\n}")
	code, stdout, stderr := runLogis("--check-config")
	want := "logis: the theme is heretical: " + mine + ":2:11: invalid type: integer `1`, expected struct GlowDoc\n"
	if code != 2 || stdout != "" || stderr != want {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	writeFile(t, mine, `{"effects": {"bloom": 2}}`)
	code, _, stderr = runLogis("--check-config")
	if want := "logis: the theme is heretical: " + mine + ": effects.bloom: 2 is out of range (0 to 1)\n"; code != 2 || stderr != want {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	// The CRT pass's settings (cd816303): each held to its range, a word
	// to its words.
	writeFile(t, mine, `{"effects": {"scanlines": {"period": 40}}}`)
	code, _, stderr = runLogis("--check-config")
	if want := "logis: the theme is heretical: " + mine + ": effects.scanlines.period: 40 is out of range (1 to 16)\n"; code != 2 || stderr != want {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	writeFile(t, mine, `{"effects": {"mask": {"kind": "hex"}}}`)
	code, _, stderr = runLogis("--check-config")
	if want := "logis: the theme is heretical: " + mine + ":1:35: unknown variant `hex`, expected one of `dots`, `grille`, `slot`\n"; code != 2 || stderr != want {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	// The glitch (33cfcf44): its parts held to their ranges, its events to
	// their words.
	writeFile(t, mine, `{"effects": {"glitch": {"wobble": {"episode": 81}}}}`)
	code, _, stderr = runLogis("--check-config")
	if want := "logis: the theme is heretical: " + mine + ": effects.glitch.wobble.episode: 81 is out of range (0 to 80)\n"; code != 2 || stderr != want {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	writeFile(t, mine, `{"effects": {"glitch": {"bursts": {"on": ["close"]}}}}`)
	code, _, stderr = runLogis("--check-config")
	if want := "logis: the theme is heretical: " + mine + ":1:49: unknown variant `close`, expected one of `open`, `tab`, `key`\n"; code != 2 || stderr != want {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	// The key burst (8ad70fee): its length and peak held to their ranges.
	writeFile(t, mine, `{"effects": {"glitch": {"bursts": {"key-ms": 1600}}}}`)
	code, _, stderr = runLogis("--check-config")
	if want := "logis: the theme is heretical: " + mine + ": effects.glitch.bursts.key-ms: 1600 is out of range (0 to 1500)\n"; code != 2 || stderr != want {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	writeFile(t, mine, `{"effects": {"glitch": {"bursts": {"key-strength": -0.5}}}}`)
	code, _, stderr = runLogis("--check-config")
	if want := "logis: the theme is heretical: " + mine + ": effects.glitch.bursts.key-strength: -0.5 is out of range (0 to 1)\n"; code != 2 || stderr != want {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
	writeFile(t, mine, `{"effects": {"glitch": {"bursts": {"on": ["key"], "key-ms": 200, "key-strength": 0.5}}}}`)
	if code, stdout, stderr := runLogis("--check-config"); code != 0 || !strings.Contains(stdout, "'mine'") {
		t.Errorf("a key burst: exit %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestCheckConfigFindsTheThemeItNames(t *testing.T) {
	home := scratchConfigHome(t)
	config := filepath.Join(home, "logis", "logis.jsonc")
	writeFile(t, config, `{"theme": "mine"}`)
	writeFile(t, filepath.Join(home, "logis", "themes", "mine.jsonc"), `{"extends": "cogitator-phosphor"}`)
	if code, stdout, stderr := runLogis("--check-config"); code != 0 || !strings.Contains(stdout, "'mine'") {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	writeFile(t, config, `{"theme": "purple"}`)
	code, _, stderr := runLogis("--check-config")
	if code != 2 || !strings.Contains(stderr, "the theme is heretical: theme 'purple'") {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
}

func TestCheckConfigWithAMissingNamedFileExits2(t *testing.T) {
	scratchConfigHome(t)
	path := filepath.Join(t.TempDir(), "nowhere.jsonc")
	code, _, stderr := runLogis("--check-config", "--config="+path)
	if code != 2 || stderr != "logis: the config is heretical: "+path+": no such file\n" {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
}

func TestCheckConfigUnwritableIsAnError(t *testing.T) {
	scratchConfigHome(t)
	var stderr bytes.Buffer
	if code := run([]string{"--check-config"}, brokenPipe{}, &stderr); code != 2 ||
		stderr.String() != "logis: cannot write to stdout: broken pipe\n" {
		t.Errorf("exit %d stderr %q", code, stderr.String())
	}
}

func TestConfigNeedsOneNonEmptyPath(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--config"}, "the rune --config stands empty: it wants one path to a config file"},
		{[]string{"--config="}, "the rune --config cannot bear '': it wants one path to a config file"},
		{[]string{"--check-config", "--config"}, "the rune --config stands empty"},
		{[]string{"daemon", "--config"}, "the rune --config stands empty"},
		{[]string{"--config", "a", "--config", "b"}, "the rune --config cannot bear 'b'"},
		{[]string{"daemon", "--config=a", "--config=b"}, "the rune --config cannot bear 'b'"},
	} {
		code, stdout, stderr := runLogis(tc.args...)
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "logis: "+tc.want) {
			t.Errorf("%q: exit %d stderr %q", tc.args, code, stderr)
		}
	}
}

// --check-config is a command of its own: a second command beside it, or
// a value given to it, is an unknown rune.
func TestCheckConfigIsACommandOfItsOwn(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		unknown string
	}{
		{[]string{"--check-config", "--version"}, "--version"},
		{[]string{"--version", "--check-config"}, "--check-config"},
		{[]string{"--check-config", "--check-config"}, "--check-config"},
		{[]string{"--check-config=x"}, "--check-config=x"},
	} {
		code, _, stderr := runLogis(tc.args...)
		if code != 2 || !strings.HasPrefix(stderr, "logis: the rune '"+tc.unknown+"' is unknown") {
			t.Errorf("%q: exit %d stderr %q", tc.args, code, stderr)
		}
	}
}

// A --config beside --version changes nothing: the version wins.
func TestAConfigBesideVersionIsIgnored(t *testing.T) {
	code, stdout, _ := runLogis("--config", "/nowhere.jsonc", "--version")
	if code != 0 || !strings.HasPrefix(stdout, "logis ") {
		t.Errorf("exit %d stdout %q", code, stdout)
	}
}

func TestTheDaemonTakesItsConfigAndKnowsWhenTheIdleTimeoutWasGiven(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	o, err := parseDaemon([]string{"--config", "named.jsonc", "--idle-timeout", "0"})
	if err != nil || o.Config == nil || o.Config.Path() != filepath.Join(dir, "named.jsonc") || !o.IdleTimeoutSet {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = parseDaemon([]string{"--config=/x/logis.jsonc", "--detach"})
	if err != nil || o.Config.Path() != "/x/logis.jsonc" || o.IdleTimeoutSet || !o.Detach {
		t.Errorf("%+v %v", o, err)
	}
	if o, _ := parseDaemon(nil); o.Config != nil || o.IdleTimeoutSet {
		t.Errorf("without runes: %+v", o)
	}
}
