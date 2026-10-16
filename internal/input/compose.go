package input

import (
	"os"

	"github.com/nerdwave-nick/logis/internal/xkb"
)

// Fed is what feeding one keysym to a Compose did.
type Fed int

const (
	// Pass: not part of a sequence; the key means what it always means.
	Pass Fed = iota
	// Swallowed: a sequence goes on, or the key cancelled one.
	Swallowed
	// Composed: a sequence completed (with its text).
	Composed
)

// Compose is a Compose table and the sequence typed so far (dead keys and
// the Compose key).
type Compose struct {
	state *xkb.ComposeState
}

// NewCompose composes with table.
func NewCompose(table *xkb.ComposeTable) *Compose { return &Compose{state: table.NewState()} }

// ComposeFromEnv is the table for the process's locale (LC_ALL, LC_CTYPE,
// LANG) as libxkbcommon finds it (XCOMPOSEFILE, then
// $XDG_CONFIG_HOME/XCompose, then ~/.XCompose, then the locale's system
// table), or for C if that locale has none; nil if neither loads (keys then
// pass through uncomposed).
func ComposeFromEnv() *Compose {
	env := xkb.EnvFromOS()
	locale := xkb.ComposeLocale(os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE"), os.Getenv("LANG"))
	t, err := xkb.ComposeFromLocale(env, locale)
	if err != nil {
		t, err = xkb.ComposeFromLocale(env, "C")
	}
	if err != nil {
		return nil
	}
	return NewCompose(t)
}

// Feed feeds a pressed key's keysym. Modifier keysyms are never fed: a
// Shift pressed for '"' must not cancel the sequence it belongs to.
func (c *Compose) Feed(ks xkb.Keysym) (Fed, string) {
	if ks.IsModifier() || !c.state.Feed(ks) {
		return Pass, ""
	}
	switch c.state.Status() {
	case xkb.ComposeComposing:
		return Swallowed, ""
	case xkb.ComposeComposed:
		text := c.state.UTF8()
		c.state.Reset()
		return Composed, text
	case xkb.ComposeCancelled:
		c.state.Reset()
		return Swallowed, ""
	}
	return Pass, ""
}

// Reset forgets a sequence in progress (focus or keymap changed).
func (c *Compose) Reset() { c.state.Reset() }
