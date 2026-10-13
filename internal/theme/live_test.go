package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/config"
)

// Ported from the theme half of Rust src/config/live.rs tests.

func named(theme string) config.Config {
	c := config.Default()
	c.Theme = theme
	return c
}

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".jsonc"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheThemeTheConfigNamesIsWornFromTheStart(t *testing.T) {
	live, err := NewLive(NewThemes(t.TempDir()), named("cogitator-phosphor"))
	if err != nil || live.Theme().Name != "cogitator-phosphor" {
		t.Fatalf("%v %q", err, live.Theme().Name)
	}
	broken := t.TempDir()
	write(t, broken, "mine", `{"glow": 1}`)
	live, err = NewLive(NewThemes(broken), named("mine"))
	if live.Theme().Name != "forge-crimson-brass" {
		t.Errorf("the default instead, not %q", live.Theme().Name)
	}
	if err == nil || !strings.Contains(err.Error(), "mine.jsonc:1:") {
		t.Errorf("the broken theme is reported: %v", err)
	}
}

func TestAnEditedThemeIsAppliedAndABrokenOneKeepsTheLastSound(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mine", `{"window": {"rows": 4}}`)
	live, err := NewLive(NewThemes(dir), named("mine"))
	if err != nil {
		t.Fatal(err)
	}
	c := named("mine")
	if r, err := live.Reload(c); r != config.Unchanged || err != nil {
		t.Errorf("%v %v", r, err)
	}
	write(t, dir, "mine", `{"window": {"rows": 5}}`)
	if r, _ := live.Reload(c); r != config.Applied || live.Theme().Window.Rows != 5 {
		t.Errorf("%v rows %d", r, live.Theme().Window.Rows)
	}
	write(t, dir, "mine", `{"window": {"rows": "many"}}`)
	r, err := live.Reload(c)
	if r != config.Rejected || err == nil || !strings.Contains(err.Error(), "mine.jsonc:1:") {
		t.Errorf("a broken theme was taken: %v %v", r, err)
	}
	if live.Theme().Window.Rows != 5 {
		t.Error("the last sound theme stands")
	}
	write(t, dir, "mine", `{"window": {"rows": 5}}`)
	if r, _ := live.Reload(c); r != config.Restored {
		t.Errorf("%v", r)
	}
}

func TestAConfigNamingAnotherThemeOrReducingMotionChangesWhatIsWorn(t *testing.T) {
	live, err := NewLive(NewThemes(t.TempDir()), named("forge-crimson-brass"))
	if err != nil || !live.Theme().IsContinuous() {
		t.Fatalf("%v", err)
	}
	still := named("forge-crimson-brass")
	still.ReduceMotion = true
	if r, _ := live.Reload(still); r != config.Applied || live.Theme().IsContinuous() {
		t.Errorf("stilled: %v", r)
	}
	if r, _ := live.Reload(named("cogitator-phosphor")); r != config.Applied || live.Theme().Name != "cogitator-phosphor" {
		t.Errorf("%v %q", r, live.Theme().Name)
	}
	r, err := live.Reload(named("purple"))
	if r != config.Rejected || err == nil || !strings.Contains(err.Error(), "theme 'purple'") {
		t.Errorf("%v %v", r, err)
	}
	if live.Theme().Name != "cogitator-phosphor" {
		t.Errorf("%q", live.Theme().Name)
	}
}

// A worn theme is never changed in place: whoever holds it (the window's
// goroutine) keeps a sound theme while the daemon reloads.
func TestAWornThemeIsNeverChangedInPlace(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mine", `{"window": {"rows": 4}}`)
	live, _ := NewLive(NewThemes(dir), named("mine"))
	held := live.Theme()
	write(t, dir, "mine", `{"window": {"rows": 5}}`)
	if _, err := live.Reload(named("mine")); err != nil {
		t.Fatal(err)
	}
	if held.Window.Rows != 4 || live.Theme() == held {
		t.Errorf("held %d", held.Window.Rows)
	}
}
