package render

// The post-processing passes' vertex shader: one full-screen triangle (no
// buffers), over which a pass's fragment shader reads the finished frame
// texel for texel.

const postVertex = `#version 300 es
void main() {
	vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}
`
