package config

import "time"

// Reload is what reading the config again did.
type Reload int

const (
	// Applied: a different sound config is in force now.
	Applied Reload = iota
	// Restored: the file is sound again after a refused edit, and the same
	// as before.
	Restored
	// Unchanged: nothing changed.
	Unchanged
	// Rejected: the edit was refused; the last sound config stays.
	Rejected
)

func (r Reload) String() string {
	return [...]string{"applied", "restored", "unchanged", "rejected"}[r]
}

// Live is the daemon's view of its config: the last sound one, what each
// reload means, and the precedence of `logis daemon --idle-timeout` over
// the config's idle-timeout. An invalid edit never replaces a sound config.
// It is not safe for concurrent use; the daemon's goroutine owns it.
type Live struct {
	config Config
	// idleFlag is --idle-timeout as given; it outranks the config, 0
	// included, when idleFlagSet.
	idleFlag    time.Duration
	idleFlagSet bool
	// rejected: the latest edit was refused.
	rejected bool
}

// NewLive holds config as the one in force.
func NewLive(config Config) *Live { return &Live{config: config} }

// WithIdleFlag records --idle-timeout d, which outranks the config.
func (l *Live) WithIdleFlag(d time.Duration) *Live {
	l.idleFlag, l.idleFlagSet = d, true
	return l
}

// Config is the config in force: the last sound one.
func (l *Live) Config() Config { return l.config }

// IdleTimeout is --idle-timeout if it was given, else the config's; zero
// keeps the daemon resident.
func (l *Live) IdleTimeout() time.Duration {
	if l.idleFlagSet {
		return l.idleFlag
	}
	return l.config.IdleTimeout
}

// Reload takes the result of loading the config again (Source.Load).
func (l *Live) Reload(config Config, loadErr error) Reload {
	if loadErr != nil {
		l.rejected = true
		return Rejected
	}
	wasRejected := l.rejected
	l.rejected = false
	switch {
	case !config.Equal(l.config):
		l.config = config
		return Applied
	case wasRejected:
		return Restored
	}
	return Unchanged
}
