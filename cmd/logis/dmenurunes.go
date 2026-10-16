package main

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nerdwave-nick/logis/internal/list"
	"github.com/nerdwave-nick/logis/internal/nth"
)

// The dmenu runes (feat/logis §4.6), fuzzel-compatible: what the list shows
// and how the choice is printed. A value follows its flag as `--flag=V` or
// `--flag V` (--password takes its character only as `--password=C`).

// dmenuOptions are what a dmenu session reads, shows and prints.
type dmenuOptions struct {
	// separator ends an input line: '\n', or NUL for --dmenu0.
	separator byte
	// readStdin: stdin is read at all (--prompt-only reads none).
	readStdin bool
	// noRunIfEmpty: exit at once, without a window, when stdin is empty.
	noRunIfEmpty bool
	// view is how the list looks and behaves.
	view list.Options
	// index prints the chosen item's index instead of its text.
	index bool
	// columns are the --with-nth, --match-nth and --accept-nth columns,
	// split at --nth-delimiter (tab by default).
	columns nth.Columns
}

func defaultDmenu() dmenuOptions {
	return dmenuOptions{
		separator: '\n', readStdin: true, view: list.DefaultOptions(),
		columns: nth.Columns{Delimiter: '\t'},
	}
}

const (
	linesWant     = "a number of rows (0 or more)"
	widthWant     = "a width in characters (1 or more)"
	indexWant     = "an item's index (0 or more)"
	nthWant       = "a column number or a format with {N}, {N..M} or {N..}"
	delimiterWant = "one ASCII character"
	passwordWant  = "one character to show in place of each typed one"
)

// dmenuRunes collects the dmenu runes of one command line.
type dmenuRunes struct {
	d dmenuOptions
	// first is the first dmenu rune seen, for the error without --dmenu.
	first                     string
	selectText, selectByIndex bool
}

func newDmenuRunes() *dmenuRunes { return &dmenuRunes{d: defaultDmenu()} }

// take takes arg (and its value from rest) if it is a dmenu rune.
func (r *dmenuRunes) take(arg string, rest *argQueue) (bool, error) {
	name, inline, hasInline := arg, "", false
	if before, after, ok := strings.Cut(arg, "="); ok && strings.HasPrefix(before, "--") {
		name, inline, hasInline = before, after, true
	}
	// value is the rune's value, as text (lossy); flag names the rune
	// in an error by its long name.
	value := func(flag, wants string) (string, error) {
		if hasInline {
			return inline, nil
		}
		if v, ok := rest.next(); ok {
			return lossy(v), nil
		}
		return "", emptyValue(flag, wants)
	}
	if err := r.apply(name, inline, hasInline, value); err != nil {
		if err == errNotARune {
			return false, nil
		}
		return true, err
	}
	if r.first == "" {
		r.first = name
	}
	return true, nil
}

// errNotARune: apply knows no such rune.
var errNotARune = runeError("")

// valueOf reads a rune's value: its long name and what it wants.
type valueOf func(flag, wants string) (string, error)

func (r *dmenuRunes) apply(name, inline string, hasInline bool, value valueOf) error {
	d := &r.d
	var err error
	text := func(flag, wants string, into *string) {
		*into, err = value(flag, wants)
	}
	switch name {
	case "--counter", "--only-match", "--no-run-if-empty", "--index":
		if hasInline { // a switch bears no value, as with every other rune
			return unknownRune(name + "=" + inline)
		}
	}
	switch name {
	case "--prompt", "-p":
		text("--prompt", "the prompt's text", &d.view.Prompt)
	case "--prompt-only":
		text("--prompt-only", "the prompt's text", &d.view.Prompt)
		zero := uint32(0)
		d.view.Lines, d.readStdin = &zero, false
	case "--placeholder":
		text("--placeholder", "the placeholder's text", &d.view.Placeholder)
	case "--password":
		d.view.Password, err = password(inline, hasInline)
	case "--lines", "-l":
		d.view.Lines, err = numberRune("--lines", linesWant, value, false)
	case "--width", "-w":
		d.view.Width, err = numberRune("--width", widthWant, value, true)
	case "--counter":
		d.view.Counter = true
	case "--search":
		text("--search", "the opening query", &d.view.Search)
	case "--select":
		p := &list.Preselect{}
		text("--select", "an item's text", &p.Text)
		d.view.Select, r.selectText = p, true
	case "--select-index":
		var v string
		if v, err = value("--select-index", indexWant); err == nil {
			i, ok := parseDecimalUint(v, 64)
			if !ok {
				return badValue("--select-index", v, indexWant)
			}
			d.view.Select, r.selectByIndex = &list.Preselect{Index: i, ByIndex: true}, true
		}
	case "--only-match":
		d.view.OnlyMatch = true
	case "--no-run-if-empty", "-R":
		d.noRunIfEmpty = true
	case "--index":
		d.index = true
	case "--with-nth":
		d.columns.With, err = nthRune("--with-nth", value)
	case "--accept-nth":
		d.columns.Accept, err = nthRune("--accept-nth", value)
	case "--match-nth":
		d.columns.Match, err = nthRune("--match-nth", value)
	case "--nth-delimiter":
		var v string
		if v, err = value("--nth-delimiter", delimiterWant); err == nil {
			delimiter, ok := nth.Delimiter(v)
			if !ok {
				return badValue("--nth-delimiter", v, delimiterWant)
			}
			d.columns.Delimiter = delimiter
		}
	default:
		return errNotARune
	}
	return err
}

// finish is the runes of a --dmenu (NUL-separated with nul) session.
func (r *dmenuRunes) finish(nul bool) (dmenuOptions, error) {
	if r.selectText && r.selectByIndex {
		return r.d, runeError("the runes --select and --select-index cannot be borne together")
	}
	if nul {
		r.d.separator = 0
	}
	return r.d, nil
}

// noneGiven refuses dmenu runes outside a dmenu session.
func (r *dmenuRunes) noneGiven() error {
	if r.first == "" {
		return nil
	}
	return runeError("the rune " + r.first + " serves only --dmenu; this cogitator has no other list yet")
}

func numberRune(flag, wants string, value valueOf, positive bool) (*uint32, error) {
	v, err := value(flag, wants)
	if err != nil {
		return nil, err
	}
	n, ok := parseDecimalUint(v, 32)
	if !ok {
		return nil, badValue(flag, v, wants)
	}
	if positive && n == 0 {
		return nil, badValue(flag, "0", wants) // the number read, not its spelling
	}
	u := uint32(n)
	return &u, nil
}

// parseDecimalUint parses an unsigned decimal that fits in bits: digits
// only, one leading '+' allowed; a value too large for bits is refused.
func parseDecimalUint(s string, bits int) (uint64, bool) {
	digits := strings.TrimPrefix(s, "+")
	if digits == "" || strings.TrimLeft(digits, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseUint(digits, 10, bits)
	return n, err == nil
}

func nthRune(flag string, value valueOf) (*nth.Nth, error) {
	v, err := value(flag, nthWant)
	if err != nil {
		return nil, err
	}
	n, err := nth.Parse(v)
	if err != nil {
		return nil, runeError("the rune " + flag + " is refused: " + err.Error())
	}
	return n, nil
}

// password is --password's mask: '*', or the one character given.
func password(text string, given bool) (rune, error) {
	if !given {
		return '*', nil
	}
	if c, size := utf8.DecodeRuneInString(text); size > 0 && size == len(text) {
		return c, nil
	}
	return 0, badValue("--password", text, passwordWant)
}
