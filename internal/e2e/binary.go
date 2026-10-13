package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// built is the logis binary the tests of this process share.
var built struct {
	once      sync.Once
	dir, path string
	out       []byte
	err       error
}

// Binary builds cmd/logis once per test process and returns its path. A
// package that uses it runs its tests through Main, which removes it.
func Binary(t testing.TB) string {
	t.Helper()
	built.once.Do(func() {
		built.dir, built.err = os.MkdirTemp("", "logis-e2e-bin-")
		if built.err != nil {
			return
		}
		built.path = filepath.Join(built.dir, "logis")
		cmd := exec.Command("go", "build", "-o", built.path, "./cmd/logis")
		cmd.Dir = moduleRoot(t)
		built.out, built.err = cmd.CombinedOutput()
	})
	if built.err != nil {
		t.Fatalf("go build ./cmd/logis: %v\n%s", built.err, built.out)
	}
	return built.path
}

// Main runs m's tests and removes the binary Binary built:
// `func TestMain(m *testing.M) { e2e.Main(m) }`.
func Main(m *testing.M) {
	code := m.Run()
	if built.dir != "" {
		_ = os.RemoveAll(built.dir)
	}
	os.Exit(code)
}

// moduleRoot is the directory of go.mod, from the working directory
// upwards (go test runs in the package's directory).
func moduleRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}
