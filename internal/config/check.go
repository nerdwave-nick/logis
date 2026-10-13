package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ThemeCheck says whether the theme name can be worn, looking for user
// themes in dir (themes/ beside the config; "" when there is none). The
// theme engine (feat/logis-go G18) supplies the real one, which reads and
// resolves the theme; FindTheme stands in until then.
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

// bundledThemes are the themes compiled into logis (Rust src/theme/load.rs
// BUNDLED); the theme engine (G18) owns them once it exists.
var bundledThemes = []string{DefaultTheme, "cogitator-phosphor"}

// FindTheme finds the theme name as the theme engine would: the user file
// dir/<name>.jsonc, else a bundled theme. It does not read the file's
// contents; that is the theme engine's (G18). Errors are worded as Rust's.
func FindTheme(dir, name string) error {
	if dir != "" {
		path := filepath.Join(dir, name+".jsonc")
		_, err := os.ReadFile(path)
		var pathErr *fs.PathError
		switch {
		case err == nil:
			return nil
		case !errors.Is(err, fs.ErrNotExist) && errors.As(err, &pathErr):
			return fmt.Errorf("%s: %w", path, pathErr.Err)
		case !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if slices.Contains(bundledThemes, name) {
		return nil
	}
	place := ""
	if dir != "" {
		place = " nor in " + dir
	}
	return fmt.Errorf("theme '%s': no such theme is bundled (%s)%s", name, strings.Join(bundledThemes, ", "), place)
}
