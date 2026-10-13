package version_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/nerdwave-nick/logis/internal/version"
)

// The Rust crate on this branch is the reference implementation until the
// switch-over (spec §2, D6); the Go build carries the same version.
func TestVersionMatchesTheRustCrate(t *testing.T) {
	manifest, err := os.ReadFile(filepath.Join("..", "..", "Cargo.toml"))
	if os.IsNotExist(err) {
		t.Skip("no Cargo.toml: the Rust reference is gone after the switch-over")
	}
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^version = "([^"]+)"$`).FindSubmatch(manifest)
	if m == nil {
		t.Fatal("Cargo.toml has no package version line")
	}
	if got, want := version.Version, string(m[1]); got != want {
		t.Errorf("version.Version = %q, Cargo.toml says %q", got, want)
	}
}
