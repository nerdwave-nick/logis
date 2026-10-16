package apps

import (
	"fmt"
	"strings"
	"unicode"
)

// The key-file format of desktop entries (Desktop Entry Specification,
// "Basic format of the file" and "Possible value types"): [Group] headers,
// Key=Value and Key[locale]=Value lines, # comments.
//
//   - Space around = is ignored; a value keeps its other spaces.
//   - Strings unescape \s, \n, \t, \r and \\; other backslashes stay (the
//     Exec key's own quoting needs them).
//   - Lists are separated by ; (\; is a literal one; a final ; is
//     optional).
//   - The first of duplicate keys counts.
//
// A line that is neither a comment, a group header nor a key, or a key
// before any group, makes the whole file invalid, as GLib's parser does.

// KeyFile is a parsed key file.
type KeyFile struct{ groups []Group }

// Group is one [Group] and its keys, values as written (still escaped).
type Group struct {
	// Name is the name between the brackets.
	Name    string
	entries []keyValue
}

type keyValue struct {
	key string
	// locale is the [locale] of a localised key ("" with localized
	// false: none).
	locale    string
	localized bool
	value     string
}

// InvalidError says why a key file was refused, and on which line (from 1).
type InvalidError struct {
	Line    int
	Message string
}

func (e *InvalidError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

// ParseKeyFile parses a key file.
func ParseKeyFile(text string) (*KeyFile, error) {
	f := &KeyFile{}
	for n, line := range lines(text) {
		invalid := func(message string) error { return &InvalidError{Line: n + 1, Message: message} }
		trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if header, ok := strings.CutPrefix(trimmed, "["); ok {
			name, ok := strings.CutSuffix(strings.TrimRightFunc(header, unicode.IsSpace), "]")
			if !ok || name == "" || strings.ContainsAny(name, "[]") {
				return nil, invalid("a group header wants [Name]")
			}
			f.groups = append(f.groups, Group{Name: name})
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			return nil, invalid("neither a group, a key nor a comment")
		}
		kv, ok := splitKey(strings.TrimRightFunc(key, unicode.IsSpace))
		if !ok {
			return nil, invalid("a malformed key")
		}
		if len(f.groups) == 0 {
			return nil, invalid("a key before any group")
		}
		g := &f.groups[len(f.groups)-1]
		if !g.has(kv) {
			kv.value = strings.TrimLeftFunc(value, unicode.IsSpace)
			g.entries = append(g.entries, kv)
		}
	}
	return f, nil
}

// lines are text's lines: split at \n, a trailing \r dropped, no empty
// line after a final \n.
func lines(text string) []string {
	all := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, l := range all {
		all[i] = strings.TrimSuffix(l, "\r")
	}
	return all
}

// splitKey reads Key or Key[locale]; keys are A-Za-z0-9-.
func splitKey(key string) (keyValue, bool) {
	name, rest, bracketed := strings.Cut(key, "[")
	kv := keyValue{key: name, localized: bracketed}
	if bracketed {
		locale, ok := strings.CutSuffix(rest, "]")
		if !ok || locale == "" {
			return kv, false
		}
		kv.locale = locale
	}
	if name == "" {
		return kv, false
	}
	for _, b := range []byte(name) {
		alnum := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
		if !alnum && b != '-' {
			return kv, false
		}
	}
	return kv, true
}

func (g *Group) has(kv keyValue) bool {
	for _, e := range g.entries {
		if e.key == kv.key && e.localized == kv.localized && e.locale == kv.locale {
			return true
		}
	}
	return false
}

// Groups are the groups in file order.
func (f *KeyFile) Groups() []Group { return f.groups }

// Group is the first group of this name.
func (f *KeyFile) Group(name string) (*Group, bool) {
	for i := range f.groups {
		if f.groups[i].Name == name {
			return &f.groups[i], true
		}
	}
	return nil, false
}

// Raw is the unlocalised value of key, as written.
func (g *Group) Raw(key string) (string, bool) {
	for _, e := range g.entries {
		if e.key == key && !e.localized {
			return e.value, true
		}
	}
	return "", false
}

// rawLocalized is the value of key best suited to l (see Locale), as
// written.
func (g *Group) rawLocalized(key string, l Locale) (string, bool) {
	best, value, found := 0, "", false
	for _, e := range g.entries {
		if e.key != key {
			continue
		}
		rank := len(l.candidates) // the plain key: after every locale
		if e.localized {
			r, ok := l.Rank(e.locale)
			if !ok {
				continue
			}
			rank = r
		}
		if !found || rank < best {
			best, value, found = rank, e.value, true
		}
	}
	return value, found
}

// String is a string value, unescaped.
func (g *Group) String(key string) (string, bool) {
	v, ok := g.Raw(key)
	return Unescape(v), ok
}

// Localized is a localised string value, unescaped.
func (g *Group) Localized(key string, l Locale) (string, bool) {
	v, ok := g.rawLocalized(key, l)
	return Unescape(v), ok
}

// Bool is a boolean: true is true, anything else (or nothing) false.
func (g *Group) Bool(key string) bool {
	v, ok := g.Raw(key)
	return ok && strings.TrimRightFunc(v, unicode.IsSpace) == "true"
}

// List is a list of strings, each unescaped.
func (g *Group) List(key string) []string {
	v, ok := g.Raw(key)
	if !ok {
		return nil
	}
	return splitList(v)
}

// LocalizedList is a localised list of strings, each unescaped.
func (g *Group) LocalizedList(key string, l Locale) []string {
	v, ok := g.rawLocalized(key, l)
	if !ok {
		return nil
	}
	return splitList(v)
}

// Unescape undoes \s, \n, \t, \r and \\; any other backslash stays.
func Unescape(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var out strings.Builder
	rs := []rune(value)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '\\' {
			out.WriteRune(rs[i])
			continue
		}
		if i+1 == len(rs) {
			out.WriteByte('\\')
			break
		}
		i++
		switch rs[i] {
		case 's':
			out.WriteByte(' ')
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case 'r':
			out.WriteByte('\r')
		case '\\':
			out.WriteByte('\\')
		default:
			out.WriteByte('\\')
			out.WriteRune(rs[i])
		}
	}
	return out.String()
}

// splitList splits a list at each ; that is not escaped, then unescapes
// each element (\; becomes ;). Empty elements are dropped.
func splitList(value string) []string {
	var items []string
	var current strings.Builder
	rs := []rune(value)
	for i := 0; i < len(rs); i++ {
		switch c := rs[i]; {
		case c == '\\' && i+1 == len(rs):
			current.WriteByte('\\')
		case c == '\\' && rs[i+1] == ';':
			i++
			current.WriteByte(';')
		case c == '\\':
			i++
			current.WriteByte('\\')
			current.WriteRune(rs[i])
		case c == ';':
			items = append(items, current.String())
			current.Reset()
		default:
			current.WriteRune(c)
		}
	}
	items = append(items, current.String())
	var out []string
	for _, item := range items {
		if item = Unescape(strings.TrimRight(item, "\n\r")); item != "" {
			out = append(out, item)
		}
	}
	return out
}
