package custom

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The custom pass's shader as logis hands it to the driver, and the
// driver's refusals in the user's lines.

const tiny = "void mainImage(out vec4 fragColor, in vec2 fragCoord) {\n" +
	"\tfragColor = texture(iChannel0, fragCoord / iResolution.xy);\n" +
	"}\n"

// lineOf is the 1-based line of the fragment that holds needle.
func lineOf(t *testing.T, fragment, needle string) int {
	t.Helper()
	for i, l := range strings.Split(fragment, "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	t.Fatalf("%q is not in the fragment", needle)
	return 0
}

func TestTheFragmentWrapsTheSourceInThePrelude(t *testing.T) {
	f, err := Fragment("glow.frag", tiny)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f, "#version 300 es\n") {
		t.Errorf("the fragment does not start with its version:\n%s", f)
	}
	for _, u := range []string{
		"uniform highp sampler2D iChannel0;", "uniform vec3 iResolution;", "uniform float iTime;",
		"uniform int iFrame;", "uniform float iGlitch;", "mainImage(",
	} {
		if !strings.Contains(f, u) {
			t.Errorf("the fragment lacks %q", u)
		}
	}
	// The user's line 2 is the fragment's line PreludeLines+2.
	if got := lineOf(t, f, "fragColor = texture"); got != PreludeLines+2 {
		t.Errorf("the user's line 2 is the fragment's %d, want %d", got, PreludeLines+2)
	}
	// main comes after the user's last line, also without a final newline.
	g, _ := Fragment("glow.frag", strings.TrimSuffix(tiny, "\n"))
	if got := lineOf(t, g, "void main()"); got <= PreludeLines+3 {
		t.Errorf("main is at line %d, inside the user's source", got)
	}
}

func TestAVersionLineOfESThreeIsTakenAsTheOneLogisGives(t *testing.T) {
	for _, version := range []string{"#version 300 es", "  #version   300  es  "} {
		f, err := Fragment("glow.frag", version+"\nprecision mediump float;\n"+tiny)
		if err != nil {
			t.Fatalf("%q: %v", version, err)
		}
		if strings.Count(f, "#version") != 1 {
			t.Errorf("%q: two versions:\n%s", version, f)
		}
		// The lines still count from the user's first.
		if got := lineOf(t, f, "precision mediump"); got != PreludeLines+2 {
			t.Errorf("%q: the user's line 2 is the fragment's %d", version, got)
		}
	}
}

func TestAnotherVersionIsRefusedAtItsLine(t *testing.T) {
	_, err := Fragment("/t/glow.frag", "// a desktop shader\n#version 330 core\n"+tiny)
	if err == nil {
		t.Fatal("#version 330 core was taken")
	}
	want := "/t/glow.frag:2: `#version 330 core`: logis speaks GLSL ES 3.00 alone (#version 300 es, which it gives itself)"
	if err.Error() != want {
		t.Errorf("got  %s\nwant %s", err, want)
	}
}

func TestTheDriversWordsAreMovedToTheUsersLines(t *testing.T) {
	user := strings.Count(tiny, "\n")
	main := PreludeLines + user + 3
	for _, c := range []struct{ name, log, want string }{
		{
			"mesa",
			"0:" + itoa(PreludeLines+2) + "(14): error: `fragColour' undeclared\n" +
				"0:" + itoa(PreludeLines+2) + "(14): error: value of type vec4 cannot be assigned\n",
			"/t/g.frag:2:14: error: `fragColour' undeclared\n" +
				"/t/g.frag:2:14: error: value of type vec4 cannot be assigned",
		},
		{
			"angle and the like",
			"ERROR: 0:" + itoa(PreludeLines+1) + ": 'mainImag' : syntax error\nERROR: 1 compilation errors.  No code generated.\n",
			"/t/g.frag:1: error: 'mainImag' : syntax error\n/t/g.frag: ERROR: 1 compilation errors.  No code generated.",
		},
		{
			"nvidia",
			"0(" + itoa(PreludeLines+3) + ") : error C0000: syntax error, unexpected '}'\n",
			"/t/g.frag:3: error C0000: syntax error, unexpected '}'",
		},
		{
			"in logis's main",
			"0:" + itoa(main) + "(2): error: no function with name 'mainImage'\n",
			"/t/g.frag: error: no function with name 'mainImage' (in logis's main, which calls " +
				"mainImage(out vec4 fragColor, in vec2 fragCoord))",
		},
		{
			"in the prelude",
			"0:3(1): error: something of logis's own\n",
			"/t/g.frag: error: something of logis's own (in logis's prelude)",
		},
		{
			"a link error without a line",
			"error: fragment shader lacks `main'\n\n",
			"/t/g.frag: error: fragment shader lacks `main'",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := Explain("/t/g.frag", tiny, c.log)
			if err == nil || err.Error() != c.want {
				t.Errorf("got\n%v\nwant\n%s", err, c.want)
			}
		})
	}
}

func TestAnEmptyLogStillSaysTheShaderWasRefused(t *testing.T) {
	if err := Explain("/t/g.frag", tiny, " \n"); err == nil || err.Error() != "/t/g.frag: the machine spirit refused it without a word" {
		t.Errorf("%v", err)
	}
}

func TestLoadReadsTheFileOrSaysWhyNot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glow.frag")
	if err := os.WriteFile(path, []byte(tiny), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if !s.Set() || s.Path != path || s.Source != tiny || s.Problem != "" {
		t.Errorf("%+v", s)
	}
	missing := Load(filepath.Join(dir, "gone.frag"))
	if !missing.Set() || missing.Source != "" || missing.Problem != "No such file or directory (os error 2)" {
		t.Errorf("%+v", missing)
	}
	if err := missing.Readable(); err == nil || err.Error() != filepath.Join(dir, "gone.frag")+": No such file or directory (os error 2)" {
		t.Errorf("%v", err)
	}
	if (Shader{}).Set() {
		t.Error("the zero shader is set")
	}
}

func TestAPathIsTakenFromItsFilesDirectory(t *testing.T) {
	for _, c := range []struct{ dir, path, want string }{
		{"/home/u/.config/logis/themes", "glow.frag", "/home/u/.config/logis/themes/glow.frag"},
		{"/home/u/.config/logis", "shaders/../glow.frag", "/home/u/.config/logis/glow.frag"},
		{"/home/u/.config/logis", "/opt/s/glow.frag", "/opt/s/glow.frag"},
		{"/home/u/.config/logis", "", ""},
	} {
		if got := Beside(c.dir, c.path); got != c.want {
			t.Errorf("Beside(%q, %q) = %q, want %q", c.dir, c.path, got, c.want)
		}
	}
}

func TestHeresyIsLogisVoice(t *testing.T) {
	err := Explain("/t/g.frag", tiny, "0:"+itoa(PreludeLines+1)+"(1): error: bad\n")
	if got, want := Heresy(err), "the custom shader is heretical: /t/g.frag:1:1: error: bad"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestALongLogIsCutShort(t *testing.T) {
	var log strings.Builder
	for i := range 10 {
		log.WriteString("0:" + itoa(PreludeLines+1) + "(" + itoa(i+1) + "): error: candidate\n")
	}
	got := Explain("/t/g.frag", tiny, log.String()).Error()
	lines := strings.Split(got, "\n")
	if len(lines) != MaxProblems+1 || lines[MaxProblems] != "/t/g.frag: … and 4 more" {
		t.Errorf("%d lines:\n%s", len(lines), got)
	}
}
