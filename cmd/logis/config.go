package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/nerdwave-nick/logis/internal/config"
)

// `logis --check-config [--config PATH]`, and `--config PATH` (or
// `--config=PATH`) wherever a config is read.

const (
	checkConfigRune = "--check-config"
	configRune      = "--config"
	configWants     = "one path to a config file"
)

// takeConfig takes `--config PATH` or `--config=PATH` out of args: the path
// ("" when not given) and the other args in order. The path must not be
// empty, and only one may be given.
func takeConfig(args []string) (string, []string, error) {
	var path string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		var value string
		switch arg := args[i]; {
		case arg == configRune && i+1 < len(args):
			i++
			value = args[i]
		case arg == configRune:
			return "", nil, runeError("the rune " + configRune + " stands empty: it wants " + configWants)
		case strings.HasPrefix(arg, configRune+"="):
			value = arg[len(configRune)+1:]
		default:
			rest = append(rest, arg)
			continue
		}
		if value == "" || path != "" {
			return "", nil, runeError(fmt.Sprintf("the rune %s cannot bear '%s': it wants %s",
				configRune, value, configWants))
		}
		path = value
	}
	return path, rest, nil
}

// runCheckConfig is --check-config: the verdict on stdout (exit 0), or the
// heresy on stderr (exit 2).
func runCheckConfig(path string, stdout, stderr io.Writer) int {
	verdict, err := config.Check(config.NewSource(path), config.FindTheme)
	if err != nil {
		return fail(stderr, err.Error())
	}
	if _, err := fmt.Fprintf(stdout, "logis: %s\n", verdict); err != nil {
		return fail(stderr, "cannot write to stdout: "+err.Error())
	}
	return 0
}
