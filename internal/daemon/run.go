// Package daemon is the daemon role, `logis daemon` (feat/logis §4.1): it
// claims $XDG_RUNTIME_DIR/logis.sock (or takes it from systemd socket
// activation), starts its resolver, reports readiness with one line on
// stdout, then serves sessions until it idles out, is dismissed (exit 0), or
// a component ends it through its Host (the compositor's loss: exit 2).
package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/protocol"
)

// exitError is the exit status of a daemon that cannot serve.
const exitError = 2

// Options are what a daemon run needs.
type Options struct {
	// RuntimeDir is $XDG_RUNTIME_DIR; it must be absolute.
	RuntimeDir string
	// IdleTimeout ends the daemon after this long without a client;
	// zero keeps it resident. Given (IdleTimeoutSet, or not zero), it is
	// --idle-timeout, which outranks the config's idle-timeout.
	IdleTimeout    time.Duration
	IdleTimeoutSet bool
	// Config is the config file the daemon reads before it takes the
	// socket, then watches; nil: none, the defaults stand.
	Config *config.Source
	// Detach leaves the starter's stdio once ready: stdout to /dev/null,
	// stderr to <RuntimeDir>/logis.log. It acts on the process's
	// descriptors 1 and 2.
	Detach bool
	// Resolver starts what answers sessions, with the Host it reports to.
	// It runs once the socket is claimed, before readiness; its error ends
	// the daemon (exit 2) and reaches a client that started it. Nil:
	// Windowless.
	Resolver func(Host) (Resolver, error)
	// Getenv is the daemon's environment for its apps: where the app index,
	// frecency and the session bus are, the current desktops, PATH and the
	// locale. Nil: the process's own.
	Getenv func(string) string
	// Preflight checks the session before anything else is read or taken
	// (Rust: wayland::preflight); its error ends the daemon (exit 2). Nil:
	// none, for a resolver that needs no compositor.
	Preflight func() error
	// Stdout takes the readiness line; Stderr is the daemon's log.
	Stdout, Stderr io.Writer
}

// Run is the daemon until it must stop; it returns the exit status. ctx's
// end stops it as an idle timeout would (exit 0).
func Run(ctx context.Context, o Options) int {
	logf := func(message string) { _, _ = fmt.Fprintf(o.Stderr, "logis: %s\n", message) }
	fail := func(message string) int {
		logf(message)
		return exitError
	}
	exe := watchExe() // what runs now, before an upgrade can replace it
	if _, err := protocol.SocketPath(o.RuntimeDir); err != nil {
		return fail("the machine spirit has no dwelling: " + err.Error())
	}
	if o.Preflight != nil {
		if err := o.Preflight(); err != nil {
			return fail(err.Error())
		}
	}
	cfg, err := openConfig(o)
	if err != nil {
		return fail(err.Error())
	}
	socket, err := claim(o.RuntimeDir)
	if err != nil {
		cfg.close()
		return fail("the machine spirit cannot take its socket: " + err.Error())
	}
	if socket == nil {
		cfg.close()
		announce(o.Stdout, "another machine spirit already listens; this one returns to rest")
		return 0
	}
	l := newLoop()
	l.live, l.themes, l.logf = cfg.live, cfg.themes, logf
	defer l.shut()
	defer socket.release()
	start := o.Resolver
	if start == nil {
		start = func(Host) (Resolver, error) { return Windowless{}, nil }
	}
	resolver, err := start(loopHost{l})
	if err != nil {
		cfg.close()
		return fail(err.Error())
	}
	l.core = newCore(resolver, os.Getpid())
	l.core.providers.spawn, l.core.logf = l.spawner, logf
	// systemd's socket: systemd starts the new binary once this one ends.
	l.core.stale = staleness(exe, !socket.ownsPath, os.Getpid())
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	stopApps := l.startApps(getenv)
	l.core.configure(cfg.live.Config())
	configure(resolver, cfg.live.Config())
	retheme(resolver, cfg.themes.Theme())
	go l.accept(socket.ln)
	announce(o.Stdout, "the machine spirit wakes and listens at "+socket.path)
	if o.Detach {
		detach(o.RuntimeDir)
	}
	// After detaching, so that what a reload logs reaches logis.log; edits
	// made meanwhile wait in the watch.
	if cfg.themeErr != nil {
		logf(themeStands(cfg.themeErr))
	}
	defer cfg.watch(l)()
	stopped := l.serve(ctx)
	l.retireAll()
	stopApps()
	if stopped.message != "" {
		logf(stopped.message)
	}
	return stopped.status
}

// announce is the readiness line: a client that started the daemon waits
// for it. The client may already be gone; the daemon carries on regardless.
func announce(stdout io.Writer, message string) {
	_, _ = fmt.Fprintf(stdout, "logis: %s\n", message)
}

// detach leaves the starter's stdio: stdout to /dev/null, stderr to
// <runtimeDir>/logis.log. Only the socket's owner gets here, so truncating
// the log is safe; it holds this daemon's diagnostics only.
func detach(runtimeDir string) {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer func() { _ = null.Close() }()
	_ = syscall.Dup3(int(null.Fd()), 1, 0)
	log, err := os.Create(filepath.Join(runtimeDir, "logis.log"))
	if err != nil {
		_ = syscall.Dup3(int(null.Fd()), 2, 0)
		return
	}
	defer func() { _ = log.Close() }()
	_ = syscall.Dup3(int(log.Fd()), 2, 0)
}
