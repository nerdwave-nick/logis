package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProgramsAreFoundOnTheSessionPath(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exe := write("rite", 0o700)
	write("inert", 0o600)
	if err := os.Mkdir(filepath.Join(dir, "chapel"), 0o700); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	pathVar := "relative:" + dir + "::" + other
	for _, tc := range []struct {
		program, want string
		found         bool
	}{
		{"rite", exe, true},
		{exe, exe, true},
		{"inert", "", false},
		{"chapel", "", false},
		{"", "", false},
		{"./rite", "", false},
		{filepath.Join(dir, "inert"), "", false},
		{"absent", "", false},
	} {
		got, found := LookPath(pathVar, tc.program)
		if got != tc.want || found != tc.found {
			t.Errorf("LookPath(%q) = %q, %v; want %q, %v", tc.program, got, found, tc.want, tc.found)
		}
	}
	if _, found := LookPath("relative", "rite"); found {
		t.Error("a relative PATH entry was searched")
	}
}

func TestTheFirstPathEntryWins(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, d := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(d, "rite"), nil, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := LookPath(second+":"+first, "rite"); got != filepath.Join(second, "rite") {
		t.Errorf("LookPath = %q; want the first entry's", got)
	}
}

// ownUnit is the last component of this process's cgroup.
func ownUnit(t *testing.T) string {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		t.Skip("no /proc/self/cgroup")
	}
	line := strings.TrimSpace(strings.Split(string(b), "\n")[0])
	return filepath.Base(line[strings.LastIndexByte(line, ':')+1:])
}

func TestAProcessIsSeenInItsUnit(t *testing.T) {
	unit := ownUnit(t)
	if unit == "/" || unit == "." {
		t.Skip("this process is in the root cgroup")
	}
	if !inUnit(os.Getpid(), unit, 0) {
		t.Errorf("inUnit(self, %q) = false", unit)
	}
	began := time.Now()
	if inUnit(os.Getpid(), "no-such-unit.scope", 30*time.Millisecond) {
		t.Error("inUnit(self, another unit) = true")
	}
	if waited := time.Since(began); waited < 30*time.Millisecond {
		t.Errorf("gave up after %v; want the whole patience", waited)
	}
	if inUnit(1<<30, unit, 0) {
		t.Error("a missing process is in a unit")
	}
}
