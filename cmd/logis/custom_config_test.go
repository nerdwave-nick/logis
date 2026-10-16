package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/render/rendertest"
)

// --check-config compiles the custom shader (a5e5f876) with a headless
// context and reports what the driver refuses at the user's lines. The
// shaders are written for these tests.

const mendedShader = `// a soft amber tint, for logis's tests
void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	vec4 c = texture(iChannel0, fragCoord / iResolution.xy);
	fragColor = vec4(c.rgb * vec3(1.0, 0.85, 0.6), c.a);
}
`

func TestCheckConfigCompilesTheCustomShader(t *testing.T) {
	rendertest.HeadlessForStructure(t) // skips without EGL
	scratchConfigHome(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "logis.jsonc")
	shader := filepath.Join(dir, "shaders", "amber.frag")
	writeFile(t, path, `{"custom-shader": "shaders/amber.frag"}`)
	writeFile(t, shader, mendedShader)
	code, stdout, stderr := runLogis("--check-config", "--config", path)
	want := "logis: the config at " + path + " is sound; the theme 'forge-crimson-brass' is sound; " +
		"the custom shader " + shader + " is sound\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	// A mistake on line 4 is reported there, not in logis's prelude.
	writeFile(t, shader, strings.Replace(mendedShader, "c.rgb *", "c.rgb * glow *", 1))
	code, stdout, stderr = runLogis("--check-config", "--config", path)
	if code != 2 || stdout != "" ||
		!strings.HasPrefix(stderr, "logis: the custom shader is heretical: "+shader+":4:") ||
		!strings.Contains(stderr, "glow") {
		t.Errorf("exit %d stdout %q stderr %q", code, stdout, stderr)
	}
	// A theme's shader is checked too; the file it cannot find is said.
	writeFile(t, path, `{"theme": "mine"}`)
	writeFile(t, filepath.Join(dir, "themes", "mine.jsonc"), `{"effects": {"custom-shader": "gone.frag"}}`)
	code, _, stderr = runLogis("--check-config", "--config", path)
	gone := filepath.Join(dir, "themes", "gone.frag")
	if code != 2 || stderr != "logis: the custom shader is heretical: "+gone+": No such file or directory (os error 2)\n" {
		t.Errorf("exit %d stderr %q", code, stderr)
	}
}
