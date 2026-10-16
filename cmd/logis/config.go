package main

import (
	"fmt"
	"io"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// `logis --check-config [--config PATH]`, and `--config PATH` (or
// `--config=PATH`) wherever a config is read.

const (
	checkConfigRune = "--check-config"
	configRune      = "--config"
	configWants     = "one path to a config file"
)

// checkShader compiles the custom shader the config c would wear (its
// own, else its theme's) on a headless context; "" when there is none.
func checkShader(dir string, c config.Config) (string, error) {
	live, err := theme.NewLive(theme.NewThemes(dir), c)
	if err != nil {
		return "", nil // the theme's heresy is reported before
	}
	s := live.Theme().Custom
	if !s.Set() {
		return "", nil
	}
	return s.Path, render.CheckCustom(s)
}

// runCheckConfig is --check-config: the verdict on stdout (exit 0), or the
// heresy on stderr (exit 2).
func runCheckConfig(path string, stdout, stderr io.Writer) int {
	verdict, err := config.Check(config.NewSource(path), theme.Check, checkShader)
	if err != nil {
		return fail(stderr, err.Error())
	}
	if _, err := fmt.Fprintf(stdout, "logis: %s\n", verdict); err != nil {
		return fail(stderr, "cannot write to stdout: "+err.Error())
	}
	return 0
}
