package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The shell completions in contrib/completions/ (bash, zsh, fish) are
// maintained by hand, so they are pinned to the parser: each offers exactly
// the runes the parser accepts, plus the `daemon` subcommand. A rune added
// to or removed from the parser without its completions fails here. The
// runes are read from the string literals of this package's code (not its
// tests), which is where a new rune must appear to be accepted at all.
//
// bash and zsh are asked what they offer (zsh through a pseudo-terminal:
// tests/fixtures/completions/zsh-capture.zsh); the fish script is read and,
// when fish is installed, asked too. A shell that is not installed skips.

// repoRoot is the repository root, two levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// nameChar reports whether c may continue a rune's name (--dmenu is not
// --dmenu0).
func nameChar(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
}

// literalRune is the rune a string literal names, if it names one: "--lines",
// "-l", and the name of "--config=".
func literalRune(lit string) (string, bool) {
	name := strings.TrimSuffix(lit, "=")
	long := strings.HasPrefix(name, "--") && len(name) > 3
	short := len(name) == 2 && name[0] == '-' && name != "--"
	if !long && !short {
		return "", false
	}
	for _, c := range name[1:] {
		if !nameChar(c) {
			return "", false
		}
	}
	return name, true
}

// literalRunes are the runes named by the string literals of source.
func literalRunes(t *testing.T, name, source string) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	runes := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("%s: %s: %v", name, lit.Value, err)
		}
		if r, ok := literalRune(text); ok {
			runes[r] = true
		}
		return true
	})
	return runes
}

// parserRunes are the runes this package's code accepts.
func parserRunes(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	runes := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for r := range literalRunes(t, file, string(source)) {
			runes[r] = true
		}
	}
	return runes
}

// fishRunes are the runes a fish completion offers: the -l name and -s x
// options of its `complete` lines, before their description; quoted text
// is skipped.
func fishRunes(script string) map[string]bool {
	runes := map[string]bool{}
	for _, line := range strings.Split(script, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "complete ") {
			continue
		}
		var unquoted strings.Builder
		var quote rune
		for _, c := range line {
			switch {
			case quote == 0 && (c == '\'' || c == '"'):
				quote = c
			case quote != 0 && c == quote:
				quote = 0
			case quote == 0:
				unquoted.WriteRune(c)
			}
		}
		var words []string
		for _, w := range strings.Fields(unquoted.String()) {
			if w == "-d" || w == "--description" {
				break
			}
			words = append(words, w)
		}
		for i := 0; i+1 < len(words); i++ {
			switch words[i] {
			case "-l", "--long-option":
				runes["--"+words[i+1]] = true
			case "-s", "--short-option":
				runes["-"+words[i+1]] = true
			}
		}
	}
	return runes
}

func sortedKeys(m map[string]bool) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func assertSameRunes(t *testing.T, shell string, offered map[string]bool) {
	t.Helper()
	known := parserRunes(t)
	var missing, unknown []string
	for _, r := range sortedKeys(known) {
		if !offered[r] {
			missing = append(missing, r)
		}
	}
	for _, r := range sortedKeys(offered) {
		if !known[r] {
			unknown = append(unknown, r)
		}
	}
	if len(missing) > 0 || len(unknown) > 0 {
		t.Errorf("the %s completion drifted from the parser:\n  runes it does not offer: %q\n"+
			"  runes the parser does not know: %q", shell, missing, unknown)
	}
}

// shell runs program with args in the repository (killed after 20 s); it
// skips the test when the program is not installed and fails unless it
// exits 0.
func shell(t *testing.T, program string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath(program); err != nil {
		t.Skipf("%s is not installed", program)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = repoRoot(t)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		t.Fatalf("%s %q did not finish within 20 s", program, args)
	}
	if err != nil {
		t.Fatalf("%s %q failed: %v: %s", program, args, err, stderr.String())
	}
	return string(out)
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// bashComplete is what the bash completion offers for words (the last one
// being completed), in its order.
func bashComplete(t *testing.T, words ...string) []string {
	t.Helper()
	program := `source contrib/completions/logis.bash; COMP_WORDS=("$@"); ` +
		`COMP_CWORD=$(($# - 1)); _logis; printf '%s\n' "${COMPREPLY[@]}"`
	args := append([]string{"--norc", "--noprofile", "-c", program, "bash"}, words...)
	return nonEmptyLines(shell(t, "bash", args...))
}

// zshComplete is what the zsh completion offers for line, in its order.
func zshComplete(t *testing.T, line string) []string {
	t.Helper()
	out := shell(t, "zsh", "-f", "tests/fixtures/completions/zsh-capture.zsh", "contrib/completions", line)
	return strings.Split(strings.TrimSuffix(out, "\n"), "\n")
}

// fishComplete is what the fish completion offers for line (candidates
// only).
func fishComplete(t *testing.T, line string) []string {
	t.Helper()
	program := "source contrib/completions/logis.fish; complete -C '" + line + "'"
	var offered []string
	for _, l := range nonEmptyLines(shell(t, "fish", "--no-config", "-c", program)) {
		candidate, _, _ := strings.Cut(l, "\t")
		offered = append(offered, candidate)
	}
	return offered
}

// offeredRunes is everything complete offers where a rune may stand: first,
// and after `daemon`.
func offeredRunes(complete func(words ...string) []string) map[string]bool {
	runes := map[string]bool{}
	for _, words := range [][]string{{"logis", "-"}, {"logis", "daemon", "-"}} {
		for _, r := range complete(words...) {
			runes[r] = true
		}
	}
	return runes
}

func TestTheParserRunesAreFoundInItsSource(t *testing.T) {
	runes := parserRunes(t)
	for _, r := range []string{"--dmenu", "--dmenu0", "-d", "--config", "--idle-timeout", "--detach",
		"--with-nth", "-R", "-V", "--apps", "--servitor", "--provider", "--check-config", "--debug-panel"} {
		if !runes[r] {
			t.Errorf("%s not found in %q", r, sortedKeys(runes))
		}
	}
	if runes["--"] {
		t.Errorf("%q", sortedKeys(runes))
	}
}

func TestRuneScannersReadWordsNotFragments(t *testing.T) {
	source := "package p\nvar _ = []string{\"--ab\", \"-b\", \"--config=\", \"--\", \"x-y\", \"the rune --c\", `-q`, \"--a\\\"b\"}\n"
	got := sortedKeys(literalRunes(t, "p.go", source))
	if want := []string{"--ab", "--config", "-b", "-q"}; !slices.Equal(got, want) {
		t.Errorf("literal runes %q, want %q", got, want)
	}
	fish := "# complete -l no\ncomplete -c logis -n 'x -l quoted' -s d -l dmenu -d 'Read -l x'\n"
	if got, want := sortedKeys(fishRunes(fish)), []string{"--dmenu", "-d"}; !slices.Equal(got, want) {
		t.Errorf("fish runes %q, want %q", got, want)
	}
}

func TestTheBashCompletionOffersEveryRune(t *testing.T) {
	assertSameRunes(t, "bash", offeredRunes(func(w ...string) []string { return bashComplete(t, w...) }))
}

func TestTheZshCompletionOffersEveryRune(t *testing.T) {
	assertSameRunes(t, "zsh", offeredRunes(func(w ...string) []string { return zshComplete(t, strings.Join(w, " ")) }))
}

func TestTheFishCompletionOffersEveryRune(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "contrib", "completions", "logis.fish"))
	if err != nil {
		t.Fatal(err)
	}
	assertSameRunes(t, "fish", fishRunes(string(script)))
	assertSameRunes(t, "fish (asked)",
		offeredRunes(func(w ...string) []string { return fishComplete(t, strings.Join(w, " ")) }))
}

// everyShell runs check with what each installed shell offers for line.
func everyShell(t *testing.T, line string, check func(t *testing.T, offered []string)) {
	t.Run("bash", func(t *testing.T) { check(t, bashComplete(t, strings.Split(line, " ")...)) })
	t.Run("zsh", func(t *testing.T) { check(t, zshComplete(t, line)) })
	t.Run("fish", func(t *testing.T) { check(t, fishComplete(t, line)) })
}

func TestEveryShellCompletesAPartialRune(t *testing.T) {
	everyShell(t, "logis --dm", func(t *testing.T, offered []string) {
		if want := []string{"--dmenu", "--dmenu0"}; !slices.Equal(offered, want) {
			t.Errorf("offered %q, want %q", offered, want)
		}
	})
}

func TestEveryShellCompletesTheDaemonSubcommandFirst(t *testing.T) {
	everyShell(t, "logis da", func(t *testing.T, offered []string) {
		if want := []string{"daemon"}; !slices.Equal(offered, want) {
			t.Errorf("offered %q, want %q", offered, want)
		}
	})
}

func TestEveryShellOffersOnlyTheDaemonRunesAfterDaemon(t *testing.T) {
	everyShell(t, "logis daemon --", func(t *testing.T, offered []string) {
		got := slices.Sorted(slices.Values(offered))
		if want := []string{"--config", "--debug-panel", "--detach", "--idle-timeout"}; !slices.Equal(got, want) {
			t.Errorf("offered %q, want %q", got, want)
		}
	})
}

func TestModesExcludeEachOtherInZshAndFish(t *testing.T) {
	modes := []string{"--dmenu", "--dmenu0", "--servitor", "--provider", "--version"}
	noMode := func(t *testing.T, offered []string) {
		for _, o := range offered {
			if slices.Contains(modes, o) {
				t.Errorf("%q offered after --apps", o)
			}
		}
		if !slices.Contains(offered, "--config") {
			t.Errorf("--config not offered: %q", offered)
		}
	}
	t.Run("zsh", func(t *testing.T) { noMode(t, zshComplete(t, "logis --apps -")) })
	t.Run("fish", func(t *testing.T) { noMode(t, fishComplete(t, "logis --apps -")) })
}
