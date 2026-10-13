package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/list"
)

// Ported from Rust src/cli/dmenu.rs and the dmenu cases of src/cli/mod.rs.

// parseRunes parses dmenu runes only, as Rust's tests' parse does.
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

func TestBadValuesAreNamedWithTheirFlag(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--lines", "many"}, "the rune --lines cannot bear 'many': it wants a number of rows (0 or more)"},
		{[]string{"--lines=-1"}, "the rune --lines cannot bear '-1': it wants a number of rows (0 or more)"},
		{[]string{"--width=0"}, "the rune --width cannot bear '0': it wants a width in characters (1 or more)"},
		{[]string{"--select-index=x"}, "the rune --select-index cannot bear 'x': it wants an item's index (0 or more)"},
		{[]string{"--with-nth={0}"}, "the rune --with-nth is refused: "},
		{[]string{"--nth-delimiter=ab"}, "the rune --nth-delimiter cannot bear 'ab': it wants one ASCII character"},
		{[]string{"--nth-delimiter=é"}, "the rune --nth-delimiter cannot bear 'é': it wants one ASCII character"},
		{[]string{"--prompt"}, "the rune --prompt stands empty: it wants the prompt's text"},
		{[]string{"--password=ab"}, "the rune --password cannot bear 'ab': it wants one character to show in place of each typed one"},
	} {
		_, err := parseRunes(c.args...)
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Fatalf("%v: %v, want %q", c.args, err, c.want)
		}
	}
}

func TestNumbersParseAsRustDoes(t *testing.T) {
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

// Ported from Rust provider_takes_every_argument_after_it_as_the_program_and_its_arguments.
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
	// As in Rust, a rune's value is taken before --config is looked for.
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
	} {
		_, err := parse(c.args)
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

// Ported from Rust src/cli/mod.rs apps_asks_for_the_app_launcher.
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
