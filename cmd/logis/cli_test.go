package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/list"
)

// The dmenu runes and the client role's command line.

// parseRunes parses dmenu runes only.
func parseRunes(args ...string) (dmenuOptions, error) {
	r := newDmenuRunes()
	rest := &argQueue{args: args}
	for {
		arg, ok := rest.next()
		if !ok {
			break
		}
		took, err := r.take(arg, rest)
		if err != nil {
			return dmenuOptions{}, err
		}
		if !took {
			panic("not a dmenu rune: " + arg)
		}
	}
	return r.finish(false)
}

func mustRunes(t *testing.T, args ...string) dmenuOptions {
	t.Helper()
	d, err := parseRunes(args...)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return d
}

func TestValuesFollowTheirFlagOrAnEqualsSign(t *testing.T) {
	d := mustRunes(t, "--prompt", "rite? ", "--lines=4", "-w", "50", "-p", "again> ")
	if d.view.Prompt != "again> " || *d.view.Lines != 4 || *d.view.Width != 50 {
		t.Fatalf("%+v", d.view)
	}
	d = mustRunes(t, "--placeholder=seek", "--search", "ab")
	if d.view.Placeholder != "seek" || d.view.Search != "ab" {
		t.Fatalf("%+v", d.view)
	}
}

func TestTheDmenuDefaultsAreFuzzels(t *testing.T) {
	d := mustRunes(t)
	if !reflect.DeepEqual(d, defaultDmenu()) {
		t.Fatalf("%+v", d)
	}
	if d.view.Prompt != "> " || d.view.Lines != nil || d.view.Width != nil {
		t.Fatalf("%+v", d.view)
	}
	if d.separator != '\n' || d.columns.Delimiter != '\t' || !d.readStdin {
		t.Fatalf("%+v", d)
	}
}

func TestPromptOnlyReadsNothingAndShowsNoRows(t *testing.T) {
	d := mustRunes(t, "--prompt-only", "pin: ")
	if d.view.Prompt != "pin: " || d.view.Lines == nil || *d.view.Lines != 0 || d.readStdin {
		t.Fatalf("%+v", d)
	}
}

func TestPasswordTakesAnOptionalCharacter(t *testing.T) {
	if d := mustRunes(t, "--password"); d.view.Password != '*' {
		t.Fatalf("%q", d.view.Password)
	}
	if d := mustRunes(t, "--password=•"); d.view.Password != '•' {
		t.Fatalf("%q", d.view.Password)
	}
	if _, err := parseRunes("--password=ab"); err == nil {
		t.Fatal("two characters accepted")
	}
}

func TestSwitchesNeedNoValue(t *testing.T) {
	d := mustRunes(t, "--counter", "--only-match", "--index", "-R")
	if !d.view.Counter || !d.view.OnlyMatch || !d.index || !d.noRunIfEmpty {
		t.Fatalf("%+v", d)
	}
}

func TestColumnsAreSelectedWithNthRunes(t *testing.T) {
	d := mustRunes(t, "--with-nth=2", "--accept-nth", "{1}", "--match-nth=3", "--nth-delimiter=,")
	line := []byte("a,b,c")
	for _, c := range []struct{ got, want string }{
		{string(d.columns.With.Apply(line, d.columns.Delimiter)), "b"},
		{string(d.columns.Accept.Apply(line, d.columns.Delimiter)), "a"},
		{string(d.columns.Match.Apply(line, d.columns.Delimiter)), "c"},
	} {
		if c.got != c.want {
			t.Fatalf("%q, want %q", c.got, c.want)
		}
	}
	if d := mustRunes(t, "--with-nth=0"); d.columns.With != nil {
		t.Fatal("--with-nth=0 selects columns")
	}
}

func TestPreselectionIsByTextOrByIndexNotBoth(t *testing.T) {
	if d := mustRunes(t, "--select", "Second"); !reflect.DeepEqual(d.view.Select, &list.Preselect{Text: "Second"}) {
		t.Fatalf("%+v", d.view.Select)
	}
	if d := mustRunes(t, "--select-index=2"); !reflect.DeepEqual(d.view.Select, &list.Preselect{Index: 2, ByIndex: true}) {
		t.Fatalf("%+v", d.view.Select)
	}
	_, err := parseRunes("--select=a", "--select-index=1")
	if err == nil || err.Error() != "the runes --select and --select-index cannot be borne together" {
		t.Fatalf("%v", err)
	}
}

func TestDmenu0SeparatesLinesWithNul(t *testing.T) {
	d, err := newDmenuRunes().finish(true)
	if err != nil || d.separator != 0 {
		t.Fatalf("%v %v", d.separator, err)
	}
}

// A value a rune cannot bear, or none where it wants one, is an error
// naming the rune (logis(1) RUNES): exit 2, empty stdout, and on stderr
// the rune, the value as text (invalid bytes replaced) and what it wants.
func TestBadValuesAreNamedWithTheirFlag(t *testing.T) {
	noSession(t)
	const (
		prompt   = "the prompt's text"
		nthWants = "a column number or a format with {N}, {N..M} or {N..}"
		noColumn = "columns are counted from 1, and a range runs forwards"
		seconds  = "a number of seconds (0 keeps the daemon resident)"
		config   = "one path to a config file"
		builtIn  = "the name of a built-in provider: servitor"
	)
	cannotBear := func(rune, value, wants string) string {
		return "the rune " + rune + " cannot bear '" + value + "': it wants " + wants
	}
	standsEmpty := func(rune, wants string) string { return "the rune " + rune + " stands empty: it wants " + wants }
	namesNoColumn := func(rune, value string) string {
		return "the rune " + rune + " is refused: '" + value + "' names no column: it wants " + nthWants
	}
	isNoColumn := func(rune, value string) string {
		return "the rune " + rune + " is refused: '" + value + "' is no column: " + noColumn
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		// Every dmenu rune that wants a value, without one; a short
		// rune is named by its long name.
		{[]string{"--prompt"}, standsEmpty("--prompt", prompt)},
		{[]string{"-p"}, standsEmpty("--prompt", prompt)},
		{[]string{"--prompt-only"}, standsEmpty("--prompt-only", prompt)},
		{[]string{"--placeholder"}, standsEmpty("--placeholder", "the placeholder's text")},
		{[]string{"--search"}, standsEmpty("--search", "the opening query")},
		{[]string{"--select"}, standsEmpty("--select", "an item's text")},
		{[]string{"--select-index"}, standsEmpty("--select-index", indexWant)},
		{[]string{"--lines"}, standsEmpty("--lines", linesWant)},
		{[]string{"-l"}, standsEmpty("--lines", linesWant)},
		{[]string{"--width"}, standsEmpty("--width", widthWant)},
		{[]string{"-w"}, standsEmpty("--width", widthWant)},
		{[]string{"--with-nth"}, standsEmpty("--with-nth", nthWants)},
		{[]string{"--accept-nth"}, standsEmpty("--accept-nth", nthWants)},
		{[]string{"--match-nth"}, standsEmpty("--match-nth", nthWants)},
		{[]string{"--nth-delimiter"}, standsEmpty("--nth-delimiter", delimiterWant)},
		// --lines: rows, 0 or more, an unsigned decimal.
		{[]string{"--lines", "many"}, cannotBear("--lines", "many", linesWant)},
		{[]string{"--lines=x"}, cannotBear("--lines", "x", linesWant)},
		{[]string{"--lines=-1"}, cannotBear("--lines", "-1", linesWant)},
		{[]string{"--lines", ""}, cannotBear("--lines", "", linesWant)},
		{[]string{"--lines", "1.5"}, cannotBear("--lines", "1.5", linesWant)},
		{[]string{"--lines", " 3"}, cannotBear("--lines", " 3", linesWant)},
		{[]string{"--lines", "0x3"}, cannotBear("--lines", "0x3", linesWant)},
		{[]string{"--lines", "4294967296"}, cannotBear("--lines", "4294967296", linesWant)},
		{[]string{"--lines", "\xff"}, cannotBear("--lines", "\uFFFD", linesWant)},
		{[]string{"-l", "\xff"}, cannotBear("--lines", "\uFFFD", linesWant)},
		// --width: characters, 1 or more; a zero is named as the number read.
		{[]string{"--width=0"}, cannotBear("--width", "0", widthWant)},
		{[]string{"--width", "+0"}, cannotBear("--width", "0", widthWant)},
		{[]string{"-w", "000"}, cannotBear("--width", "0", widthWant)},
		{[]string{"--width", "x"}, cannotBear("--width", "x", widthWant)},
		{[]string{"--width", "-3"}, cannotBear("--width", "-3", widthWant)},
		{[]string{"--width", "4294967296"}, cannotBear("--width", "4294967296", widthWant)},
		// --select-index: an index from 0, and not beside --select.
		{[]string{"--select-index=x"}, cannotBear("--select-index", "x", indexWant)},
		{[]string{"--select-index", "-1"}, cannotBear("--select-index", "-1", indexWant)},
		{[]string{"--select-index", "1.0"}, cannotBear("--select-index", "1.0", indexWant)},
		{[]string{"--select", "a", "--select-index", "1"}, "the runes --select and --select-index cannot be borne together"},
		{[]string{"--select-index", "1", "--select", "a"}, "the runes --select and --select-index cannot be borne together"},
		// --password=C: exactly one character.
		{[]string{"--password=ab"}, cannotBear("--password", "ab", passwordWant)},
		{[]string{"--password=éé"}, cannotBear("--password", "éé", passwordWant)},
		{[]string{"--password="}, cannotBear("--password", "", passwordWant)},
		// --nth-delimiter: one ASCII character.
		{[]string{"--nth-delimiter=ab"}, cannotBear("--nth-delimiter", "ab", delimiterWant)},
		{[]string{"--nth-delimiter=é"}, cannotBear("--nth-delimiter", "é", delimiterWant)},
		{[]string{"--nth-delimiter", ""}, cannotBear("--nth-delimiter", "", delimiterWant)},
		{[]string{"--nth-delimiter="}, cannotBear("--nth-delimiter", "", delimiterWant)},
		{[]string{"--nth-delimiter", "\xff"}, cannotBear("--nth-delimiter", "\uFFFD", delimiterWant)},
		// A column format names a column, counted from 1, ranges forwards.
		{[]string{"--with-nth={0}"}, isNoColumn("--with-nth", "{0}")},
		{[]string{"--with-nth", "{2..1}"}, isNoColumn("--with-nth", "{2..1}")},
		{[]string{"--accept-nth", "{0..}"}, isNoColumn("--accept-nth", "{0..}")},
		{[]string{"--accept-nth", "{1..0}"}, isNoColumn("--accept-nth", "{1..0}")},
		{[]string{"--with-nth", "x"}, namesNoColumn("--with-nth", "x")},
		{[]string{"--with-nth", ""}, namesNoColumn("--with-nth", "")},
		{[]string{"--with-nth", "-1"}, namesNoColumn("--with-nth", "-1")},
		{[]string{"--with-nth", "{1"}, namesNoColumn("--with-nth", "{1")},
		{[]string{"--with-nth", "0", "--accept-nth", "x"}, namesNoColumn("--accept-nth", "x")},
		{[]string{"--match-nth", "{}"}, namesNoColumn("--match-nth", "{}")},
		{[]string{"--match-nth", "free text"}, namesNoColumn("--match-nth", "free text")},
	} {
		args := append([]string{"--dmenu"}, c.args...)
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := runLogis(args...)
			if want := "logis: " + c.want + "\n"; code != 2 || stdout != "" || stderr != want {
				t.Errorf("exit %d, stdout %q, stderr %q; want 2 and %q", code, stdout, stderr, want)
			}
		})
	}
	// --config, the daemon's runes and the built-in providers alike.
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--config"}, standsEmpty("--config", config)},
		{[]string{"--dmenu", "--config"}, standsEmpty("--config", config)},
		{[]string{"--config="}, cannotBear("--config", "", config)},
		{[]string{"--config", ""}, cannotBear("--config", "", config)},
		{[]string{"--config", "a", "--config", "b"}, cannotBear("--config", "b", config)},
		{[]string{"--config", "a", "--config", "\xff"}, cannotBear("--config", "\uFFFD", config)},
		{[]string{"--provider"}, standsEmpty("--provider", "a program to run, then its arguments")},
		{[]string{"daemon", "--config"}, standsEmpty("--config", config)},
		{[]string{"daemon", "--config="}, cannotBear("--config", "", config)},
		{[]string{"daemon", "--config", ""}, cannotBear("--config", "", config)},
		{[]string{"daemon", "--config=a", "--config=b"}, cannotBear("--config", "b", config)},
		{[]string{"daemon", "--idle-timeout"}, standsEmpty("--idle-timeout", seconds)},
		{[]string{"daemon", "--idle-timeout="}, cannotBear("--idle-timeout", "", seconds)},
		{[]string{"daemon", "--idle-timeout", "--config", "x"}, cannotBear("--idle-timeout", "--config", seconds)},
		{[]string{"daemon", "--idle-timeout", "5", "--idle-timeout", "soon"}, cannotBear("--idle-timeout", "soon", seconds)},
		{[]string{"daemon", "--config", "x", "--idle-timeout", "soon"}, cannotBear("--idle-timeout", "soon", seconds)},
		{[]string{"provide"}, standsEmpty("provide", builtIn)},
		{[]string{"provide", ""}, cannotBear("provide", "", builtIn)},
		{[]string{"provide", "apps"}, cannotBear("provide", "apps", builtIn)},
		{[]string{"provide", "--servitor"}, cannotBear("provide", "--servitor", builtIn)},
		{[]string{"provide", "\xff"}, cannotBear("provide", "\uFFFD", builtIn)},
	} {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			code, stdout, stderr := runLogis(c.args...)
			if want := "logis: " + c.want + "\n"; code != 2 || stdout != "" || stderr != want {
				t.Errorf("exit %d, stdout %q, stderr %q; want 2 and %q", code, stdout, stderr, want)
			}
		})
	}
	// An idle timeout is a number of seconds, 0 or more, finite, below
	// 2^64 s.
	for _, bad := range []string{"soon", "-1", "NaN", "nan", "inf", "-inf", "infinity", "", " 5", "5 ",
		"1e400", "18446744073709551616", "0x10", "1_0", "１"} {
		t.Run("daemon --idle-timeout "+bad, func(t *testing.T) {
			code, stdout, stderr := runLogis("daemon", "--idle-timeout", bad)
			if want := "logis: " + cannotBear("--idle-timeout", bad, seconds) + "\n"; code != 2 || stdout != "" || stderr != want {
				t.Errorf("exit %d, stdout %q, stderr %q; want 2 and %q", code, stdout, stderr, want)
			}
		})
	}
}

// Every value the docs name for a rune is borne: unsigned decimals with an
// optional '+', a value spelled like a rune, an empty prompt, a column
// beyond any line, free text around a column, the last of a repeated
// rune, and idle timeouts in any decimal spelling of seconds.
func TestTheValuesARuneCanBearAreBorne(t *testing.T) {
	for _, c := range []struct {
		args  []string
		check func(dmenuOptions) bool
	}{
		{[]string{"--lines", "+3", "--width=+7"}, func(d dmenuOptions) bool { return *d.view.Lines == 3 && *d.view.Width == 7 }},
		{[]string{"--lines", "0"}, func(d dmenuOptions) bool { return *d.view.Lines == 0 }},
		{[]string{"--password=é"}, func(d dmenuOptions) bool { return d.view.Password == 'é' }},
		{[]string{"--prompt", "-p"}, func(d dmenuOptions) bool { return d.view.Prompt == "-p" }},
		{[]string{"--prompt", "--dmenu"}, func(d dmenuOptions) bool { return d.view.Prompt == "--dmenu" }},
		{[]string{"--prompt="}, func(d dmenuOptions) bool { return d.view.Prompt == "" }},
		{[]string{"--prompt-only", "pin: "}, func(d dmenuOptions) bool { return d.view.Prompt == "pin: " && !d.readStdin }},
		{[]string{"--select", "a", "--select", "b"}, func(d dmenuOptions) bool { return d.view.Select.Text == "b" }},
		{[]string{"--select-index", "1", "--select-index", "2"}, func(d dmenuOptions) bool { return d.view.Select.Index == 2 }},
		{[]string{"--accept-nth", "4294967296"}, func(d dmenuOptions) bool {
			return string(d.columns.Accept.Apply([]byte("a\tb"), '\t')) == ""
		}},
		{[]string{"--with-nth", "a{1}b{x}"}, func(d dmenuOptions) bool {
			return string(d.columns.With.Apply([]byte("A\tB"), '\t')) == "aAb{x}"
		}},
		{[]string{"--with-nth", "{2} {1..}", "--accept-nth", "2", "--match-nth", "0", "--nth-delimiter", ","},
			func(d dmenuOptions) bool {
				return string(d.columns.With.Apply([]byte("a,b"), ',')) == "b a,b" && d.columns.Match == nil && d.columns.Delimiter == ','
			}},
	} {
		args := append([]string{"--dmenu"}, c.args...)
		got, err := parse(args)
		if err != nil || got.kind != dmenuCommand || !c.check(got.dmenu) {
			t.Errorf("%q: %+v, %v", args, got.dmenu, err)
		}
	}
	for v, want := range map[string]time.Duration{
		"0": 0, "-0": 0, "+5": 5 * time.Second, ".5": 500 * time.Millisecond, "5.": 5 * time.Second,
		"1e3": 1000 * time.Second, "1E3": 1000 * time.Second, "0.25": 250 * time.Millisecond, "600": 600 * time.Second,
	} {
		if o, err := parseDaemon([]string{"--idle-timeout", v}); err != nil || o.IdleTimeout != want {
			t.Errorf("--idle-timeout %q: %v, %v; want %v", v, o.IdleTimeout, err, want)
		}
	}
	if o, err := parseDaemon([]string{"--idle-timeout", "0.0000000001"}); err != nil || o.IdleTimeout > time.Microsecond {
		t.Errorf("a tiny idle timeout: %v, %v", o.IdleTimeout, err)
	}
}

// A rune's value is taken as text, invalid UTF-8 replaced.
func TestValuesAreReadAsTextWithInvalidBytesReplaced(t *testing.T) {
	d := mustRunes(t, "--prompt", "rite\xff ", "--search", "\xe2\x82", "--select", "a\xff\xfeb")
	if d.view.Prompt != "rite\uFFFD " || d.view.Search != "\uFFFD" || d.view.Select.Text != "a\uFFFD\uFFFDb" {
		t.Fatalf("%q %q %q", d.view.Prompt, d.view.Search, d.view.Select.Text)
	}
}

func TestNumbersParseAsUnsignedDecimalsOnly(t *testing.T) {
	if d := mustRunes(t, "--lines=+3"); *d.view.Lines != 3 {
		t.Fatalf("%d", *d.view.Lines)
	}
	for _, bad := range []string{"+", " 3", "0x3", "3_0", "4294967296"} {
		if _, err := parseRunes("--lines=" + bad); err == nil {
			t.Fatalf("--lines=%q accepted", bad)
		}
	}
}

func TestOtherArgumentsAreNotTaken(t *testing.T) {
	r := newDmenuRunes()
	rest := &argQueue{}
	for _, arg := range []string{"--dmenu", "--config"} {
		if took, err := r.take(arg, rest); took || err != nil {
			t.Fatalf("%s taken (%v)", arg, err)
		}
	}
	if err := r.noneGiven(); err != nil {
		t.Fatal(err)
	}
	if took, err := r.take("--index", rest); !took || err != nil {
		t.Fatal("--index not taken")
	}
	if err := r.noneGiven(); err == nil || err.Error() != "the rune --index serves only --dmenu; this cogitator has no other list yet" {
		t.Fatalf("%v", err)
	}
}

func mustParse(t *testing.T, args ...string) command {
	t.Helper()
	c, err := parse(args)
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return c
}

func TestDmenuAsksForADmenuSession(t *testing.T) {
	for _, flag := range []string{"--dmenu", "-d"} {
		if c := mustParse(t, flag); c.kind != dmenuCommand || !reflect.DeepEqual(c.dmenu, defaultDmenu()) {
			t.Fatalf("%s: %+v", flag, c)
		}
	}
	nul := defaultDmenu()
	nul.separator = 0
	if c := mustParse(t, "--dmenu0"); c.kind != dmenuCommand || !reflect.DeepEqual(c.dmenu, nul) {
		t.Fatalf("%+v", c)
	}
}

func TestListRunesGoWithDmenuInAnyOrder(t *testing.T) {
	c := mustParse(t, "--index", "--dmenu", "-i", "--lines", "3")
	if c.kind != dmenuCommand || !c.dmenu.index || *c.dmenu.view.Lines != 3 {
		t.Fatalf("%+v", c)
	}
}

func TestListRunesWithoutDmenuAreRefused(t *testing.T) {
	if _, err := parse([]string{"--prompt", "x"}); err == nil ||
		err.Error() != "the rune --prompt serves only --dmenu; this cogitator has no other list yet" {
		t.Fatalf("%v", err)
	}
	if _, err := parse([]string{"--check-config", "--index"}); err == nil {
		t.Fatal("--index with --check-config accepted")
	}
}

func TestProviderTakesEveryArgumentAfterItAsTheProgramAndItsArguments(t *testing.T) {
	c := mustParse(t, "--config", "/c", "--provider", "rite", "--dmenu", "-x")
	if c.kind != providerCommand || c.config != "/c" || !reflect.DeepEqual(c.argv, []string{"rite", "--dmenu", "-x"}) {
		t.Fatalf("%+v", c)
	}
	if _, err := parse([]string{"--provider"}); err == nil ||
		err.Error() != "the rune --provider stands empty: it wants a program to run, then its arguments" {
		t.Fatalf("%v", err)
	}
	for _, args := range [][]string{{"--dmenu", "--provider", "x"}, {"--prompt", "p", "--provider", "x"}, {"--version", "--provider", "x"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("%q accepted", args)
		}
	}
}

func TestConfigNamesTheConfigFileBeforeOrAfterTheCommand(t *testing.T) {
	if c := mustParse(t, "--config", "/x/logis.jsonc", "--dmenu"); c.kind != dmenuCommand || c.config != "/x/logis.jsonc" {
		t.Fatalf("%+v", c)
	}
	if c := mustParse(t, "--check-config", "--config=/x/logis.jsonc"); c.kind != checkConfigCommand || c.config != "/x/logis.jsonc" {
		t.Fatalf("%+v", c)
	}
	if c := mustParse(t, "--config", "/x/logis.jsonc"); c.kind != appsCommand || c.config != "/x/logis.jsonc" {
		t.Fatalf("%+v", c)
	}
	// A rune's value is taken before --config is looked for.
	if c := mustParse(t, "--dmenu", "--prompt", "--config"); c.dmenu.view.Prompt != "--config" || c.config != "" {
		t.Fatalf("%+v", c)
	}
}

func TestConfigWantsOnePath(t *testing.T) {
	for _, args := range [][]string{{"--config"}, {"--config="}, {"--dmenu", "--config"}, {"--config", "a", "--config", "b"}} {
		if _, err := parse(args); err == nil || !strings.Contains(err.Error(), "--config") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestASecondCommandOrAForeignRuneIsUnknown(t *testing.T) {
	for _, c := range []struct {
		args    []string
		unknown string
	}{
		{[]string{"--dmenu", "--dmenu"}, "--dmenu"},
		{[]string{"--dmenu", "--dmenu0"}, "--dmenu0"},
		{[]string{"--dmenu", "--check-config"}, "--check-config"},
		{[]string{"--debug-panel"}, "--debug-panel"},
		{[]string{"--dmenu", "--debug-panel"}, "--debug-panel"},
		{[]string{"--h\xffresy"}, "--h\uFFFDresy"},
		{[]string{"--dmenu", "\xff\xfe"}, "\uFFFD\uFFFD"},
		{[]string{"--h\xe2\x82"}, "--h\uFFFD"},
		// Only runes that are valid UTF-8 are matched.
		{[]string{"--config=\xff"}, "--config=\uFFFD"},
		{[]string{"--dmenu", "--prompt=\xff"}, "--prompt=\uFFFD"},
		{[]string{"daemon", "--config=\xff"}, "--config=\uFFFD"},
		{[]string{"provide", "servitor", "\xff"}, "\uFFFD"},
	} {
		var err error
		switch c.args[0] {
		case "daemon":
			_, err = parseDaemon(c.args[1:])
		case "provide":
			err = parseProvide(c.args[1:])
		default:
			_, err = parse(c.args)
		}
		want := "the rune '" + c.unknown + "' is unknown to this cogitator. It knows only " + knownRunes + "."
		if err == nil || err.Error() != want {
			t.Fatalf("%v: %v", c.args, err)
		}
	}
}

func TestNoArgumentsOpenTheAppLauncher(t *testing.T) {
	if c := mustParse(t); c.kind != appsCommand {
		t.Fatalf("%+v", c)
	}
	if c := mustParse(t, "-V"); c.kind != versionCommand {
		t.Fatalf("%+v", c)
	}
}

func TestAppsAsksForTheAppLauncher(t *testing.T) {
	if c := mustParse(t, "--apps"); c.kind != appsCommand || c.config != "" {
		t.Fatalf("%+v", c)
	}
	if c := mustParse(t, "--apps", "--config", "/c"); c.kind != appsCommand || c.config != "/c" {
		t.Fatalf("%+v", c)
	}
	for _, args := range [][]string{{"--apps", "--dmenu"}, {"--apps", "--prompt", "x"}, {"--apps", "--apps"}} {
		if _, err := parse(args); err == nil {
			t.Errorf("%v parsed", args)
		}
	}
	if !strings.Contains(knownRunes, "--apps, --provider") {
		t.Errorf("knownRunes %q", knownRunes)
	}
}

// Each maximal ill-formed subsequence (Unicode's "maximal subpart")
// becomes one U+FFFD.
func TestInvalidUTF8IsReplacedPerMaximalSubpart(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"plain é ✓ 🜏", "plain é ✓ 🜏"},
		{"Hello \xf0\x90\x80World", "Hello \uFFFDWorld"},
		{"\xff\xfe", "\uFFFD\uFFFD"},
		{"\xe2\x82", "\uFFFD"},
		{"\xe2\x82a", "\uFFFDa"},
		{"\xed\xa0\x80", "\uFFFD\uFFFD\uFFFD"},
		{"\xf4\x90\x80\x80", "\uFFFD\uFFFD\uFFFD\uFFFD"},
		{"\xc0\x80", "\uFFFD\uFFFD"},
		{"\xe0\x80\x80", "\uFFFD\uFFFD\uFFFD"},
		{"\xe0\xa0", "\uFFFD"},
		{"\xf0\x90\x80\x80", "\U00010000"},
		{"\x80", "\uFFFD"},
	} {
		if got := lossy(c.in); got != c.want {
			t.Errorf("lossy(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A refused value is named as text, invalid bytes replaced.
func TestRefusedValuesAreNamedAsText(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{func() error { _, err := parse([]string{"--config", "a", "--config", "\xff"}); return err }(),
			"the rune --config cannot bear '\uFFFD': it wants one path to a config file"},
		{parseProvide([]string{"\xff"}), "the rune provide cannot bear '\uFFFD': it wants the name of a built-in provider: servitor"},
	} {
		if c.err == nil || c.err.Error() != c.want {
			t.Errorf("%v, want %q", c.err, c.want)
		}
	}
}
