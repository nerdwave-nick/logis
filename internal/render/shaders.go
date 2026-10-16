package render

// The renderer's shaders, GLSL ES 3.00: signed-distance rounded rectangles
// with borders, glows and gradients (sdf), and image quads from the atlas
// (image). Fragment coordinates are turned into a top-left origin (pixel
// centres at +0.5) with u_size, inputs and outputs are premultiplied, and
// every quad's corners come from gl_VertexID of a four-vertex strip.
//
// logis's EGL surfaces and offscreen targets are plain RGBA8, so colours
// are stored as given.

// sdfVertex: one instance per shape, a quad over the shape plus its glow
// and one pixel of anti-aliasing. Stops travel as half floats, two
// colours per uvec4, so the varyings stay within 15 vectors.
const sdfVertex = `#version 300 es
precision highp float;
uniform vec2 u_size;  // target size in buffer pixels
uniform float u_scale; // logical -> buffer pixels
layout(location = 0) in vec4 a_rect;   // x, y, w, h (logical)
layout(location = 1) in vec4 a_params; // radius, border width, glow radius, highlight angle
layout(location = 2) in vec4 a_fill0;
layout(location = 3) in vec4 a_fill1;
layout(location = 4) in vec4 a_fill2;
layout(location = 5) in vec4 a_fill3;
layout(location = 6) in vec4 a_fill_line; // start xy, end xy (logical)
layout(location = 7) in vec4 a_border0;
layout(location = 8) in vec4 a_border1;
layout(location = 9) in vec4 a_border2;
layout(location = 10) in vec4 a_border3;
layout(location = 11) in vec4 a_border_line;
layout(location = 12) in vec4 a_glow;
layout(location = 13) in vec4 a_highlight; // its colour
layout(location = 14) in vec4 a_extra;     // fill stops, border stops, highlight width (rad), -
flat out vec4 v_rect; // centre xy, half extent xy (buffer pixels)
flat out vec4 v_params;
flat out uvec4 v_fill01;
flat out uvec4 v_fill23;
flat out vec4 v_fill_line;
flat out uvec4 v_border01;
flat out uvec4 v_border23;
flat out vec4 v_border_line;
flat out vec4 v_glow;
flat out vec4 v_highlight;
flat out vec4 v_extra;

uvec4 pack_pair(vec4 a, vec4 b) {
	return uvec4(packHalf2x16(a.xy), packHalf2x16(a.zw), packHalf2x16(b.xy), packHalf2x16(b.zw));
}

void main() {
	float s = u_scale;
	vec2 corner = vec2(float(gl_VertexID & 1), float((gl_VertexID >> 1) & 1));
	float outset = a_params.z * s + 1.0;
	vec2 origin = a_rect.xy * s - vec2(outset);
	vec2 extent = a_rect.zw * s + vec2(2.0 * outset);
	vec2 p = origin + corner * extent;
	gl_Position = vec4(p.x / u_size.x * 2.0 - 1.0, 1.0 - p.y / u_size.y * 2.0, 0.0, 1.0);
	vec2 half_extent = a_rect.zw * s * 0.5;
	v_rect = vec4(a_rect.xy * s + half_extent, half_extent);
	// Radius, border and glow scale; the highlight's angle does not.
	v_params = vec4(a_params.xyz * s, a_params.w);
	v_fill01 = pack_pair(a_fill0, a_fill1);
	v_fill23 = pack_pair(a_fill2, a_fill3);
	v_fill_line = a_fill_line * s;
	v_border01 = pack_pair(a_border0, a_border1);
	v_border23 = pack_pair(a_border2, a_border3);
	v_border_line = a_border_line * s;
	v_glow = a_glow;
	v_highlight = a_highlight;
	v_extra = a_extra;
}
`

const sdfFragment = `#version 300 es
precision highp float;
uniform vec2 u_size;
flat in vec4 v_rect;
flat in vec4 v_params;
flat in uvec4 v_fill01;
flat in uvec4 v_fill23;
flat in vec4 v_fill_line;
flat in uvec4 v_border01;
flat in uvec4 v_border23;
flat in vec4 v_border_line;
flat in vec4 v_glow;
flat in vec4 v_highlight;
flat in vec4 v_extra;
out vec4 o_color;

vec4 first(uvec4 p) { return vec4(unpackHalf2x16(p.x), unpackHalf2x16(p.y)); }
vec4 second(uvec4 p) { return vec4(unpackHalf2x16(p.z), unpackHalf2x16(p.w)); }

// Signed distance to a rounded box centred at the origin.
float sd_round_box(vec2 p, vec2 half_extent, float r) {
	vec2 q = abs(p) - half_extent + vec2(r);
	return length(max(q, vec2(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

// A paint at p: count premultiplied stops evenly spaced along the line.
vec4 paint(vec2 p, uvec4 s01, uvec4 s23, float count, vec4 line) {
	vec4 s0 = first(s01);
	if (count < 1.5) return s0;
	vec2 dir = line.zw - line.xy;
	float len2 = dot(dir, dir);
	float t = 0.0;
	if (len2 > 1e-6) t = clamp(dot(p - line.xy, dir) / len2, 0.0, 1.0);
	float x = t * (count - 1.0);
	if (x <= 1.0) return mix(s0, second(s01), x);
	if (x <= 2.0) return mix(second(s01), first(s23), x - 1.0);
	return mix(first(s23), second(s23), x - 2.0);
}

// How much of the highlight lies at p: 1 at its angle, fading to almost
// nothing at its angular width either side.
float highlight_at(vec2 p, vec2 centre, vec2 half_extent, float angle, float width) {
	vec2 rel = (p - centre) / max(half_extent, vec2(1e-3));
	float d = atan(rel.y, rel.x) - angle;
	d = d - 6.2831853 * round(d / 6.2831853);
	float x = d / max(width, 1e-4);
	return exp(-4.0 * x * x);
}

void main() {
	vec2 p = vec2(gl_FragCoord.x, u_size.y - gl_FragCoord.y);
	float d = sd_round_box(p - v_rect.xy, v_rect.zw, v_params.x);
	// Coverage of the whole shape and of the interior inside the border,
	// anti-aliased over one buffer pixel.
	float cover = clamp(0.5 - d, 0.0, 1.0);
	float inner = clamp(0.5 - (d + v_params.y), 0.0, 1.0);
	vec4 col = paint(p, v_fill01, v_fill23, v_extra.x, v_fill_line) * inner;
	vec4 border = paint(p, v_border01, v_border23, v_extra.y, v_border_line);
	if (v_highlight.a > 0.0) {
		float k = highlight_at(p, v_rect.xy, v_rect.zw, v_params.w, v_extra.z);
		border = border * (1.0 - k * v_highlight.a) + v_highlight * k;
	}
	col += border * (cover - inner);
	float reach = v_params.z;
	if (reach > 0.0) {
		float t = clamp(max(d, 0.0) / reach, 0.0, 1.0);
		col += v_glow * (exp(-3.0 * t) * (1.0 - t)) * (1.0 - cover);
	}
	o_color = col;
}
`

// imageVertex: one instance per image, its rect in whole buffer pixels.
const imageVertex = `#version 300 es
precision highp float;
uniform vec2 u_size;
layout(location = 0) in vec4 a_rect;  // x, y, w, h in whole buffer pixels
layout(location = 1) in vec4 a_texel; // the image's top-left atlas texel, opacity, -
flat out vec2 v_offset; // atlas texel minus buffer pixel
flat out float v_opacity;

void main() {
	vec2 corner = vec2(float(gl_VertexID & 1), float((gl_VertexID >> 1) & 1));
	vec2 p = a_rect.xy + corner * a_rect.zw;
	gl_Position = vec4(p.x / u_size.x * 2.0 - 1.0, 1.0 - p.y / u_size.y * 2.0, 0.0, 1.0);
	v_offset = a_texel.xy - a_rect.xy;
	v_opacity = a_texel.z;
}
`

// imageFragment draws texel for texel: each pixel loads exactly one
// premultiplied texel, no filtering; fading scales every channel.
const imageFragment = `#version 300 es
precision highp float;
uniform vec2 u_size;
uniform sampler2D u_atlas;
flat in vec2 v_offset;
flat in float v_opacity;
out vec4 o_color;

void main() {
	// p is the pixel centre (x + 0.5); its floor is the pixel.
	vec2 p = vec2(gl_FragCoord.x, u_size.y - gl_FragCoord.y);
	o_color = texelFetch(u_atlas, ivec2(floor(p + v_offset)), 0) * v_opacity;
}
`
