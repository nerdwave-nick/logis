package window

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/wltest"
)

// Tests of connecting: the socket, the globals, preflight and the errors.

func TestARelativeDisplayIsJoinedToTheRuntimeDir(t *testing.T) {
	path, err := socketPath("wayland-1", "/run/user/1000")
	if err != nil || path != "/run/user/1000/wayland-1" {
		t.Errorf("%q, %v", path, err)
	}
}

func TestAnAbsoluteDisplayIsUsedAsIs(t *testing.T) {
	path, err := socketPath("/tmp/wayland-x", "")
	if err != nil || path != "/tmp/wayland-x" {
		t.Errorf("%q, %v", path, err)
	}
}

func TestAnUnsetOrEmptyDisplayMeansNoSession(t *testing.T) {
	if _, err := socketPath("", "/run/user/1000"); err == nil || err.Error() != "WAYLAND_DISPLAY is not set" {
		t.Errorf("%v", err)
	}
}

func TestARelativeDisplayNeedsAnAbsoluteRuntimeDir(t *testing.T) {
	for _, dir := range []string{"", "run/user"} {
		if _, err := socketPath("wayland-1", dir); err == nil || err.Error() != "XDG_RUNTIME_DIR is not set or not absolute" {
			t.Errorf("runtime dir %q: %v", dir, err)
		}
	}
}

func TestLayerShellIsFoundAmongTheGlobals(t *testing.T) {
	if err := required([]string{"wl_compositor", "zwlr_layer_shell_v1", "wp_fractional_scale_manager_v1", "wp_viewporter", "wl_seat"}); err != nil {
		t.Error(err)
	}
}

func TestGlobalsWithoutLayerShellAreRefused(t *testing.T) {
	for _, globals := range [][]string{
		{"wl_compositor", "xdg_wm_base", "wl_seat"},
		nil,
		// A similar interface name is not layer shell.
		{"zwlr_layer_shell_v1_extra", "wlr_layer_shell"},
	} {
		var noAltar *NoLayerShellError
		if err := required(globals); !errors.As(err, &noAltar) {
			t.Errorf("%v: %v", globals, err)
		}
	}
}

func TestEachMissingRequiredGlobalIsNamed(t *testing.T) {
	all := []string{"wl_compositor", "zwlr_layer_shell_v1", "wp_fractional_scale_manager_v1", "wp_viewporter"}
	for _, missing := range []string{"wl_compositor", "wp_fractional_scale_manager_v1", "wp_viewporter"} {
		var globals []string
		for _, g := range all {
			if g != missing {
				globals = append(globals, g)
			}
		}
		var rite *MissingGlobalError
		if err := required(globals); !errors.As(err, &rite) || rite.Interface != missing {
			t.Errorf("without %s: %v", missing, err)
		}
	}
}

// The messages are the cogitator's, word for word.
func TestTheErrorsSpeakAsTheCogitator(t *testing.T) {
	for err, want := range map[error]string{
		&NoSessionError{Reason: "WAYLAND_DISPLAY is not set"}: "the noosphere is silent: no Wayland session answers " +
			"(WAYLAND_DISPLAY is not set). The cogitator manifests only within a Wayland session.",
		&NoLayerShellError{}: "this compositor offers no altar: it does not support wlr-layer-shell " +
			"(zwlr_layer_shell_v1). The cogitator rises only on compositors that do, " +
			"such as niri or sway; GNOME/Mutter is not one of them.",
		&MissingGlobalError{Interface: "wp_viewporter"}: "this compositor withholds a sacred rite: it does not offer wp_viewporter",
		&DisconnectedError{Reason: "read: broken pipe"}: "the noosphere fell silent mid-rite: read: broken pipe",
		&SurfaceError{Reason: "eglSwapBuffers: EGL_BAD_SURFACE"}: "the cogitator's glass would not take the image: " +
			"eglSwapBuffers: EGL_BAD_SURFACE",
	} {
		if got := err.Error(); got != want {
			t.Errorf("%q\nwant %q", got, want)
		}
	}
}

var (
	withoutLayerShell = []wltest.Global{
		{Interface: "wl_compositor", Version: 6},
		{Interface: "wl_shm", Version: 1},
		{Interface: "wl_seat", Version: 9},
		{Interface: "xdg_wm_base", Version: 6},
	}
	withLayerShell = []wltest.Global{
		{Interface: "wl_compositor", Version: 6},
		{Interface: "wl_shm", Version: 1},
		{Interface: "zwlr_layer_shell_v1", Version: 4},
		{Interface: "wp_fractional_scale_manager_v1", Version: 1},
		{Interface: "wp_viewporter", Version: 1},
		{Interface: "xdg_wm_base", Version: 6},
	}
	withoutFractionalScale = []wltest.Global{
		{Interface: "wl_compositor", Version: 6},
		{Interface: "wl_shm", Version: 1},
		{Interface: "zwlr_layer_shell_v1", Version: 4},
		{Interface: "wp_viewporter", Version: 1},
	}
)

// session sets the Wayland environment for a test: no WAYLAND_SOCKET, the
// given WAYLAND_DISPLAY (empty: unset), the runtime dir.
func session(t *testing.T, runtimeDir, display string) {
	t.Helper()
	t.Setenv("WAYLAND_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("WAYLAND_DISPLAY", display)
}

func serve(t *testing.T, dir string, globals []wltest.Global) *wltest.Compositor {
	t.Helper()
	c, err := wltest.Serve(dir, "wayland-test", globals)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestPreflightWithoutWaylandDisplayNamesIt(t *testing.T) {
	session(t, t.TempDir(), "")
	err := Preflight()
	var none *NoSessionError
	if !errors.As(err, &none) || !strings.Contains(err.Error(), "WAYLAND_DISPLAY is not set") {
		t.Errorf("%v", err)
	}
}

func TestPreflightWithANonexistentSocketNamesTheSocket(t *testing.T) {
	dir := t.TempDir()
	session(t, dir, "wayland-nowhere")
	err := Preflight()
	var none *NoSessionError
	if !errors.As(err, &none) || !strings.Contains(err.Error(), filepath.Join(dir, "wayland-nowhere")) {
		t.Errorf("%v", err)
	}
	// The OS error reads "<description> (os error N)" (caller contract).
	if want := "cannot reach " + filepath.Join(dir, "wayland-nowhere") + ": No such file or directory (os error 2)"; none == nil || none.Reason != want {
		t.Errorf("reason %q, want %q", none.Reason, want)
	}
}

func TestPreflightWithoutXDGRuntimeDirNamesIt(t *testing.T) {
	session(t, "", "wayland-test")
	if err := Preflight(); err == nil || !strings.Contains(err.Error(), "XDG_RUNTIME_DIR is not set") {
		t.Errorf("%v", err)
	}
}

func TestPreflightOnACompositorWithoutLayerShellFindsNoAltar(t *testing.T) {
	dir := t.TempDir()
	serve(t, dir, withoutLayerShell)
	session(t, dir, "wayland-test")
	err := Preflight()
	var noAltar *NoLayerShellError
	if !errors.As(err, &noAltar) || !strings.Contains(err.Error(), "zwlr_layer_shell_v1") {
		t.Errorf("%v", err)
	}
}

func TestPreflightOnACompositorWithoutFractionalScaleNamesIt(t *testing.T) {
	dir := t.TempDir()
	serve(t, dir, withoutFractionalScale)
	session(t, dir, "wayland-test")
	if err := Preflight(); err == nil || !strings.Contains(err.Error(), "wp_fractional_scale_manager_v1") {
		t.Errorf("%v", err)
	}
}

func TestPreflightAcceptsACompositorOfferingEverything(t *testing.T) {
	dir := t.TempDir()
	serve(t, dir, withLayerShell)
	session(t, dir, "wayland-test")
	if err := Preflight(); err != nil {
		t.Error(err)
	}
}

func TestPreflightWhenTheCompositorHangsUpAtOnceIsNoSession(t *testing.T) {
	dir := t.TempDir()
	c := serve(t, dir, withLayerShell)
	c.Close() // listening socket gone, clients hung up
	session(t, dir, "wayland-test")
	var none *NoSessionError
	if err := Preflight(); !errors.As(err, &none) {
		t.Errorf("%v", err)
	}
}
