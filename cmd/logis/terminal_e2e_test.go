package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/e2e"
)

// A Terminal=true app with no "terminal" in the config and no $TERMINAL:
// the daemon seeks a terminal on the client's PATH, never on its own (the
// test's, which may hold a real terminal). The fixture app
// tests/fixtures/apps/terminal is the only one; the terminal found is a
// stub foot, the fixture program anoint, so no real terminal ever opens.
// Runs only with LOGIS_E2E=niri|sway.

// startTerminalApps starts a daemon whose only app is a terminal app.
func startTerminalApps(t *testing.T, dir string) *e2e.Cogitator {
	t.Helper()
	fixtures := filepath.Dir(appFixtures(t))
	env := []string{
		"XDG_DATA_DIRS=" + filepath.Join(fixtures, "apps/terminal"),
		"XDG_DATA_HOME=" + filepath.Join(dir, "data-home"),
		"XDG_STATE_HOME=" + filepath.Join(dir, "state"),
		"TERMINAL=",
	}
	return e2e.StartCogitator(t, env, "")
}

func TestE2EATerminalAppRunsInATerminalFoundOnTheClientsPath(t *testing.T) {
	dir, bin := t.TempDir(), t.TempDir()
	if err := os.Symlink(filepath.Join(appFixtures(t), "bin", "anoint"), filepath.Join(bin, "foot")); err != nil {
		t.Fatal(err)
	}
	c := startTerminalApps(t, dir)
	env := append(appsClientEnv(t, dir), "TERMINAL=")
	env[0] = "PATH=" + bin + ":/usr/bin:/bin"
	client := c.ClientWith(env, "--apps")
	focused := c.Shown(1)
	c.Tap(focused, e2e.KeyEnter)
	next(t, c, "accept", "0")
	if ended := client.Ended(); ended.Code != 0 {
		t.Fatalf("%+v", ended)
	}
	// foot takes the command directly: the stub is foot, so it hears the
	// app's whole command line.
	if got := recordOf(t, dir)["arg"]; !slices.Equal(got, []string{"anoint", "--terminal"}) {
		t.Errorf("args %q", got)
	}
}

func TestE2EWithoutATerminalTheToastSaysHowToNameOneInTheConfig(t *testing.T) {
	dir := t.TempDir()
	c := startTerminalApps(t, dir)
	env := append(appsClientEnv(t, dir), "TERMINAL=")
	env[0] = "PATH=" + t.TempDir()
	c.ClientWith(env, "--apps")
	focused := c.Shown(1)
	c.Tap(focused, e2e.KeyEnter)
	var toast string
	if err := json.Unmarshal([]byte(c.Next("toast")), &toast); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(c.Compositor.ConfigHome(), "logis", "logis.jsonc")
	want := `Cogitator shell cannot be summoned: it must run in a terminal, and no terminal was found. ` +
		`Set "terminal" in ` + config + `, e.g. "terminal": ["foot", "-e"]`
	if toast != want {
		t.Errorf("toast %q, want %q", toast, want)
	}
	waitUntil(t, "the daemon to log it", func() bool { return strings.Contains(c.Daemon.Stderr(), want) })
	if strings.Count(c.Daemon.Stderr(), want) != 1 {
		t.Errorf("log %s", c.Daemon.Stderr())
	}
}
