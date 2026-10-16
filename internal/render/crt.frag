#version 300 es
// Original implementation for logis from a behavioural specification (clean
// room), extending logis's post shader. MIT, the logis project.
//
// One full-screen pass over the finished frame. Within a rounded rectangle
// (the panel's body) it applies chromatic aberration, glow, scanlines, a
// phosphor dot mask, flicker, grain, a vignette and rounded tube corners;
// outside it the frame passes through. Curvature bends the whole frame
// (panel, border, glass and the glow around it) as one convex tube face,
// and every effect lies on the bent glass. Pixels are read with texelFetch
// (no sampler filtering) and every random term comes from an integer hash,
// so a frozen u_time gives the same image on any GPU. Premultiplied in, premultiplied out.
// Coordinates have a top-left origin with pixel centres at +0.5; lengths in
// uniforms are logical pixels and are multiplied by u_scale here.
//
// Compatibility: with every control that is new relative to logis's post
// shader at its default, each effect computes exactly what that shader
// computes for the same strength: new work sits behind branches that the
// defaults skip, and the shared arithmetic keeps today's form (the same
// multiply-adds, no runtime divisions, squares as products) so compilers
// round it the same way. Curvature 0 is today's flat pass; above 0 the
// bend deliberately replaces today's warp-behind-flat-glass.
precision highp float;
precision highp int;

uniform vec2 u_size;    // frame size, buffer pixels
uniform float u_scale;  // buffer pixels per logical pixel
uniform float u_time;   // seconds
uniform vec4 u_body;    // left, top, width, height (buffer pixels)
uniform float u_radius; // body corner radius (buffer pixels)
uniform float u_inset;  // straight edge kept inside the body (buffer pixels)
// Extra screen regions (tab plates): rounded rectangles, left, top, width,
// height in buffer pixels with a top-left origin like u_body, treated as
// glass. Only the first u_region_count (0..4) count.
uniform vec4 u_region[4];
uniform float u_region_radius[4];
uniform int u_region_count;
uniform sampler2D u_frame;

// 1. Scanlines
uniform float u_scan_strength;  // 0..1, darkness of the lines
uniform float u_scan_period;    // px between lines (default 3)
uniform float u_scan_sharpness; // 0 soft wave .. 1 thin hard line (default 0.25)
uniform float u_scan_roll;      // px/s, positive rolls down (default 0)
// 2. Glow
uniform float u_glow_strength;  // 0..1 (today's bloom)
uniform float u_glow_threshold; // brightness below which nothing glows (0.35)
uniform float u_glow_reach;     // outer ring radius, px (7; inner ring 3/7 of it)
uniform float u_glow_bleed;     // 0 neutral, 1 source colour, >1 saturated (1)
uniform float u_glow_adapt;     // 0 uniform .. 1 dim lifted, bright spared (0)
// 3. Chromatic aberration
uniform float u_ab_amount;      // 0..1, offset scale (2 px at 1)
uniform float u_ab_edges;       // 0 uniform .. 1 none at centre, growing outward (0)
uniform float u_ab_wobble;      // 0..1, variation over time (0)
uniform float u_ab_rate;        // Hz of the variation (1)
uniform float u_ab_twitch;      // 0 smooth breathing .. 1 calm with spikes (0)
// 4. Dot mask
uniform float u_mask_strength;  // 0..1, 0 is off
uniform float u_mask_size;      // pitch, px (3)
uniform int u_mask_kind;        // 0 dots, 1 grille, 2 slot
// 5. Roundness
uniform float u_round_curvature; // 0..1, barrel bow of the glass
uniform float u_round_corners;   // 0..1, extra rounding of the visible screen
uniform float u_round_vignette;  // 0..1
// 6. Grain
uniform float u_grain_strength; // 0..1
uniform float u_grain_rate;     // changes per second (24)
// 7. Flicker
uniform float u_flicker_strength; // 0..1
uniform float u_flicker_freq;     // Hz of the fast tremble (47/2pi = 7.4803)

out vec4 o_color;

const float TAU = 6.2831853;
const vec3 LUMA = vec3(0.2126, 0.7152, 0.0722);
const vec2 RING[12] = vec2[12](
	vec2(1.0, 0.0), vec2(0.8660254, 0.5), vec2(0.5, 0.8660254),
	vec2(0.0, 1.0), vec2(-0.5, 0.8660254), vec2(-0.8660254, 0.5),
	vec2(-1.0, 0.0), vec2(-0.8660254, -0.5), vec2(-0.5, -0.8660254),
	vec2(0.0, -1.0), vec2(0.5, -0.8660254), vec2(0.8660254, -0.5));

float sd_round_box(vec2 p, vec2 hs, float r) {
	vec2 q = abs(p) - hs + vec2(r);
	return length(max(q, vec2(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

// logis's frames have GL's bottom-up rows (texel row 0 is the bottom), so
// load and load_clear flip a top-down p: they read row (last.y - p.y).
// (The clean-room delivery read top-down frames unflipped.)
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

// The frame beyond its edges is transparent (premultiplied zero) for the
// bent panel: what the bend uncovers at the frame's edge shows nothing.
vec4 load_clear(ivec2 p) {
	if (any(lessThan(p, ivec2(0))) || any(greaterThanEqual(p, ivec2(u_size)))) return vec4(0.0);
	return texelFetch(u_frame, ivec2(p.x, int(u_size.y) - 1 - p.y), 0);
}

// Catmull-Rom weights for the four texels around a fraction t.
vec4 catmull_rom(float t) {
	return vec4(t * (-0.5 + t * (1.0 - 0.5 * t)),
		1.0 + t * t * (-2.5 + 1.5 * t),
		t * (0.5 + t * (2.0 - 1.5 * t)),
		t * t * (-0.5 + 0.5 * t));
}

// A sharp read for the bent picture: bicubic (Catmull-Rom) keeps text crisp
// where bilinear blurs it; clamped to the 4 nearest texels against ringing.
vec4 sample_sharp(vec2 p) {
	vec2 q = p - vec2(0.5);
	ivec2 i = ivec2(floor(q));
	vec2 f = q - floor(q);
	vec4 wx = catmull_rom(f.x);
	vec4 wy = catmull_rom(f.y);
	vec4 sum = vec4(0.0);
	for (int y = 0; y < 4; y++) {
		vec4 row = vec4(0.0);
		for (int x = 0; x < 4; x++) row += wx[x] * load_clear(i + ivec2(x - 1, y - 1));
		sum += wy[y] * row;
	}
	vec4 a = load_clear(i), b = load_clear(i + ivec2(1, 0));
	vec4 c = load_clear(i + ivec2(0, 1)), d = load_clear(i + ivec2(1, 1));
	sum = clamp(sum, min(min(a, b), min(c, d)), max(max(a, b), max(c, d)));
	return vec4(min(sum.rgb, vec3(sum.a)), sum.a);
}

// The bent panel: the point of the unbent frame that pixel p shows, at
// g * (1 + a|g|^2) / (1 + a) with g in body half-sizes from its centre. The
// centre and the edges' middles stay, the rest is read from further out:
// lines bow, corners draw in, the outline turns convex. Never folds over.
vec2 bend(vec2 p, vec2 centre, vec2 hs, float curvature) {
	vec2 g = (p - centre) / hs;
	float a = 0.25 * curvature;
	return centre + g * ((1.0 + a * dot(g, g)) / (1.0 + a)) * hs;
}

uint hash_bits(uvec2 cell, uint seed) {
	uint h = cell.x * 747796405u + cell.y * 2891336453u + seed * 277803737u + 1u;
	h = ((h >> ((h >> 28u) + 4u)) ^ h) * 277803737u;
	h = (h >> 22u) ^ h;
	return h & 0xffffu;
}

float hash(uvec2 cell, uint seed) {
	return float(hash_bits(cell, seed)) / 65535.0;
}

// hash - 0.5 rounded as today's shader rounds it (product, then subtraction)
// whatever a compiler fuses: fl(1/65535) = 2^-16 + 2^-32, so the product is
// v * 65537 * 2^-32, rounded to 24 bits (nearest, ties to even) in integers;
// the float steps left are exact or round once either way.
float hash_centred(uvec2 cell, uint seed) {
	uint v = hash_bits(cell, seed);
	uint n = v * 65537u;
	if (v < 256u) return float(n) * (1.0 / 4294967296.0) - 0.5;
	// v has e + 1 bits, n has e + 17: drop s = e - 7 of them.
	uint e = (floatBitsToUint(float(v)) >> 23u) - 127u;
	uint s = e - 7u;
	uint r = n >> s;
	uint rem = n & ((1u << s) - 1u);
	uint half_ulp = 1u << (s - 1u);
	if (rem > half_ulp || (rem == half_ulp && (r & 1u) == 1u)) r += 1u;
	return float(r) * uintBitsToFloat((s + 95u) << 23u) - 0.5;
}

vec4 darken(vec4 c, float k) {
	return vec4(c.rgb * (1.0 - k), c.a + (1.0 - c.a) * k * c.a);
}

// --- chromatic aberration over time ---------------------------------------

// A smooth breathing in -1..1: two incommensurate sines.
float ab_breathe(float t, float rate) {
	float ph = TAU * rate * t;
	return 0.7 * sin(ph) + 0.3 * sin(1.618034 * ph + 1.3);
}

// Calm with sudden short spikes. Time is cut into slots of 1/rate s; a slot
// holds a spike with a probability of 0.55, at a hashed moment within it,
// with a hashed width (35..95 ms) and height, so spikes come at about half
// the rate on average and never on a beat. Within a spike the height
// jitters per 16 ms so it reads as a nervous jump, then it snaps back.
float ab_spikes(float t, float rate) {
	float r = max(rate, 0.01);
	float k0 = floor(t * r);
	float s = 0.0;
	for (int i = -1; i <= 0; i++) {
		float k = k0 + float(i);
		if (k < 0.0) continue;
		uint ku = uint(k);
		if (hash(uvec2(ku, 1u), 91u) > 0.55) continue;
		float at = (k + 0.85 * hash(uvec2(ku, 2u), 91u)) / r;
		float len = min(0.035 + 0.06 * hash(uvec2(ku, 3u), 91u), 0.9 / r);
		float dt = t - at;
		if (dt < 0.0 || dt > len) continue;
		float height = 1.5 + 3.5 * hash(uvec2(ku, 4u), 91u);
		float jitter = hash(uvec2(ku, 16u + uint(dt / 0.016)), 92u);
		float body = 1.0 - smoothstep(0.75 * len, len, dt);
		s = max(s, height * (0.55 + 0.45 * jitter) * body);
	}
	// The calm between spikes: a faint shiver, 20 times a second.
	float shiver = hash(uvec2(uint(max(t, 0.0) * 20.0), 6u), 93u) - 0.5;
	return s + 0.1 * shiver;
}

// --- scanlines -------------------------------------------------------------

// The dark line's profile at row (0..1 within a period, line centred on 0):
// a raised cosine to the power e. At the default sharpness it is squared
// by multiplication, as today's pow(x, 2.0) compiles, so the default stays
// bit-exact.
float scan_line(float row, float e, bool today) {
	float b = 0.5 + 0.5 * cos(row * 6.2831853);
	return today ? b * b : pow(b, e);
}

// --- dot mask --------------------------------------------------------------

// Integral over [0, x] of a stripe [lo, lo + w) repeating every 1.
float stripe_int(float x, float lo, float w) {
	float y = x - lo;
	return floor(y) * w + min(fract(y), w);
}

// Fraction of [a, b] that the stripe covers (a box filter over the pixel).
float stripe_cover(float a, float b, float lo, float w) {
	return (stripe_int(b, lo, w) - stripe_int(a, lo, w)) / (b - a);
}

// Per-channel transmission of the mask at p (buffer pixels, top-left
// origin), each channel in 1 - strength .. 1.
vec3 dot_mask(vec2 p) {
	float pitch = max(u_mask_size * u_scale, 1.0);
	vec2 o = (p - u_body.xy) / pitch;     // in triads
	vec2 h = vec2(0.5 / pitch);           // half a pixel, in triads
	float s = u_mask_strength;
	if (u_mask_kind == 0) {
		// A soft round dot per cell, the same for every channel.
		vec2 f = fract(o) - 0.5;
		float d = length(f) * 2.0;        // 0 centre, 1 mid-edge
		// The edge softens over about a pixel and a half (more at small
		// pitches, where a hard dot would only alias).
		float soft = max(0.3, 3.0 / pitch);
		float lit = 1.0 - smoothstep(0.75 - 0.5 * soft, 0.75 + 0.5 * soft, d);
		return vec3(1.0 - s * (1.0 - lit));
	}
	vec3 cover = vec3(
		stripe_cover(o.x - h.x, o.x + h.x, 0.0, 1.0 / 3.0),
		stripe_cover(o.x - h.x, o.x + h.x, 1.0 / 3.0, 1.0 / 3.0),
		stripe_cover(o.x - h.x, o.x + h.x, 2.0 / 3.0, 1.0 / 3.0));
	if (u_mask_kind == 2) {
		// Slots one triad tall with a dark bar between them; every other
		// column is shifted by half a slot (brickwork).
		float shift = mod(floor(o.x), 2.0) * 0.5;
		float y = o.y + shift;
		cover *= stripe_cover(y - h.y, y + h.y, 0.0, 0.72);
	}
	return vec3(1.0) - s * (vec3(1.0) - cover);
}

// --- the pass --------------------------------------------------------------

void main() {
	vec2 p = vec2(gl_FragCoord.x, u_size.y - gl_FragCoord.y);
	vec2 hs = u_body.zw * 0.5;
	vec2 centre = u_body.xy + hs;

	// 5. Roundness. Curvature bends the whole frame (panel, border, glass
	// and the glow around it): from here on every step works at q, the
	// point of the unbent frame this pixel shows, so the effects lie on the
	// bent glass too. Unbent, q is p and the frame is read texel for texel.
	float curvature = u_round_curvature;
	vec2 q = p;
	vec4 here;
	if (curvature > 0.0) {
		q = bend(p, centre, max(hs, vec2(1.0)), curvature);
		here = sample_sharp(q);
	} else {
		here = load(ivec2(p));
	}
	float inside = clamp(0.5 - sd_round_box(q - centre, hs, u_radius), 0.0, 1.0);

	// The screen here: the body, or a region (tab plate) covering it more,
	// tested at the bent position. Patterns keep the panel's coordinates;
	// vignette, corners and edges-only use the screen's own rectangle. A
	// region is glass to its edge (no inset).
	vec2 s_centre = centre;
	vec2 s_hs = hs;
	float s_radius = u_radius;
	float s_inset = u_inset;
	for (int i = 0; i < 4; i++) {
		if (i >= u_region_count) break;
		vec2 rhs = max(u_region[i].zw * 0.5, vec2(0.0));
		vec2 rc = u_region[i].xy + rhs;
		float rr = clamp(u_region_radius[i], 0.0, min(rhs.x, rhs.y));
		float cover = clamp(0.5 - sd_round_box(q - rc, rhs, rr), 0.0, 1.0);
		if (cover > inside) {
			inside = cover;
			s_centre = rc;
			s_hs = rhs;
			s_radius = rr;
			s_inset = 0.0;
		}
	}
	if (inside <= 0.0) { o_color = here; return; }
	vec2 uv = (q - s_centre) / max(s_hs, vec2(1.0));

	// Corners: the visible glass rounded further, on the bent glass; beyond
	// it the screen is dark.
	vec2 glass = max(s_hs - vec2(s_inset), vec2(1.0));
	float glass_r = max(s_radius - s_inset, 0.0);
	float tube_lit = 1.0;
	if (u_round_corners > 0.0 && sd_round_box(q - s_centre, glass, glass_r) < 0.0) {
		float k = u_round_corners;
		float m = min(glass.x, glass.y);
		float tube_r = mix(glass_r, max(glass_r, 0.42 * m), k);
		// Sides bow out a little towards their middles, as a tube's do.
		vec2 g = (q - s_centre) / glass;
		vec2 tube = glass * (1.0 - 0.03 * k * vec2(g.y * g.y, g.x * g.x));
		float feather = 0.5 + 1.5 * k * u_scale;
		float d = sd_round_box(q - s_centre, tube, min(tube_r, min(tube.x, tube.y)));
		tube_lit = clamp(0.5 - d / (2.0 * feather), 0.0, 1.0);
	}

	// 3. Chromatic aberration.
	vec4 c = here;
	float aberration = u_ab_amount;
	if (aberration > 0.0) {
		vec2 off = vec2(2.0 * aberration * u_scale, 0.0);
		if (u_ab_edges > 0.0) {
			// Radial and growing with the square of the distance from the
			// centre: none there, today's at the edges' middles.
			vec2 g = (q - s_centre) / max(s_hs, vec2(1.0));
			off = 2.0 * aberration * u_scale * mix(vec2(1.0, 0.0), g * length(g), u_ab_edges);
		}
		if (u_ab_wobble > 0.0) {
			float w = mix(ab_breathe(u_time, u_ab_rate), ab_spikes(u_time, u_ab_rate), u_ab_twitch);
			off *= max(1.0 + u_ab_wobble * w, 0.0);
		}
		vec4 mid, red, blue;
		if (curvature > 0.0) {
			// The bent picture is read sharp, as `here` is.
			mid = here;
			red = sample_sharp(q + off);
			blue = sample_sharp(q - off);
		} else {
			mid = sample_at(q);
			red = sample_at(q + off);
			blue = sample_at(q - off);
		}
		c = vec4(red.r, mid.g, blue.b, max(mid.a, max(red.a, blue.a)));
	}
	// The dark beyond the tube's corners is opaque: mixing to black with
	// any other picture's alpha would let that picture show through
	// translucent glass as a ghost.
	if (tube_lit < 1.0) c = mix(vec4(0.0, 0.0, 0.0, 1.0), c, tube_lit);

	// 2. Glow: light above the threshold from rings of twelve points around
	// the pixel. `lit` counts how many of the points see light at all.
	float bloom = u_glow_strength;
	if (bloom > 0.0) {
		vec3 light = vec3(0.0);
		float lit = 0.0;
		// Up to a reach of 9 px: today's two rings (3 and 7 px at the
		// default reach 7), scaled to the reach. Further out, more rings
		// between 3/7 of the reach and the reach so the halo stays smooth.
		int rings = u_glow_reach <= 9.0 ? 2
			: clamp(2 + int(floor((u_glow_reach * u_scale - 5.0) / 4.0)), 3, 6);
		if (rings == 2) {
			for (int i = 0; i < 12; i++) {
				for (int ring = 0; ring < 2; ring++) {
					// (u_glow_reach * (1/7) is exactly 1 at the default 7.)
					float reach = (3.0 + 4.0 * float(ring)) * u_scale * (u_glow_reach * (1.0 / 7.0));
					vec4 s;
					if (curvature > 0.0) s = sample_at(q + RING[i] * reach);
					else s = load(ivec2(floor(q + RING[i] * reach)));
					vec3 add = max(s.rgb - vec3(u_glow_threshold * s.a), vec3(0.0));
					light += add;
					lit += min(max(add.r, max(add.g, add.b)) * 8.0, 1.0);
				}
			}
		} else {
			// Each ring is turned by a fraction of the 30 degree spacing so
			// the points interleave, and read with bilinear filtering; the
			// sum is weighted back to the two rings' 24 points.
			float outer = u_glow_reach * u_scale;
			float inner = outer * (3.0 / 7.0);
			for (int ring = 0; ring < 6; ring++) {
				if (ring >= rings) break;
				float rad = mix(inner, outer, float(ring) / float(rings - 1));
				float turn = 0.5235988 * float(ring) / float(rings);
				mat2 rot = mat2(cos(turn), sin(turn), -sin(turn), cos(turn));
				for (int i = 0; i < 12; i++) {
					vec4 s = sample_at(q + rot * RING[i] * rad);
					vec3 add = max(s.rgb - vec3(u_glow_threshold * s.a), vec3(0.0));
					light += add;
					lit += min(max(add.r, max(add.g, add.b)) * 8.0, 1.0);
				}
			}
			light *= 2.0 / float(rings);
			lit *= 2.0 / float(rings);
		}
		if (u_glow_bleed != 1.0) {
			// Around the light's own grey: 0 is grey, above 1 more colour.
			vec3 grey = vec3(dot(light, LUMA));
			light = max(grey + (light - grey) * u_glow_bleed, vec3(0.0));
		}
		if (u_glow_adapt > 0.0) {
			// Dim pixels take more of their neighbours' light, bright ones
			// only a little. A pixel most of whose points see light lies
			// within a filled band (dark text on a highlight, say) and is
			// treated as bright, so bands keep their text legible.
			float lum = dot(c.rgb, LUMA);
			float bright = smoothstep(0.04, 0.55, lum);
			float filled = smoothstep(0.45, 0.85, lit / 24.0);
			float take = mix(1.7, 0.15, max(bright, filled));
			light *= mix(1.0, take, u_glow_adapt);
		}
		// One multiply-add, exactly today's expression.
		c = vec4(c.rgb + light * (bloom * 2.0 / 24.0), c.a);
	}

	// 4. Dot mask, on the (bent) glass.
	if (u_mask_strength > 0.0) {
		vec3 m = dot_mask(q);
		float k = 1.0 - (m.r + m.g + m.b) / 3.0;
		c = vec4(c.rgb * m, c.a + (1.0 - c.a) * k * c.a);
	}

	// 1. Scanlines, every period from the body's top. Sharpness 0..0.25: a
	// raised cosine from a soft wave to today's square; above, a hard band
	// narrowing to one pixel at 1, box-filtered over the pixel and moved up
	// to half a pixel onto pixel centres so it stays a crisp dark row.
	float scanlines = u_scan_strength;
	if (scanlines > 0.0) {
		float period = u_scan_period * u_scale;
		float y = q.y - u_body.y - u_scan_roll * u_scale * u_time;
		float row = fract(y / period);
		float sharp = u_scan_sharpness;
		float line = scan_line(row, 2.0 * exp2(7.0 * min(sharp - 0.25, 0.0)), sharp == 0.25);
		if (sharp > 0.25) {
			float t = min((sharp - 0.25) / 0.75, 1.0);
			// From the squared cosine's width at half depth (0.364 of a
			// period) down to one pixel.
			float half_w = 0.5 * mix(0.364 * period, min(1.0, 0.364 * period), t);
			// Distance (px) from the pixel's centre to the nearest line's.
			float d = abs(fract((y + 0.5 * t) / period + 0.5) - 0.5) * period;
			float band = clamp(min(d + 0.5, half_w) - max(d - 0.5, -half_w), 0.0, 1.0);
			line = mix(line, band, min(3.0 * t, 1.0));
		}
		c = darken(c, scanlines * line);
	}

	// 7. Flicker: a fast tremble with a slower irregular modulation on it.
	float flicker = u_flicker_strength;
	if (flicker > 0.0) {
		// (max also keeps the compiler from reassociating u_time * f * TAU,
		// which would round differently from today's u_time * 47.0.)
		float w = max(u_flicker_freq * TAU, 0.0);
		float wave = 0.5 + 0.5 * sin(u_time * w + 0.7 * sin(u_time * (w * (13.0 / 47.0))));
		c = vec4(c.rgb * (1.0 - flicker * wave), c.a);
	}

	// 6. Grain.
	float grain = u_grain_strength;
	if (grain > 0.0) {
		uvec2 cell = uvec2(floor(q));
		float n = hash_centred(cell, uint(floor(u_time * u_grain_rate)));
		c = vec4(c.rgb + vec3(n * grain * 0.5 * c.a), c.a);
	}

	// 5. Vignette.
	float vignette = u_round_vignette;
	if (vignette > 0.0) {
		float r = length(uv) * 0.70710678;
		c = darken(c, vignette * clamp((r - 0.55) / 0.45, 0.0, 1.0));
	}
	c = vec4(clamp(c.rgb, vec3(0.0), vec3(c.a)), clamp(c.a, 0.0, 1.0));
	o_color = mix(here, c, inside);
}
