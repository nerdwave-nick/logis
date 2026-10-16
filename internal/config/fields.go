package config

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// Each key's value is checked as it is read, and each expectation names its
// key, so the message says which setting is wrong — in the words serde gave
// Rust logis v1.

// field reads one key into a Config.
type field struct {
	key  string
	read func(r reader, c *Config) error
}

// fields are the config's keys, in the order the schema lists them.
var fields = []field{
	{"$schema", func(r reader, _ *Config) error {
		_, err := r.text("`$schema` as the URL or path of the JSON Schema", nil)
		return err
	}},
	{"theme", func(r reader, c *Config) (err error) {
		c.Theme, err = r.text("`theme` as a theme's name (its file name in themes/ without .jsonc: "+
			"not empty, no '/', not starting with '.')", isThemeName)
		return err
	}},
	{"idle-timeout", func(r reader, c *Config) (err error) {
		c.IdleTimeout, err = r.seconds()
		return err
	}},
	{"terminal", func(r reader, c *Config) (err error) {
		c.Terminal, err = r.command()
		return err
	}},
	{"icon-theme", func(r reader, c *Config) (err error) {
		c.IconTheme, err = r.text("`icon-theme` as the name of an icon theme, not empty",
			func(s string) bool { return s != "" })
		return err
	}},
	{"focus-loss", func(r reader, c *Config) error {
		word, err := r.text("`focus-loss` as \"dismiss\" or \"keep\"",
			func(s string) bool { return s == "dismiss" || s == "keep" })
		if word == "keep" {
			c.FocusLoss = FocusKeep
		}
		return err
	}},
	{"blur", func(r reader, c *Config) (err error) {
		c.Blur, err = r.flag("blur")
		return err
	}},
	{"reduce-motion", func(r reader, c *Config) (err error) {
		c.ReduceMotion, err = r.flag("reduce-motion")
		return err
	}},
	{"custom-shader", func(r reader, c *Config) (err error) {
		c.CustomShader, err = r.text("`custom-shader` as the path of a GLSL ES 3.00 fragment shader file", nil)
		return err
	}},
}

// keys are the config's keys, in the order the schema lists them.
var keys = func() []string {
	k := make([]string, len(fields))
	for i, f := range fields {
		k[i] = f.key
	}
	return k
}()

func fieldNamed(key string) (field, bool) {
	for _, f := range fields {
		if f.key == key {
			return f, true
		}
	}
	return field{}, false
}

// keyList is the keys as an "unknown field" message lists them.
func keyList() string { return "`" + strings.Join(keys, "`, `") + "`" }

// isThemeName says name is a file stem in themes/: not empty, no '/', and
// not starting with '.' (no hidden files, no ".."). The schema's pattern for
// "theme" says the same.
func isThemeName(name string) bool {
	return name != "" && !strings.HasPrefix(name, ".") && !strings.Contains(name, "/")
}

// reader reads one value of the document.
type reader struct {
	doc *jsonc.Doc
	v   *jsonc.Value
}

func (r reader) invalidType(v *jsonc.Value, expected string) error {
	return r.doc.Errorf(v.End, "invalid type: %s, expected %s", v.Unexpected(), expected)
}

func (r reader) invalidValue(v *jsonc.Value, expected string) error {
	return r.doc.Errorf(v.End, "invalid value: %s, expected %s", v.Unexpected(), expected)
}

// text is a string that valid (if any) accepts.
func (r reader) text(expected string, valid func(string) bool) (string, error) {
	switch {
	case r.v.Kind != jsonc.String:
		return "", r.invalidType(r.v, expected)
	case valid != nil && !valid(r.v.String):
		return "", r.invalidValue(r.v, expected)
	}
	return r.v.String, nil
}

// flag is true or false.
func (r reader) flag(key string) (bool, error) {
	if r.v.Kind != jsonc.Bool {
		return false, r.invalidType(r.v, "`"+key+"` as true or false")
	}
	return r.v.Bool, nil
}

// seconds is a number of seconds, 0 or more. Rust held up to 2⁶⁴ − 1
// seconds; a Duration ends at 292 years, which stands for any longer one.
func (r reader) seconds() (time.Duration, error) {
	const expected = "`idle-timeout` as seconds, 0 or more (0 keeps the daemon resident)"
	if r.v.Kind != jsonc.Number {
		return 0, r.invalidType(r.v, expected)
	}
	// Integers as integers (any u64 held), else as a float, as serde_json.
	s, ferr := strconv.ParseFloat(string(r.v.Number), 64)
	_, uerr := strconv.ParseUint(string(r.v.Number), 10, 64)
	if uerr != nil && (ferr != nil || s < 0 || s >= math.Exp2(64)) {
		return 0, r.invalidValue(r.v, expected)
	}
	if s >= float64(math.MaxInt64)/float64(time.Second) {
		return math.MaxInt64, nil
	}
	return time.Duration(math.Round(s * float64(time.Second))), nil
}

// command is a command line as an array: the program (not empty), then its
// arguments.
func (r reader) command() ([]string, error) {
	const expected = "`terminal` as a command line array such as [\"foot\", \"-e\"], its program not empty"
	if r.v.Kind != jsonc.Array {
		return nil, r.invalidType(r.v, expected)
	}
	argv := make([]string, 0, len(r.v.Elems))
	for _, elem := range r.v.Elems {
		if elem.Kind != jsonc.String {
			return nil, r.invalidType(elem, "`terminal` as an array of strings")
		}
		argv = append(argv, elem.String)
	}
	switch {
	case len(argv) == 0:
		return nil, r.doc.Errorf(r.v.End, "invalid length 0, expected %s", expected)
	case argv[0] == "":
		return nil, r.doc.Errorf(r.v.End, "invalid value: string \"\", expected %s", expected)
	}
	return argv, nil
}
