package main

import (
	"debug/elf"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
// linked into the binary: the interface table of every interface of every
// protocol in internal/wlproto/protocols but the core one, whose tables
// libwayland-client defines.
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
	defined := map[string]bool{}
	for _, s := range symbols {
		if s.Section != elf.SHN_UNDEF {
			defined[s.Name] = true
		}
	}
	xmls, err := filepath.Glob("../../internal/wlproto/protocols/*.xml")
	if err != nil || len(xmls) == 0 {
		t.Fatalf("no protocol XML files: %v", err)
	}
	tables := 0
	for _, path := range xmls {
		if filepath.Base(path) == "wayland.xml" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range regexp.MustCompile(`<interface name="(\w+)"`).FindAllStringSubmatch(string(data), -1) {
			tables++
			if !defined[m[1]+"_interface"] {
				t.Errorf("%s_interface (%s) is not defined in the binary", m[1], filepath.Base(path))
			}
		}
	}
	if !defined["wp_viewporter_interface"] || tables < 2 {
		t.Errorf("checked %d tables; wp_viewporter_interface defined: %v", tables, defined["wp_viewporter_interface"])
	}
}

// libfontconfig and libfreetype are linked because the font and glyph
// bindings call them (D2, D4), not merely pinned: the binary imports the
// calls fonts are resolved and glyphs are rendered with.
func TestTheBinaryCallsFontconfigAndFreeType(t *testing.T) {
	bin, err := elf.Open(buildLogis(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bin.Close() }()
	imported, err := bin.ImportedSymbols()
	if err != nil {
		t.Fatal(err)
	}
	from := map[string]string{}
	for _, s := range imported {
		from[s.Name] = s.Library
	}
	for _, name := range []string{
		"FcInitLoadConfigAndFonts", "FcFontMatch", "FcFontSort", "FcCharSetHasChar",
		"FT_New_Memory_Face", "FT_Load_Glyph", "FT_Render_Glyph", "FT_Select_Size",
	} {
		if _, ok := from[name]; !ok {
			t.Errorf("the binary does not call %s", name)
		}
	}
}
