package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/wltest"
)

// Globals of a compositor without layer shell (GNOME/Mutter-like).
var withoutLayerShell = []wltest.Global{
	{Interface: "wl_compositor", Version: 6},
	{Interface: "wl_shm", Version: 1},
	{Interface: "wl_seat", Version: 9},
	{Interface: "xdg_wm_base", Version: 6},
}

// Globals of a compositor offering everything the window needs to pass the
// preflight (niri/sway-like): layer shell, fractional scale and the
// viewporter.
var withLayerShell = []wltest.Global{
	{Interface: "wl_compositor", Version: 6},
	{Interface: "wl_shm", Version: 1},
	{Interface: "zwlr_layer_shell_v1", Version: 4},
	{Interface: "wp_fractional_scale_manager_v1", Version: 1},
	{Interface: "wp_viewporter", Version: 1},
	{Interface: "xdg_wm_base", Version: 6},
}

// Layer shell, but no fractional scaling: the window cannot be crisp.
var withoutFractionalScale = []wltest.Global{
	{Interface: "wl_compositor", Version: 6},
	{Interface: "wl_shm", Version: 1},
	{Interface: "zwlr_layer_shell_v1", Version: 4},
	{Interface: "wp_viewporter", Version: 1},
}

// session gives the test a Wayland environment it fully controls: a fake
// compositor with globals at $XDG_RUNTIME_DIR/wayland-test (none for nil),
// no WAYLAND_SOCKET, and WAYLAND_DISPLAY set only with a compositor. It
// returns the runtime dir.
func session(t *testing.T, globals []wltest.Global) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WAYLAND_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("WAYLAND_DISPLAY", "")
	if globals != nil {
		c, err := wltest.Serve(dir, "wayland-test", globals)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	}
	return dir
}

// fails runs the built logis with args in the test's environment (without
// LOGIS_RESOLVER: the daemon a client starts opens the window) and checks
// the caller contract for "cannot serve": exit 2, empty stdout, every
// wanted fragment on stderr, and no daemon left behind.
func fails(t *testing.T, args []string, want ...string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(buildLogis(t), args...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "LOGIS_RESOLVER=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	if code := cmd.ProcessState.ExitCode(); code != 2 {
		t.Errorf("%q: exit %d, want 2; stderr %q", args, code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("%q: stdout %q, want empty", args, stdout.String())
	}
	for _, w := range want {
		if !strings.Contains(stderr.String(), w) {
			t.Errorf("%q: stderr %q lacks %q", args, stderr.String(), w)
		}
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(dir) {
		socket := filepath.Join(dir, "logis.sock")
		waitUntil(t, "no daemon's socket", func() bool { return gone(socket) })
	}
}

// sessionCommand is an invocation that needs a usable Wayland session;
// dwells says whether it needs the runtime directory for the daemon's
// socket too (all but the debug panel, which serves no clients).
type sessionCommand struct {
	name   string
	args   []string
	dwells bool
}

// sessionCommands are every command that needs a session: each client
// (whose daemon is started for it and fails), the daemon, and the debug
// panel.
var sessionCommands = []sessionCommand{
	{"plain", nil, true},
	{"apps", []string{"--apps"}, true},
	{"dmenu", []string{"--dmenu"}, true},
	{"dmenu0", []string{"--dmenu0"}, true},
	{"servitor", []string{"--servitor"}, true},
	{"provider", []string{"--provider", "true"}, true},
	{"daemon", []string{"daemon"}, true},
	{"detached daemon", []string{"daemon", "--detach"}, true},
	{"debug panel", []string{"daemon", "--debug-panel"}, false},
}

// eachSessionCommand runs check for every command that needs a session,
// each in an environment of its own made by setup.
func eachSessionCommand(t *testing.T, setup func(t *testing.T) string, check func(t *testing.T, c sessionCommand, dir string)) {
	t.Helper()
	for _, c := range sessionCommands {
		t.Run(c.name, func(t *testing.T) { check(t, c, setup(t)) })
	}
}

// Without WAYLAND_DISPLAY nothing can serve, and the message says so in
// the cogitator's words.
func TestWithoutWaylandDisplayExits2WithFlavouredMessage(t *testing.T) {
	eachSessionCommand(t, func(t *testing.T) string { return session(t, nil) }, func(t *testing.T, c sessionCommand, _ string) {
		fails(t, c.args, "noosphere is silent", "WAYLAND_DISPLAY is not set")
	})
}

// An unreachable display is named by its socket's path, relative to
// XDG_RUNTIME_DIR or absolute.
func TestWithNonexistentWaylandSocketExits2NamingTheSocket(t *testing.T) {
	eachSessionCommand(t, func(t *testing.T) string { return session(t, nil) }, func(t *testing.T, c sessionCommand, dir string) {
		t.Setenv("WAYLAND_DISPLAY", "wayland-nowhere")
		fails(t, c.args, "noosphere is silent", filepath.Join(dir, "wayland-nowhere"))
		absolute := filepath.Join(t.TempDir(), "nowhere")
		t.Setenv("WAYLAND_DISPLAY", absolute)
		fails(t, c.args, "noosphere is silent", absolute)
	})
}

func TestOnCompositorWithoutLayerShellExits2NamingTheProtocol(t *testing.T) {
	eachSessionCommand(t, func(t *testing.T) string { return session(t, withoutLayerShell) }, func(t *testing.T, c sessionCommand, _ string) {
		fails(t, c.args, "zwlr_layer_shell_v1", "no altar")
	})
}

func TestOnCompositorWithoutFractionalScaleExits2NamingTheProtocol(t *testing.T) {
	eachSessionCommand(t, func(t *testing.T) string { return session(t, withoutFractionalScale) }, func(t *testing.T, c sessionCommand, _ string) {
		fails(t, c.args, "wp_fractional_scale_manager_v1")
	})
}

// XDG_RUNTIME_DIR unset or not absolute is named: with a display relative
// to it, no command can reach the compositor; with an absolute display, a
// client and the daemon still have nowhere for the daemon's socket.
func TestWithoutXDGRuntimeDirExits2NamingIt(t *testing.T) {
	eachSessionCommand(t, func(t *testing.T) string { return session(t, withoutLayerShell) }, func(t *testing.T, c sessionCommand, dir string) {
		for _, runtimeDir := range []string{"", "run"} {
			t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
			t.Setenv("WAYLAND_DISPLAY", "wayland-test")
			fails(t, c.args, "XDG_RUNTIME_DIR", "not absolute")
			if c.dwells {
				t.Setenv("WAYLAND_DISPLAY", filepath.Join(dir, "wayland-test"))
				fails(t, c.args, "the machine spirit has no dwelling: XDG_RUNTIME_DIR is not set or not absolute")
			}
		}
	})
}

// --debug-panel belongs to the daemon: a daemon flag only.
func TestDebugPanelIsADaemonFlagOnly(t *testing.T) {
	session(t, nil)
	fails(t, []string{"--debug-panel"}, "the rune '--debug-panel' is unknown")
}
