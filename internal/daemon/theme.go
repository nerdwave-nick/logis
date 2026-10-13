package daemon

import (
	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The theme the config names (Rust src/daemon/mod.rs, lifecycle.rs
// reconfigure): read with the config at the start (one that cannot be worn
// leaves the bundled default, said once the log is in place), its files
// watched in themes/ beside the config, read again after every settled
// edit to either, a sound one handed to the resolver at once, a broken one
// logged and toasted while the last sound theme stands.

// Rethemer is a Resolver that wears themes: the daemon calls Retheme once
// before the first session, then whenever the theme in force changes. A
// visible window wears it at once. The theme is never changed after it is
// handed over. Called on the daemon's goroutine; it must not block.
type Rethemer interface {
	Retheme(*theme.Theme)
}

// retheme hands t to a resolver that wears themes.
func retheme(r Resolver, t *theme.Theme) {
	if rethemer, ok := r.(Rethemer); ok {
		rethemer.Retheme(t)
	}
}

// openTheme wears the theme c names, with user themes in dir ("" for
// none); why it could not be worn is kept for the log.
func (c *configuration) openTheme(dir string, loaded config.Config) {
	c.themes, c.themeErr = theme.NewLive(theme.NewThemes(dir), loaded)
}

// themeStands is the log line for a theme that could not be worn at the
// start.
func themeStands(err error) string {
	return config.ThemeHeresy(err) + "; the bundled " + theme.DefaultName + " stands"
}

// rewear reads the theme the config in force names again (its file or the
// config may be what changed).
func (l *loop) rewear() {
	switch r, err := l.themes.Reload(l.live.Config()); r {
	case config.Applied:
		l.logf("the cogitator dons the theme '" + l.themes.Theme().Name + "'")
		retheme(l.core.resolver, l.themes.Theme())
	case config.Restored:
		l.logf("the theme is sound once more; nothing has changed")
	case config.Rejected:
		l.logf(config.ThemeHeresy(err) + "; the last sound theme stands")
		if toaster, ok := l.core.resolver.(Toaster); ok {
			toaster.Toast(Toast{Kind: ToastThemeRejected, Error: err.Error()})
		}
	}
}
