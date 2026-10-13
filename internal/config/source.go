package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// Where the config comes from: --config PATH, else
// $XDG_CONFIG_HOME/logis/logis.jsonc ($HOME/.config when unset, empty or
// relative). A missing default file means the defaults; a missing --config
// file is an error, since the user named it.

// Problem is why a config could not be loaded.
type Problem int

const (
	// Missing: --config names a file that does not exist.
	Missing Problem = iota
	// Unreadable: the file exists but cannot be read as UTF-8 text.
	Unreadable
	// Invalid: the text is not a valid config.
	Invalid
)

// Source is the config file a process reads (and the daemon watches).
type Source struct {
	// path is "" when neither --config, XDG_CONFIG_HOME nor HOME names one.
	path string
	// explicit: named with --config, so it must exist.
	explicit bool
}

// Error is a config that could not be loaded, worded "PATH: why" or, for
// an invalid text, "PATH:LINE:COLUMN: why".
type Error struct {
	Path    string
	Problem Problem
	// Err is the reason: the read error, or the *jsonc.Invalid.
	Err error
}

func (e *Error) Error() string {
	switch e.Problem {
	case Missing:
		return e.Path + ": no such file"
	case Invalid:
		return e.Path + ":" + e.Err.Error()
	}
	return e.Path + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// NewSource is --config PATH if flag is not empty, else the default place
// from the environment. A relative PATH is taken from the current directory.
func NewSource(flag string) Source {
	if flag == "" {
		return Source{path: defaultPath(os.Getenv("XDG_CONFIG_HOME"), os.Getenv("HOME"))}
	}
	if abs, err := filepath.Abs(flag); err == nil {
		flag = abs
	}
	return Source{path: flag, explicit: true}
}

// defaultPath is $XDG_CONFIG_HOME/logis/logis.jsonc; the XDG base directory
// spec says to ignore an empty or relative value and use $HOME/.config.
func defaultPath(xdgConfigHome, home string) string {
	base := xdgConfigHome
	if !filepath.IsAbs(base) {
		if !filepath.IsAbs(home) {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "logis", "logis.jsonc")
}

// Path is the file, or "" when there is none to read.
func (s Source) Path() string { return s.path }

// Exists says the file exists (for messages; Load decides).
func (s Source) Exists() bool {
	if s.path == "" {
		return false
	}
	_, err := os.Stat(s.path)
	return err == nil
}

// Load reads and parses the file; a missing default file means the
// defaults. Its error is an *Error.
func (s Source) Load() (Config, error) {
	if s.path == "" {
		return Default(), nil
	}
	text, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !s.explicit:
		return Default(), nil
	case errors.Is(err, fs.ErrNotExist):
		return Default(), &Error{Path: s.path, Problem: Missing}
	case err != nil:
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		return Default(), &Error{Path: s.path, Problem: Unreadable, Err: err}
	case !utf8.Valid(text):
		return Default(), &Error{Path: s.path, Problem: Unreadable,
			Err: errors.New("stream did not contain valid UTF-8")}
	}
	c, err := Parse(text)
	if err != nil {
		return Default(), &Error{Path: s.path, Problem: Invalid, Err: err}
	}
	return c, nil
}

// Heresy is how an invalid config is reported on the command line and in
// the log.
func Heresy(err error) string { return "the config is heretical: " + err.Error() }

// ThemeHeresy is how an invalid theme is reported on the command line and
// in the log.
func ThemeHeresy(err error) string { return "the theme is heretical: " + err.Error() }
