package gles

/*
#include <GLES3/gl3.h>
*/
import "C"

// Sampler is a sampler object: how a texture bound to unit 0 is read
// while it is bound, whatever the texture's own parameters.
type Sampler uint32

// NewSmoothSampler reads between texels (linear) and holds coordinates
// outside the texture at its edge, as Shadertoy's and Ghostty's screen
// channel does.
func NewSmoothSampler() Sampler {
	var id C.GLuint
	C.glGenSamplers(1, &id)
	C.glSamplerParameteri(id, C.GL_TEXTURE_MIN_FILTER, C.GL_LINEAR)
	C.glSamplerParameteri(id, C.GL_TEXTURE_MAG_FILTER, C.GL_LINEAR)
	C.glSamplerParameteri(id, C.GL_TEXTURE_WRAP_S, C.GL_CLAMP_TO_EDGE)
	C.glSamplerParameteri(id, C.GL_TEXTURE_WRAP_T, C.GL_CLAMP_TO_EDGE)
	return Sampler(id)
}

// Bind reads unit 0 through s; Sampler(0).Bind() leaves it to the
// texture's parameters again.
func (s Sampler) Bind() { C.glBindSampler(0, C.GLuint(s)) }

// Delete releases s.
func (s Sampler) Delete() {
	id := C.GLuint(s)
	C.glDeleteSamplers(1, &id)
}
