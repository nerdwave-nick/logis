package theme

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/nerdwave-nick/logis/themes"
)

// A differential check against Rust logis v1 (test-only oracle): with
// LOGIS_RUST_LOGIS naming a Rust logis binary, every text of a corpus is
// worn as a user theme by both, and Rust's `--check-config` verdict must be
// Go's word for word, place for place. The corpus is every text these tests
// name plus each bundled theme with one value at a time replaced by values
// of other kinds, and with one key at a time misspelt.

// oracleOddities are values put in place of each value of a bundled theme.
var oracleOddities = []string{`1`, `-1`, `0`, `1.5`, `2e3`, `"x"`, `""`, `true`, `false`, `null`,
	`[]`, `{}`, `[1, 2]`, `"#000000"`, `"brass"`, `["brass", "nope"]`, `{"x": 1}`}

// valueAt finds each value after a colon or inside an array: numbers,
// strings and literals.
var valueAt = regexp.MustCompile(`([:\[,]\s*)(-?[0-9][0-9.eE+-]*|"(?:[^"\\]|\\.)*"|true|false|null)`)

// keyAt finds each key.
var keyAt = regexp.MustCompile(`"([a-z$][a-z0-9-]*)"\s*:`)

func oracleCorpus(t *testing.T) []string {
	var corpus []string
	for _, c := range refusals {
		corpus = append(corpus, c.text)
	}
	corpus = append(corpus,
		`{"border": {"paint": ["brass", "embr"]}}`, `{"effects": {"bloom": 2}}`, `{"row": {"font": "heading"}}`,
		`{"window": {"rows": 0}}`, `{"rule": {"dash": [1, -2]}}`, `{"extends": "cogitator-phosphor"}`,
		`{"extends": "nowhere"}`, `{"extends": "t"}`, `{"extends": "forge-crimson-brass", "glow": {"radius": 3}}`,
		`{"fonts": {"heading": {"size": 3}}, "row": {"font": "heading"}}`, `{"glow": [1, 2, 3, 4, 5]}`,
		`{"palette": {"Brass": 1}}`, `{"palette": {"b": "#000000", "A": "#000000", "C": "#000000"}}`, ``, `// nothing`)
	for _, b := range themes.Bundled {
		text := b.Text
		for _, m := range valueAt.FindAllStringSubmatchIndex(text, -1) {
			for _, odd := range oracleOddities {
				corpus = append(corpus, text[:m[4]]+odd+text[m[5]:])
			}
		}
		for _, m := range keyAt.FindAllStringSubmatchIndex(text, -1) {
			corpus = append(corpus, text[:m[2]]+"x"+text[m[2]:])
		}
	}
	return corpus
}

// goVerdict is what Go's --check-config says of theme "t" in dir.
func goVerdict(dir string) string {
	err := Check(dir, "t")
	var e *Error
	if errors.As(err, &e) {
		return "the theme is heretical: " + e.Error()
	}
	if err != nil {
		return err.Error()
	}
	return "sound"
}

// rustVerdict is what Rust's says, with the scratch directory as dir.
func rustVerdict(t *testing.T, bin, home string) string {
	cmd := exec.Command(bin, "--check-config")
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+home)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := strings.TrimSuffix(strings.TrimPrefix(out.String(), "logis: "), "\n")
	if err == nil && strings.HasSuffix(text, "the theme 't' is sound") {
		return "sound"
	}
	return text
}

func TestThemeVerdictsAgreeWithRustLogis(t *testing.T) {
	bin := os.Getenv("LOGIS_RUST_LOGIS")
	if bin == "" {
		t.Skip("LOGIS_RUST_LOGIS names no Rust logis to compare with")
	}
	corpus := oracleCorpus(t)
	var (
		mu       sync.Mutex
		differ   int
		wg       sync.WaitGroup
		texts    = make(chan string)
		reported = 0
	)
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for text := range texts {
				home, err := os.MkdirTemp(t.TempDir(), "oracle")
				if err != nil {
					t.Error(err)
					continue
				}
				dir := filepath.Join(home, "logis", "themes")
				_ = os.MkdirAll(dir, 0o700)
				_ = os.WriteFile(filepath.Join(home, "logis", "logis.jsonc"), []byte(`{"theme": "t"}`), 0o600)
				_ = os.WriteFile(filepath.Join(dir, "t.jsonc"), []byte(text), 0o600)
				g, r := goVerdict(dir), rustVerdict(t, bin, home)
				mu.Lock()
				if g != r {
					differ++
					if reported < 25 {
						reported++
						t.Errorf("%s\n  go:   %s\n  rust: %s", text, g, r)
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, text := range corpus {
		texts <- text
	}
	close(texts)
	wg.Wait()
	t.Logf("%d texts, %d verdicts differ", len(corpus), differ)
}
