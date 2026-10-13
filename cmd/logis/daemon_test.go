package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// Ported from Rust src/cli daemon_takes_an_optional_idle_timeout_in_seconds
// and daemon_can_detach_from_its_starter.
func TestDaemonTakesAnIdleTimeoutInSecondsAndCanDetach(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		idle   time.Duration
		detach bool
	}{
		{nil, 0, false},
		{[]string{"--idle-timeout", "0.25"}, 250 * time.Millisecond, false},
		{[]string{"--idle-timeout=600"}, 600 * time.Second, false},
		{[]string{"--idle-timeout", "0"}, 0, false},
		{[]string{"--detach", "--idle-timeout", "5"}, 5 * time.Second, true},
	} {
		o, err := parseDaemon(tc.args)
		if err != nil {
			t.Errorf("%q: %v", tc.args, err)
			continue
		}
		if o.IdleTimeout != tc.idle || o.Detach != tc.detach {
			t.Errorf("%q: idle %v detach %v", tc.args, o.IdleTimeout, o.Detach)
		}
	}
}

// Ported from an_invalid_idle_timeout_is_refused.
func TestAnInvalidIdleTimeoutIsRefused(t *testing.T) {
	for _, bad := range []string{"soon", "-1", "NaN", "inf", "", "0x10"} {
		_, err := parseDaemon([]string{"--idle-timeout", bad})
		want := "the rune --idle-timeout cannot bear '" + bad +
			"': it wants a number of seconds (0 keeps the daemon resident)"
		if err == nil || err.Error() != want {
			t.Errorf("%q: %v", bad, err)
		}
	}
	_, err := parseDaemon([]string{"--idle-timeout"})
	if err == nil || !strings.Contains(err.Error(), "the rune --idle-timeout stands empty") {
		t.Errorf("missing value: %v", err)
	}
}

// Ported from an_unknown_argument_is_named_in_the_error, for daemon runes.
func TestAnUnknownDaemonRuneIsNamed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--debug"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Errorf("exit %d, stdout %q", code, stdout.String())
	}
	want := "logis: the rune '--debug' is unknown to this cogitator. " +
		"It knows only --version, --dmenu or --dmenu0 (with fuzzel's list runes), " +
		"--provider PROGRAM [ARG]..., --servitor, --check-config, " +
		"--config PATH and daemon [--idle-timeout SECONDS] [--config PATH] [--detach] [--debug-panel].\n"
	if stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
}

// A huge idle timeout is as good as none ending.
func TestAHugeIdleTimeoutIsKeptAsTheLongestThereIs(t *testing.T) {
	o, err := parseDaemon([]string{"--idle-timeout", "1e300"})
	if err != nil || o.IdleTimeout < 100*365*24*time.Hour {
		t.Errorf("%v, %v", o.IdleTimeout, err)
	}
}
