package window

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/nerdwave-nick/logis/internal/oserr"
	"github.com/nerdwave-nick/logis/internal/wlproto"
)

// layerShell is the global a compositor must offer for logis to raise its
// window (spec §4.2: a missing layer shell is a clear error).
const layerShell = "zwlr_layer_shell_v1"

// requiredGlobals are the other globals the window cannot do without.
var requiredGlobals = []string{"wl_compositor", "wp_fractional_scale_manager_v1", "wp_viewporter"}

// socketPath resolves the compositor socket the way libwayland does,
// without its wayland-0 default: an unset WAYLAND_DISPLAY means there is
// no session.
func socketPath(display, runtimeDir string) (string, error) {
	if display == "" {
		return "", errors.New("WAYLAND_DISPLAY is not set")
	}
	if filepath.IsAbs(display) {
		return display, nil
	}
	if !filepath.IsAbs(runtimeDir) {
		return "", errors.New("XDG_RUNTIME_DIR is not set or not absolute")
	}
	return filepath.Join(runtimeDir, display), nil
}

// required checks the advertised interfaces: layer shell first (the
// clearest refusal), then the rest of what the window needs.
func required(interfaces []string) error {
	if !slices.Contains(interfaces, layerShell) {
		return &NoLayerShellError{}
	}
	for _, need := range requiredGlobals {
		if !slices.Contains(interfaces, need) {
			return &MissingGlobalError{Interface: need}
		}
	}
	return nil
}

// Global is a global the compositor advertises.
type Global struct {
	Name      uint32
	Interface string
	Version   uint32
}

// connection is a greeted compositor: the display, its registry and the
// globals it advertises now.
type connection struct {
	display  *wlproto.Display
	registry *wlproto.WlRegistry
	globals  []Global
	// removed is told of each global the compositor withdraws.
	removed func(Global)
}

// connect reaches the session's compositor (a socket it handed over in
// WAYLAND_SOCKET, else WAYLAND_DISPLAY) and reads its globals.
func connect() (*connection, error) {
	var d *wlproto.Display
	if os.Getenv("WAYLAND_SOCKET") != "" {
		var err error
		if d, err = wlproto.ConnectEnv(); err != nil {
			return nil, &NoSessionError{Reason: "WAYLAND_SOCKET: " + err.Error()}
		}
	} else {
		path, err := socketPath(os.Getenv("WAYLAND_DISPLAY"), os.Getenv("XDG_RUNTIME_DIR"))
		if err != nil {
			return nil, &NoSessionError{Reason: err.Error()}
		}
		fd, err := dial(path)
		if err != nil {
			return nil, &NoSessionError{Reason: fmt.Sprintf("cannot reach %s: %s", path, oserr.Words(err))}
		}
		if d, err = wlproto.ConnectFd(fd); err != nil {
			return nil, &NoSessionError{Reason: err.Error()}
		}
	}
	c := &connection{display: d}
	c.registry = d.Registry()
	c.registry.OnGlobal = func(name uint32, iface string, version uint32) {
		c.globals = append(c.globals, Global{name, iface, version})
	}
	c.registry.OnGlobalRemove = func(name uint32) {
		i := slices.IndexFunc(c.globals, func(g Global) bool { return g.Name == name })
		if i < 0 {
			return
		}
		g := c.globals[i]
		c.globals = slices.Delete(c.globals, i, i+1)
		if c.removed != nil {
			c.removed(g)
		}
	}
	if err := d.Roundtrip(); err != nil {
		d.Close()
		return nil, &NoSessionError{Reason: "the compositor broke off the greeting: " + err.Error()}
	}
	return c, nil
}

func dial(path string) (int, error) {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	if err := syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

func (c *connection) interfaces() []string {
	names := make([]string, len(c.globals))
	for i, g := range c.globals {
		names[i] = g.Interface
	}
	return names
}

// bind binds the first advertised global of iface at its version, up to
// max; nil if it is not advertised.
func (c *connection) bind(iface *wlproto.Interface, max uint32) wlproto.Object {
	for _, g := range c.globals {
		if g.Interface == iface.Name {
			return c.registry.Bind(g.Name, iface, min(g.Version, max, iface.Version))
		}
	}
	return nil
}

// Preflight checks that the session's compositor can host the cogitator:
// a reachable session offering wlr-layer-shell and the other globals the
// window needs. The connection is closed again.
func Preflight() error {
	c, err := connect()
	if err != nil {
		return err
	}
	defer c.display.Close()
	return required(c.interfaces())
}

func closeFd(fd int) {
	if fd >= 0 {
		_ = syscall.Close(fd)
	}
}
