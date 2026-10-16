package theme

import (
	"reflect"

	"github.com/nerdwave-nick/logis/internal/config"
	"github.com/nerdwave-nick/logis/internal/custom"
)

// Live is the theme in force while the daemon runs (Rust src/config/live.rs
// Worn): the last sound theme the config named, stilled when the config
// reduces motion. A theme that cannot be worn leaves the last sound one in
// force. A worn theme is never changed in place, so the window may hold
// one while the daemon reloads. Not safe for concurrent use; the daemon's
// goroutine owns it.
type Live struct {
	themes Themes
	// sound is the theme as loaded; worn is what is worn.
	sound, worn *Theme
	rejected    bool
}

// NewLive wears the theme c names from th. One that cannot be worn leaves
// the bundled default in force; the error says why.
func NewLive(th Themes, c config.Config) (*Live, error) {
	sound, err := th.Load(c.Theme)
	if err != nil {
		sound = Default()
	}
	l := &Live{themes: th, sound: &sound}
	l.worn = wornAs(l.sound, c)
	return l, err
}

// Theme is the theme worn now.
func (l *Live) Theme() *Theme { return l.worn }

// Reload reads the theme the config c names again (after a theme file or
// the config changed). A theme that cannot be worn is Rejected with the
// error; the last sound one stays in force.
func (l *Live) Reload(c config.Config) (config.Reload, error) {
	theme, err := l.themes.Load(c.Theme)
	if err != nil {
		l.rejected = true
		return config.Rejected, err
	}
	wasRejected := l.rejected
	l.rejected = false
	l.sound = &theme
	switch now := wornAs(l.sound, c); {
	case !reflect.DeepEqual(*now, *l.worn):
		l.worn = now
		return config.Applied, nil
	case wasRejected:
		return config.Restored, nil
	}
	return config.Unchanged, nil
}

// wornAs is t as the config c would have it worn: with the config's
// custom shader (read now) over the theme's, stilled when motion is
// reduced.
func wornAs(t *Theme, c config.Config) *Theme {
	if c.CustomShader == "" && !c.ReduceMotion {
		return t
	}
	worn := *t
	if c.CustomShader != "" {
		worn.Custom = custom.Load(c.CustomShader)
	}
	if c.ReduceMotion {
		worn = worn.Stilled()
	}
	return &worn
}
