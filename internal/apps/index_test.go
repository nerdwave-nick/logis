package apps

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Ported from Rust src/apps/index.rs.

func testContext(t *testing.T) Context {
	return Context{Desktops: []string{"niri"}, Path: []string{filepath.Join(fixtures(t), "bin")}}
}

func testDirs(t *testing.T) Dirs {
	f := fixtures(t)
	return Dirs{
		filepath.Join(f, "home/applications"),
		filepath.Join(f, "system/applications"),
		filepath.Join(f, "vendor/applications"),
	}
}

func ids(ix *Index) []string {
	var out []string
	for _, a := range ix.Apps() {
		out = append(out, a.ID)
	}
	return out
}

func TestDataHomeComesBeforeTheDataDirsAndRelativePathsAreIgnored(t *testing.T) {
	got := DirsFromVars(vars("HOME", "/home/adept", "XDG_DATA_DIRS", "/opt/share:relative:/usr/share"))
	want := Dirs{"/home/adept/.local/share/applications", "/opt/share/applications", "/usr/share/applications"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
	got = DirsFromVars(vars("XDG_DATA_HOME", "/data"))
	want = Dirs{"/data/applications", "/usr/local/share/applications", "/usr/share/applications"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
}

func TestTheContextReadsDesktopsPathAndLocale(t *testing.T) {
	cx := ContextFromVars(vars("XDG_CURRENT_DESKTOP", "niri:wlroots", "PATH", "/usr/bin:/bin", "LANG", "de_DE.UTF-8"))
	if !slices.Equal(cx.Desktops, []string{"niri", "wlroots"}) || !slices.Equal(cx.Path, []string{"/usr/bin", "/bin"}) {
		t.Errorf("%+v", cx)
	}
	if !reflect.DeepEqual(cx.Locale, ParseLocale("de_DE")) {
		t.Errorf("locale %+v", cx.Locale)
	}
}

func TestIDsFollowThePathBelowApplicationsAndTheFirstFileWins(t *testing.T) {
	ix := Scan(testDirs(t), testContext(t))
	oil, ok := ix.Get("forge-oil.desktop")
	if !ok || oil.Entry.Name != "Oil (home)" || !strings.HasSuffix(oil.Location, "home/applications/forge-oil.desktop") {
		t.Errorf("%+v", oil)
	}
	litany, ok := ix.Get("forge-litany.desktop")
	if !ok || !strings.HasSuffix(litany.Location, "home/applications/forge/litany.desktop") {
		t.Errorf("%+v", litany)
	}
	if litany.AppID() != "forge-litany" {
		t.Errorf("app id %q", litany.AppID())
	}
	if !slices.Contains(ix.Dirs, filepath.Join(fixtures(t), "home/applications/forge")) {
		t.Errorf("dirs %q", ix.Dirs)
	}
}

func TestHiddenDeletesAndABrokenOverrideOverridesNothing(t *testing.T) {
	ix := Scan(testDirs(t), testContext(t))
	if _, ok := ix.Get("forge-deleted.desktop"); ok {
		t.Error("a deleted entry is shown")
	}
	if b, ok := ix.Get("forge-broken.desktop"); !ok || b.Entry.Name != "Broken in home" {
		t.Errorf("%+v", b)
	}
}

func TestOnlyShownApplicationsAreIndexedByName(t *testing.T) {
	want := []string{
		"forge-anoint.desktop",
		"forge-broken.desktop",
		"org.forge.Bus.desktop",
		"forge-terminal.desktop",
		"forge-litany.desktop",
		"forge-oil.desktop",
		"forge-only-niri.desktop",
		"forge-tryexec-present.desktop",
	}
	if got := ids(Scan(testDirs(t), testContext(t))); !slices.Equal(got, want) {
		t.Errorf("%q\nwant %q", got, want)
	}
}

func TestShowInRulesFollowTheCurrentDesktops(t *testing.T) {
	gnome := testContext(t)
	gnome.Desktops = []string{"GNOME"}
	shown := ids(Scan(testDirs(t), gnome))
	for _, id := range []string{"forge-only-gnome.desktop", "forge-not-niri.desktop", "forge-only-niri.desktop"} {
		if !slices.Contains(shown, id) {
			t.Errorf("GNOME: %s missing", id)
		}
	}
	nowhere := testContext(t)
	nowhere.Desktops = nil
	shown = ids(Scan(testDirs(t), nowhere))
	if slices.Contains(shown, "forge-only-niri.desktop") {
		t.Error("OnlyShowIn needs a desktop")
	}
	if !slices.Contains(shown, "forge-not-niri.desktop") {
		t.Error("NotShowIn hides only on those desktops")
	}
}

func TestTryExecNeedsAnExecutableOnThePathOrAtItsAbsolutePath(t *testing.T) {
	cx := testContext(t)
	shows := func(tryExec string) bool { return cx.Shows(DesktopEntry{TryExec: tryExec}) }
	if !shows("anoint") || shows("forge-no-such-program") {
		t.Error("TryExec on the PATH")
	}
	if !shows(filepath.Join(fixtures(t), "bin/anoint")) {
		t.Error("TryExec at its absolute path")
	}
	if shows(filepath.Join(fixtures(t), "system/applications/forge-oil.desktop")) || shows("/no/such/forge") {
		t.Error("TryExec of something not executable")
	}
}
