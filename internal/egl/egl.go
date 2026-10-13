// Package egl is logis's own thin binding to EGL (spec §5, D1): one
// OpenGL ES 3 context on the EGL Wayland platform, woken when the daemon
// starts, and window surfaces on layer surfaces through libwayland-egl
// (wl_egl_window + eglSwapBuffers). NewSurfaceless gives the same kind of
// context without a window system, for offscreen renders and tests.
//
// A context is current on one thread: everything here runs on the
// window's locked owner goroutine, and is released explicitly (Close,
// Window.Destroy), never by finalizers.
package egl

/*
#cgo pkg-config: egl
#include <EGL/egl.h>
#include <EGL/eglext.h>

#ifndef EGL_PLATFORM_SURFACELESS_MESA
#define EGL_PLATFORM_SURFACELESS_MESA 0x31DD
#endif

// logis_egl_open initialises display d and makes an OpenGL ES 3 context
// with an RGBA8 config (window-capable if window is set).
static EGLint logis_egl_open(EGLDisplay d, int window, EGLConfig *cfg, EGLContext *ctx) {
	EGLint major, minor;
	if (!eglInitialize(d, &major, &minor)) return -1;
	if (!eglBindAPI(EGL_OPENGL_ES_API)) return -2;
	EGLint attrs[] = {
		EGL_SURFACE_TYPE, window ? EGL_WINDOW_BIT : 0,
		EGL_RENDERABLE_TYPE, EGL_OPENGL_ES3_BIT,
		EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8, EGL_ALPHA_SIZE, 8,
		EGL_NONE,
	};
	EGLint n = 0;
	if (!eglChooseConfig(d, attrs, cfg, 1, &n) || n < 1) return -3;
	EGLint cattrs[] = { EGL_CONTEXT_MAJOR_VERSION, 3, EGL_NONE };
	*ctx = eglCreateContext(d, *cfg, EGL_NO_CONTEXT, cattrs);
	if (*ctx == EGL_NO_CONTEXT) return -4;
	return 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/nerdwave-nick/logis/internal/gles"
)

// Context is an EGL display with one OpenGL ES 3 context.
type Context struct {
	dpy C.EGLDisplay
	cfg C.EGLConfig
	ctx C.EGLContext
}

// Info names the GL implementation.
type Info struct{ Vendor, Renderer, Version string }

// Error is why EGL could not give logis a context or a surface, in the
// cogitator's words.
type Error struct {
	// Refused: a device exists but would not bind; otherwise none answered.
	Refused bool
	Reason  string
}

func (e *Error) Error() string {
	if e.Refused {
		return "the GPU adapter refused the rite of binding: " + e.Reason
	}
	return "no machine spirit answers: " + e.Reason +
		" (OpenGL ES 3 through EGL, or Mesa llvmpipe for software rendering)"
}

// NewWayland wakes the GPU on a libwayland wl_display*: the EGL Wayland
// platform, the context made current without a surface (so programs and
// buffers can exist before any window).
func NewWayland(display unsafe.Pointer) (*Context, error) {
	return open(C.EGL_PLATFORM_WAYLAND_KHR, display, true)
}

// NewSurfaceless is an offscreen context (Mesa's surfaceless platform):
// frames go to gles.Targets.
func NewSurfaceless() (*Context, error) {
	return open(C.EGL_PLATFORM_SURFACELESS_MESA, nil, false)
}

func open(platform C.EGLenum, native unsafe.Pointer, window bool) (*Context, error) {
	dpy := C.eglGetPlatformDisplay(platform, native, nil)
	if dpy == 0 {
		return nil, &Error{Reason: fmt.Sprintf("no EGL display for platform %#x (%s)",
			int(platform), errorName(int(C.eglGetError())))}
	}
	c := &Context{dpy: dpy}
	w := C.int(0)
	if window {
		w = 1
	}
	if rc := C.logis_egl_open(dpy, w, &c.cfg, &c.ctx); rc != 0 {
		code := errorName(int(C.eglGetError()))
		C.eglTerminate(dpy)
		switch rc {
		case -4:
			return nil, &Error{Refused: true, Reason: "eglCreateContext: " + code}
		case -3:
			return nil, &Error{Reason: "no RGBA8 OpenGL ES 3 config (" + code + ")"}
		default:
			return nil, &Error{Reason: "eglInitialize: " + code}
		}
	}
	if err := c.MakeCurrentNone(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// MakeCurrentNone makes the context current without a surface.
func (c *Context) MakeCurrentNone() error {
	if C.eglMakeCurrent(c.dpy, nil, nil, c.ctx) == 0 {
		return &Error{Refused: true, Reason: "eglMakeCurrent without a surface: " + errorName(int(C.eglGetError()))}
	}
	return nil
}

// Info names the GL implementation of the context.
func (c *Context) Info() Info {
	return Info{
		Vendor:   gles.String(gles.Vendor),
		Renderer: gles.String(gles.Renderer),
		Version:  gles.String(gles.Version),
	}
}

// Close releases the context and its display; window surfaces must be
// destroyed first.
func (c *Context) Close() {
	if c.dpy == 0 {
		return
	}
	C.eglMakeCurrent(c.dpy, nil, nil, nil)
	C.eglDestroyContext(c.dpy, c.ctx)
	C.eglTerminate(c.dpy)
	C.eglReleaseThread()
	c.dpy, c.ctx = 0, nil
}

var errorNames = map[int]string{
	0x3000: "EGL_SUCCESS",
	0x3001: "EGL_NOT_INITIALIZED",
	0x3002: "EGL_BAD_ACCESS",
	0x3003: "EGL_BAD_ALLOC",
	0x3004: "EGL_BAD_ATTRIBUTE",
	0x3005: "EGL_BAD_CONFIG",
	0x3006: "EGL_BAD_CONTEXT",
	0x3007: "EGL_BAD_CURRENT_SURFACE",
	0x3008: "EGL_BAD_DISPLAY",
	0x3009: "EGL_BAD_MATCH",
	0x300A: "EGL_BAD_NATIVE_PIXMAP",
	0x300B: "EGL_BAD_NATIVE_WINDOW",
	0x300C: "EGL_BAD_PARAMETER",
	0x300D: "EGL_BAD_SURFACE",
	0x300E: "EGL_CONTEXT_LOST",
}

// errorName names an eglGetError code.
func errorName(code int) string {
	if name, ok := errorNames[code]; ok {
		return name
	}
	return fmt.Sprintf("EGL error %#x", code)
}
