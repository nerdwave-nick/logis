// Package config is logis's configuration (feat/logis §4.9): JSONC at
// $XDG_CONFIG_HOME/logis/logis.jsonc (or --config PATH), described by the
// JSON Schema schema/logis.schema.json that a config names under "$schema".
//
// Every key is optional; an absent key, an empty file and a missing default
// file all mean the defaults. Unknown keys and values of the wrong kind are
// errors that name the key and its line and column. While the daemon runs,
// Live holds the last sound config and Watcher reports edits to the file.
package config

import (
	"slices"
	"time"

	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// FocusLoss is what losing keyboard focus does to a visible session.
type FocusLoss int

const (
	// FocusDismiss dismisses it, as fuzzel does by default.
	FocusDismiss FocusLoss = iota
	// FocusKeep keeps it open until it is answered or dismissed otherwise.
	FocusKeep
)

// The names used when the config names none.
const (
	DefaultTheme     = "forge-crimson-brass"
	DefaultIconTheme = "hicolor"
)

// Config is the settings of this slice; later slices add their own.
type Config struct {
	// Theme is the theme's name: themes/<name>.jsonc beside the config, or
	// a bundled theme.
	Theme string
	// IdleTimeout ends the daemon after this long without a client; zero
	// keeps it resident. `logis daemon --idle-timeout` outranks it.
	IdleTimeout time.Duration
	// Terminal is the program and leading arguments that run a terminal
	// app's command (Terminal=true), such as ["foot", "-e"]; nil means
	// $TERMINAL -e.
	Terminal []string
	// IconTheme is the freedesktop icon theme; lookups fall back to hicolor.
	IconTheme string
	// FocusLoss is what losing keyboard focus does to a visible session.
	FocusLoss FocusLoss
	// Blur asks the compositor to blur behind the panel (when it offers
	// ext-background-effect).
	Blur bool
	// ReduceMotion stills the theme's continuous effects (glow pulse,
	// travelling highlight, flicker, shimmer, the CRT pass's motion),
	// turns the glitch off and freezes a custom shader's time; transitions
	// stay.
	ReduceMotion bool
	// CustomShader is the custom pass's shader file (absolute once
	// loaded: a relative path starts beside the config); "" leaves the
	// theme's, if it names one. It outranks the theme's.
	CustomShader string
}

// Default is the config of an absent or empty file.
func Default() Config {
	return Config{Theme: DefaultTheme, IconTheme: DefaultIconTheme, Blur: true}
}

// Equal says two configs hold the same settings.
func (c Config) Equal(o Config) bool {
	return c.Theme == o.Theme && c.IdleTimeout == o.IdleTimeout &&
		(c.Terminal == nil) == (o.Terminal == nil) && slices.Equal(c.Terminal, o.Terminal) &&
		c.IconTheme == o.IconTheme && c.FocusLoss == o.FocusLoss &&
		c.Blur == o.Blur && c.ReduceMotion == o.ReduceMotion && c.CustomShader == o.CustomShader
}

// Parse reads a config text; only whitespace and comments mean the
// defaults. A refused text is a *jsonc.Invalid.
func Parse(text []byte) (Config, error) {
	c := Default()
	doc, err := jsonc.Parse(text, "config")
	if err != nil || doc.Root == nil {
		return c, err
	}
	seen := map[string]bool{}
	for _, m := range doc.Root.Members {
		f, known := fieldNamed(m.Key)
		switch {
		case !known:
			return Default(), doc.Errorf(m.KeyEnd, "unknown field `%s`, expected one of %s", m.Key, keyList())
		case seen[m.Key]:
			return Default(), doc.Errorf(m.KeyEnd, "duplicate field `%s`", m.Key)
		}
		seen[m.Key] = true
		if err := f.read(reader{doc, m.Value}, &c); err != nil {
			return Default(), err
		}
	}
	return c, nil
}
