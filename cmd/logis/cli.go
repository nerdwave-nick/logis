package main

import (
	"fmt"
	"strings"
)

// Command-line parsing of the client role (Rust src/cli/mod.rs): one pass
// over the arguments, --config and the dmenu runes taken wherever they
// stand, every other argument naming the command. `logis daemon …` is
// parsed apart (daemon.go).

// knownRunes are the runes this build knows, for the unknown-rune error.
const knownRunes = "--version, --dmenu or --dmenu0 (with fuzzel's list runes), --check-config, " +
	"--config PATH and daemon [--idle-timeout SECONDS] [--config PATH] [--detach] [--debug-panel]"

type commandKind int

const (
	// appsCommand: plain logis, the app launcher.
	appsCommand commandKind = iota
	versionCommand
	checkConfigCommand
	// dmenuCommand: --dmenu or --dmenu0, with command.dmenu.
	dmenuCommand
)

// command is what one invocation of logis was asked to do; config is
// --config PATH ("" when not given), which a client passes on to a daemon
// it starts.
type command struct {
	kind   commandKind
	config string
	dmenu  dmenuOptions
}

// argQueue hands out the arguments in order; a rune takes its value from it.
type argQueue struct {
	args []string
	at   int
}

func (q *argQueue) next() (string, bool) {
	if q.at == len(q.args) {
		return "", false
	}
	q.at++
	return q.args[q.at-1], true
}

// parse parses the arguments after the program name (not `daemon`).
func parse(args []string) (command, error) {
	var c command
	started, nul := false, false
	runes := newDmenuRunes()
	rest := &argQueue{args: args}
	for {
		arg, ok := rest.next()
		if !ok {
			break
		}
		if took, err := configFlag(arg, rest, &c.config); took || err != nil {
			if err != nil {
				return c, err
			}
			continue
		}
		if took, err := runes.take(arg, rest); took || err != nil {
			if err != nil {
				return c, err
			}
			continue
		}
		var kind commandKind
		switch arg {
		case "--version", "-V":
			kind = versionCommand
		case "--dmenu", "-d":
			kind = dmenuCommand
		case "--dmenu0":
			kind, nul = dmenuCommand, true
		case checkConfigRune:
			kind = checkConfigCommand
		case "-i":
			continue // dmenu's case-insensitivity switch: logis always ignores case
		default:
			return c, unknownRune(arg)
		}
		if started {
			return c, unknownRune(arg)
		}
		started, c.kind = true, kind
	}
	if c.kind == dmenuCommand {
		d, err := runes.finish(nul)
		c.dmenu = d
		return c, err
	}
	return c, runes.noneGiven()
}

// configFlag takes `--config PATH` or `--config=PATH` into config; false
// if arg is another rune.
func configFlag(arg string, rest *argQueue, config *string) (bool, error) {
	var path string
	switch {
	case arg == configRune:
		value, ok := rest.next()
		if !ok {
			return true, runeError("the rune " + configRune + " stands empty: it wants " + configWants)
		}
		path = value
	case strings.HasPrefix(arg, configRune+"="):
		path = arg[len(configRune)+1:]
	default:
		return false, nil
	}
	if path == "" || *config != "" {
		return true, badValue(configRune, path, configWants)
	}
	*config = path
	return true, nil
}

func unknownRune(arg string) error {
	return runeError(fmt.Sprintf("the rune '%s' is unknown to this cogitator. It knows only %s.", arg, knownRunes))
}

func badValue(flag, value, wants string) error {
	return runeError(fmt.Sprintf("the rune %s cannot bear '%s': it wants %s", flag, value, wants))
}

func emptyValue(flag, wants string) error {
	return runeError(fmt.Sprintf("the rune %s stands empty: it wants %s", flag, wants))
}
