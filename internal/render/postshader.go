package render

// The post-processing pass's GLSL ES 3.00 port of Rust's src/render/
// post.wgsl (after the research port in research/go/render-gpu/gles): one
// full-screen triangle over the finished frame. Within a rounded
// rectangle (the panel's body) it applies curvature, chromatic aberration,
// bloom, scanlines, flicker, grain and a vignette, each by its strength
// (0 is off); outside it the frame passes through (the glow). Pixels are
// read with texelFetch (no sampler filtering) and every random term comes
// from an integer hash, so a frozen time gives the same image on any GPU.
// Premultiplied in, premultiplied out. Coordinates are turned into a
// top-left origin (pixel centres at +0.5); the frame's texture has GL's
// bottom-up rows, so load flips them.

const postVertex = `#version 300 es
void main() {
	vec2 p = vec2(float((gl_VertexID << 1) & 2), float(gl_VertexID & 2));
	gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}
`

const postFragment = `#version 300 es
precision highp float;
precision highp int;
uniform vec2 u_size;
uniform float u_scale;
uniform float u_time;
uniform vec4 u_body;
uniform float u_radius;
uniform float u_inset;
uniform vec4 u_a; // scanlines, grain, flicker, vignette
uniform vec4 u_b; // bloom, curvature, aberration, -
uniform sampler2D u_frame;
out vec4 o_color;
const vec2 RING[12] = vec2[12](
	vec2(1.0, 0.0), vec2(0.8660254, 0.5), vec2(0.5, 0.8660254),
	vec2(0.0, 1.0), vec2(-0.5, 0.8660254), vec2(-0.8660254, 0.5),
	vec2(-1.0, 0.0), vec2(-0.8660254, -0.5), vec2(-0.5, -0.8660254),
	vec2(0.0, -1.0), vec2(0.5, -0.8660254), vec2(0.8660254, -0.5));
float sd_round_box(vec2 p, vec2 hs, float r) {
	vec2 q = abs(p) - hs + vec2(r);
	return length(max(q, vec2(0.0))) + min(max(q.x, q.y), 0.0) - r;
}
vec4 load(ivec2 p) {
	ivec2 last = ivec2(u_size) - ivec2(1);
	p = clamp(p, ivec2(0), last);
	return texelFetch(u_frame, ivec2(p.x, last.y - p.y), 0);
}
vec4 sample_at(vec2 p) {
	vec2 q = p - vec2(0.5);
	ivec2 i = ivec2(floor(q));
	vec2 f = q - floor(q);
	vec4 top = mix(load(i), load(i + ivec2(1, 0)), f.x);
	vec4 bottom = mix(load(i + ivec2(0, 1)), load(i + ivec2(1, 1)), f.x);
	return mix(top, bottom, f.y);
}
float hash(uvec2 cell, uint seed) {
	uint h = cell.x * 747796405u + cell.y * 2891336453u + seed * 277803737u + 1u;
	h = ((h >> ((h >> 28u) + 4u)) ^ h) * 277803737u;
	h = (h >> 22u) ^ h;
	return float(h & 0xffffu) / 65535.0;
}
vec4 darken(vec4 c, float k) {
	return vec4(c.rgb * (1.0 - k), c.a + (1.0 - c.a) * k * c.a);
}
void main() {
	vec2 p = vec2(gl_FragCoord.x, u_size.y - gl_FragCoord.y);
	vec4 here = load(ivec2(p));
	vec2 hs = u_body.zw * 0.5;
	vec2 centre = u_body.xy + hs;
	float inside = clamp(0.5 - sd_round_box(p - centre, hs, u_radius), 0.0, 1.0);
	if (inside <= 0.0) { o_color = here; return; }
	vec2 uv = (p - centre) / max(hs, vec2(1.0));
	vec2 q = p;
	float screen = 1.0;
	float curvature = u_b.y;
	vec2 glass = max(hs - vec2(u_inset), vec2(1.0));
	float glass_r = max(u_radius - u_inset, 0.0);
	bool on_glass = sd_round_box(p - centre, glass, glass_r) < 0.0;
	if (curvature > 0.0 && on_glass) {
		vec2 g = (p - centre) / glass;
		q = centre + g * (1.0 + 0.25 * curvature * dot(g, g)) * glass;
		screen = clamp(0.5 - sd_round_box(q - centre, glass, glass_r), 0.0, 1.0);
	}
	vec4 c = here;
	float aberration = u_b.z;
	if (aberration > 0.0) {
		vec2 off = vec2(2.0 * aberration * u_scale, 0.0);
		vec4 mid = sample_at(q);
		vec4 red = sample_at(q + off);
		vec4 blue = sample_at(q - off);
		c = vec4(red.r, mid.g, blue.b, max(mid.a, max(red.a, blue.a)));
	} else if (curvature > 0.0) {
		c = sample_at(q);
	}
	if (screen < 1.0) c = mix(vec4(0.0, 0.0, 0.0, here.a), c, screen);
	float bloom = u_b.x;
	if (bloom > 0.0) {
		vec3 light = vec3(0.0);
		for (int i = 0; i < 12; i++) {
			for (int ring = 0; ring < 2; ring++) {
				float reach = (3.0 + 4.0 * float(ring)) * u_scale;
				vec4 s;
				if (curvature > 0.0) s = sample_at(q + RING[i] * reach);
				else s = load(ivec2(floor(q + RING[i] * reach)));
				light += max(s.rgb - vec3(0.35 * s.a), vec3(0.0));
			}
		}
		c = vec4(c.rgb + light * (bloom * 2.0 / 24.0), c.a);
	}
	float scanlines = u_a.x;
	if (scanlines > 0.0) {
		float row = fract((p.y - u_body.y) / (3.0 * u_scale));
		float line = pow(0.5 + 0.5 * cos(row * 6.2831853), 2.0);
		c = darken(c, scanlines * line);
	}
	float flicker = u_a.z;
	if (flicker > 0.0) {
		float wave = 0.5 + 0.5 * sin(u_time * 47.0 + 0.7 * sin(u_time * 13.0));
		c = vec4(c.rgb * (1.0 - flicker * wave), c.a);
	}
	float grain = u_a.y;
	if (grain > 0.0) {
		uvec2 cell = uvec2(floor(p));
		float n = hash(cell, uint(floor(u_time * 24.0))) - 0.5;
		c = vec4(c.rgb + vec3(n * grain * 0.5 * c.a), c.a);
	}
	float vignette = u_a.w;
	if (vignette > 0.0) {
		float r = length(uv) * 0.70710678;
		c = darken(c, vignette * clamp((r - 0.55) / 0.45, 0.0, 1.0));
	}
	c = vec4(clamp(c.rgb, vec3(0.0), vec3(c.a)), clamp(c.a, 0.0, 1.0));
	o_color = mix(here, c, inside);
}
`
