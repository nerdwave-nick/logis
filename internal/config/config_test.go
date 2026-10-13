package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/jsonc"
)

// Parsing and validating config texts (ported from Rust src/config/tests.rs).

func parse(t *testing.T, text string) Config {
	t.Helper()
	c, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return c
}

func refused(t *testing.T, text string) *jsonc.Invalid {
	t.Helper()
	_, err := Parse([]byte(text))
	var invalid *jsonc.Invalid
	if !errors.As(err, &invalid) {
		t.Fatalf("%q: want *jsonc.Invalid, got %v", text, err)
	}
	return invalid
}

func TestAnEmptyOrCommentOnlyConfigMeansTheDefaults(t *testing.T) {
	for _, text := range []string{"", "  \n\t", "// nothing yet\n/* at all */", "{}", "{ } // ok"} {
		if c := parse(t, text); !c.Equal(Default()) {
			t.Errorf("%q: %+v", text, c)
		}
	}
}

func TestTheDefaultsAreTheDocumentedOnes(t *testing.T) {
	c := Default()
	if c.Theme != "forge-crimson-brass" || c.IdleTimeout != 0 || c.Terminal != nil ||
		c.IconTheme != "hicolor" || c.FocusLoss != FocusDismiss || !c.Blur || c.ReduceMotion {
		t.Errorf("%+v", c)
	}
}

func TestEverySettingIsReadFromJSONC(t *testing.T) {
	c := parse(t, `{
        // the editor's guide; logis passes over it
        "$schema": "https://example.org/logis.schema.json",
        "theme": "cogitator-phosphor",
        "idle-timeout": 1.5, /* seconds */
        "terminal": ["foot", "-e",],
        "icon-theme": "Papirus-Dark",
        "focus-loss": "keep",
        "blur": false,
    }`)
	want := Config{
		Theme:       "cogitator-phosphor",
		IdleTimeout: 1500 * time.Millisecond,
		Terminal:    []string{"foot", "-e"},
		IconTheme:   "Papirus-Dark",
		FocusLoss:   FocusKeep,
	}
	if !c.Equal(want) {
		t.Errorf("%+v, want %+v", c, want)
	}
}

func TestBlurIsAFlagAndAnythingElseNamesIt(t *testing.T) {
	if parse(t, `{"blur": false}`).Blur || !parse(t, `{"blur": true}`).Blur {
		t.Error("blur not read")
	}
	for _, wrong := range []string{`"yes"`, "1", "null"} {
		if e := refused(t, `{"blur": `+wrong+`}`); !strings.Contains(e.Message, "`blur`") {
			t.Errorf("%s: %s", wrong, e.Message)
		}
	}
}

func TestAbsentKeysKeepTheirDefaults(t *testing.T) {
	c := parse(t, `{"focus-loss": "dismiss", "idle-timeout": 600}`)
	want := Default()
	want.IdleTimeout = 600 * time.Second
	if !c.Equal(want) {
		t.Errorf("%+v", c)
	}
}

func TestAnIdleTimeoutOfZeroKeepsTheDaemonResident(t *testing.T) {
	for _, text := range []string{`{"idle-timeout": 0}`, `{"idle-timeout": 0.0}`, `{"idle-timeout": -0}`} {
		if c := parse(t, text); c.IdleTimeout != 0 {
			t.Errorf("%s: %v", text, c.IdleTimeout)
		}
	}
}

func TestAHugeIdleTimeoutIsTheLongestThereIs(t *testing.T) {
	// Rust kept any u64 of seconds; a Duration ends at 292 years.
	for _, text := range []string{`{"idle-timeout": 18446744073709551615}`, `{"idle-timeout": 1e18}`} {
		if c := parse(t, text); c.IdleTimeout < 200*365*24*time.Hour {
			t.Errorf("%s: %v", text, c.IdleTimeout)
		}
	}
}

func TestAnUnknownKeyIsNamedWithItsLineAndColumn(t *testing.T) {
	// The position is the key's last character: its closing quote.
	e := refused(t, "{\n  \"theme\": \"x\",\n  \"thme\": \"y\"\n}")
	if e.Line != 3 || e.Column != 8 {
		t.Errorf("%v", e)
	}
	if !strings.Contains(e.Message, "unknown field `thme`") || !strings.Contains(e.Message, "`theme`") {
		t.Errorf("names the key and lists the known ones: %v", e)
	}
	want := "unknown field `thme`, expected one of `$schema`, `theme`, `idle-timeout`, " +
		"`terminal`, `icon-theme`, `focus-loss`, `blur`, `reduce-motion`"
	if e.Message != want {
		t.Errorf("%q", e.Message)
	}
}

func TestAValueOfTheWrongKindNamesItsKey(t *testing.T) {
	for _, tc := range []struct{ text, key string }{
		{`{"theme": 7}`, "`theme`"},
		{`{"idle-timeout": "soon"}`, "`idle-timeout`"},
		{`{"terminal": "foot -e"}`, "`terminal`"},
		{`{"terminal": ["foot", 1]}`, "`terminal`"},
		{`{"icon-theme": null}`, "`icon-theme`"},
		{`{"focus-loss": true}`, "`focus-loss`"},
		{`{"$schema": 1}`, "`$schema`"},
	} {
		e := refused(t, tc.text)
		if e.Line != 1 || !strings.Contains(e.Message, "invalid type") || !strings.Contains(e.Message, tc.key) {
			t.Errorf("%s: %v", tc.text, e)
		}
	}
}

func TestTheMessagesAreSerdesWords(t *testing.T) {
	for _, tc := range []struct{ text, message string }{
		{`{"theme": 7}`, "invalid type: integer `7`, expected `theme` as a theme's name " +
			"(its file name in themes/ without .jsonc: not empty, no '/', not starting with '.')"},
		{`{"focus-loss": "stay"}`, `invalid value: string "stay", expected ` + "`focus-loss`" +
			` as "dismiss" or "keep"`},
		{`{"terminal": []}`, "invalid length 0, expected `terminal` as a command line array " +
			`such as ["foot", "-e"], its program not empty`},
		{`{"idle-timeout": -1}`, "invalid value: integer `-1`, expected `idle-timeout` as " +
			"seconds, 0 or more (0 keeps the daemon resident)"},
		{`{"blur": "yes"}`, "invalid type: string \"yes\", expected `blur` as true or false"},
		{`{"terminal": ["foot", 1]}`, "invalid type: integer `1`, expected `terminal` as an array of strings"},
	} {
		if e := refused(t, tc.text); e.Message != tc.message {
			t.Errorf("%s:\n got %q\nwant %q", tc.text, e.Message, tc.message)
		}
	}
}

func TestTheColumnOfAWrongValuePointsAtItsEnd(t *testing.T) {
	if e := refused(t, "{\n  \"idle-timeout\": \"soon\"\n}"); e.Line != 2 || e.Column != 24 {
		t.Errorf("%v", e)
	}
	if e := refused(t, `{"terminal": ["foot", 1]}`); e.Column != 23 {
		t.Errorf("the element, not the array: %v", e)
	}
}

func TestInvalidValuesAreRefusedWithWhatIsWanted(t *testing.T) {
	for _, tc := range []struct{ text, wanted string }{
		{`{"idle-timeout": -1}`, "`idle-timeout`"},
		{`{"idle-timeout": -0.5}`, "`idle-timeout`"},
		{`{"idle-timeout": 1e300}`, "`idle-timeout`"},
		{`{"theme": ""}`, "`theme`"},
		{`{"theme": "a/b"}`, "`theme`"},
		{`{"theme": ".hidden"}`, "`theme`"},
		{`{"terminal": []}`, "`terminal`"},
		{`{"terminal": [""]}`, "`terminal`"},
		{`{"icon-theme": ""}`, "`icon-theme`"},
		{`{"focus-loss": "stay"}`, `"dismiss" or "keep"`},
	} {
		e := refused(t, tc.text)
		if e.Line != 1 || !strings.Contains(e.Message, tc.wanted) {
			t.Errorf("%s: %v", tc.text, e)
		}
	}
}

func TestAThemeNameMayContainDotsAndSpacesInside(t *testing.T) {
	if c := parse(t, `{"theme": "my forge.v2"}`); c.Theme != "my forge.v2" {
		t.Errorf("%q", c.Theme)
	}
}

func TestATerminalMayHaveEmptyArgumentsAfterItsProgram(t *testing.T) {
	c := parse(t, `{"terminal": ["kitty", "--title", ""]}`)
	if !slices.Equal(c.Terminal, []string{"kitty", "--title", ""}) {
		t.Errorf("%q", c.Terminal)
	}
}

func TestARepeatedKeyIsRefused(t *testing.T) {
	e := refused(t, `{"theme": "a", "theme": "b"}`)
	if !strings.Contains(e.Message, "duplicate field `theme`") || e.Column != 22 {
		t.Errorf("%v", e)
	}
}

func TestTheConfigMustBeAnObject(t *testing.T) {
	for _, text := range []string{"[]", `"theme"`, "null", "  // c\n 7"} {
		if e := refused(t, text); !strings.Contains(e.Message, "object") {
			t.Errorf("%s: %v", text, e)
		}
	}
	if e := refused(t, "  // c\n 7"); e.Line != 2 || e.Column != 2 {
		t.Errorf("%v", e)
	}
}

func TestBrokenJSONIsLocated(t *testing.T) {
	e := refused(t, "{\n  \"theme\": \"x\"\n  \"icon-theme\": \"y\"\n}")
	if e.Line != 3 || !strings.Contains(e.Message, "expected `,` or `}`") {
		t.Errorf("%v", e)
	}
}

func TestAnUnterminatedCommentIsLocated(t *testing.T) {
	e := refused(t, "{\n /* the rest")
	if e.Line != 2 || e.Column != 2 || !strings.Contains(e.Message, "comment") {
		t.Errorf("%v", e)
	}
}

func TestColumnsCountCharactersNotBytes(t *testing.T) {
	// "⚙" is three bytes; the unknown key "x" ends at the 18th character.
	if e := refused(t, `{"theme": "⚙", "x": 1}`); e.Line != 1 || e.Column != 18 {
		t.Errorf("%v", e)
	}
}

func TestTheMessageCarriesNoSecondPosition(t *testing.T) {
	e := refused(t, `{"thme": 1}`)
	if strings.Contains(e.Message, "line") || e.Error() != fmt.Sprintf("1:%d: %s", e.Column, e.Message) {
		t.Errorf("%q", e.Error())
	}
}

func TestReduceMotionIsAFlagAndOffByDefault(t *testing.T) {
	if Default().ReduceMotion {
		t.Error("continuous effects are on by default")
	}
	if !parse(t, `{"reduce-motion": true}`).ReduceMotion || parse(t, `{"reduce-motion": false}`).ReduceMotion {
		t.Error("reduce-motion not read")
	}
	if e := refused(t, `{"reduce-motion": "yes"}`); !strings.Contains(e.Message, "`reduce-motion`") {
		t.Errorf("%v", e)
	}
}

func TestEqualComparesEverySetting(t *testing.T) {
	base := Default()
	changes := []func(*Config){
		func(c *Config) { c.Theme = "x" },
		func(c *Config) { c.IdleTimeout = time.Second },
		func(c *Config) { c.Terminal = []string{"foot"} },
		func(c *Config) { c.IconTheme = "x" },
		func(c *Config) { c.FocusLoss = FocusKeep },
		func(c *Config) { c.Blur = false },
		func(c *Config) { c.ReduceMotion = true },
	}
	for i, change := range changes {
		c := Default()
		change(&c)
		if c.Equal(base) || base.Equal(c) {
			t.Errorf("change %d went unnoticed", i)
		}
	}
	empty := Default()
	empty.Terminal = []string{}
	if empty.Equal(base) {
		t.Error("an empty terminal is not an absent one")
	}
}
