package e2e

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Compositor is a test compositor the harness started, with its scratch
// runtime directory; t's cleanup stops it and removes the directory.
type Compositor struct {
	t      testing.TB
	kind   Kind
	cmd    *exec.Cmd
	dir    string
	socket string // absolute path of its wayland-N socket
	exited chan struct{}
	repo   string
}

// Option adjusts a compositor before it starts.
type Option func(*options)

type options struct{ scale string }

// Scale sets the output scale (niri: any; sway: its one output). The
// default is 1.0.
func Scale(s string) Option { return func(o *options) { o.scale = s } }

// Start starts kind and waits for its Wayland socket.
func Start(t testing.TB, kind Kind, opts ...Option) *Compositor {
	t.Helper()
	o := options{scale: "1.0"}
	for _, opt := range opts {
		opt(&o)
	}
	// Not t.TempDir(): a socket path must stay under 108 bytes.
	dir, err := os.MkdirTemp("", "logis-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	c := &Compositor{t: t, kind: kind, dir: dir, exited: make(chan struct{}), repo: moduleRoot(t)}
	t.Cleanup(c.stop)
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	c.write("fonts.conf", fmt.Sprintf(fontsConf, filepath.Join(c.repo, "tests", "fonts"), filepath.Join(dir, "fontconfig")))
	c.write("XCompose", XCompose)
	if err := os.MkdirAll(c.ConfigHome(), 0o700); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "compositor.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	switch kind {
	case NestedNiri:
		c.cmd = c.niri(t, o.scale)
	case HeadlessSway:
		c.cmd = c.sway(o.scale)
	default:
		t.Fatalf("no test compositor %v", kind)
	}
	c.cmd.Env = append(c.cmd.Env, "XDG_RUNTIME_DIR="+dir)
	c.cmd.Stdout, c.cmd.Stderr = log, log
	if err := c.cmd.Start(); err != nil {
		t.Fatalf("cannot start the %s test compositor: %v", kind.Name(), err)
	}
	go func() { _ = c.cmd.Wait(); close(c.exited) }()
	c.socket = c.waitForSocket()
	return c
}

func (c *Compositor) write(name, text string) {
	if err := os.WriteFile(filepath.Join(c.dir, name), []byte(text), 0o600); err != nil {
		c.t.Fatal(err)
	}
}

// base is the minimal environment of a test compositor: nothing of the
// session but what it needs to run (and, nested, to open its window).
func base(program string, args ...string) *exec.Cmd {
	cmd := exec.Command(program, args...)
	cmd.Env = []string{}
	for _, name := range []string{"PATH", "HOME", "LANG", "VK_DRIVER_FILES", "LIBGL_ALWAYS_SOFTWARE"} {
		if v, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+v)
		}
	}
	return cmd
}

func (c *Compositor) niri(t testing.TB, scale string) *exec.Cmd {
	config := filepath.Join(c.dir, "niri.kdl")
	c.write("niri.kdl", fmt.Sprintf(NiriConfig, scale))
	cmd := base("niri", "-c", config)
	// The nested window opens in the running session; its own socket goes
	// to the scratch runtime directory, so the parent is named absolutely.
	cmd.Env = append(cmd.Env, "WAYLAND_DISPLAY="+parentSocket(t), "RUST_LOG=niri=warn")
	return cmd
}

// parentSocket is the running session's socket, absolute.
func parentSocket(t testing.TB) string {
	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		t.Fatal("nested niri opens a window, so it needs WAYLAND_DISPLAY of a running session")
	}
	if filepath.IsAbs(display) {
		return display
	}
	return filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), display)
}

func (c *Compositor) sway(scale string) *exec.Cmd {
	config := filepath.Join(c.dir, "sway.conf")
	c.write("sway.conf", fmt.Sprintf(SwayConfig, scale))
	cmd := base("sway", "--config", config)
	renderer := os.Getenv("WLR_RENDERER")
	if renderer == "" {
		// Software composition without a GPU; the caller's choice wins.
		renderer = "pixman"
	}
	cmd.Env = append(cmd.Env, "WLR_BACKENDS=headless", "WLR_HEADLESS_OUTPUTS=1",
		"WLR_LIBINPUT_NO_DEVICES=1", "WLR_RENDERER="+renderer)
	return cmd
}

var numbered = regexp.MustCompile(`^wayland-[0-9]+$`)

func (c *Compositor) waitForSocket() string {
	deadline := time.Now().Add(Patience)
	for {
		entries, _ := os.ReadDir(c.dir)
		for _, e := range entries {
			if numbered.MatchString(e.Name()) && e.Type()&fs.ModeSocket != 0 {
				return filepath.Join(c.dir, e.Name())
			}
		}
		select {
		case <-c.exited:
			c.t.Fatalf("the %s test compositor exited (%v) before opening its socket:\n%s",
				c.kind.Name(), c.cmd.ProcessState, c.Log())
		default:
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("the %s test compositor opened no socket in %v:\n%s", c.kind.Name(), Patience, c.Log())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stop ends the compositor (its log goes to the test's output first when
// the test failed) and removes its directory.
func (c *Compositor) stop() {
	if c.cmd != nil && c.cmd.Process != nil {
		if c.t.Failed() {
			c.t.Logf("%s test compositor log:\n%s", c.kind.Name(), c.Log())
		}
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-c.exited:
		case <-time.After(Patience):
			_ = c.cmd.Process.Kill()
			<-c.exited
		}
	}
	_ = os.RemoveAll(c.dir)
}

// Kill kills the compositor at once and waits for it: the compositor's
// loss, as its clients see it.
func (c *Compositor) Kill() {
	_ = c.cmd.Process.Kill()
	<-c.exited
}

// Kind is which compositor this is.
func (c *Compositor) Kind() Kind { return c.kind }

// PID is the compositor process, the server of Socket.
func (c *Compositor) PID() int { return c.cmd.Process.Pid }

// Socket is its Wayland socket, inside RuntimeDir.
func (c *Compositor) Socket() string { return c.socket }

// RuntimeDir is the scratch XDG_RUNTIME_DIR it and its clients use.
func (c *Compositor) RuntimeDir() string { return c.dir }

// ConfigHome is the clients' scratch XDG_CONFIG_HOME; logis/logis.jsonc
// in it is their config.
func (c *Compositor) ConfigHome() string { return filepath.Join(c.dir, "config") }

// Log is everything the compositor logged so far.
func (c *Compositor) Log() string {
	b, _ := os.ReadFile(filepath.Join(c.dir, "compositor.log"))
	return string(b)
}

// ScratchFile writes data to a file in the runtime directory.
func (c *Compositor) ScratchFile(name string, data []byte) string {
	path := filepath.Join(c.dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		c.t.Fatal(err)
	}
	return path
}

// isolated are the variables a client must not inherit: they point at
// another session (and the scratch runtime dir has no session bus, so
// nothing a test daemon launches reaches the real one).
var isolated = []string{"WAYLAND_SOCKET", "DISPLAY", "NIRI_SOCKET", "SWAYSOCK",
	"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES", "DBUS_SESSION_BUS_ADDRESS"}

// Command is program as a client of this compositor: its runtime directory
// and socket, the pinned fonts, the harness's Compose table, a scratch
// XDG_CONFIG_HOME (never the owner's logis config), and nothing that
// points at another session.
func (c *Compositor) Command(program string, args ...string) *exec.Cmd {
	cmd := exec.Command(program, args...)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "XDG_RUNTIME_DIR", "WAYLAND_DISPLAY", "FONTCONFIG_FILE", "XCOMPOSEFILE", "XDG_CONFIG_HOME":
			continue
		}
		if !slices.Contains(isolated, name) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env,
		"XDG_RUNTIME_DIR="+c.dir,
		"WAYLAND_DISPLAY="+filepath.Base(c.socket),
		"FONTCONFIG_FILE="+filepath.Join(c.dir, "fonts.conf"),
		"XCOMPOSEFILE="+filepath.Join(c.dir, "XCompose"),
		"XDG_CONFIG_HOME="+c.ConfigHome())
	return cmd
}

// Logis is the logis binary (Binary) as a client of this compositor.
func (c *Compositor) Logis(args ...string) *exec.Cmd {
	return c.Command(Binary(c.t), args...)
}

// Msg runs `niri msg args…` against the nested niri's IPC socket.
func (c *Compositor) Msg(args ...string) {
	c.t.Helper()
	if c.kind != NestedNiri {
		c.t.Fatalf("niri msg needs nested niri, not %s", c.kind.Name())
	}
	sockets, _ := filepath.Glob(filepath.Join(c.dir, "niri.*.sock"))
	if len(sockets) == 0 {
		c.t.Fatal("the nested niri has no IPC socket")
	}
	cmd := c.Command("niri", append([]string{"msg"}, args...)...)
	cmd.Env = append(cmd.Env, "NIRI_SOCKET="+sockets[0])
	if out, err := cmd.CombinedOutput(); err != nil {
		c.t.Fatalf("niri msg %v: %v\n%s", args, err, out)
	}
}
