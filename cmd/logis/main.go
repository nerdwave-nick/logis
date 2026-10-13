// Command logis is a grimdark Wayland launcher and dmenu-compatible picker.
//
// This build knows --version, --dmenu/--dmenu0 with fuzzel's list runes,
// --provider PROGRAM [ARG]..., --check-config [--config PATH] and daemon;
// the other runes arrive with their tickets (feat/logis-go: apps,
// servitor, CLI surface).
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/nerdwave-nick/logis/internal/version"
	"github.com/nerdwave-nick/logis/internal/window"
)

// exitError is the exit status for every error (caller contract): the message
// goes to stderr.
const exitError = 2

func main() {
	os.Exit(runWith(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes one invocation without stdin and returns its exit status.
func run(args []string, stdout, stderr io.Writer) int {
	return runWith(args, eofReader{}, stdout, stderr)
}

// runWith executes one invocation and returns its exit status.
func runWith(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "daemon" {
		return runDaemon(args[1:], stdout, stderr)
	}
	c, err := parse(args)
	if err != nil {
		return fail(stderr, err.Error())
	}
	switch c.kind {
	case dmenuCommand:
		return runDmenu(c.config, c.dmenu, stdin, stdout, stderr)
	case providerCommand:
		return runProvider(c.config, c.argv, stderr)
	case checkConfigCommand:
		return runCheckConfig(c.config, stdout, stderr)
	case versionCommand:
		if _, err := fmt.Fprintf(stdout, "logis %s\n", version.Version); err != nil {
			return fail(stderr, "cannot write to stdout: "+err.Error())
		}
		return 0
	}
	// Plain logis is the app launcher; a session that cannot host it is
	// named first (caller contract: exit 2, the reason on stderr).
	if err := window.Preflight(); err != nil {
		return fail(stderr, err.Error())
	}
	return fail(stderr, "this cogitator has no app launcher yet. It knows only "+knownRunes+".")
}

// eofReader is an empty stdin.
type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

// fail reports message on stderr and returns the error status. A message
// stderr cannot take has nowhere else to go.
func fail(stderr io.Writer, message string) int {
	_, _ = fmt.Fprintf(stderr, "logis: %s\n", message)
	return exitError
}
