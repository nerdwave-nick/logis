package apps

import (
	"os"
	"path/filepath"
	"testing"
)

// Ported from Rust src/apps/frecency.rs.

const frecencyNow = 1_760_000_000

func TestTheFileLivesInTheStateHome(t *testing.T) {
	for _, tc := range []struct {
		env  func(string) string
		want string
	}{
		{vars("XDG_STATE_HOME", "/state", "HOME", "/h"), "/state/logis/frecency.json"},
		{vars("XDG_STATE_HOME", "rel", "HOME", "/h"), "/h/.local/state/logis/frecency.json"},
		{vars(), ""},
	} {
		if got := FrecencyPath(tc.env); got != tc.want {
			t.Errorf("%q, want %q", got, tc.want)
		}
	}
}

func TestLaunchesRaiseTheBoostAndRecentOnesMore(t *testing.T) {
	f := NewFrecency()
	if b := f.Boost("oil.desktop", frecencyNow); b != 0 {
		t.Fatalf("boost %d before any launch", b)
	}
	f.Record("oil.desktop", frecencyNow)
	once := f.Boost("oil.desktop", frecencyNow)
	if once == 0 {
		t.Fatal("no boost after a launch")
	}
	for range 9 {
		f.Record("oil.desktop", frecencyNow)
	}
	often := f.Boost("oil.desktop", frecencyNow)
	if often <= once {
		t.Errorf("%d > %d expected", often, once)
	}
	later := func(dt uint64) uint32 { return f.Boost("oil.desktop", frecencyNow+dt) }
	if later(2*hour) >= often || later(2*day) >= later(2*hour) || later(30*day) >= later(2*day) {
		t.Errorf("older launches count less: %d %d %d %d", often, later(2*hour), later(2*day), later(30*day))
	}
	if later(30*day) == 0 {
		t.Error("forgotten by age alone")
	}
	if f.Boost("fire.desktop", frecencyNow) != 0 {
		t.Error("an app never launched is boosted")
	}
}

func TestTheBoostIsBounded(t *testing.T) {
	f := NewFrecency()
	for range 900 {
		f.Record("oil.desktop", frecencyNow)
	}
	if b := f.Boost("oil.desktop", frecencyNow); b != MaxBoost {
		t.Errorf("boost %d, want %d", b, MaxBoost)
	}
}

func TestRanksAgeOnceTheyAddUpTooHigh(t *testing.T) {
	f := NewFrecency()
	f.Record("once.desktop", frecencyNow)
	for range 1000 {
		f.Record("oil.desktop", frecencyNow)
	}
	if r := f.apps["oil.desktop"].Rank; r >= 1000 || r < 900 {
		t.Errorf("rank %g: not aged by a tenth", r)
	}
	if _, ok := f.apps["once.desktop"]; ok {
		t.Error("faded ranks are kept")
	}
}

func TestASavedHistoryLoadsBackAndAMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logis/frecency.json")
	empty, err := LoadFrecency(path)
	if err != nil || len(empty.apps) != 0 {
		t.Fatalf("%+v %v", empty, err)
	}
	f := NewFrecency()
	f.Record("oil.desktop", frecencyNow)
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadFrecency(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := back.Boost("oil.desktop", frecencyNow), f.Boost("oil.desktop", frecencyNow); got != want {
		t.Errorf("boost %d after loading, want %d", got, want)
	}
	left, _ := os.ReadDir(filepath.Dir(path))
	if len(left) != 1 || left[0].Name() != "frecency.json" {
		t.Errorf("left behind: %v", left)
	}
}

// The Rust daemon's file (serde: floats with a decimal point) loads too.
func TestRustsFileLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	text := `{"version":1,"apps":{"oil.desktop":{"rank":2.0,"last":1760000000}}}`
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := LoadFrecency(path)
	if err != nil || f.apps["oil.desktop"] != (use{Rank: 2, Last: frecencyNow}) {
		t.Errorf("%+v %v", f, err)
	}
}

func TestACorruptFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	if err := os.WriteFile(path, []byte("{ heresy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrecency(path); err == nil {
		t.Error("a corrupt file loaded")
	}
}
