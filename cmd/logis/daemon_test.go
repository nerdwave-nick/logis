package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// The daemon takes an optional idle timeout in seconds and can detach from
// its starter.
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

// An idle timeout that is not a non-negative number of seconds, or none at
// all, is refused.
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

// An unknown daemon rune is named in the error, with every rune known.
func TestAnUnknownDaemonRuneIsNamed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon", "--debug"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 {
		t.Errorf("exit %d, stdout %q", code, stdout.String())
	}
	want := "logis: the rune '--debug' is unknown to this cogitator. " +
		"It knows only --version, --dmenu or --dmenu0 (with fuzzel's list runes), " +
		"--apps, --provider PROGRAM [ARG]..., --servitor, --check-config, " +
		"--config PATH and daemon [--idle-timeout SECONDS] [--config PATH] [--detach] [--debug-panel].\n"
	if stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
}

// A huge idle timeout under 2^64 seconds is as good as none ending; a
// longer one is refused.
func TestAHugeIdleTimeoutIsKeptAsTheLongestThereIs(t *testing.T) {
	o, err := parseDaemon([]string{"--idle-timeout", "1e19"})
	if err != nil || o.IdleTimeout < 100*365*24*time.Hour {
		t.Errorf("%v, %v", o.IdleTimeout, err)
	}
	for _, bad := range []string{"18446744073709551616", "1e300"} {
		if _, err := parseDaemon([]string{"--idle-timeout", bad}); err == nil ||
			!strings.Contains(err.Error(), "cannot bear '"+bad+"'") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

// The daemon's runes are read in order: the first
// refused one is named, and --idle-timeout takes whatever follows it.
func TestDaemonRunesAreReadInOrder(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--idle-timeout", "--config", "x"}, "the rune --idle-timeout cannot bear '--config'"},
		{[]string{"--bogus", "--config"}, "the rune '--bogus' is unknown"},
		{[]string{"--idle-timeout", "soon", "--config"}, "the rune --idle-timeout cannot bear 'soon'"},
		{[]string{"--config", "x", "--bogus"}, "the rune '--bogus' is unknown"},
		{[]string{"--config", "a", "--config", "b"}, "the rune --config cannot bear 'b'"},
		{[]string{"--idle-timeout", "\xff"}, "the rune --idle-timeout cannot bear '\uFFFD'"},
		{[]string{"\xff"}, "the rune '\uFFFD' is unknown"},
	} {
		_, err := parseDaemon(c.args)
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.args, err, c.want)
		}
	}
	o, err := parseDaemon([]string{"--detach", "--config=/c", "--idle-timeout=2"})
	if err != nil || !o.Detach || o.Config == nil || o.IdleTimeout != 2*time.Second {
		t.Errorf("%+v, %v", o, err)
	}
}
