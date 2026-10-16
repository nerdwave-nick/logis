// Command logis is a grimdark Wayland launcher and dmenu-compatible picker.
//
// This build knows plain logis and --apps (the app launcher), --version,
// --dmenu/--dmenu0 with fuzzel's list runes, --provider PROGRAM [ARG]...,
// --servitor (and its provider, `provide servitor`), --check-config
// [--config PATH] and daemon; the other runes arrive with their tickets
// (feat/logis-go: CLI surface).
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/nerdwave-nick/logis/internal/version"
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
	if len(args) > 0 && args[0] == "provide" {
		return runProvide(args[1:], stdin, stdout, stderr)
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
	case servitorCommand:
		return runServitor(c.config, stderr)
	case checkConfigCommand:
		return runCheckConfig(c.config, stdout, stderr)
	case versionCommand:
		if _, err := fmt.Fprintf(stdout, "logis %s\n", version.Version); err != nil {
			return fail(stderr, "cannot write to stdout: "+err.Error())
		}
		return 0
	}
	// Plain logis is the app launcher, as in fuzzel; with servitor on the
	// PATH, tabbed with servitor mode's rites.
	return runApps(c.config, c.tabbed, stderr)
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
