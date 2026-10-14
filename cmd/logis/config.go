package main

import (
	"fmt"
	"io"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// `logis --check-config [--config PATH]`, and `--config PATH` (or
// `--config=PATH`) wherever a config is read.

const (
	checkConfigRune = "--check-config"
	configRune      = "--config"
	configWants     = "one path to a config file"
)

// runCheckConfig is --check-config: the verdict on stdout (exit 0), or the
// heresy on stderr (exit 2).
func runCheckConfig(path string, stdout, stderr io.Writer) int {
	verdict, err := config.Check(config.NewSource(path), theme.Check)
	if err != nil {
		return fail(stderr, err.Error())
	}
	if _, err := fmt.Fprintf(stdout, "logis: %s\n", verdict); err != nil {
		return fail(stderr, "cannot write to stdout: "+err.Error())
	}
	return 0
}
