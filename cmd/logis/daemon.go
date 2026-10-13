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

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/daemon"
)

// The `daemon` command: `logis daemon [--idle-timeout SECONDS]
// [--config PATH] [--detach] [--debug-panel]`; --debug-panel shows the
// window's debug panel instead of serving sessions (debugpanel.go).
// G3 (the window) replaces resolverFromEnv's default.

const (
	idleRune  = "--idle-timeout"
	idleWants = "a number of seconds (0 keeps the daemon resident)"
	// knownRunes are the runes this build knows, for the unknown-rune error.
	knownRunes = "--version, --check-config, --config PATH and " +
		"daemon [--idle-timeout SECONDS] [--config PATH] [--detach] [--debug-panel]"
)

// runDaemon is `logis daemon ARGS`.
func runDaemon(args []string, stdout, stderr io.Writer) int {
	o, err := parseDaemon(args)
	if err != nil {
		return fail(stderr, err.Error())
	}
	if o.debugPanel {
		return debugPanel(stderr)
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
	o.Resolver = resolverFromEnv
	o.Stdout, o.Stderr = stdout, stderr
	return daemon.Run(ctx, o.Options)
}

// resolverFromEnv is what answers sessions: with LOGIS_RESOLVER=scripted
// the Scripted resolver (tests and measurements without a window);
// otherwise, until the window exists, none.
func resolverFromEnv(host daemon.Host) (daemon.Resolver, error) {
	name, set := os.LookupEnv("LOGIS_RESOLVER")
	switch {
	case !set:
		return daemon.Windowless{}, nil
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

// parseDaemon parses the arguments after `daemon`.
func parseDaemon(args []string) (daemonArgs, error) {
	var o daemonArgs
	path, args, err := takeConfig(args)
	if err != nil {
		return o, err
	}
	if path != "" {
		source := config.NewSource(path)
		o.Config = &source
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var value string
		switch {
		case arg == "--detach":
			o.Detach = true
			continue
		case arg == "--debug-panel":
			o.debugPanel = true
			continue
		case arg == idleRune && i+1 < len(args):
			i++
			value = args[i]
		case arg == idleRune:
			return o, runeError("the rune " + idleRune + " stands empty: it wants " + idleWants)
		case strings.HasPrefix(arg, idleRune+"="):
			value = arg[len(idleRune)+1:]
		default:
			return o, runeError(fmt.Sprintf(
				"the rune '%s' is unknown to this cogitator. It knows only %s.", arg, knownRunes))
		}
		idle, ok := seconds(value)
		if !ok {
			return o, runeError(fmt.Sprintf("the rune %s cannot bear '%s': it wants %s",
				idleRune, value, idleWants))
		}
		o.IdleTimeout, o.IdleTimeoutSet = idle, true
	}
	return o, nil
}

// seconds parses a finite, non-negative decimal number of seconds; one too
// large for a Duration is the longest there is.
func seconds(text string) (time.Duration, bool) {
	if strings.ContainsAny(text, "xX_") {
		return 0, false // ParseFloat's hexadecimal and digit separators
	}
	s, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(s) || math.IsInf(s, 0) || s < 0 {
		return 0, false
	}
	if s >= float64(math.MaxInt64)/float64(time.Second) {
		return math.MaxInt64, true
	}
	return time.Duration(s * float64(time.Second)), true
}
