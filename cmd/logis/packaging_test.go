package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The package stays consistent with the repository: the PKGBUILD
// (packaging/arch/) takes the version from git, as the binary and the man
// page do (version_test.go), the package installs everything the
// repository ships for it, and it never enables the systemd units (spec §3:
// shipped, not enabled). It builds cmd/logis with go and cgo, and depends on
// the package of every C library the binary links (link_test.go's
// specLibraries) and on an OpenGL driver.

// readRepo reads a file relative to the repository root.
func readRepo(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	return string(data)
}

// pkgbuildCode is the PKGBUILD without its comment lines.
func pkgbuildCode(t *testing.T) string {
	t.Helper()
	var code []string
	for line := range strings.Lines(readRepo(t, "packaging/arch/PKGBUILD")) {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			code = append(code, line)
		}
	}
	return strings.Join(code, "")
}

// pkgbuildArray is the single-quoted words of the PKGBUILD array
// `name=( ... )`, written on one line or one word per line up to a lone `)`.
func pkgbuildArray(t *testing.T, name string) []string {
	t.Helper()
	code := pkgbuildCode(t)
	_, body, ok := strings.Cut(code, "\n"+name+"=(")
	if !ok {
		t.Fatalf("the PKGBUILD has no %s", name)
	}
	end := ")"
	if strings.HasPrefix(body, "\n") {
		end = "\n)"
	}
	body, _, ok = strings.Cut(body, end)
	if !ok {
		t.Fatalf("the PKGBUILD's %s is not closed", name)
	}
	var words []string
	for i, part := range strings.Split(body, "'") {
		if i%2 == 1 {
			words = append(words, part)
		}
	}
	return words
}

func sorted(words []string) []string {
	words = slices.Clone(words)
	slices.Sort(words)
	return words
}

func TestThePackageInstallsWhatTheRepositoryShips(t *testing.T) {
	code := pkgbuildCode(t)
	shipped := []string{"docs/man/logis.1", "LICENSE"}
	for _, dir := range []string{"contrib/completions", "contrib/systemd", "schema"} {
		entries, err := os.ReadDir(filepath.Join(repoRoot(t), filepath.FromSlash(dir)))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			shipped = append(shipped, dir+"/"+e.Name())
		}
	}
	var missing []string
	for _, f := range shipped {
		if !strings.Contains(code, f) {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		t.Errorf("the PKGBUILD does not install %q", missing)
	}
	for _, unit := range []string{"logis.service", "logis.socket"} {
		if !strings.Contains(code, "contrib/systemd/"+unit) {
			t.Errorf("the PKGBUILD does not install %s", unit)
		}
	}
	if !strings.Contains(code, "$pkgdir/usr/lib/systemd/user") {
		t.Errorf("the units do not go to /usr/lib/systemd/user:\n%s", code)
	}
}

func TestThePackageNeverEnablesTheUnits(t *testing.T) {
	code := pkgbuildCode(t)
	for _, word := range []string{"systemctl", ".wants", "install="} {
		if strings.Contains(code, word) {
			t.Errorf("the PKGBUILD uses %s", word)
		}
	}
}

func TestTheServiceSparesWhatTheDaemonStarted(t *testing.T) {
	service := readRepo(t, "contrib/systemd/logis.service")
	if !slices.Contains(strings.Split(service, "\n"), "KillMode=process") {
		t.Errorf("logis.service lacks KillMode=process:\n%s", service)
	}
}

// The package builds the Go binary, with cgo, hardened as Arch's Go package
// guidelines ask (makepkg's flags for the C parts, a PIE linked by the
// system linker, no build paths), from the module's own go.sum, and tests it
// without reaching the session that runs makepkg.
func TestThePackageBuildsTheGoBinaryWithCgo(t *testing.T) {
	code := pkgbuildCode(t)
	if got, want := sorted(pkgbuildArray(t, "makedepends")), []string{"git", "go", "pkgconf"}; !slices.Equal(got, want) {
		t.Errorf("makedepends = %q, want %q", got, want)
	}
	for _, word := range []string{
		"go build", "./cmd/logis", "go test",
		`CGO_CFLAGS="$CFLAGS"`, `CGO_CPPFLAGS="$CPPFLAGS"`, `CGO_LDFLAGS="$LDFLAGS"`,
		"-buildmode=pie", "-trimpath", "-linkmode=external", "-mod=readonly",
		"GOTOOLCHAIN=local", "-u WAYLAND_DISPLAY", "-u WAYLAND_SOCKET", "-u LOGIS_E2E",
	} {
		if !strings.Contains(code, word) {
			t.Errorf("the PKGBUILD lacks %s", word)
		}
	}
	if strings.Contains(code, "CGO_ENABLED=0") {
		t.Error("the PKGBUILD turns cgo off")
	}
}

// pkgbuildFunction is the body of the PKGBUILD's function name, without
// its comment lines.
func pkgbuildFunction(t *testing.T, name string) string {
	t.Helper()
	_, body, ok := strings.Cut(pkgbuildCode(t), "\n"+name+"() {\n")
	if !ok {
		t.Fatalf("the PKGBUILD has no %s()", name)
	}
	body, _, ok = strings.Cut(body, "\n}\n")
	if !ok {
		t.Fatalf("the PKGBUILD's %s() is not closed", name)
	}
	return body
}

// Wall-clock timing tests and GPU pixel tests are for CI and development:
// they never decide whether makepkg installs logis, so check() runs go test
// -short, which they skip.
func TestThePackageCheckLeavesTheTimingTestsOut(t *testing.T) {
	if check := pkgbuildFunction(t, "check"); !strings.Contains(check, "go test -short ./...") {
		t.Errorf("check() does not run go test -short ./...:\n%s", check)
	}
}

// archLibraryPackages names the Arch package that ships each C library the
// binary links (spec §4): libglvnd ships the libEGL and libGLESv2 the binary
// links, which dispatch to a vendor's driver.
var archLibraryPackages = map[string]string{
	"libEGL.so.1":            "libglvnd",
	"libGLESv2.so.2":         "libglvnd",
	"libfontconfig.so.1":     "fontconfig",
	"libfreetype.so.6":       "freetype2",
	"libwayland-client.so.0": "wayland",
	"libwayland-egl.so.1":    "wayland",
}

// The package depends on what the binary links, on glibc for libc and libm,
// and on opengl-driver, the EGL and OpenGL ES driver behind libglvnd:
// whichever vendor's package provides it (mesa, with llvmpipe where no GPU
// serves, or nvidia-utils), never mesa by name.
func TestThePackageDependsOnWhatTheBinaryLinks(t *testing.T) {
	want := []string{"glibc", "opengl-driver"}
	for _, lib := range specLibraries {
		pkg, ok := archLibraryPackages[lib]
		if !ok {
			t.Fatalf("no Arch package known for %s: add it to archLibraryPackages", lib)
		}
		if !slices.Contains(want, pkg) {
			want = append(want, pkg)
		}
	}
	if got := sorted(pkgbuildArray(t, "depends")); !slices.Equal(got, sorted(want)) {
		t.Errorf("depends = %q, want %q", got, sorted(want))
	}
}

// The package has no optional dependencies: nothing logis does needs a
// package beyond depends (servitor is a caller, not a dependency).
func TestThePackageHasNoOptionalDependencies(t *testing.T) {
	if strings.Contains(pkgbuildCode(t), "optdepends") {
		t.Error("the PKGBUILD has an optdepends array")
	}
}
