package egl

/*
#cgo pkg-config: egl wayland-egl
#include <EGL/egl.h>
#include <wayland-egl.h>
*/
import "C"

import (
	"unsafe"

	"github.com/nerdwave-nick/logis/internal/scale"
)

// Window is an EGL window surface on a wl_surface (libwayland-egl). Its
// swaps attach and commit on that surface.
type Window struct {
	c       *Context
	native  *C.struct_wl_egl_window
	surf    C.EGLSurface
	current bool // made current once (and its swap interval set)
}

// NewWindow makes a window surface of size buffer pixels on the wl_surface*
// surface. Swaps never wait for the compositor (swap interval 0): the
// caller paces frames with frame callbacks. It is not made current here:
// Mesa takes a back buffer when it is, and defers resizes while it holds
// one, so the first MakeCurrent belongs right before the first frame.
func (c *Context) NewWindow(surface unsafe.Pointer, size scale.Size) (*Window, error) {
	native := C.wl_egl_window_create((*C.struct_wl_surface)(surface), C.int(size.W), C.int(size.H))
	if native == nil {
		return nil, &Error{Refused: true, Reason: "wl_egl_window_create failed"}
	}
	surf := C.eglCreatePlatformWindowSurface(c.dpy, c.cfg, unsafe.Pointer(native), nil)
	if surf == nil {
		code := errorName(int(C.eglGetError()))
		C.wl_egl_window_destroy(native)
		return nil, &Error{Refused: true, Reason: "eglCreatePlatformWindowSurface: " + code}
	}
	return &Window{c: c, native: native, surf: surf}, nil
}

// MakeCurrent makes the window the context's draw target.
func (w *Window) MakeCurrent() error {
	if C.eglMakeCurrent(w.c.dpy, w.surf, w.surf, w.c.ctx) == 0 {
		return &Error{Refused: true, Reason: "eglMakeCurrent: " + errorName(int(C.eglGetError()))}
	}
	if !w.current {
		C.eglSwapInterval(w.c.dpy, 0)
		w.current = true
	}
	return nil
}

// Resize sets the buffer size from the next frame on.
func (w *Window) Resize(size scale.Size) {
	C.wl_egl_window_resize(w.native, C.int(size.W), C.int(size.H), 0, 0)
}

// Swap presents the frame: attach, damage and commit on the wl_surface.
func (w *Window) Swap() error {
	if C.eglSwapBuffers(w.c.dpy, w.surf) == 0 {
		return &Error{Refused: true, Reason: "eglSwapBuffers: " + errorName(int(C.eglGetError()))}
	}
	return nil
}

// Destroy releases the surface; it goes before its wl_surface does. The
// context stays current without a surface.
func (w *Window) Destroy() {
	if w.native == nil {
		return
	}
	_ = w.c.MakeCurrentNone()
	C.eglDestroySurface(w.c.dpy, w.surf)
	C.wl_egl_window_destroy(w.native)
	w.native, w.surf = nil, nil
}
