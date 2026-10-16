package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/cogitator"
	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/daemon"
	"github.com/nerdwave-nick/logis/internal/window"
)

// The `daemon` command: `logis daemon [--idle-timeout SECONDS]
// [--config PATH] [--detach] [--debug-panel]`; --debug-panel shows the
// window's debug panel instead of serving sessions (debugpanel.go).

const (
	idleRune  = "--idle-timeout"
	idleWants = "a number of seconds (0 keeps the daemon resident)"
)

// runDaemon is `logis daemon ARGS`.
func runDaemon(args []string, stdout, stderr io.Writer) int {
	o, err := parseDaemon(args)
	if err != nil {
		return fail(stderr, err.Error())
	}
	if o.debugPanel {
		return debugPanel(o.Config, stdout, stderr)
	}
	// A starter that stopped reading must not kill the daemon (the runtime
	// would on a broken stdout or stderr); a notified signal also leaves
	// launched programs SIGPIPE's default.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	// systemctl stop, or Ctrl+C in a terminal running it: leave cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	o.RuntimeDir = os.Getenv("XDG_RUNTIME_DIR")
	if o.Config == nil {
		source := config.NewSource("")
		o.Config = &source
	}
	o.Resolver = func(host daemon.Host) (daemon.Resolver, error) { return resolverFromEnv(host, stderr) }
	if _, set := os.LookupEnv("LOGIS_RESOLVER"); !set {
		// The window's session is checked first: before the config, and
		// before a second daemon defers to a first.
		o.Preflight = window.Preflight
	}
	o.Stdout, o.Stderr = stdout, stderr
	return daemon.Run(ctx, o.Options)
}

// resolverFromEnv is what answers sessions: the cogitator window, or with
// LOGIS_RESOLVER=scripted the Scripted resolver (tests and measurements
// without a window). The window is opened here, after the socket is
// claimed and before readiness; a session that cannot host it ends the
// daemon (exit 2, its reason relayed to the client that started it), and
// was already named by the preflight before the config was read.
func resolverFromEnv(host daemon.Host, log io.Writer) (daemon.Resolver, error) {
	name, set := os.LookupEnv("LOGIS_RESOLVER")
	switch {
	case !set:
		return cogitator.Start(host, log)
	case name == "scripted":
		return daemon.NewScripted(host), nil
	default:
		return nil, fmt.Errorf("LOGIS_RESOLVER names no known resolver: '%s'", name)
	}
}

// runeError is a refused argument; its text is the whole message.
type runeError string

func (e runeError) Error() string { return string(e) }

// daemonArgs are the parsed arguments after `daemon`: the daemon's
// options, or the debug panel.
type daemonArgs struct {
	daemon.Options
	// debugPanel: `logis daemon --debug-panel`, the window's standalone
	// debug entry point (G3), not the session daemon.
	debugPanel bool
}

// parseDaemon parses the arguments after `daemon` in order: the first
// refused one is named, and --idle-timeout takes whatever follows it as its
// value.
func parseDaemon(args []string) (daemonArgs, error) {
	var o daemonArgs
	var path string
	rest := &argQueue{args: args}
	for {
		arg, ok := rest.next()
		if !ok {
			break
		}
		if !utf8.ValidString(arg) {
			return o, unknownRune(arg)
		}
		if took, err := configFlag(arg, rest, &path); took || err != nil {
			if err != nil {
				return o, err
			}
			continue
		}
		var value string
		switch {
		case arg == "--detach":
			o.Detach = true
			continue
		case arg == "--debug-panel":
			o.debugPanel = true
			continue
		case arg == idleRune:
			if value, ok = rest.next(); !ok {
				return o, emptyValue(idleRune, idleWants)
			}
		case strings.HasPrefix(arg, idleRune+"="):
			value = arg[len(idleRune)+1:]
		default:
			return o, unknownRune(arg)
		}
		idle, ok := seconds(value)
		if !ok {
			return o, badValue(idleRune, value, idleWants)
		}
		o.IdleTimeout, o.IdleTimeoutSet = idle, true
	}
	if path != "" {
		source := config.NewSource(path)
		o.Config = &source
	}
	return o, nil
}

// seconds parses a non-negative decimal number of seconds under 2^64; one
// too large for a time.Duration is the longest there is.
func seconds(text string) (time.Duration, bool) {
	if strings.ContainsAny(text, "xX_") {
		return 0, false // ParseFloat's hexadecimal and digit separators
	}
	s, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(s) || s < 0 || s >= 0x1p64 {
		return 0, false
	}
	if s >= float64(math.MaxInt64)/float64(time.Second) {
		return math.MaxInt64, true
	}
	return time.Duration(s * float64(time.Second)), true
}
