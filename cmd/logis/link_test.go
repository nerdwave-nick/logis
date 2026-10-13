package main

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// specLibraries are the C libraries spec §4 allows in the shipped binary
// (D1, D2, D4, D5), by soname.
var specLibraries = []string{
	"libEGL.so.1",
	"libGLESv2.so.2",
	"libfontconfig.so.1",
	"libfreetype.so.6",
	"libwayland-client.so.0",
	"libwayland-egl.so.1",
}

// systemLibraries are what the Go runtime's own cgo brings with it.
var systemLibraries = []string{"libc.so.6", "libm.so.6", "libpthread.so.0", "libdl.so.2"}

// buildDir holds the binary the tests build; TestMain removes it.
var buildDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "logis-link-test-")
	if err != nil {
		panic(err)
	}
	buildDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var built struct {
	once sync.Once
	path string
	err  error
	out  []byte
}

// buildLogis builds the shipped binary the documented way (`go build
// ./cmd/logis`) once per test run.
func buildLogis(t *testing.T) string {
	t.Helper()
	built.once.Do(func() {
		built.path = filepath.Join(buildDir, "logis")
		cmd := exec.Command("go", "build", "-o", built.path, ".")
		built.out, built.err = cmd.CombinedOutput()
	})
	if built.err != nil {
		t.Fatalf("go build ./cmd/logis: %v\n%s", built.err, built.out)
	}
	return built.path
}

func TestTheBinaryLinksExactlyTheSpecLibraries(t *testing.T) {
	bin, err := elf.Open(buildLogis(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bin.Close() }()
	needed, err := bin.ImportedLibraries()
	if err != nil {
		t.Fatal(err)
	}
	var own []string
	for _, lib := range needed {
		if !slices.Contains(systemLibraries, lib) {
			own = append(own, lib)
		}
	}
	slices.Sort(own)
	if !slices.Equal(own, specLibraries) {
		t.Errorf("DT_NEEDED beyond libc: %v\nwant exactly %v", own, specLibraries)
	}
}

// The protocol glue wayland-scanner generates (go generate) is compiled and
// linked into the binary.
func TestTheBinaryCarriesTheScannerGlue(t *testing.T) {
	bin, err := elf.Open(buildLogis(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bin.Close() }()
	symbols, err := bin.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range symbols {
		if s.Name == "wp_viewporter_interface" && s.Section != elf.SHN_UNDEF {
			return
		}
	}
	t.Error("wp_viewporter_interface is not defined in the binary")
}
