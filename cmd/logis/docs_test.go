package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The caller contract stays pinned: every promise of the README's "Caller
// contract" section names the Go tests that pin it, as
// `path_test.go::TestName`, and each exists as a top-level
// `func TestX(t *testing.T)` in the file named, so no promise is left
// without a test, and a renamed or deleted test cannot leave one without
// its proof. Only code spans of that form count as references.

// contractTests is the least number of distinct Go tests the caller
// contract names (135 when this floor was set): a section that lost its
// names would otherwise pass with nothing to check.
const contractTests = 130

// contractSection is the README's section under `## Caller contract`, up
// to the next `## `.
func contractSection(t *testing.T, readme string) string {
	t.Helper()
	start := strings.Index(readme, "\n## Caller contract\n")
	if start < 0 {
		t.Fatal("README.md has no '## Caller contract' section")
	}
	body := readme[start+1:]
	if end := strings.Index(body[3:], "\n## "); end >= 0 {
		return body[:end+3]
	}
	return body
}

// referenceWord reports whether s is a path or name of a reference: ASCII
// letters, digits, '_' and '/'.
func referenceWord(s string) bool {
	if s == "" {
		return false
	}
	for _, b := range []byte(s) {
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '/') {
			return false
		}
	}
	return true
}

// declaresTest reports whether the Go source declares name as a top-level
// test function, `func name(t *testing.T)`.
func declaresTest(t *testing.T, file, source, name string) bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != name || !strings.HasPrefix(name, "Test") {
			continue
		}
		params := fn.Type.Params.List
		if len(params) != 1 || len(params[0].Names) > 1 || fn.Type.Results != nil {
			continue
		}
		star, ok := params[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "T" {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "testing" {
				return true
			}
		}
	}
	return false
}

func TestATestCountsOnlyAsATopLevelTestFunction(t *testing.T) {
	source := "package p\nimport \"testing\"\n" +
		"func TestPinned(t *testing.T) {}\n" +
		"func helper(t *testing.T) {}\n" +
		"func TestPinnedTooLong(t *testing.T) {}\n" +
		"func (r *rig) TestMethod(t *testing.T) {}\n" +
		"func TestBenchmarkLike(b *testing.B) {}\n" +
		"func TestNoT() {}\n"
	for name, want := range map[string]bool{
		"TestPinned": true, "TestPinnedTooLong": true, "helper": false, "TestPinnedToo": false,
		"TestMethod": false, "TestBenchmarkLike": false, "TestNoT": false,
	} {
		if got := declaresTest(t, "p_test.go", source, name); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}

// goReferences are the `path_test.go::TestName` written inside backticks,
// in order, each once.
func goReferences(text string) []string {
	var refs []string
	spans := strings.Split(text, "`")
	for i := 1; i < len(spans); i += 2 {
		path, name, ok := strings.Cut(spans[i], "_test.go::")
		if !ok || !referenceWord(path) || !referenceWord(name) || !strings.HasPrefix(name, "Test") {
			continue
		}
		if ref := path + "_test.go::" + name; !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	return refs
}

func TestGoReferencesAreReadFromCodeSpansOnly(t *testing.T) {
	text := "see `cmd/logis/a_test.go::TestOne`, `internal/x/y_test.go::TestTwo` and b_test.go::TestThree; " +
		"`docs/a.md::one`, `cmd/logis/a_test.go::TestOne` again"
	want := []string{"cmd/logis/a_test.go::TestOne", "internal/x/y_test.go::TestTwo"}
	if got := goReferences(text); !slices.Equal(got, want) {
		t.Errorf("goReferences %q, want %q", got, want)
	}
}

// Every Go test the caller contract names exists.
func TestEveryGoTestTheCallerContractNamesExists(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	refs := goReferences(contractSection(t, string(readme)))
	if len(refs) < contractTests {
		t.Fatalf("the caller contract names only %d Go tests, want at least %d", len(refs), contractTests)
	}
	for _, ref := range refs {
		file, name, _ := strings.Cut(ref, "::")
		source, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Errorf("%s: %v", ref, err)
			continue
		}
		if !declaresTest(t, file, string(source), name) {
			t.Errorf("no such Go test %s", ref)
		}
	}
}
