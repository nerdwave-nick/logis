package apps

import (
	"fmt"
	"strings"
)

// The Exec key (Desktop Entry Specification, "The Exec key"; Rust
// src/apps/exec.rs): its arguments and their field codes.
//
// The key-file escapes are undone first (keyfile.go); then the value is
// split into arguments the way a shell splits words, which is what the
// spec's quoting rules amount to and what GLib accepts:
//   - arguments are separated by spaces;
//   - in double quotes, a backslash escapes ", `, $ and \ (and stays before
//     any other character);
//   - single quotes quote everything up to the next one (GLib's leniency);
//   - outside quotes, a backslash escapes the next character.
//
// Field codes, as a launcher without files to open expands them:
//   - %f, %F, %u, %U: no file, so an argument that is only the code
//     disappears, and inside an argument the code becomes nothing;
//   - %i: --icon <Icon> (two arguments) when the entry has an icon;
//   - %c: the translated Name; %k: the desktop file's location;
//   - %%: a %;
//   - the deprecated %d %D %n %N %v %m: removed.
//
// Any other field code makes the Exec key (and its entry) invalid.

// ExecErrorKind is why an Exec value is refused.
type ExecErrorKind int

const (
	// ExecEmpty: no program to run.
	ExecEmpty ExecErrorKind = iota
	// ExecUnterminated: a quote that is never closed.
	ExecUnterminated
	// ExecUnknownCode: a field code the spec does not define (Code).
	ExecUnknownCode
	// ExecLonePercent: a % that ends an argument.
	ExecLonePercent
)

// ExecError is why an Exec value is refused.
type ExecError struct {
	Kind ExecErrorKind
	// Code is the unknown field code's letter.
	Code rune
}

func (e *ExecError) Error() string {
	switch e.Kind {
	case ExecUnterminated:
		return "Exec has a quote that never closes"
	case ExecUnknownCode:
		return fmt.Sprintf("Exec has an unknown field code %%%c", e.Code)
	case ExecLonePercent:
		return "Exec has a lone % (a literal one is %%)"
	}
	return "Exec names no program"
}

// Exec is the arguments of an Exec key, unquoted, field codes still in
// them.
type Exec struct{ args []string }

// Fields are what the field codes expand to.
type Fields struct {
	// Icon is the entry's Icon, for %i ("" for none).
	Icon string
	// Name is the entry's translated Name, for %c.
	Name string
	// Location is the desktop file's location, for %k.
	Location string
}

// knownCodes are every field code: the spec's, %% and the deprecated ones.
const knownCodes = "fFuUickdDnNvm%"

// ParseExec splits an (unescaped) Exec value into arguments and checks its
// field codes.
func ParseExec(value string) (Exec, error) {
	args, err := split(value)
	if err != nil {
		return Exec{}, err
	}
	if len(args) == 0 || args[0] == "" {
		return Exec{}, &ExecError{Kind: ExecEmpty}
	}
	for _, arg := range args {
		rs := []rune(arg)
		for i := 0; i < len(rs); i++ {
			if rs[i] != '%' {
				continue
			}
			if i+1 == len(rs) {
				return Exec{}, &ExecError{Kind: ExecLonePercent}
			}
			i++
			if !strings.ContainsRune(knownCodes, rs[i]) {
				return Exec{}, &ExecError{Kind: ExecUnknownCode, Code: rs[i]}
			}
		}
	}
	return Exec{args: args}, nil
}

// Program is the program: the first argument, as written.
func (e Exec) Program() string {
	if len(e.args) == 0 {
		return ""
	}
	return e.args[0]
}

// Expand is the command line, field codes expanded for a launch without
// files.
func (e Exec) Expand(f Fields) []string {
	out := make([]string, 0, len(e.args))
	for _, arg := range e.args {
		switch arg {
		case "%f", "%F", "%u", "%U", "%d", "%D", "%n", "%N", "%v", "%m":
		case "%i":
			if f.Icon != "" {
				out = append(out, "--icon", f.Icon)
			}
		default:
			out = append(out, inline(arg, f))
		}
	}
	return out
}

// inline expands the field codes inside an argument (checked by ParseExec).
func inline(arg string, f Fields) string {
	if !strings.Contains(arg, "%") {
		return arg
	}
	var out strings.Builder
	rs := []rune(arg)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '%' {
			out.WriteRune(rs[i])
			continue
		}
		if i++; i == len(rs) {
			break
		}
		switch rs[i] {
		case '%':
			out.WriteByte('%')
		case 'c':
			out.WriteString(f.Name)
		case 'k':
			out.WriteString(f.Location)
		}
	}
	return out.String()
}

// split splits a value into words, as described above.
func split(value string) ([]string, error) {
	var words []string
	var word strings.Builder
	inWord := false
	rs := []rune(value)
	unterminated := &ExecError{Kind: ExecUnterminated}
	for i := 0; i < len(rs); i++ {
		switch c := rs[i]; c {
		case ' ', '\t', '\n':
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		case '"':
			inWord = true
			for {
				if i++; i == len(rs) {
					return nil, unterminated
				}
				if rs[i] == '"' {
					break
				}
				if rs[i] != '\\' {
					word.WriteRune(rs[i])
					continue
				}
				if i++; i == len(rs) {
					return nil, unterminated
				}
				if !strings.ContainsRune("\"`$\\", rs[i]) {
					word.WriteByte('\\')
				}
				word.WriteRune(rs[i])
			}
		case '\'':
			inWord = true
			for {
				if i++; i == len(rs) {
					return nil, unterminated
				}
				if rs[i] == '\'' {
					break
				}
				word.WriteRune(rs[i])
			}
		case '\\':
			inWord = true
			if i+1 < len(rs) {
				i++
				word.WriteRune(rs[i])
			} else {
				word.WriteByte('\\')
			}
		default:
			inWord = true
			word.WriteRune(c)
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, nil
}
