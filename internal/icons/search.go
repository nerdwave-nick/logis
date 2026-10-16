// Package icons finds and draws icons (spec §4.7): freedesktop icon-theme
// lookup for the configured theme, its parents, hicolor and the pixmaps
// directories; SVG and PNG rasterisation; and a cache that puts each icon
// into the renderer's image atlas once per pixel size.
package icons

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SearchPaths are the Icon Theme Specification's base directories, in
// priority order.
type SearchPaths struct {
	// IconDirs hold icon themes (<dir>/<theme>/index.theme).
	IconDirs []string
	// PixmapDirs are searched last, for unthemed icons directly inside.
	PixmapDirs []string
}

// pixmaps is the pixmaps directory the spec names explicitly.
const pixmaps = "/usr/share/pixmaps"

// FromEnv reads $HOME, $XDG_DATA_HOME and $XDG_DATA_DIRS; see FromVars.
func FromEnv() SearchPaths {
	return FromVars(os.Getenv("HOME"), os.Getenv("XDG_DATA_HOME"), os.Getenv("XDG_DATA_DIRS"))
}

// FromVars is $HOME/.icons, $XDG_DATA_HOME/icons (default ~/.local/share),
// then icons in each of $XDG_DATA_DIRS (default /usr/local/share:/usr/share);
// pixmaps from pixmaps in each data dir, ending with /usr/share/pixmaps.
// Empty means unset. Relative paths are ignored, as the XDG spec requires;
// duplicates are dropped.
func FromVars(home, dataHome, dataDirs string) SearchPaths {
	if !filepath.IsAbs(home) {
		home = ""
	}
	if !filepath.IsAbs(dataHome) {
		dataHome = ""
		if home != "" {
			dataHome = filepath.Join(home, ".local/share")
		}
	}
	var dirs []string
	for _, d := range strings.Split(dataDirs, ":") {
		if filepath.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		dirs = []string{"/usr/local/share", "/usr/share"}
	}
	var icons, pix []string
	if home != "" {
		icons = append(icons, filepath.Join(home, ".icons"))
	}
	if dataHome != "" {
		icons = append(icons, filepath.Join(dataHome, "icons"))
	}
	for _, d := range dirs {
		icons = append(icons, filepath.Join(d, "icons"))
		pix = append(pix, filepath.Join(d, "pixmaps"))
	}
	pix = append(pix, pixmaps)
	return SearchPaths{IconDirs: dedup(icons), PixmapDirs: dedup(pix)}
}

func dedup(paths []string) []string {
	var out []string
	for _, p := range paths {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}
