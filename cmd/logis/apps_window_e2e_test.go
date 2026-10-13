package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/list/scene"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The app launcher end to end (Rust tests/apps_window.rs): a real daemon
// in the test compositor reading the fixture applications
// tests/fixtures/apps/e2e with icons from the fixture theme, `logis
// --apps`, keys from the harness's virtual keyboard, and the fixture
// program tests/fixtures/apps/bin/anoint recording how it was started.
// The daemon finds no session bus (its runtime dir is scratch), so apps
// start in the client's environment, outside any scope. Only fixture apps
// are launched, inside the test compositor's session. Runs only with
// LOGIS_E2E=niri|sway.

// startApps starts a daemon over the E2E fixtures, its state in dir.
func startApps(t *testing.T, dir string) *e2e.Cogitator {
	t.Helper()
	fixtures := filepath.Dir(appFixtures(t))
	config, err := os.ReadFile(filepath.Join(fixtures, "apps/e2e/logis.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	env := []string{
		"XDG_DATA_DIRS=" + filepath.Join(fixtures, "apps/e2e") + ":" + filepath.Join(fixtures, "icons/share"),
		"XDG_DATA_HOME=" + filepath.Join(dir, "data-home"),
		"XDG_STATE_HOME=" + filepath.Join(dir, "state"),
	}
	return e2e.StartCogitator(t, env, string(config))
}

// appsClientEnv is the client's environment: the fixture program on the
// PATH, where it records itself, and the file that keeps it running.
func appsClientEnv(t *testing.T, dir string) []string {
	return []string{
		"PATH=" + filepath.Join(appFixtures(t), "bin") + ":/usr/bin:/bin",
		"HOME=" + dir,
		"ANOINT_RECORD=" + filepath.Join(dir, "record"),
		"ANOINT_HOLD=" + filepath.Join(dir, "hold"),
	}
}

// recordOf is what anoint recorded in dir, by key.
func recordOf(t *testing.T, dir string) map[string][]string {
	t.Helper()
	record := filepath.Join(dir, "record")
	waitUntil(t, "the app to run", func() bool { _, err := os.Stat(record); return err == nil })
	text, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	heard := map[string][]string{}
	for line := range strings.Lines(string(text)) {
		k, v, _ := strings.Cut(strings.TrimSuffix(line, "\n"), " ")
		heard[k] = append(heard[k], v)
	}
	return heard
}

func TestE2EReturnLaunchesTheAppDetachedWithAnActivationToken(t *testing.T) {
	dir := t.TempDir()
	c := startApps(t, dir)
	hold := filepath.Join(dir, "hold")
	if err := os.WriteFile(hold, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	client := c.ClientWith(appsClientEnv(t, dir), "--apps")
	focused := c.Shown(2)
	// The app launcher opens in the theme's own window.
	th := theme.Default()
	size, _ := scene.Fit(list.DefaultOptions(), &th)
	checkWindow(t, c.Harness, "apps", int(size.W), int(size.H))
	c.Type(focused, "unguent")
	next(t, c, "select", "0") // matched by its keywords
	c.Tap(focused, e2e.KeyEnter)
	next(t, c, "accept", "0")
	c.Next("token")
	if ended := client.Ended(); ended.Code != 0 || len(ended.Stdout) != 0 {
		t.Fatalf("%+v", ended)
	}
	c.Next("hide")

	heard := recordOf(t, dir)
	if got := heard["arg"]; !slices.Equal(got, []string{"--e2e", "--name=Anoint the vessel"}) {
		t.Errorf("args %q", got)
	}
	token := heard["token"]
	if len(token) != 1 || token[0] == "" {
		t.Fatalf("the compositor's activation token: %q", token)
	}
	if !slices.Equal(heard["startup"], token) {
		t.Errorf("startup %q, token %q", heard["startup"], token)
	}
	pid, err := strconv.Atoi(heard["pid"][0])
	if err != nil || !slices.Equal(heard["sid"], heard["pid"]) {
		t.Fatalf("pid %q, sid %q: not a session of its own", heard["pid"], heard["sid"])
	}
	if slices.Equal(heard["ppid"], []string{"0"}) {
		t.Errorf("ppid %q", heard["ppid"])
	}

	// It outlives the daemon (and so logis).
	_ = syscall.Kill(c.Daemon.PID(), syscall.SIGKILL) // the daemon this test started
	time.Sleep(200 * time.Millisecond)
	if !alive(pid) {
		t.Fatal("the app died with the daemon")
	}
	if err := os.Remove(hold); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the app to end", func() bool { return !alive(pid) })
}

func TestE2EShiftReturnOpensTheDesktopActionsAsASecondStage(t *testing.T) {
	dir := t.TempDir()
	c := startApps(t, dir)
	client := c.ClientWith(appsClientEnv(t, dir), "--apps")
	focused := c.Shown(2)
	c.Keyboard.Hold(focused, e2e.Shift)
	c.Tap(focused, e2e.KeyEnter)
	c.Keyboard.Release(focused, e2e.Shift)
	next(t, c, "descend", "0")
	c.Next("stage")
	next(t, c, "items", "2") // the app itself and its one action
	c.Tap(focused, e2e.KeyDown)
	next(t, c, "select", "1")
	c.Tap(focused, e2e.KeyEnter)
	next(t, c, "accept", "1")
	if ended := client.Ended(); ended.Code != 0 {
		t.Fatalf("%+v", ended)
	}
	if got := recordOf(t, dir)["arg"]; !slices.Equal(got, []string{"--purge"}) {
		t.Errorf("args %q", got)
	}
	frecency := filepath.Join(dir, "state/logis/frecency.json")
	waitUntil(t, "frecency to be kept", func() bool {
		b, err := os.ReadFile(frecency)
		return err == nil && strings.Contains(string(b), "forge-anoint.desktop")
	})
}

// Plain logis is the app launcher (as in fuzzel); dismissing it launches
// nothing and exits 1 with an empty stdout.
func TestE2EPlainLogisOpensTheLauncherAndEscapeExits1(t *testing.T) {
	dir := t.TempDir()
	c := startApps(t, dir)
	client := c.ClientWith(appsClientEnv(t, dir))
	focused := c.Shown(2)
	c.Tap(focused, e2e.KeyEsc)
	c.Next("dismiss")
	if ended := client.Ended(); ended.Code != 1 || len(ended.Stdout) != 0 {
		t.Fatalf("%+v", ended)
	}
	if _, err := os.Stat(filepath.Join(dir, "record")); err == nil {
		t.Error("something was launched")
	}
}
