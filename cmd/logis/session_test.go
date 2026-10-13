package main

import (
	"bytes"
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

// Globals of a compositor offering everything the window needs
// (niri/sway-like): layer shell, fractional scale and the viewporter.
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

// session gives the test a Wayland environment it fully controls, as the
// Rust CLI tests do: a fake compositor with globals at
// $XDG_RUNTIME_DIR/wayland-test (none for nil), no WAYLAND_SOCKET, and
// WAYLAND_DISPLAY set only with a compositor. It returns the runtime dir.
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

// fails runs logis with args and checks the caller contract for "cannot
// serve": exit 2, empty stdout, every wanted fragment on stderr.
func fails(t *testing.T, args []string, want ...string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != 2 {
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
}

// Ported from Rust tests/cli.rs, for plain logis and the debug panel.
var unusable = []struct {
	name string
	args []string
}{
	{"plain", nil},
	{"debug panel", []string{"daemon", "--debug-panel"}},
}

func TestWithoutWaylandDisplayExits2WithFlavouredMessage(t *testing.T) {
	for _, tc := range unusable {
		t.Run(tc.name, func(t *testing.T) {
			session(t, nil)
			fails(t, tc.args, "logis: the noosphere is silent", "WAYLAND_DISPLAY is not set")
		})
	}
}

func TestWithNonexistentWaylandSocketExits2NamingTheSocket(t *testing.T) {
	for _, tc := range unusable {
		t.Run(tc.name, func(t *testing.T) {
			dir := session(t, nil)
			t.Setenv("WAYLAND_DISPLAY", "wayland-nowhere")
			fails(t, tc.args, "noosphere is silent", filepath.Join(dir, "wayland-nowhere"))
		})
	}
}

func TestOnCompositorWithoutLayerShellExits2NamingTheProtocol(t *testing.T) {
	for _, tc := range unusable {
		t.Run(tc.name, func(t *testing.T) {
			session(t, withoutLayerShell)
			fails(t, tc.args, "zwlr_layer_shell_v1", "no altar")
		})
	}
}

func TestOnCompositorWithoutFractionalScaleExits2NamingTheProtocol(t *testing.T) {
	for _, tc := range unusable {
		t.Run(tc.name, func(t *testing.T) {
			session(t, withoutFractionalScale)
			fails(t, tc.args, "wp_fractional_scale_manager_v1")
		})
	}
}

func TestWithoutXDGRuntimeDirExits2NamingIt(t *testing.T) {
	for _, tc := range unusable {
		t.Run(tc.name, func(t *testing.T) {
			session(t, withoutLayerShell)
			t.Setenv("XDG_RUNTIME_DIR", "")
			fails(t, tc.args, "XDG_RUNTIME_DIR is not set")
		})
	}
}

// --debug-panel belongs to the daemon (Rust src/cli: a daemon flag only).
func TestDebugPanelIsADaemonFlagOnly(t *testing.T) {
	session(t, nil)
	fails(t, []string{"--debug-panel"}, "the rune '--debug-panel' is unknown")
}
