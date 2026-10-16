package apps

import (
	"errors"
	"slices"
	"testing"
)

// Splitting the Exec key into arguments and its field codes.

func execArgs(t *testing.T, value string) []string {
	t.Helper()
	e, err := ParseExec(value)
	if err != nil {
		t.Fatalf("%q: %v", value, err)
	}
	return e.args
}

func wantArgs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
}

func TestArgumentsSplitAtSpaces(t *testing.T) {
	wantArgs(t, execArgs(t, "foot  --server   -e"), "foot", "--server", "-e")
}

func TestDoubleQuotesKeepSpacesAndTakeFourEscapes(t *testing.T) {
	wantArgs(t, execArgs(t, `sh -c "echo \"a b\" \$HOME \`+"`"+`x\`+"`"+` \\ \n"`),
		"sh", "-c", `echo "a b" $HOME `+"`x`"+` \ \n`)
	wantArgs(t, execArgs(t, `run ""`), "run", "")
	wantArgs(t, execArgs(t, `a"b c"d`), "ab cd")
}

func TestSingleQuotesAndBareBackslashesFollowTheShell(t *testing.T) {
	wantArgs(t, execArgs(t, `sh -c 'a \b'`), "sh", "-c", `a \b`)
	wantArgs(t, execArgs(t, `a\ b c\\d`), "a b", `c\d`)
}

func execErr(value string) ExecError {
	_, err := ParseExec(value)
	var e *ExecError
	if errors.As(err, &e) {
		return *e
	}
	return ExecError{Kind: -1}
}

func TestUnclosedQuotesAndNoProgramAreRefused(t *testing.T) {
	for value, want := range map[string]ExecErrorKind{
		`sh -c "x`: ExecUnterminated,
		`sh 'x`:    ExecUnterminated,
		"   ":      ExecEmpty,
		`""`:       ExecEmpty,
	} {
		if got := execErr(value); got.Kind != want {
			t.Errorf("%q: %+v, want kind %d", value, got, want)
		}
	}
}

func TestUnknownFieldCodesAreRefused(t *testing.T) {
	for value, want := range map[string]ExecError{
		"app %x":      {Kind: ExecUnknownCode, Code: 'x'},
		"app --a=%Z":  {Kind: ExecUnknownCode, Code: 'Z'},
		"app 100%":    {Kind: ExecLonePercent},
		"app %x %%":   {Kind: ExecUnknownCode, Code: 'x'},
		"app --one=%": {Kind: ExecLonePercent},
	} {
		if got := execErr(value); got != want {
			t.Errorf("%q: %+v, want %+v", value, got, want)
		}
	}
	if got := (&ExecError{Kind: ExecUnknownCode, Code: 'q'}).Error(); got != "Exec has an unknown field code %q" {
		t.Errorf("%q", got)
	}
}

var execFields = Fields{Icon: "forge-cog", Name: "Rite of Oil", Location: "/apps/oil.desktop"}

func expanded(t *testing.T, value string, f Fields) []string {
	t.Helper()
	e, err := ParseExec(value)
	if err != nil {
		t.Fatalf("%q: %v", value, err)
	}
	return e.Expand(f)
}

func TestFileAndURLCodesVanishWithoutFiles(t *testing.T) {
	wantArgs(t, expanded(t, "app %f %F %u %U", execFields), "app")
	wantArgs(t, expanded(t, "app --open=%u x", execFields), "app", "--open=", "x")
	wantArgs(t, expanded(t, "app -- %U", execFields), "app", "--")
}

func TestIconNameAndLocationExpand(t *testing.T) {
	wantArgs(t, expanded(t, "app %i --title=%c %k", execFields),
		"app", "--icon", "forge-cog", "--title=Rite of Oil", "/apps/oil.desktop")
	iconless := execFields
	iconless.Icon = ""
	wantArgs(t, expanded(t, "app %i x", iconless), "app", "x")
	// A name with spaces stays one argument.
	wantArgs(t, expanded(t, `sh -c "echo %c"`, execFields), "sh", "-c", "echo Rite of Oil")
}

func TestADoublePercentIsAPercentAndDeprecatedCodesVanish(t *testing.T) {
	wantArgs(t, expanded(t, "app 50%% %d %D %n %N %v %m x%dy", execFields), "app", "50%", "xy")
}

func TestTheProgramIsTheFirstArgument(t *testing.T) {
	e, err := ParseExec(`"/opt/the forge/bin" %U`)
	if err != nil || e.Program() != "/opt/the forge/bin" {
		t.Errorf("%q, %v", e.Program(), err)
	}
}
