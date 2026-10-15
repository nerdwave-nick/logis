package egl

import (
	"math"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/nerdwave-nick/logis/internal/gles"
)

// surfaceless is an offscreen context for the test, on the test's locked
// thread (a GL context is current on one thread only). Without an EGL
// driver the test is skipped, unless LOGIS_REQUIRE_EGL is set (CI, which
// installs Mesa's llvmpipe, sets it so the GL tests cannot skip unseen).
func surfaceless(t *testing.T) *Context {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	c, err := NewSurfaceless()
	if err != nil {
		if os.Getenv("LOGIS_REQUIRE_EGL") != "" {
			t.Fatalf("LOGIS_REQUIRE_EGL is set, but: %v", err)
		}
		t.Skipf("no surfaceless EGL here: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestASurfacelessContextIsOpenGLES3(t *testing.T) {
	c := surfaceless(t)
	info := c.Info()
	t.Logf("%s / %s / %s", info.Vendor, info.Renderer, info.Version)
	if !strings.HasPrefix(info.Version, "OpenGL ES 3.") {
		t.Errorf("GL_VERSION %q, want OpenGL ES 3", info.Version)
	}
	if info.Renderer == "" {
		t.Error("no GL_RENDERER")
	}
}

func TestASurfacelessContextClearsAnOffscreenTarget(t *testing.T) {
	surfaceless(t)
	target := gles.NewTarget(4, 2)
	defer target.Delete()
	gles.Viewport(0, 0, 4, 2)
	gles.ClearColor(1, 0.5, 0, 1)
	gles.Clear()
	// A scissored clear paints only its rectangle (window frames use it).
	gles.Scissor(0, 0, 1, 1)
	gles.ClearColor(0, 0, 1, 1)
	gles.Clear()
	gles.NoScissor()
	px := gles.ReadPixels(0, 0, 4, 2)
	if len(px) != 4*2*4 {
		t.Fatalf("%d bytes read", len(px))
	}
	want := func(x, y int, rgba [4]byte) {
		t.Helper()
		i := (y*4 + x) * 4
		if got := [4]byte(px[i : i+4]); got != rgba {
			t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, rgba)
		}
	}
	// The scissored pixel holds only exactly representable values.
	want(0, 0, [4]byte{0, 0, 255, 255})
	// The cleared ones hold 0.5, which no 8-bit value is: either neighbour
	// of 127.5 is a conforming answer.
	for _, p := range [][2]int{{1, 0}, {3, 1}} {
		i := (p[1]*4 + p[0]) * 4
		got := [4]byte(px[i : i+4])
		for k, f := range [4]float32{1, 0.5, 0, 1} {
			if !unorm8(f, got[k]) {
				t.Errorf("pixel %v = %v, want (1, 0.5, 0, 1) as 8-bit values", p, got)
				break
			}
		}
	}
}

// unorm8 reports whether b is a conforming 8-bit unsigned normalized value
// for f. OpenGL ES converts a float to normalized fixed point by scaling it
// to [0, 255] and taking "one of the two" integers closest to the result,
// round to nearest preferred, not required: a value an 8-bit integer
// cannot hold may become either neighbour, and for 0.5 (127.5) drivers
// differ (Mesa writes 128, NVIDIA 127). A value it can hold stays exact.
func unorm8(f float32, b byte) bool {
	return math.Abs(float64(b)-float64(f)*255) < 1
}

func TestAClearValueHalfwayBetweenTwoBytesMayBecomeEither(t *testing.T) {
	for _, tc := range []struct {
		f    float32
		b    byte
		want bool
	}{
		{0.5, 127, true}, {0.5, 128, true}, {0.5, 126, false}, {0.5, 129, false},
		{1, 255, true}, {1, 254, false},
		{0, 0, true}, {0, 1, false},
		{0.25, 63, true}, {0.25, 64, true}, {0.25, 62, false}, {0.25, 65, false},
	} {
		if got := unorm8(tc.f, tc.b); got != tc.want {
			t.Errorf("unorm8(%g, %d) = %v, want %v", tc.f, tc.b, got, tc.want)
		}
	}
}

func TestEGLErrorsAreNamed(t *testing.T) {
	for code, want := range map[int]string{
		0x3003: "EGL_BAD_ALLOC",
		0x300B: "EGL_BAD_NATIVE_WINDOW",
		0x1234: "EGL error 0x1234",
	} {
		if got := errorName(code); got != want {
			t.Errorf("errorName(%#x) = %q, want %q", code, got, want)
		}
	}
}
