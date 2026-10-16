package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/custom"
	"github.com/nerdwave-nick/logis/internal/effects"
	"github.com/nerdwave-nick/logis/internal/render"
	"github.com/nerdwave-nick/logis/internal/render/rendertest"
	"github.com/nerdwave-nick/logis/internal/scale"
)

// The custom pass (feat/logis-go a5e5f876, new beside Rust v1): a
// theme's or the config's shader, written as for Shadertoy, runs after
// the built-in effects over the whole frame. Every shader here is written
// for these tests.

// identity hands the frame on as it came: rows bottom-up as fragCoord's,
// premultiplied alpha kept.
const identity = `void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	fragColor = texture(iChannel0, fragCoord / iResolution.xy);
}
`

func shader(source string) custom.Shader {
	return custom.Shader{Path: "/test/custom.frag", Source: source}
}

// drawn is sc drawn with post at scale one.
func drawn(t *testing.T, h *render.Headless, sc *render.Scene, logical scale.Size, post render.Post) *render.Image {
	t.Helper()
	sc.Post = post
	img, err := h.Offscreen(sc, logical, scale.One)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestAnIdentityCustomPassChangesNothing(t *testing.T) {
	h := rendertest.Headless(t)
	sc, logical := panelFrame(t, h, "forge-crimson-brass", scale.One)
	crt := sc.Post
	for name, base := range map[string]render.Post{
		"alone":         {Rect: crt.Rect, Radius: crt.Radius, Inset: crt.Inset},
		"after the CRT": crt,
	} {
		want := drawn(t, h, sc, logical, base)
		with := base
		with.Custom = shader(identity)
		if !with.Any() {
			t.Errorf("%s: a custom shader asks for no pass", name)
		}
		if got := drawn(t, h, sc, logical, with); !bytes.Equal(got.Pixels, want.Pixels) {
			t.Errorf("%s: the identity changed the frame", name)
		}
	}
}

func TestTheCustomPassCountsRowsFromTheBottomAsShadertoyDoes(t *testing.T) {
	h := rendertest.Headless(t)
	sc := postScene(render.Post{})
	img := drawn(t, h, sc, postLogical, render.Post{Custom: shader(`void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	fragColor = fragCoord.y < 10.0 ? vec4(1.0, 0.0, 0.0, 1.0) : vec4(0.0, 0.0, 1.0, 1.0);
}`)})
	if bottom, top := img.Pixel(5, img.Height-1), img.Pixel(5, 0); bottom != [4]byte{255, 0, 0, 255} || top != [4]byte{0, 0, 255, 255} {
		t.Errorf("bottom row %v, top row %v", bottom, top)
	}
}

// inputs writes the pass's inputs into the colour: iTime/10, iFrame/100,
// iGlitch, and alpha 1 only when iResolution is the frame's buffer size.
const inputs = `void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	bool sized = iResolution == vec3(240.0, 180.0, 1.0);
	fragColor = vec4(iTime / 10.0, float(iFrame) / 100.0, iGlitch, sized ? 1.0 : 0.5);
}
`

func TestTheCustomPassTakesTimeFrameGlitchAndResolution(t *testing.T) {
	h := rendertest.Headless(t)
	sc := postScene(render.Post{})
	g := effects.GlitchDefaults()
	g.Strength, g.Episodes.Duty = 0.5, 0
	post := render.Post{Custom: shader(inputs), Moment: effects.Moment{Time: 5, Event: 1}, Frame: 60, Glitch: g}
	img, err := h.Offscreen(&render.Scene{Clear: sc.Clear, Post: post}, postLogical, scale.FromInteger(2))
	if err != nil {
		t.Fatal(err)
	}
	near := func(got byte, want float32) bool { return float32(got) >= want*255-1 && float32(got) <= want*255+1 }
	px := img.Pixel(30, 30)
	if !near(px[0], 0.5) || !near(px[1], 0.6) || !near(px[2], 0.5) || px[3] != 255 {
		t.Errorf("time, frame, glitch, size: %v", px)
	}
	post.Custom.Frozen = true
	img, err = h.Offscreen(&render.Scene{Clear: sc.Clear, Post: post}, postLogical, scale.FromInteger(2))
	if err != nil {
		t.Fatal(err)
	}
	if px := img.Pixel(30, 30); px[0] != 0 || !near(px[1], 0.6) {
		t.Errorf("frozen: the time is not 0: %v", px)
	}
}

// broken has a mistake on its line 3.
const broken = `void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	vec2 uv = fragCoord / iResolution.xy;
	fragColor = texture(iChannel0, uw);
}
`

func TestABrokenCustomShaderIsSkippedWithOneLoggedError(t *testing.T) {
	h := rendertest.Headless(t)
	var logged []string
	h.SetLog(func(m string) { logged = append(logged, m) })
	sc, logical := panelFrame(t, h, "forge-crimson-brass", scale.One)
	crt := sc.Post
	want := drawn(t, h, sc, logical, crt)
	for _, c := range []struct {
		name  string
		bad   custom.Shader
		place string
	}{
		{"does not compile", shader(broken), "/test/custom.frag:3:"},
		{"cannot be read", custom.Shader{Path: "/test/gone.frag", Problem: "No such file or directory (os error 2)"},
			"/test/gone.frag: No such file or directory (os error 2)"},
		{"defines no mainImage", shader("float glow(float x) { return x; }\n"), "logis's main"},
	} {
		// No subtests: the context is the test goroutine's.
		logged = nil
		post := crt
		post.Custom = c.bad
		for range 3 {
			if got := drawn(t, h, sc, logical, post); !bytes.Equal(got.Pixels, want.Pixels) {
				t.Fatalf("%s: the built-in effects did not draw as without the custom pass", c.name)
			}
		}
		if len(logged) != 1 {
			t.Fatalf("%s: %d log lines: %q", c.name, len(logged), logged)
		}
		if !strings.HasPrefix(logged[0], "the custom shader is heretical: ") ||
			!strings.Contains(logged[0], c.place) ||
			!strings.HasSuffix(logged[0], "; it is skipped, the built-in effects still draw") {
			t.Errorf("%s: %q", c.name, logged[0])
		}
	}
	// Mended, it runs.
	logged = nil
	post := crt
	post.Custom = shader(inputs)
	if got := drawn(t, h, sc, logical, post); bytes.Equal(got.Pixels, want.Pixels) || len(logged) != 0 {
		t.Errorf("the mended shader did not run (log %q)", logged)
	}
}

func TestCheckCustomCompilesInTheUsersLines(t *testing.T) {
	rendertest.HeadlessForStructure(t) // skips without EGL
	if err := render.CheckCustom(shader(identity)); err != nil {
		t.Errorf("the identity: %v", err)
	}
	err := render.CheckCustom(shader(broken))
	if err == nil || !strings.HasPrefix(err.Error(), "/test/custom.frag:3:") {
		t.Errorf("%v", err)
	}
}
