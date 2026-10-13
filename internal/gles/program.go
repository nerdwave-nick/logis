package gles

/*
#include <stdlib.h>
#include <GLES3/gl3.h>
*/
import "C"

import (
	"strings"
	"unsafe"
)

// Program is a linked GLSL ES program.
type Program struct{ id C.GLuint }

// ShaderError is a shader the driver would not compile or link, with the
// driver's log.
type ShaderError struct {
	// Stage is "vertex", "fragment" or "link".
	Stage string
	Log   string
}

func (e *ShaderError) Error() string {
	return "the " + e.Stage + " shader was refused by the machine spirit: " + strings.TrimSpace(e.Log)
}

// NewProgram compiles vertex and fragment sources and links them.
func NewProgram(vertex, fragment string) (*Program, error) {
	vs, err := compile(C.GL_VERTEX_SHADER, "vertex", vertex)
	if err != nil {
		return nil, err
	}
	defer C.glDeleteShader(vs)
	fs, err := compile(C.GL_FRAGMENT_SHADER, "fragment", fragment)
	if err != nil {
		return nil, err
	}
	defer C.glDeleteShader(fs)
	id := C.glCreateProgram()
	C.glAttachShader(id, vs)
	C.glAttachShader(id, fs)
	C.glLinkProgram(id)
	var ok C.GLint
	C.glGetProgramiv(id, C.GL_LINK_STATUS, &ok)
	if ok == 0 {
		var n C.GLint
		C.glGetProgramiv(id, C.GL_INFO_LOG_LENGTH, &n)
		log := infoLog(int(n), func(size C.GLsizei, out *C.GLchar) {
			C.glGetProgramInfoLog(id, size, nil, out)
		})
		C.glDeleteProgram(id)
		return nil, &ShaderError{Stage: "link", Log: log}
	}
	return &Program{id: id}, nil
}

func compile(kind C.GLenum, stage, source string) (C.GLuint, error) {
	s := C.glCreateShader(kind)
	src := C.CString(source)
	defer C.free(unsafe.Pointer(src))
	C.glShaderSource(s, 1, &src, nil)
	C.glCompileShader(s)
	var ok C.GLint
	C.glGetShaderiv(s, C.GL_COMPILE_STATUS, &ok)
	if ok == 0 {
		var n C.GLint
		C.glGetShaderiv(s, C.GL_INFO_LOG_LENGTH, &n)
		log := infoLog(int(n), func(size C.GLsizei, out *C.GLchar) {
			C.glGetShaderInfoLog(s, size, nil, out)
		})
		C.glDeleteShader(s)
		return 0, &ShaderError{Stage: stage, Log: log}
	}
	return s, nil
}

func infoLog(n int, get func(C.GLsizei, *C.GLchar)) string {
	if n <= 1 {
		return "(no log)"
	}
	buf := make([]byte, n)
	get(C.GLsizei(n), (*C.GLchar)(unsafe.Pointer(&buf[0])))
	return strings.TrimRight(string(buf), "\x00")
}

// Use makes p the current program.
func (p *Program) Use() { C.glUseProgram(p.id) }

// Uniform is the location of the uniform name (-1 if p has none).
func (p *Program) Uniform(name string) int32 {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return int32(C.glGetUniformLocation(p.id, cs))
}

// Delete releases the program.
func (p *Program) Delete() {
	if p.id != 0 {
		C.glDeleteProgram(p.id)
		p.id = 0
	}
}

// Uniform1f sets a float uniform of the current program.
func Uniform1f(loc int32, v float32) { C.glUniform1f(C.GLint(loc), C.GLfloat(v)) }

// Uniform2f sets a vec2 uniform of the current program.
func Uniform2f(loc int32, x, y float32) {
	C.glUniform2f(C.GLint(loc), C.GLfloat(x), C.GLfloat(y))
}

// Uniform1i sets an int (or sampler unit) uniform of the current program.
func Uniform1i(loc int32, v int) { C.glUniform1i(C.GLint(loc), C.GLint(v)) }
