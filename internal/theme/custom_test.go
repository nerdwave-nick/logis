package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/config"
)

// The custom pass's shader (a5e5f876, new beside Rust v1): a theme names
// it under effects.custom-shader, a path from the theme's file; the
// config's custom-shader outranks it. A shader that cannot be read leaves
// the theme sound: the renderer says why once and skips it.

const glow = "void mainImage(out vec4 fragColor, in vec2 fragCoord) {\n" +
	"\tfragColor = texture(iChannel0, fragCoord / iResolution.xy);\n}\n"

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAThemesCustomShaderIsReadFromBesideIt(t *testing.T) {
	th, dir := themesDir(t, map[string]string{
		"mine":  `{"effects": {"custom-shader": "shaders/glow.frag"}}`,
		"other": `{"extends": "mine", "effects": {"scanlines": 0}}`,
	})
	path := filepath.Join(dir, "shaders", "glow.frag")
	writeFile(t, path, glow)
	for _, name := range []string{"mine", "other"} {
		c := load(t, th, name).Custom
		if c.Path != path || c.Source != glow || c.Problem != "" || c.Frozen {
			t.Errorf("%s: %+v", name, c)
		}
	}
	if c := load(t, Themes{}, "forge-crimson-brass").Custom; c.Set() {
		t.Errorf("a bundled theme has a custom shader: %+v", c)
	}
}

func TestAnAbsolutePathOrNoneAtAll(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "glow.frag")
	writeFile(t, abs, glow)
	th, _ := themesDir(t, map[string]string{
		"mine": `{"effects": {"custom-shader": "` + abs + `"}}`,
		"bare": `{"extends": "mine", "effects": {"custom-shader": ""}}`,
	})
	if c := load(t, th, "mine").Custom; c.Path != abs || c.Source != glow {
		t.Errorf("%+v", c)
	}
	if c := load(t, th, "bare").Custom; c.Set() {
		t.Errorf("an empty path keeps the shader: %+v", c)
	}
}

func TestAShaderThatCannotBeReadLeavesTheThemeSound(t *testing.T) {
	th, dir := themesDir(t, map[string]string{"mine": `{"effects": {"custom-shader": "gone.frag"}}`})
	c := load(t, th, "mine").Custom
	if c.Path != filepath.Join(dir, "gone.frag") || c.Problem != "No such file or directory (os error 2)" {
		t.Errorf("%+v", c)
	}
}

func TestACustomShaderThatIsNoPathIsRefusedAtItsPlace(t *testing.T) {
	th, _ := themesDir(t, map[string]string{"mine": `{"effects": {"custom-shader": 3}}`})
	if e := refusal(t, th, "mine"); !strings.HasSuffix(e.Source, "mine.jsonc:1:31") ||
		e.Problem != "invalid type: integer `3`, expected a string" {
		t.Errorf("%s: %s", e.Source, e.Problem)
	}
}

func TestACustomPassKeepsDrawingUntilMotionIsReduced(t *testing.T) {
	th, dir := themesDir(t, map[string]string{"mine": `{"extends": "forge-crimson-brass",
		"glow": {"pulse": 0}, "border": {"highlight": false},
		"effects": {"scanlines": 0, "aberration": 0, "grain": 0, "flicker": 0, "glitch": 0,
			"custom-shader": "glow.frag"}}`})
	writeFile(t, filepath.Join(dir, "glow.frag"), glow)
	mine := load(t, th, "mine")
	if !mine.IsContinuous() {
		t.Error("a custom pass does not keep the panel drawing")
	}
	mine.Custom.Path = ""
	if mine.IsContinuous() {
		t.Fatal("the theme moves without its custom pass")
	}
	still := load(t, th, "mine").Stilled()
	if !still.Custom.Frozen || !still.Custom.Set() || still.IsContinuous() {
		t.Errorf("stilled: %+v, continuous %v", still.Custom, still.IsContinuous())
	}
}

func TestTheConfigsCustomShaderOutranksTheThemes(t *testing.T) {
	th, dir := themesDir(t, map[string]string{"mine": `{"effects": {"custom-shader": "glow.frag"}}`})
	writeFile(t, filepath.Join(dir, "glow.frag"), glow)
	own := filepath.Join(t.TempDir(), "own.frag")
	writeFile(t, own, glow)
	c := named("mine")
	c.CustomShader = own
	live, err := NewLive(th, c)
	if err != nil || live.Theme().Custom.Path != own {
		t.Fatalf("%v %+v", err, live.Theme().Custom)
	}
	// An edited shader is read again with the config.
	writeFile(t, own, "// amended\n"+glow)
	if r, _ := live.Reload(c); r != config.Applied || !strings.HasPrefix(live.Theme().Custom.Source, "// amended") {
		t.Errorf("%v %+v", r, live.Theme().Custom)
	}
	c.ReduceMotion = true
	if r, _ := live.Reload(c); r != config.Applied || !live.Theme().Custom.Frozen || live.Theme().Custom.Path != own {
		t.Errorf("%v %+v", r, live.Theme().Custom)
	}
	c.CustomShader, c.ReduceMotion = "", false
	if r, _ := live.Reload(c); r != config.Applied || live.Theme().Custom.Path != filepath.Join(dir, "glow.frag") {
		t.Errorf("%v %+v", r, live.Theme().Custom)
	}
}
