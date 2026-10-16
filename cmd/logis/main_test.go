package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/version"
)

// --version and -V print `logis VERSION` on stdout, nothing on stderr, and
// exit 0.
func TestVersionRunesPrintNameAndVersion(t *testing.T) {
	for _, flag := range []string{"--version", "-V"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{flag}, &stdout, &stderr)
			if code != 0 {
				t.Errorf("exit %d, want 0; stderr %q", code, stderr.String())
			}
			if got, want := stdout.String(), "logis "+version.Version+"\n"; got != want {
				t.Errorf("stdout %q, want %q", got, want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr %q, want empty", stderr.String())
			}
		})
	}
}

// An unknown rune, or a second command, is named in the error, exit 2 with
// empty stdout (logis(1) RUNES: "An unknown rune ... is an error naming
// it"). There is no --help: the error lists every rune this build knows.
func TestUnknownRunesAreNamedAndExit2(t *testing.T) {
	noSession(t)
	for _, tc := range []struct {
		args    []string
		unknown string
	}{
		{[]string{"--heresy"}, "--heresy"},
		{[]string{"-x"}, "-x"},
		{[]string{"--help"}, "--help"},
		{[]string{"-h"}, "-h"},
		{[]string{"help"}, "help"},
		{[]string{"--"}, "--"},
		{[]string{"-"}, "-"},
		{[]string{""}, ""},
		// A second command.
		{[]string{"--version", "extra"}, "extra"},
		{[]string{"-V", "--version"}, "--version"},
		{[]string{"--version", "--dmenu"}, "--dmenu"},
		{[]string{"--dmenu", "--version"}, "--version"},
		{[]string{"--dmenu", "--dmenu"}, "--dmenu"},
		{[]string{"-d", "-d"}, "-d"},
		{[]string{"--dmenu0", "--dmenu"}, "--dmenu"},
		{[]string{"--apps", "--apps"}, "--apps"},
		{[]string{"--apps", "--servitor"}, "--servitor"},
		{[]string{"--servitor", "--dmenu"}, "--dmenu"},
		{[]string{"--servitor", "--provider", "x"}, "--provider"},
		{[]string{"--dmenu", "--provider", "x"}, "--provider"},
		{[]string{"--check-config", "--apps"}, "--apps"},
		// daemon and provide are commands only as the first argument.
		{[]string{"--dmenu", "daemon"}, "daemon"},
		{[]string{"--apps", "daemon"}, "daemon"},
		{[]string{"--dmenu", "provide"}, "provide"},
		// Daemon runes outside the daemon, and the other way round.
		{[]string{"--detach"}, "--detach"},
		{[]string{"--idle-timeout", "5"}, "--idle-timeout"},
		{[]string{"--debug-panel"}, "--debug-panel"},
		{[]string{"daemon", "--version"}, "--version"},
		{[]string{"daemon", "-V"}, "-V"},
		{[]string{"daemon", "--dmenu"}, "--dmenu"},
		{[]string{"daemon", "daemon"}, "daemon"},
		{[]string{"daemon", "--bogus", "--config"}, "--bogus"},
		{[]string{"provide", "servitor", "x"}, "x"},
		{[]string{"provide", "servitor", "--config", "x"}, "--config"},
		// A short rune takes its value only as the next argument.
		{[]string{"--dmenu", "-p=x"}, "-p=x"},
		{[]string{"--dmenu", "-l=3"}, "-l=3"},
		// --password takes its character only as --password=C.
		{[]string{"--dmenu", "--password", "x"}, "x"},
		// An argument that is not UTF-8 is named as text.
		{[]string{"--h\xffresy"}, "--h\uFFFDresy"},
		{[]string{"daemon", "\xff"}, "\uFFFD"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, stdout, stderr := runLogis(tc.args...)
			if code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			if stdout != "" {
				t.Errorf("stdout %q, want empty", stdout)
			}
			if want := unknownRuneMessage(tc.unknown); stderr != want {
				t.Errorf("stderr %q, want %q", stderr, want)
			}
		})
	}
}

// noSession leaves the test without a Wayland session or a runtime
// directory, so an invocation that the parser wrongly let through fails
// at once instead of starting a daemon or opening a window.
func noSession(t *testing.T) {
	t.Helper()
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("WAYLAND_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// unknownRuneMessage is the error for an unknown rune, the flavour text of
// logis(1)'s "An unknown rune ... is an error naming it".
func unknownRuneMessage(arg string) string {
	return "logis: the rune '" + arg + "' is unknown to this cogitator. It knows only " + knownRunes + ".\n"
}

// A rune that takes no value refuses one given as `--rune=V` (logis(1):
// "a value a rune cannot bear is an error naming it"), as fuzzel does:
// the switches of the dmenu runes like every other.
func TestARuneThatTakesNoValueRefusesOne(t *testing.T) {
	noSession(t)
	for _, args := range [][]string{
		{"--dmenu", "--counter=x"}, {"--dmenu", "--index=y"}, {"--dmenu", "--only-match=z"},
		{"--dmenu", "--no-run-if-empty=w"}, {"--dmenu", "--counter="},
		{"--dmenu=x"}, {"--dmenu0=x"}, {"--apps=x"}, {"--version=x"}, {"-V=x"},
		{"--check-config=x"}, {"--servitor=x"}, {"--dmenu", "-R=1"}, {"daemon", "--detach=1"},
		{"daemon", "--debug-panel=1"},
	} {
		code, stdout, stderr := runLogis(args...)
		if want := unknownRuneMessage(args[len(args)-1]); code != 2 || stdout != "" || stderr != want {
			t.Errorf("%q: exit %d, stdout %q, stderr %q; want 2 and %q", args, code, stdout, stderr, want)
		}
	}
}

type brokenPipe struct{}

func (brokenPipe) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// A version nobody can read is an error, not a success.
func TestVersionUnwritableIsAnError(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"--version"}, brokenPipe{}, &stderr)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if want := "logis: cannot write to stdout: broken pipe\n"; stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
}
