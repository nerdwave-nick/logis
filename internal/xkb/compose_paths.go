package xkb

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ComposeEnv is the environment Compose file lookup reads.
type ComposeEnv struct {
	XComposeFile, XDGConfigHome, Home, XLocaleDir string
}

// EnvFromOS reads ComposeEnv from the process environment.
func EnvFromOS() ComposeEnv {
	dir := os.Getenv("XLOCALEDIR")
	if dir == "" {
		dir = "/usr/share/X11/locale"
	}
	return ComposeEnv{os.Getenv("XCOMPOSEFILE"), os.Getenv("XDG_CONFIG_HOME"), os.Getenv("HOME"), dir}
}

// ComposeLocale picks the locale whose Compose table applies: LC_ALL,
// LC_CTYPE or LANG, the first that is set and not empty, else C.
func ComposeLocale(lcAll, lcCtype, lang string) string {
	for _, v := range []string{lcAll, lcCtype, lang} {
		if v != "" {
			return v
		}
	}
	return "C"
}

// resolveName looks name up in a "LEFT: RIGHT" registry file
// (compose.dir, locale.alias); leftToRight selects the direction.
func resolveName(env ComposeEnv, file, name string, leftToRight bool) string {
	f, err := os.Open(filepath.Join(env.XLocaleDir, file))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimLeft(sc.Text(), " \t")
		if line == "" || line[0] == '#' {
			continue
		}
		i := strings.IndexAny(line, " \t:")
		if i < 0 {
			i = len(line)
		}
		left, rest := line[:i], line[i:]
		rest = strings.TrimPrefix(rest, ":")
		rest = strings.TrimLeft(rest, " \t")
		right := rest
		if j := strings.IndexAny(rest, " \t"); j >= 0 {
			right = rest[:j]
		}
		if leftToRight && left == name {
			return right
		}
		if !leftToRight && right == name {
			return left
		}
	}
	return ""
}

func localeComposePath(env ComposeEnv, locale string) string {
	if locale == "C" {
		locale = "en_US.UTF-8"
	}
	p := resolveName(env, "compose.dir", locale, false)
	if p == "" {
		// libxkbcommon falls back to en_US.UTF-8 for an installed locale
		// without a Compose entry.
		p = resolveName(env, "compose.dir", "en_US.UTF-8", false)
	}
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(env.XLocaleDir, p)
}

// ComposeFromLocale loads the table libxkbcommon would load:
// XCOMPOSEFILE, $XDG_CONFIG_HOME/XCompose (or ~/.config/XCompose),
// ~/.XCompose, then the locale's system table.
func ComposeFromLocale(env ComposeEnv, locale string) (*ComposeTable, error) {
	if alias := resolveName(env, "locale.alias", locale, true); alias != "" {
		locale = alias
	}
	var candidates []string
	if env.XComposeFile != "" {
		candidates = append(candidates, env.XComposeFile)
	}
	if env.XDGConfigHome != "" && filepath.IsAbs(env.XDGConfigHome) {
		candidates = append(candidates, filepath.Join(env.XDGConfigHome, "XCompose"))
	} else if env.Home != "" {
		candidates = append(candidates, filepath.Join(env.Home, ".config", "XCompose"))
	}
	if env.Home != "" {
		candidates = append(candidates, filepath.Join(env.Home, ".XCompose"))
	}
	if p := localeComposePath(env, locale); p != "" {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		// A file that cannot be read is passed over; one that reads but
		// does not parse fails the lookup, as in libxkbcommon.
		t := &ComposeTable{root: &composeNode{}, locale: locale, env: env, Path: p}
		data, err := t.readFile(p)
		if err != nil {
			continue
		}
		if err := t.parse(data, p, 0); err != nil {
			return nil, err
		}
		return t, nil
	}
	return nil, fmt.Errorf("no Compose file for locale %q", locale)
}
