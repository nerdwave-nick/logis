package apps

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// What launching an application or one of its actions runs.

func systemIndex(t *testing.T) *Index {
	t.Helper()
	return Scan(Dirs{filepath.Join(fixtures(t), "system/applications")}, testContext(t))
}

func app(t *testing.T, ix *Index, id string) *App {
	t.Helper()
	a, ok := ix.Get(id)
	if !ok {
		t.Fatalf("no %s", id)
	}
	return a
}

func TestAnAppRunsItsExecWithTheFieldCodesExpanded(t *testing.T) {
	p, err := PlanFor(app(t, systemIndex(t), "forge-anoint.desktop"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs(t, p.Argv, "anoint", "--vessel", "--name=Anoint", "--icon", "forge-cog")
	if p.AppID != "forge-anoint" || p.Name != "Anoint" || p.DBus || p.Action != "" || p.Cwd != "" {
		t.Errorf("%+v", p)
	}
}

func TestAnActionRunsItsOwnExec(t *testing.T) {
	anoint := app(t, systemIndex(t), "forge-anoint.desktop")
	p, err := PlanFor(anoint, "purge", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs(t, p.Argv, "anoint", "--purge")
	if p.Action != "purge" || p.Name != "Anoint: Purge" {
		t.Errorf("%+v", p)
	}
	var none *NoActionError
	if _, err := PlanFor(anoint, "absent", nil); !errors.As(err, &none) || none.ID != "absent" {
		t.Errorf("%v", err)
	} else if err.Error() != "it knows no action 'absent'" {
		t.Errorf("%q", err)
	}
}

func TestATerminalAppRunsBehindTheTerminal(t *testing.T) {
	shell := app(t, systemIndex(t), "forge-terminal.desktop")
	p, err := PlanFor(shell, "", func() ([]string, error) { return []string{"foot", "-e"}, nil })
	if err != nil {
		t.Fatal(err)
	}
	wantArgs(t, p.Argv, "foot", "-e", "anoint", "--terminal")
}

// The terminal is sought once for a terminal app, and never for another.
func TestTheTerminalIsSoughtOnlyForATerminalApp(t *testing.T) {
	ix := systemIndex(t)
	for _, tc := range []struct {
		id    string
		times int
	}{{"forge-terminal.desktop", 1}, {"forge-anoint.desktop", 0}} {
		sought := 0
		_, err := PlanFor(app(t, ix, tc.id), "", func() ([]string, error) { sought++; return []string{"foot"}, nil })
		if err != nil || sought != tc.times {
			t.Errorf("%s: sought %d times (%v)", tc.id, sought, err)
		}
	}
}

// Without a terminal, the error says how to name one: in the config file
// the daemon reads, with an example.
func TestWithoutATerminalTheErrorSaysHowToNameOne(t *testing.T) {
	shell := app(t, systemIndex(t), "forge-terminal.desktop")
	const want = `it must run in a terminal, and no terminal was found. ` +
		`Set "terminal" in /home/adept/.config/logis/logis.jsonc, e.g. "terminal": ["foot", "-e"]`
	_, err := PlanFor(shell, "", func() ([]string, error) {
		return nil, &NoTerminalError{Config: "/home/adept/.config/logis/logis.jsonc"}
	})
	var none *NoTerminalError
	if !errors.As(err, &none) || err.Error() != want {
		t.Errorf("%v", err)
	}
	_, err = PlanFor(shell, "", nil)
	if !errors.As(err, &none) || !strings.Contains(err.Error(), `Set "terminal" in the config, e.g.`) {
		t.Errorf("%v", err)
	}
}

func TestADBusAppIsActivatedByItsIDAndItsWorkingDirIsKept(t *testing.T) {
	ix := systemIndex(t)
	p, err := PlanFor(app(t, ix, "org.forge.Bus.desktop"), "wake", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.DBus || len(p.Argv) != 0 || p.AppID != "org.forge.Bus" || p.Action != "wake" {
		t.Errorf("%+v", p)
	}
	withPath := *app(t, ix, "forge-anoint.desktop")
	withPath.Entry.Path = "/srv/forge"
	if p, _ := PlanFor(&withPath, "", nil); p.Cwd != "/srv/forge" {
		t.Errorf("cwd %q", p.Cwd)
	}
}
