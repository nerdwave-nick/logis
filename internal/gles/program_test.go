package gles_test

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/egl"
	"github.com/nerdwave-nick/logis/internal/gles"
)

// current makes an offscreen context current on the test's locked thread,
// skipping without EGL unless LOGIS_REQUIRE_EGL is set.
func current(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	c, err := egl.NewSurfaceless()
	if err != nil {
		if os.Getenv("LOGIS_REQUIRE_EGL") != "" {
			t.Fatalf("LOGIS_REQUIRE_EGL is set, but: %v", err)
		}
		t.Skipf("no surfaceless EGL here: %v", err)
	}
	t.Cleanup(c.Close)
}

const vertex = `#version 300 es
void main() { gl_Position = vec4(0.0); }
`

const fragment = `#version 300 es
precision highp float;
uniform vec2 u_size;
out vec4 o;
void main() { o = vec4(u_size, 0.0, 1.0); }
`

func TestAProgramLinksAndNamesItsUniforms(t *testing.T) {
	current(t)
	p, err := gles.NewProgram(vertex, fragment)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Delete()
	if loc := p.Uniform("u_size"); loc < 0 {
		t.Errorf("u_size at %d", loc)
	}
	if loc := p.Uniform("u_absent"); loc != -1 {
		t.Errorf("an absent uniform at %d, want -1", loc)
	}
	if err := gles.Err(); err != nil {
		t.Error(err)
	}
}

func TestAShaderTheDriverRefusesNamesItsStageAndLog(t *testing.T) {
	current(t)
	for _, tc := range []struct{ stage, vs, fs string }{
		{"vertex", "#version 300 es\nvoid main() { gl_Position = nonsense; }\n", fragment},
		{"fragment", vertex, "#version 300 es\nprecision highp float;\nout vec4 o;\nvoid main() { o = heresy; }\n"},
		// Each stage compiles, but the fragment shader reads a varying
		// the vertex shader never writes.
		{"link", vertex, "#version 300 es\nprecision highp float;\nin vec4 v_missing;\nout vec4 o;\nvoid main() { o = v_missing; }\n"},
	} {
		_, err := gles.NewProgram(tc.vs, tc.fs)
		var shaderErr *gles.ShaderError
		if !errors.As(err, &shaderErr) {
			t.Fatalf("%s: err %v, want a ShaderError", tc.stage, err)
		}
		if shaderErr.Stage != tc.stage || shaderErr.Log == "" {
			t.Errorf("stage %q log %q, want stage %q with the driver's log", shaderErr.Stage, shaderErr.Log, tc.stage)
		}
		if !strings.HasPrefix(err.Error(), "the "+tc.stage+" shader was refused by the machine spirit: ") {
			t.Errorf("message %q", err)
		}
	}
}
