// Package gles is logis's own thin binding to OpenGL ES 3 (spec §5, D1):
// for now what a window needs to clear its frames and tests need to read
// them back. The renderer (the sdf/post shaders) grows it. Every call runs
// on the thread the context is current on: the window's owner goroutine.
package gles

/*
#cgo pkg-config: glesv2
#include <GLES3/gl3.h>
*/
import "C"

import "unsafe"

// Names for String.
const (
	Vendor   = C.GL_VENDOR
	Renderer = C.GL_RENDERER
	Version  = C.GL_VERSION
)

// Viewport sets the viewport in pixels.
func Viewport(x, y, w, h int) {
	C.glViewport(C.GLint(x), C.GLint(y), C.GLsizei(w), C.GLsizei(h))
}

// ClearColor sets the clear colour (premultiplied alpha for windows).
func ClearColor(r, g, b, a float32) {
	C.glClearColor(C.GLfloat(r), C.GLfloat(g), C.GLfloat(b), C.GLfloat(a))
}

// Clear clears the colour buffer (within the scissor box, if one is set).
func Clear() { C.glClear(C.GL_COLOR_BUFFER_BIT) }

// Scissor limits clears and draws to a rectangle, origin bottom left.
func Scissor(x, y, w, h int) {
	C.glEnable(C.GL_SCISSOR_TEST)
	C.glScissor(C.GLint(x), C.GLint(y), C.GLsizei(w), C.GLsizei(h))
}

// NoScissor lifts the scissor box.
func NoScissor() { C.glDisable(C.GL_SCISSOR_TEST) }

// ReadPixels reads RGBA8 pixels of the bound framebuffer, bottom row first.
func ReadPixels(x, y, w, h int) []byte {
	px := make([]byte, w*h*4)
	if len(px) == 0 {
		return px
	}
	C.glPixelStorei(C.GL_PACK_ALIGNMENT, 1)
	C.glReadPixels(C.GLint(x), C.GLint(y), C.GLsizei(w), C.GLsizei(h),
		C.GL_RGBA, C.GL_UNSIGNED_BYTE, unsafe.Pointer(&px[0]))
	return px
}

// String is a glGetString value (Vendor, Renderer, Version).
func String(name uint32) string {
	s := C.glGetString(C.GLenum(name))
	if s == nil {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(s)))
}

// Target is a texture with a framebuffer to render into offscreen (tests,
// goldens).
type Target struct {
	FBO, Tex uint32
	W, H     int
}

// NewTarget makes and binds an RGBA8 w×h target.
func NewTarget(w, h int) *Target {
	var tex, fbo C.GLuint
	C.glGenTextures(1, &tex)
	C.glBindTexture(C.GL_TEXTURE_2D, tex)
	C.glTexStorage2D(C.GL_TEXTURE_2D, 1, C.GL_RGBA8, C.GLsizei(w), C.GLsizei(h))
	C.glGenFramebuffers(1, &fbo)
	C.glBindFramebuffer(C.GL_FRAMEBUFFER, fbo)
	C.glFramebufferTexture2D(C.GL_FRAMEBUFFER, C.GL_COLOR_ATTACHMENT0, C.GL_TEXTURE_2D, tex, 0)
	return &Target{FBO: uint32(fbo), Tex: uint32(tex), W: w, H: h}
}

// Delete releases the target and binds the default framebuffer.
func (t *Target) Delete() {
	fbo, tex := C.GLuint(t.FBO), C.GLuint(t.Tex)
	C.glBindFramebuffer(C.GL_FRAMEBUFFER, 0)
	C.glDeleteFramebuffers(1, &fbo)
	C.glDeleteTextures(1, &tex)
}
