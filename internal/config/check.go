package config

import (
	"errors"
	"fmt"
	"path/filepath"
)

// ThemeCheck says whether the theme name can be worn, looking for user
// themes in dir (themes/ beside the config; "" when there is none): the
// theme engine's theme.Check, which reads and resolves the theme.
type ThemeCheck func(dir, name string) error

// ThemesDir is the themes directory beside the config file at path ("" for
// none).
func ThemesDir(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "themes")
}

// Check is --check-config: the verdict on the config s names and the theme
// it names, or the flavoured error.
func Check(s Source, theme ThemeCheck) (string, error) {
	c, err := s.Load()
	if err != nil {
		return "", errors.New(Heresy(err))
	}
	if err := theme(ThemesDir(s.Path()), c.Theme); err != nil {
		return "", errors.New(ThemeHeresy(err))
	}
	var verdict string
	switch {
	case s.Path() == "":
		verdict = "no config to be found (neither XDG_CONFIG_HOME nor HOME is set); the defaults stand"
	case s.Exists():
		verdict = "the config at " + s.Path() + " is sound"
	default:
		verdict = "no config at " + s.Path() + "; the defaults stand"
	}
	return fmt.Sprintf("%s; the theme '%s' is sound", verdict, c.Theme), nil
}
