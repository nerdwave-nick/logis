// Package apps is logis's own reading of desktop applications (feat/logis
// stories 5–7, §4.7): desktop entries (the Desktop Entry Specification 1.5,
// not GIO's reading), the index of what the launcher shows, what launching
// one runs, frecency, and the watch that keeps the index current.
package apps

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/launch"
)

// The app index: every desktop entry the launcher may show, read from the
// applications directories of the XDG base directories (Desktop Entry
// Specification, "Desktop File ID" and "Hidden"):
//
//   - $XDG_DATA_HOME/applications (default ~/.local/share) first, then
//     applications in each of $XDG_DATA_DIRS (default
//     /usr/local/share:/usr/share), in order; relative paths are ignored;
//   - a file's desktop-file ID is its path below applications with / turned
//     into - (forge/litany.desktop is forge-litany.desktop);
//   - the first file with an ID wins: earlier directories override later
//     ones, and an override with Hidden=true deletes the entry. A file that
//     is not a usable application entry (malformed, a link …) is skipped
//     and overrides nothing;
//   - shown are entries without NoDisplay, whose OnlyShowIn names one of
//     the current desktops ($XDG_CURRENT_DESKTOP, colon-separated) if
//     given, whose NotShowIn names none of them, and whose TryExec program
//     exists and is executable (an absolute path, or found in $PATH).

// Dirs are the applications directories, highest precedence first.
type Dirs []string

// Context is what decides how entries read and which show: the user's
// locale, the current desktops and the program search path.
type Context struct {
	Locale Locale
	// Desktops is $XDG_CURRENT_DESKTOP, split at ':'.
	Desktops []string
	// Path is $PATH, split.
	Path []string
}

// App is one application the launcher shows.
type App struct {
	// ID is its desktop-file ID, such as org.gnome.Nautilus.desktop.
	ID string
	// Location is the file it was read from (for %k).
	Location string
	Entry    DesktopEntry
}

// AppID is the ID without .desktop: the application ID (D-Bus name,
// app_id).
func (a *App) AppID() string { return strings.TrimSuffix(a.ID, ".desktop") }

// Index is the applications to show, by name, and the directories they
// came from.
type Index struct {
	apps []App
	// before runs before each directory is read (a Watcher's watch).
	before func(dir string)
	// Dirs is every directory read (the applications directories and those
	// below).
	Dirs []string
}

// DirsFromVars are the applications directories of an environment.
func DirsFromVars(get func(string) string) Dirs {
	var roots []string
	if h := get("XDG_DATA_HOME"); filepath.IsAbs(h) {
		roots = append(roots, h)
	} else if home := get("HOME"); filepath.IsAbs(home) {
		roots = append(roots, filepath.Join(home, ".local/share"))
	}
	var data []string
	for _, d := range filepath.SplitList(get("XDG_DATA_DIRS")) {
		if filepath.IsAbs(d) {
			data = append(data, d)
		}
	}
	if len(data) == 0 {
		data = []string{"/usr/local/share", "/usr/share"}
	}
	var dirs Dirs
	for _, root := range append(roots, data...) {
		if apps := filepath.Join(root, "applications"); !slices.Contains(dirs, apps) {
			dirs = append(dirs, apps)
		}
	}
	return dirs
}

// ContextFromVars is the context of an environment.
func ContextFromVars(get func(string) string) Context {
	cx := Context{Locale: LocaleFromVars(get)}
	for _, d := range strings.Split(get("XDG_CURRENT_DESKTOP"), ":") {
		if d != "" {
			cx.Desktops = append(cx.Desktops, d)
		}
	}
	if path := get("PATH"); path != "" {
		cx.Path = filepath.SplitList(path)
	}
	return cx
}

// Shows says whether an entry shows (see above).
func (cx Context) Shows(e DesktopEntry) bool {
	on := func(list []string) bool {
		return slices.ContainsFunc(list, func(d string) bool { return slices.Contains(cx.Desktops, d) })
	}
	return !e.Hidden && !e.NoDisplay &&
		(len(e.OnlyShowIn) == 0 || on(e.OnlyShowIn)) && !on(e.NotShowIn) &&
		(e.TryExec == "" || cx.finds(e.TryExec))
}

// finds says whether program is an executable file: at its absolute path,
// or in one of the search path's directories.
func (cx Context) finds(program string) bool {
	_, ok := launch.LookPath(strings.Join(cx.Path, string(filepath.ListSeparator)), program)
	return ok
}

// afterRead, in tests, runs right after each directory is read (or found
// missing): where a change could slip past a scan.
var afterRead func(dir string)

// maxDepth is how deep below an applications directory entries are looked
// for.
const maxDepth = 8

// Scan reads every entry of dirs.
func Scan(dirs Dirs, cx Context) *Index { return scan(dirs, cx, nil) }

// scan reads every entry of dirs, calling before ahead of reading each
// directory.
func scan(dirs Dirs, cx Context, before func(dir string)) *Index {
	ix := &Index{before: before}
	seen := map[string]bool{}
	for _, dir := range dirs {
		ix.walk(dir, "", 0, seen, cx)
	}
	lower := make(map[string]string, len(ix.apps))
	for _, a := range ix.apps {
		lower[a.ID] = strings.ToLower(a.Entry.Name)
	}
	slices.SortFunc(ix.apps, func(a, b App) int {
		if c := strings.Compare(lower[a.ID], lower[b.ID]); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return ix
}

// walk reads the files below dir, whose IDs start with prefix.
func (ix *Index) walk(dir, prefix string, depth int, seen map[string]bool, cx Context) {
	if ix.before != nil {
		ix.before(dir)
	}
	names, err := os.ReadDir(dir)
	if afterRead != nil {
		afterRead(dir)
	}
	if err != nil {
		return
	}
	ix.Dirs = append(ix.Dirs, dir)
	for _, n := range names { // sorted by name
		name := n.Name()
		if !utf8.ValidString(name) {
			continue
		}
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if depth < maxDepth {
				ix.walk(path, prefix+name+"-", depth+1, seen, cx)
			}
			continue
		}
		id := prefix + name
		if !strings.HasSuffix(name, ".desktop") || seen[id] {
			continue
		}
		text, err := os.ReadFile(path)
		if err != nil || !utf8.Valid(text) {
			continue
		}
		// A file that is not a usable entry overrides nothing.
		e, err := ParseEntry(string(text), cx.Locale)
		if err != nil {
			continue
		}
		seen[id] = true
		if cx.Shows(e) {
			ix.apps = append(ix.apps, App{ID: id, Location: path, Entry: e})
		}
	}
}

// Apps are the applications, by name (ignoring case), then ID.
func (ix *Index) Apps() []App { return ix.apps }

// Get is the application with this desktop-file ID.
func (ix *Index) Get(id string) (*App, bool) {
	for i := range ix.apps {
		if ix.apps[i].ID == id {
			return &ix.apps[i], true
		}
	}
	return nil, false
}
