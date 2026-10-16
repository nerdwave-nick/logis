package config

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/nerdwave-nick/logis/internal/custom"
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

// ShaderCheck says which custom shader the config c would wear (its own,
// else its theme's, with user themes in dir) and whether it compiles: its
// path ("" for none) and why not, in the user's lines (--check-config's
// render.CheckCustom on a headless context).
type ShaderCheck func(dir string, c Config) (string, error)

// Check is --check-config: the verdict on the config s names, the theme it
// names and the custom shader it wears, or the flavoured error.
func Check(s Source, theme ThemeCheck, shader ShaderCheck) (string, error) {
	c, err := s.Load()
	if err != nil {
		return "", errors.New(Heresy(err))
	}
	if err := theme(ThemesDir(s.Path()), c.Theme); err != nil {
		return "", errors.New(ThemeHeresy(err))
	}
	path, err := shader(ThemesDir(s.Path()), c)
	if err != nil {
		return "", errors.New(custom.Heresy(err))
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
	verdict = fmt.Sprintf("%s; the theme '%s' is sound", verdict, c.Theme)
	if path != "" {
		verdict += "; the custom shader " + path + " is sound"
	}
	return verdict, nil
}
