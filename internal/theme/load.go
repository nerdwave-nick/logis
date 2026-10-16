package theme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nerdwave-nick/logis/internal/jsonc"
	"github.com/nerdwave-nick/logis/internal/oserr"
	"github.com/nerdwave-nick/logis/themes"
)

// Finding a theme by name and reading it with what it extends: a user file in the themes directory (themes/ beside
// the config) wins over a bundled theme of the same name; a theme may
// extend one other theme, and every theme lies over the bundled default,
// which gives every key.

// DefaultName is the bundled theme every other theme falls back to.
const DefaultName = "forge-crimson-brass"

// Error is why a theme could not be worn: the file (or bundled theme) the
// problem is in, with the line and column of a malformed text.
type Error struct{ Source, Problem string }

func (e *Error) Error() string { return e.Source + ": " + e.Problem }

// Themes is where themes are looked for: the user's themes directory, if
// there is one ("" for none).
type Themes struct{ dir string }

// NewThemes looks for user themes in dir ("" for none).
func NewThemes(dir string) Themes { return Themes{dir: dir} }

// Dir is the user's themes directory ("" for none).
func (th Themes) Dir() string { return th.dir }

// Bundled names the themes compiled into logis, the default first.
func Bundled() []string {
	names := make([]string, len(themes.Bundled))
	for i, b := range themes.Bundled {
		names[i] = b.Name
	}
	return names
}

// Default is the bundled default theme.
func Default() Theme {
	t, err := Themes{}.Load(DefaultName)
	if err != nil {
		panic("theme: the bundled default theme is unsound: " + err.Error())
	}
	return t
}

// found is one theme's text and where it came from.
type found struct {
	name string
	// path is the user file, "" for a bundled theme.
	path string
	text string
}

func (f found) source() string {
	if f.path == "" {
		return fmt.Sprintf("the bundled theme '%s'", f.name)
	}
	return f.path
}

func (f found) error(problem string) *Error { return &Error{Source: f.source(), Problem: problem} }

func (f found) parse() (node, error) {
	n, err := parseDoc([]byte(f.text))
	var invalid *jsonc.Invalid
	if errors.As(err, &invalid) {
		return nil, &Error{Source: fmt.Sprintf("%s:%d:%d", f.source(), invalid.Line, invalid.Column), Problem: invalid.Message}
	}
	return n, err
}

// Load is the theme name, resolved over what it extends and the default.
func (th Themes) Load(name string) (Theme, error) {
	f, err := th.find(name)
	if err != nil {
		return Theme{}, err
	}
	d, err := f.parse()
	if err != nil {
		return Theme{}, err
	}
	if parent, ok := d["extends"].(string); ok {
		p, err := th.parent(f, parent)
		if err != nil {
			return Theme{}, err
		}
		d = d.over(p)
	}
	def, _ := bundledTheme(DefaultName)
	base, err := def.parse()
	if err != nil {
		return Theme{}, err
	}
	dir := ""
	if f.path != "" {
		dir = filepath.Dir(f.path)
	}
	t, err := resolve(name, dir, d.over(base))
	if err != nil {
		return Theme{}, f.error(err.Error())
	}
	return t, nil
}

// parent is the theme child extends: by name, except that a theme
// extending its own name extends the bundled theme of that name (a user
// file amending a bundled theme). One level only.
func (th Themes) parent(child found, name string) (node, error) {
	var f found
	if name == child.name {
		var ok bool
		if f, ok = bundledTheme(name); !ok {
			return nil, child.error(fmt.Sprintf(`"extends": "%s" names this theme itself, and no bundled theme bears that name`, name))
		}
	} else {
		var err error
		if f, err = th.find(name); err != nil {
			var e *Error
			errors.As(err, &e)
			return nil, child.error(`"extends": ` + e.Problem)
		}
	}
	d, err := f.parse()
	if err != nil {
		return nil, err
	}
	switch next, ok := d["extends"].(string); {
	case !ok:
		return d, nil
	case next == child.name:
		return nil, child.error(fmt.Sprintf(`"extends" goes round in a circle: '%s' extends '%s', which extends '%s'`, child.name, name, next))
	default:
		return nil, child.error(fmt.Sprintf("'%s' extends '%s' in turn; a theme extends at most one level", name, next))
	}
}

// find is the user file name if it exists, else the bundled theme.
func (th Themes) find(name string) (found, error) {
	if th.dir != "" {
		path := filepath.Join(th.dir, name+".jsonc")
		text, err := os.ReadFile(path)
		switch {
		case err == nil:
			return found{name: name, path: path, text: string(text)}, nil
		case !errors.Is(err, fs.ErrNotExist):
			var pathErr *fs.PathError
			if errors.As(err, &pathErr) {
				err = pathErr.Err
			}
			return found{}, &Error{Source: path, Problem: oserr.Words(err)}
		}
	}
	if f, ok := bundledTheme(name); ok {
		return f, nil
	}
	place := ""
	if th.dir != "" {
		place = " nor in " + th.dir
	}
	return found{}, &Error{
		Source:  fmt.Sprintf("theme '%s'", name),
		Problem: fmt.Sprintf("no such theme is bundled (%s)%s", strings.Join(Bundled(), ", "), place),
	}
}

func bundledTheme(name string) (found, bool) {
	for _, b := range themes.Bundled {
		if b.Name == name {
			return found{name: name, text: b.Text}, true
		}
	}
	return found{}, false
}

// Check says whether the theme name can be worn, with user themes in dir
// ("" for none): it is read and resolved with what it extends
// (--check-config's config.ThemeCheck).
func Check(dir, name string) error {
	_, err := NewThemes(dir).Load(name)
	return err
}
