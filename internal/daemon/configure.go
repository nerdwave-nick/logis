package daemon

import (
	"errors"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/theme"
)

// The daemon's config (feat/logis §4.9): read before the socket is taken (an
// invalid one keeps the daemon from starting), watched from before that
// read, and reloaded on every settled edit. A sound edit is applied at once;
// an invalid one leaves the last sound config in force and is reported in
// the log and as a toast (Rust src/daemon/lifecycle.rs reconfigure).

// ToastKind is what a toast reports.
type ToastKind int

const (
	// ToastConfigRejected: an edit to the config was refused; the last
	// sound config stays in force.
	ToastConfigRejected ToastKind = iota
	// ToastThemeRejected: the theme the config names could not be worn;
	// the last sound theme stays in force.
	ToastThemeRejected
)

// Toast is a short, themed notice for the user (CONTEXT.md: toast,
// vox-cast). The window shows it over a visible session, or on its own; the
// daemon's log carries the same words, also while no window exists.
type Toast struct {
	Kind ToastKind
	// Error is what was refused and where: "PATH:LINE:COLUMN: why".
	Error string
}

// Configurer is a Resolver that applies the config: the daemon calls
// Configure once before the first session, then after every applied reload.
// A visible window applies it at once. Like the Resolver's methods it is
// called on the daemon's goroutine and must not block.
type Configurer interface {
	Configure(config.Config)
}

// Toaster is a Resolver that shows toasts, such as a refused config edit.
// Called on the daemon's goroutine; it must not block.
type Toaster interface {
	Toast(Toast)
}

// configuration is the config of a daemon run: where it comes from, the one
// in force, and the watch on its file.
type configuration struct {
	source   *config.Source
	live     *config.Live
	watcher  *config.Watcher
	watchErr error
	// themes is the theme in force, themeErr why the one named at the
	// start could not be worn; themeWatcher watches the themes directory.
	themes        *theme.Live
	themeErr      error
	themeWatcher  *config.Watcher
	themeWatchErr error
}

// openConfig watches the config, then reads it, so that no edit falls
// between the two. Its error is the flavoured heresy.
func openConfig(o Options) (*configuration, error) {
	c := &configuration{source: o.Config}
	loaded := config.Default()
	themesDir := ""
	if o.Config != nil {
		if path := o.Config.Path(); path != "" {
			c.watcher, c.watchErr = config.Watch(path)
			themesDir = config.ThemesDir(path)
			c.themeWatcher, c.themeWatchErr = config.WatchFiles(themesDir, ".jsonc")
		}
		var err error
		if loaded, err = o.Config.Load(); err != nil {
			c.close()
			return nil, errors.New(config.Heresy(err))
		}
	}
	c.live = config.NewLive(loaded)
	c.openTheme(themesDir, loaded)
	// A non-zero IdleTimeout counts as given (callers before the config).
	if o.IdleTimeoutSet || o.IdleTimeout != 0 {
		c.live.WithIdleFlag(o.IdleTimeout)
	}
	return c, nil
}

func (c *configuration) close() {
	for _, w := range []*config.Watcher{c.watcher, c.themeWatcher} {
		if w != nil {
			_ = w.Close()
		}
	}
}

// watch hands every settled edit to l as a reload, until the returned stop:
// of the config, and of the themes (a changed theme file reads the config
// again too: the theme the config names is read with it). What cannot be
// watched is said in the log.
func (c *configuration) watch(l *loop) (stop func()) {
	var done []chan struct{}
	for _, w := range []struct {
		what    string
		watcher *config.Watcher
		err     error
	}{{"config", c.watcher, c.watchErr}, {"themes", c.themeWatcher, c.themeWatchErr}} {
		if w.err != nil {
			l.logf("the " + w.what + " is not watched, edits need a restart: " + w.err.Error())
		}
		if w.watcher == nil {
			continue
		}
		ended := make(chan struct{})
		done = append(done, ended)
		go func() {
			defer close(ended)
			w.watcher.Run(func() bool {
				loaded, err := c.source.Load()
				l.post(func(l *loop) { l.reconfigure(loaded, err) })
				return true
			})
		}()
	}
	return func() {
		c.close()
		for _, ended := range done {
			<-ended
		}
	}
}

// configure hands the config in force to a resolver that applies it.
func configure(r Resolver, c config.Config) {
	if configurer, ok := r.(Configurer); ok {
		configurer.Configure(c)
	}
}

// reconfigure applies a reread config, or keeps the last sound one and says
// why; then the same for the theme it names.
func (l *loop) reconfigure(loaded config.Config, err error) {
	switch l.live.Reload(loaded, err) {
	case config.Applied:
		l.logf("the machine spirit heeds the amended config")
		configure(l.core.resolver, l.live.Config())
	case config.Restored:
		l.logf("the config is sound once more; nothing has changed")
	case config.Rejected:
		l.logf(config.Heresy(err) + "; the last sound config stands")
		if toaster, ok := l.core.resolver.(Toaster); ok {
			toaster.Toast(Toast{Kind: ToastConfigRejected, Error: err.Error()})
		}
	}
	l.rewear()
}
