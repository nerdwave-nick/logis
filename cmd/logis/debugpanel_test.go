package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// --debug-panel is a daemon rune; the daemon's other runes may stand
// beside it, and plain `logis daemon` stays the daemon.
func TestDebugPanelIsADaemonRune(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		panel bool
	}{
		{nil, false},
		{[]string{"--detach"}, false},
		{[]string{"--debug-panel"}, true},
		{[]string{"--idle-timeout", "5", "--debug-panel"}, true},
	} {
		o, err := parseDaemon(tc.args)
		if err != nil || o.debugPanel != tc.panel {
			t.Errorf("%q: debug panel %v, err %v; want %v", tc.args, o.debugPanel, err, tc.panel)
		}
	}
}

// At the process boundary, as a caller sees it: `logis daemon
// --debug-panel` reaches the window (without a session: exit 2, the "no
// session" message, empty stdout), not the daemon's rune parser.
func TestTheBinaryRoutesDaemonDebugPanelToTheWindow(t *testing.T) {
	cmd := exec.Command(buildLogis(t), "daemon", "--debug-panel")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=" + t.TempDir()}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if code := cmd.ProcessState.ExitCode(); code != 2 {
		t.Errorf("exit %d (%v), want 2", code, err)
	}
	want := "logis: the noosphere is silent: no Wayland session answers (WAYLAND_DISPLAY is not set). " +
		"The cogitator manifests only within a Wayland session.\n"
	if stderr.String() != want || stdout.Len() != 0 {
		t.Errorf("stdout %q, stderr %q\nwant stderr %q", stdout.String(), stderr.String(), want)
	}
}

// The daemon's refusal names --debug-panel among its runes.
func TestTheDaemonRunesIncludeTheDebugPanel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"daemon", "--heresy"}, &stdout, &stderr); code != 2 {
		t.Errorf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "daemon [--idle-timeout SECONDS] [--config PATH] [--detach] [--debug-panel]") {
		t.Errorf("stderr %q", stderr.String())
	}
}
