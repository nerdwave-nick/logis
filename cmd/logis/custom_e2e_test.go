package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nerdwave-nick/logis/internal/e2e"
)

// The custom pass end to end (a5e5f876): a theme's shader that does not
// compile is said once in the daemon's log and the panel shows with the
// built-in effects; mended, it draws on the screen, and keeps the window
// drawing unless motion is reduced. Runs only with LOGIS_E2E=niri|sway.
// The shaders are written for this test.

const (
	shadedTheme = `{ "extends": "forge-crimson-brass", "effects": { "custom-shader": "shaders/red.frag" } }`
	// redBroken has its mistake on line 2.
	redBroken = "void mainImage(out vec4 fragColor, in vec2 fragCoord) {\n" +
		"\tfragColor = vec4(0.8, 0.0, 0.0, 1.0) * glow;\n}\n"
	// red paints the whole surface opaque red.
	red = "void mainImage(out vec4 fragColor, in vec2 fragCoord) {\n" +
		"\tfragColor = vec4(0.8, 0.0, 0.0, 1.0);\n}\n"
)

func TestE2EACustomShaderIsSkippedOnceWhenBrokenAndDrawsWhenMended(t *testing.T) {
	c := e2e.StartCogitator(t, nil, `{ "reduce-motion": true, "theme": "shaded" }`)
	dir := themesDir(t, c)
	shader := filepath.Join(dir, "shaders", "red.frag")
	if err := os.MkdirAll(filepath.Dir(shader), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTheme(t, shader, redBroken)
	writeTheme(t, filepath.Join(dir, "shaded.jsonc"), shadedTheme)
	c.Daemon.WaitFor("the theme the config names", e2e.Is("theme shaded"))
	for range 2 {
		client := c.DmenuAll(dmenuItems)
		c.Shown(3)
		c.OpenSettled()
		_ = client.Kill()
		client.Ended()
	}
	heresy := "logis: the custom shader is heretical: " + shader + ":2:"
	if log := c.Daemon.Stderr(); strings.Count(log, "the custom shader is heretical") != 1 || !strings.Contains(log, heresy) ||
		!strings.Contains(log, "; it is skipped, the built-in effects still draw") {
		t.Fatalf("the daemon's log:\n%s", log)
	}

	// Mended (the theme is saved again, so it is read with its shader):
	// the shader paints the panel's surface, and with motion reduced the
	// window rests.
	writeTheme(t, shader, red)
	writeTheme(t, filepath.Join(dir, "shaded.jsonc"), shadedTheme+"\n")
	c.Daemon.WaitFor("the mended theme", e2e.Is("theme shaded"))
	client := c.DmenuAll(dmenuItems)
	c.Shown(3)
	c.OpenSettled()
	capture := c.Harness.SettledCapture()
	if px := capture.Pixel(capture.Width/2, capture.Height/2); px != [4]byte{204, 0, 0, 255} {
		t.Errorf("the panel's centre is %v, not the shader's red", px)
	}
	time.Sleep(200 * time.Millisecond) // past a frame in flight
	if still := commitsFor(c, 500*time.Millisecond); still != 0 {
		t.Errorf("with motion reduced the custom pass kept drawing: %d frames", still)
	}
	_ = client.Kill()
	client.Ended()

	// Motion allowed: the custom pass keeps the shown window drawing.
	writeTheme(t, filepath.Join(c.Compositor.ConfigHome(), "logis", "logis.jsonc"), `{ "reduce-motion": false, "theme": "shaded" }`)
	c.Daemon.WaitFor("the amended config", e2e.Is("theme shaded"))
	client = c.DmenuAll(dmenuItems)
	c.Shown(3)
	if frames := commitsFor(c, 500*time.Millisecond); frames < 10 {
		t.Errorf("the custom pass let the window rest: %d frames in 0.5 s", frames)
	}
	_ = client.Kill()
	if strings.Count(c.Daemon.Stderr(), "the custom shader is heretical") != 1 {
		t.Errorf("the mended shader was refused:\n%s", c.Daemon.Stderr())
	}
}
