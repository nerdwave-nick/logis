package main

import (
	"bytes"
	"testing"

	"github.com/nerdwave-nick/logis/internal/version"
)

// Ported from Rust tests/cli.rs version_flag_prints_name_and_version_and_succeeds
// and src/cli version_flags_ask_for_the_version.
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

// Ported from Rust src/cli an_unknown_argument_is_named_in_the_error, for the
// runes this build knows: an unknown rune, or a second command, is named in
// the error, exit 2 with empty stdout.
func TestUnknownRunesAreNamedAndExit2(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		unknown string
	}{
		{[]string{"--heresy"}, "--heresy"},
		{[]string{"--version", "extra"}, "extra"},
		{[]string{"-V", "--version"}, "--version"},
	} {
		t.Run(tc.unknown, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			if code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout %q, want empty", stdout.String())
			}
			want := "logis: the rune '" + tc.unknown + "' is unknown to this cogitator. " +
				"It knows only --version.\n"
			if got := stderr.String(); got != want {
				t.Errorf("stderr %q, want %q", got, want)
			}
		})
	}
}

// Plain `logis` opens the app launcher in Rust; until that is ported the Go
// build says so and fails with exit 2 instead of pretending.
func TestNoRunesFailUntilTheAppLauncherExists(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout %q, want empty", stdout.String())
	}
	want := "logis: this cogitator has no app launcher yet. It knows only --version.\n"
	if got := stderr.String(); got != want {
		t.Errorf("stderr %q, want %q", got, want)
	}
}
