package servitor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Test helpers: the fake servitor (tests/fixtures/servitor/servitor) and
// scratch directories for its records.

// fixture is a file of tests/fixtures/servitor.
func fixture(t testing.TB, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "servitor", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// fake is a `servitor` in dir that runs the fake on census, keeping its
// records in dir; extra are more VAR=value words for it.
func fake(t testing.TB, dir, census string, extra ...string) string {
	t.Helper()
	wrapper := filepath.Join(dir, "servitor")
	script := "#!/bin/sh\n" + strings.Join(extra, " ") + " FAKE_SERVITOR_DIR='" + dir +
		"' FAKE_SERVITOR_CENSUS='" + census + "' exec '" + fixture(t, "servitor") + "' \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

// recorded are the heralds of a recorded vigil, in order.
func recorded(t testing.TB, name string) []Herald {
	t.Helper()
	text, err := os.ReadFile(fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}
	var out []Herald
	for _, l := range strings.Split(string(text), "\n") {
		if !strings.HasPrefix(l, "{") {
			continue
		}
		h, err := ParseHerald([]byte(l))
		if err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		out = append(out, h)
	}
	return out
}

func rite(t testing.TB, h Herald) Entry {
	t.Helper()
	r, ok := h.(Rite)
	if !ok {
		t.Fatalf("not a rite: %#v", h)
	}
	return r.Entry
}

func mustParse(t testing.TB, line string) Herald {
	t.Helper()
	h, err := ParseHerald([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// waitFor polls until path exists, within patience.
func waitFor(path string, patience time.Duration) bool {
	until := time.Now().Add(patience)
	for time.Now().Before(until) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func record(t testing.TB, dir, name string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, "invoke", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// censusPid is the pid the fake census wrote.
func censusPid(t testing.TB, dir string) int {
	t.Helper()
	path := filepath.Join(dir, "census.pid")
	deadline := time.Now().Add(5 * time.Second)
	for {
		text, err := os.ReadFile(path)
		if err == nil && strings.HasSuffix(string(text), "\n") {
			pid, err := strconv.Atoi(strings.TrimSpace(string(text)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatal("the census never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// alive: process pid still runs (a zombie counts as gone).
func alive(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(stat)
	return !strings.HasPrefix(strings.TrimSpace(s[strings.LastIndexByte(s, ')')+1:]), "Z")
}
