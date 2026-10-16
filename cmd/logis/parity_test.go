package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/wltest"
)

// The caller-facing surface of Rust logis v1, held against the Go binary
// argument by argument: for every case of the corpus below, exit status,
// stdout and stderr must be Rust's byte for byte. Rust's answers are recorded
// in testdata/parity.json (from the reference, Rust main 2ffdd02, built with
// `cargo build --release`), so the comparison runs everywhere without the
// Rust build. With LOGIS_RUST_LOGIS naming a Rust logis v1 binary the cases
// also run against it live, and `-update-parity` rewrites the record from
// its answers.
//
// Every case runs in a scratch directory of its own (its working directory,
// HOME, XDG_RUNTIME_DIR=$DIR/run, XDG_CONFIG_HOME=$DIR/config), with no
// Wayland session unless the case names one and no LOGIS_* variable, so no
// case ever reaches a compositor or starts a daemon that stays. The
// directory's path is written $DIR in the record.

var updateParity = flag.Bool("update-parity", false,
	"rewrite testdata/parity.json from the Rust logis that LOGIS_RUST_LOGIS names")

const parityRecord = "testdata/parity.json"

// divergences are the answers the Go binary gives unlike Rust's on
// purpose, for features new beside v1: case key → Rust's recorded answer
// as the Go binary gives it. The record stays Rust's.
var divergences = map[string]func(answer) answer{
	// a5e5f876: the config's custom-shader key is among the keys an
	// unknown one is told of.
	`[config/logis/logis.jsonc={"heresy": 1}] logis --check-config`: func(a answer) answer {
		a.Stderr = strings.Replace(a.Stderr, "`reduce-motion`\n", "`reduce-motion`, `custom-shader`\n", 1)
		return a
	},
}

// parityCase is one invocation: its arguments ("hex:" marks an argument
// given as hexadecimal bytes, for ones that are not UTF-8), stdin, extra
// environment ("NAME=" unsets NAME), files written below $DIR first, and
// the fake compositor (a key of parityCompositors) served at
// $DIR/run/wayland-test as WAYLAND_DISPLAY=wayland-test, if any.
type parityCase struct {
	args       []string
	stdin      string
	env        []string
	files      map[string]string
	compositor string
}

// parityCompositors are the fake compositors a case may name: ones the
// window cannot use, or (for scripted cases only) one that passes the
// preflight, so no case ever opens a window.
var parityCompositors = map[string][]wltest.Global{
	"no-layer-shell":      withoutLayerShell,
	"no-fractional-scale": withoutFractionalScale,
	"everything":          withLayerShell,
}

// answer is how one invocation ended.
type answer struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// key names the case in the record: its environment, files and arguments.
func (c parityCase) key() string {
	var parts []string
	if c.compositor != "" {
		parts = append(parts, "[compositor="+c.compositor+"]")
	}
	parts = append(parts, c.env...)
	var names []string
	for name := range c.files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parts = append(parts, "["+name+"="+c.files[name]+"]")
	}
	parts = append(parts, "logis")
	for _, a := range c.args {
		parts = append(parts, quoteArg(a))
	}
	if c.stdin != "" {
		parts = append(parts, "< "+quoteArg(c.stdin))
	}
	return strings.Join(parts, " ")
}

func quoteArg(a string) string {
	if a != "" && !strings.ContainsAny(a, " \t\n'\"{}") {
		return a
	}
	b, _ := json.Marshal(a)
	return string(b)
}

func (c parityCase) argv(t *testing.T) []string {
	out := make([]string, len(c.args))
	for i, a := range c.args {
		if h, ok := strings.CutPrefix(a, "hex:"); ok {
			b, err := hex.DecodeString(h)
			if err != nil {
				t.Fatalf("%s: %v", a, err)
			}
			a = string(b)
		}
		out[i] = a
	}
	return out
}

// runParity runs bin for one case in a fresh scratch directory.
func runParity(t *testing.T, bin string, c parityCase) answer {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"run", "config", "home"} {
		if err := os.Mkdir(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range c.files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{
		"PATH":            os.Getenv("PATH"),
		"HOME":            filepath.Join(dir, "home"),
		"XDG_RUNTIME_DIR": filepath.Join(dir, "run"),
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"LANG":            "C.UTF-8",
	}
	if c.compositor != "" {
		globals, ok := parityCompositors[c.compositor]
		if !ok {
			t.Fatalf("%s: no such compositor", c.key())
		}
		if c.compositor == "everything" && !slices.Contains(c.env, "LOGIS_RESOLVER=scripted") {
			t.Fatalf("%s: a window would open: everything is for scripted cases", c.key())
		}
		fake, err := wltest.Serve(filepath.Join(dir, "run"), "wayland-test", globals)
		if err != nil {
			t.Fatal(err)
		}
		defer fake.Close()
		env["WAYLAND_DISPLAY"] = "wayland-test"
	}
	for _, kv := range c.env {
		name, value, _ := strings.Cut(kv, "=")
		if value == "" {
			delete(env, name)
			continue
		}
		env[name] = strings.ReplaceAll(value, "$DIR", dir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, c.argv(t)...)
	cmd.Dir = dir
	cmd.Env = nil
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(c.stdin), &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s: %s did not end within 10 s", c.key(), bin)
	}
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("%s: %v", c.key(), err)
	}
	// A daemon a case started (only the scripted ones may) idles out.
	socket := filepath.Join(dir, "run", "logis.sock")
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(socket); err != nil {
			break
		}
	}
	if _, err := os.Stat(socket); err == nil {
		t.Errorf("%s: %s left a daemon's socket behind", c.key(), bin)
	}
	unDir := func(b []byte) string { return strings.ReplaceAll(string(b), dir, "$DIR") }
	return answer{cmd.ProcessState.ExitCode(), unDir(stdout.Bytes()), unDir(stderr.Bytes())}
}

func readParityRecord(t *testing.T) map[string]answer {
	t.Helper()
	data, err := os.ReadFile(parityRecord)
	if err != nil {
		t.Fatal(err)
	}
	record := map[string]answer{}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func writeParityRecord(t *testing.T, record map[string]answer) {
	t.Helper()
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parityRecord, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheCallerFacingSurfaceIsRustsArgumentByArgument(t *testing.T) {
	cases := parityCorpus()
	rust := os.Getenv("LOGIS_RUST_LOGIS")
	if *updateParity {
		if rust == "" {
			t.Fatal("-update-parity needs LOGIS_RUST_LOGIS naming a Rust logis v1 binary")
		}
		record := map[string]answer{}
		for _, c := range cases {
			record[c.key()] = runParity(t, rust, c)
		}
		writeParityRecord(t, record)
	}
	record := readParityRecord(t)
	if len(record) != len(cases) {
		t.Errorf("the record holds %d cases, the corpus %d: rerun with -update-parity", len(record), len(cases))
	}
	bin := buildLogis(t)
	for _, c := range cases {
		want, ok := record[c.key()]
		if !ok {
			t.Errorf("%s: not in the record: rerun with -update-parity", c.key())
			continue
		}
		if rust != "" && !*updateParity {
			if live := runParity(t, rust, c); live != want {
				t.Errorf("%s: Rust answers\n  %+v\nnot as recorded\n  %+v", c.key(), live, want)
			}
		}
		if diverge, ok := divergences[c.key()]; ok {
			want = diverge(want)
		}
		if got := runParity(t, bin, c); got != want {
			t.Errorf("%s:\n  Go   %+v\n  Rust %+v", c.key(), got, want)
		}
	}
}

func TestParityKeysAreUniqueAndQuoted(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range parityCorpus() {
		if seen[c.key()] {
			t.Errorf("two cases are %s", c.key())
		}
		seen[c.key()] = true
	}
	if got := (parityCase{args: []string{"--prompt", "a b", ""}, env: []string{"X="}}).key(); got != `X= logis --prompt "a b" ""` {
		t.Errorf("key %s", got)
	}
}
