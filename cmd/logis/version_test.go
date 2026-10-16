package main

import (
	"bytes"
	"debug/buildinfo"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/version"
)

// The version's single source is the git tag vX.Y.Z (internal/version): a
// build stamped through -X reports what it was given, any other build what
// the go command recorded of the module's version, and the PKGBUILD stamps
// `git describe` of the commit it packages. Only the changelog's headings
// name a number, and a tagged commit's changelog has the tag's section.

// versionNumber is a version number written out: MAJOR.MINOR.PATCH.
var versionNumber = regexp.MustCompile(`\b[0-9]+\.[0-9]+\.[0-9]+\b`)

// binaryVersion is the version the logis binary at path reports: its build
// info's module version (no test stamps a binary it shares).
func binaryVersion(t *testing.T, path string) string {
	t.Helper()
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return version.FromBuildInfo(bi)
}

// versionOf runs bin --version and returns its stdout, failing t unless it
// exits 0 with nothing on stderr.
func versionOf(t *testing.T, bin string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, "--version")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		t.Fatalf("%s --version: %v, stderr %q", bin, err, stderr.String())
	}
	return stdout.String()
}

// The PKGBUILD's way: -X stamps the version the binary reports.
func TestAStampedBuildReportsTheStampedVersion(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "logis")
	const stamped = "0.1.1-3-gabc1234"
	cmd := exec.Command("go", "build", "-o", bin,
		"-ldflags", "-X github.com/nerdwave-nick/logis/internal/version.Version="+stamped, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if got := versionOf(t, bin); got != "logis "+stamped+"\n" {
		t.Errorf("--version printed %q, want %q", got, "logis "+stamped+"\n")
	}
}

// A plain `go build` reports the module version the go command recorded in
// it ("dev" where it recorded none, as with -buildvcs=false).
func TestAPlainBuildReportsTheModuleVersionItsBuildRecorded(t *testing.T) {
	bin := buildLogis(t)
	want := "logis " + binaryVersion(t, bin) + "\n"
	if got := versionOf(t, bin); got != want {
		t.Errorf("--version printed %q, want %q", got, want)
	}
}

// git runs git in the repository's root and returns its trimmed output.
func git(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoRoot(t)}, args...)...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// releaseTag is the vX.Y.Z tag at HEAD, skipping t where there is none: no
// git, no .git (makepkg's exported tree), or an untagged commit.
func releaseTag(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	if _, err := os.Stat(filepath.Join(repoRoot(t), ".git")); err != nil {
		t.Skip("not a git checkout")
	}
	tag, err := git(t, "describe", "--tags", "--exact-match", "--match", "v[0-9]*", "HEAD")
	if err != nil {
		t.Skip("HEAD is not at a release tag")
	}
	return tag
}

// A clean checkout at a release tag builds a binary that names the tag, as
// `go install …@vX.Y.Z` does.
func TestAPlainBuildAtAReleaseTagReportsTheTag(t *testing.T) {
	tag := releaseTag(t)
	if strings.Contains(os.Getenv("GOFLAGS"), "-buildvcs=false") {
		t.Skip("GOFLAGS turns VCS stamping off")
	}
	if status, err := git(t, "status", "--porcelain"); err != nil || status != "" {
		t.Skipf("the checkout is not clean (go build reports +dirty): %v %s", err, status)
	}
	if got, want := versionOf(t, buildLogis(t)), "logis "+strings.TrimPrefix(tag, "v")+"\n"; got != want {
		t.Errorf("--version printed %q, want %q", got, want)
	}
}

// A release's changelog has its section: CI runs this on the tag's push.
func TestTheChangelogHasASectionForTheReleaseTagAtHead(t *testing.T) {
	tag := releaseTag(t)
	heading := "\n## [" + strings.TrimPrefix(tag, "v") + "] - "
	if !strings.Contains(readRepo(t, "CHANGELOG.md"), heading) {
		t.Errorf("CHANGELOG.md has no %q section for the tag %s", strings.TrimSpace(heading), tag)
	}
}

// No file of the tree but the changelog names a version number: not
// internal/version (Version has no value of its own), the PKGBUILD's code
// (pkgver is computed) or the man page's title (the package adds it).
func TestNoFileButTheChangelogNamesTheVersion(t *testing.T) {
	source := readRepo(t, "internal/version/version.go")
	f, err := parser.ParseFile(token.NewFileSet(), "version.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if name.Name == "Version" && i < len(n.Values) {
					t.Errorf("version.go gives Version a value of its own")
				}
			}
		case *ast.BasicLit:
			if versionNumber.MatchString(n.Value) {
				t.Errorf("version.go names a version: %s", n.Value)
			}
		}
		return true
	})
	for _, v := range versionNumber.FindAllString(pkgbuildCode(t), -1) {
		if v != "0.0.0" { // the base before the first release tag
			t.Errorf("the PKGBUILD's code names a version: %s", v)
		}
	}
	if strings.Contains(pkgbuildCode(t), "\npkgver() {") {
		t.Error("the PKGBUILD has a pkgver() function, which makepkg writes back into it")
	}
	for line := range strings.Lines(readRepo(t, "docs/man/logis.1")) {
		if strings.HasPrefix(line, ".TH ") && versionNumber.MatchString(line) {
			t.Errorf("the man page's title names a version: %s", line)
		}
	}
}

// The package stamps the version git describes into the binary and the man
// page's title.
func TestThePackageStampsTheDescribedVersion(t *testing.T) {
	code := pkgbuildCode(t)
	if !strings.Contains(code, "describe --tags --match 'v[0-9]*'") {
		t.Error("the PKGBUILD does not take its version from git describe")
	}
	if b := pkgbuildFunction(t, "build"); !strings.Contains(b, "-X github.com/nerdwave-nick/logis/internal/version.Version=$_version") {
		t.Errorf("build() does not stamp the version:\n%s", b)
	}
	if p := pkgbuildFunction(t, "package"); !strings.Contains(p, `logis $_version`) {
		t.Errorf("package() does not put the version in the man page's title:\n%s", p)
	}
}

// The PKGBUILD's versions in a scratch checkout: before the first release
// tag, at one, after one and with uncommitted changes (which the package
// does not build, so they do not show), and vercmp orders them as releases
// and the commits between them follow each other.
func TestThePkgbuildTakesItsVersionFromTheCheckout(t *testing.T) {
	for _, tool := range []string{"git", "bash"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s", tool)
		}
	}
	repo := t.TempDir()
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir, cmd.Env = repo, env
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s %q: %v", name, args, err)
		}
		return strings.TrimSpace(string(out))
	}
	arch := filepath.Join(repo, "packaging", "arch")
	if err := os.MkdirAll(arch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(arch, "PKGBUILD"), []byte(readRepo(t, "packaging/arch/PKGBUILD")), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := func(message string) string {
		t.Helper()
		run("git", "add", "-A")
		run("git", "commit", "-q", "--allow-empty", "-m", message)
		return run("git", "rev-parse", "--short", "HEAD")
	}
	versions := func() string {
		t.Helper()
		return run("bash", "-c", `startdir=$1; source "$1/PKGBUILD"; printf '%s %s' "$_version" "$pkgver"`, "bash", arch)
	}
	run("git", "init", "-q")

	first := commit("first")
	before := versions()
	if want := "0.0.0-1-g" + first + " 0.0.0.r1.g" + first; before != want {
		t.Errorf("before a release tag: %q, want %q", before, want)
	}
	run("git", "tag", "v0.1.1")
	run("git", "tag", "nightly") // not a release tag: never described
	if got := versions(); got != "0.1.1 0.1.1" {
		t.Errorf("at v0.1.1: %q, want %q", got, "0.1.1 0.1.1")
	}
	commit("second")
	third := commit("third")
	after := versions()
	if want := "0.1.1-2-g" + third + " 0.1.1.r2.g" + third; after != want {
		t.Errorf("two commits after v0.1.1: %q, want %q", after, want)
	}
	if err := os.WriteFile(filepath.Join(repo, "uncommitted"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := versions(); got != after {
		t.Errorf("with uncommitted changes: %q, want %q (HEAD is what is built)", got, after)
	}

	if t.Failed() {
		return
	}
	if _, err := exec.LookPath("vercmp"); err != nil {
		t.Log("no vercmp: the order of pkgver is not checked")
		return
	}
	order := []string{strings.Fields(before)[1], "0.1.1", strings.Fields(after)[1], "0.1.2"}
	for i := 1; i < len(order); i++ {
		if got := run("vercmp", order[i-1], order[i]); got != "-1" {
			t.Errorf("vercmp %s %s = %s, want -1", order[i-1], order[i], got)
		}
	}
}
