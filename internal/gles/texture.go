package gles

/*
#include <GLES3/gl3.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Texture is an RGBA8 texture read texel for texel (nearest, no mipmaps).
type Texture uint32

// NewTexture makes a w×h RGBA8 texture; its contents are undefined until
// uploaded.
func NewTexture(w, h int) Texture {
	var id C.GLuint
	C.glGenTextures(1, &id)
	C.glBindTexture(C.GL_TEXTURE_2D, id)
	C.glTexStorage2D(C.GL_TEXTURE_2D, 1, C.GL_RGBA8, C.GLsizei(w), C.GLsizei(h))
	C.glTexParameteri(C.GL_TEXTURE_2D, C.GL_TEXTURE_MIN_FILTER, C.GL_NEAREST)
	C.glTexParameteri(C.GL_TEXTURE_2D, C.GL_TEXTURE_MAG_FILTER, C.GL_NEAREST)
	return Texture(id)
}

// Upload writes w×h RGBA8 pixels (rows top to bottom, no padding) at x, y;
// the first row lands on texel row y. The driver copies them; nothing
// waits for the GPU.
func (t Texture) Upload(x, y, w, h int, rgba []byte) {
	if w*h == 0 {
		return
	}
	C.glBindTexture(C.GL_TEXTURE_2D, C.GLuint(t))
	C.glPixelStorei(C.GL_UNPACK_ALIGNMENT, 1)
	C.glTexSubImage2D(C.GL_TEXTURE_2D, 0, C.GLint(x), C.GLint(y), C.GLsizei(w), C.GLsizei(h),
		C.GL_RGBA, C.GL_UNSIGNED_BYTE, unsafe.Pointer(&rgba[0]))
}

// Bind binds t to texture unit 0.
func (t Texture) Bind() {
	C.glActiveTexture(C.GL_TEXTURE0)
	C.glBindTexture(C.GL_TEXTURE_2D, C.GLuint(t))
}

// Delete releases t.
func (t Texture) Delete() {
	id := C.GLuint(t)
	C.glDeleteTextures(1, &id)
}

// CopyTexture copies the w×h top-left corner of src into dst's, on the
// GPU. It leaves the default framebuffer bound.
func CopyTexture(src, dst Texture, w, h int) {
	var fbo C.GLuint
	C.glGenFramebuffers(1, &fbo)
	C.glBindFramebuffer(C.GL_FRAMEBUFFER, fbo)
	C.glFramebufferTexture2D(C.GL_FRAMEBUFFER, C.GL_COLOR_ATTACHMENT0, C.GL_TEXTURE_2D, C.GLuint(src), 0)
	C.glBindTexture(C.GL_TEXTURE_2D, C.GLuint(dst))
	C.glCopyTexSubImage2D(C.GL_TEXTURE_2D, 0, 0, 0, 0, 0, C.GLsizei(w), C.GLsizei(h))
	C.glBindFramebuffer(C.GL_FRAMEBUFFER, 0)
	C.glDeleteFramebuffers(1, &fbo)
}

// MaxTextureSize is the largest texture side the context takes.
func MaxTextureSize() int {
	var n C.GLint
	C.glGetIntegerv(C.GL_MAX_TEXTURE_SIZE, &n)
	return int(n)
}

// BindFramebuffer makes fbo the draw target (0: the window surface).
func BindFramebuffer(fbo uint32) { C.glBindFramebuffer(C.GL_FRAMEBUFFER, C.GLuint(fbo)) }

// BlendPremultiplied blends premultiplied colours over what is there:
// src + dst × (1 − src alpha), for colour and alpha alike.
func BlendPremultiplied() {
	C.glEnable(C.GL_BLEND)
	C.glBlendFunc(C.GL_ONE, C.GL_ONE_MINUS_SRC_ALPHA)
}

// NoBlend writes colours as they are.
func NoBlend() { C.glDisable(C.GL_BLEND) }

// Err is the context's first recorded GL error since the last call, if
// any. It does not wait for the GPU, but it is not for the frame loop.
func Err() error {
	if e := C.glGetError(); e != C.GL_NO_ERROR {
		return fmt.Errorf("GL error %#x", int(e))
	}
	return nil
}
