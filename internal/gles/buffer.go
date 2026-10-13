package gles

/*
#include <stdint.h>
#include <GLES3/gl3.h>

// logis_instance_attrib points attribute i at off bytes into the bound
// array buffer, advancing once per instance.
static void logis_instance_attrib(GLuint i, GLint n, GLsizei stride, uintptr_t off) {
	glEnableVertexAttribArray(i);
	glVertexAttribPointer(i, n, GL_FLOAT, GL_FALSE, stride, (const void *)off);
	glVertexAttribDivisor(i, 1);
}
*/
import "C"

import "unsafe"

// VertexArray is a vertex array object: the attribute layout of a draw.
type VertexArray uint32

// NewVertexArray makes a vertex array object.
func NewVertexArray() VertexArray {
	var id C.GLuint
	C.glGenVertexArrays(1, &id)
	return VertexArray(id)
}

// Bind makes v the current vertex array.
func (v VertexArray) Bind() { C.glBindVertexArray(C.GLuint(v)) }

// Delete releases v.
func (v VertexArray) Delete() {
	id := C.GLuint(v)
	C.glDeleteVertexArrays(1, &id)
}

// Buffer is a vertex buffer.
type Buffer uint32

// NewBuffer makes a vertex buffer.
func NewBuffer() Buffer {
	var id C.GLuint
	C.glGenBuffers(1, &id)
	return Buffer(id)
}

// Bind binds b as the array buffer.
func (b Buffer) Bind() { C.glBindBuffer(C.GL_ARRAY_BUFFER, C.GLuint(b)) }

// Stream replaces b's contents with data for this frame. The driver copies
// it and gives the buffer new storage if the GPU still reads the old one,
// so nothing waits.
func (b Buffer) Stream(data []float32) {
	b.Bind()
	if len(data) == 0 {
		return
	}
	C.glBufferData(C.GL_ARRAY_BUFFER, C.GLsizeiptr(len(data)*4), unsafe.Pointer(&data[0]), C.GL_STREAM_DRAW)
}

// Delete releases b.
func (b Buffer) Delete() {
	id := C.GLuint(b)
	C.glDeleteBuffers(1, &id)
}

// InstanceAttrib points attribute index at size floats, offset floats into
// each stride-float instance of the bound array buffer (recorded in the
// bound vertex array).
func InstanceAttrib(index, size, stride, offset int) {
	C.logis_instance_attrib(C.GLuint(index), C.GLint(size), C.GLsizei(stride*4), C.uintptr_t(offset*4))
}

// DrawQuads draws instances quads as four-vertex triangle strips: the
// vertex shader makes each corner from gl_VertexID.
func DrawQuads(instances int) {
	C.glDrawArraysInstanced(C.GL_TRIANGLE_STRIP, 0, 4, C.GLsizei(instances))
}
