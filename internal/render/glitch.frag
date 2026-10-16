#version 300 es
// Original implementation for logis from a behavioural specification (clean
// room). MIT, the logis project.
//
// The glitch pass: one full-screen triangle (the vertex shader is
// postshader.go's) over the finished frame, run before the post pass. On the
// panel's glass (u_body shrunk by u_inset, corner radius u_radius) it
// disturbs the picture like an analog/digital signal breaking up; elsewhere
// the frame passes through. Coordinates have a top-left origin (pixel centres
// at +0.5); logis's frames have GL's bottom-up rows (texel row 0 is the
// bottom), so load flips a top-down p. (The clean-room delivery read
// top-down frames unflipped.)
// Up to four extra screen regions (u_region, the tab plates) are treated like
// glass: every part works in them, row-tied patterns share the glass's
// screen coordinates, and displaced reads stay inside their own region.
// Lengths in the controls are logical pixels (times u_scale here). Every
// random term is an integer hash of integer cells and time steps, so a frozen
// u_time gives the same image on any GPU. Premultiplied in and out.
//
// Most parts have a rest and an episode amount: the effective amount is
// mix(rest × strength, episode, E), with
// E = clamp(max(ambient(t), u_event) × strength, 0, 1).
precision highp float;
precision highp int;

uniform sampler2D u_frame;
uniform vec2 u_size;
uniform float u_scale;
uniform float u_time;
uniform vec4 u_body;
uniform float u_radius;
uniform float u_inset;
uniform float u_event;
uniform vec4 u_region[4]; // left, top, width, height; buffer px, top-left origin
uniform float u_region_radius[4];
uniform int u_region_count; // 0 to 4

uniform float u_strength;
uniform float u_episode_period_s, u_episode_duty;
uniform float u_wobble_rest, u_wobble_episode, u_wobble_band_px, u_wobble_speed_hz;
uniform float u_tracking_px, u_tracking_height_px, u_tracking_a_hz, u_tracking_b_hz;
uniform float u_fringe_px, u_fringe_growth;
uniform float u_slice_rest, u_slice_episode, u_slice_px, u_slice_shift_px, u_slice_group, u_slice_rate_hz;
uniform float u_block_rest, u_block_episode, u_block_size, u_block_shift_px, u_block_split_px;
uniform float u_block_brightness, u_block_rate_hz;
uniform float u_flash_rest, u_flash_episode, u_flash_gain, u_flash_rate_hz;
uniform float u_band_rest, u_band_episode, u_band_cell_px, u_band_min, u_band_max, u_band_rate_hz;
uniform float u_static_rest, u_static_episode, u_static_rate_hz;
uniform float u_pulse_rest, u_pulse_episode, u_pulse_pitch_px, u_pulse_drift_px_s;

out vec4 o_color;

// Glass geometry, the vertical span of glass and regions together (used
// only with regions), and the sampling bounds of the current row on the
// screen (glass or region) the fragment is on; set in main.
vec2 g_centre, g_half;
float g_span_top, g_span_h;
float g_lo, g_hi;

float sd_round_box(vec2 p, vec2 hs, float r) {
	vec2 q = abs(p) - hs + vec2(r);
	return length(max(q, vec2(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

// A 32-bit avalanche mixer (xor-shift, odd multiply, twice) and a hash of
// three words built from it; seeds keep the parts' streams apart.
uint mix32(uint h) {
	h ^= h >> 16u;
	h *= 0x7feb352du;
	h ^= h >> 15u;
	h *= 0x846ca68bu;
	h ^= h >> 16u;
	return h;
}
uint hash3(uint a, uint b, uint c) {
	return mix32(a ^ mix32(b ^ mix32(c + 0x9e3779b9u)));
}
float rnd(uint a, uint b, uint c) {
	return float(hash3(a, b, c) >> 8u) * (1.0 / 16777216.0);
}
// Cells and steps are non-negative in practice; the bias keeps them so.
uint cell(float x) { return uint(int(floor(x)) + 0x100000); }
uint step_of(float hz) { return cell(u_time * max(hz, 0.0)); }

// Value noise along one axis: a random value per unit cell, eased between.
float vnoise(float x, uint s, uint seed) {
	float f = fract(x);
	uint i = cell(x);
	return mix(rnd(i, s, seed), rnd(i + 1u, s, seed), f * f * (3.0 - 2.0 * f));
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
// A horizontally displaced read of the current row, mirrored back at the
// glass edges so the border and the outside never smear in.
vec4 fetch(vec2 p, float x) {
	if (g_hi > g_lo) {
		float span = g_hi - g_lo;
		float t = mod(x - g_lo, 2.0 * span);
		x = g_lo + (t > span ? 2.0 * span - t : t);
	} else {
		x = p.x;
	}
	return sample_at(vec2(x, p.y));
}
// Red from x + split, green from x, blue from x - split.
vec4 fetch_split(vec2 p, float x, float split) {
	vec4 r = fetch(p, x + split), g = fetch(p, x), b = fetch(p, x - split);
	return vec4(r.r, g.g, b.b, max(g.a, max(r.a, b.a)));
}

float amount(float rest, float episode, float master, float e) {
	return mix(rest * master, episode, e);
}

// The ambient cycle: a smooth bell, (4x(1-x))², over the last duty share of
// every period, so a period starts calm. A period of 0 turns it off.
float ambient() {
	float period = u_episode_period_s;
	float len = period * clamp(u_episode_duty, 0.0, 1.0);
	if (period <= 0.0 || len <= 0.0) return 0.0;
	float x = (mod(u_time, period) - (period - len)) / len;
	if (x <= 0.0 || x >= 1.0) return 0.0;
	float b = 4.0 * x * (1.0 - x);
	return b * b;
}

// The signal wobble: two stepped band layers of different heights, both
// re-rolled (and their grids moved) every step, multiplied. In episodes the
// curve sparsens, so most bands stay near and a few tear far.
float wobble(float y, float px, float e) {
	uint s = step_of(u_wobble_speed_hz);
	float h1 = max(u_wobble_band_px * u_scale, 1.0), h2 = h1 * 0.29;
	float n1 = rnd(cell((y + rnd(s, 1u, 31u) * h1) / h1), s, 1u) * 2.0 - 1.0;
	float n2 = rnd(cell((y + rnd(s, 2u, 31u) * h2) / h2), s, 2u);
	float w = n1 * (0.55 + 0.45 * n2);
	return sign(w) * pow(abs(w), mix(1.0, 3.0, e)) * px * u_scale;
}

// One tracking sweep: a thin kink that runs down the glass at hz sweeps per
// second, its edge roughened anew at the wobble's speed.
float tracking(float y, float hz, float phase, uint seed) {
	float pad = 4.0 * u_tracking_height_px * u_scale;
	float top, span;
	if (u_region_count > 0) {
		top = g_span_top - pad;
		span = g_span_h + 2.0 * pad;
	} else {
		top = g_centre.y - g_half.y - pad;
		span = 2.0 * g_half.y + 2.0 * pad;
	}
	float at = top + fract(u_time * hz + phase) * span;
	float k = max(1.0 - abs(y - at) / max(u_tracking_height_px * u_scale, 0.5), 0.0);
	float rough = 0.6 + 0.4 * rnd(cell(y), step_of(u_wobble_speed_hz), seed);
	return k * k * rough * u_tracking_px * u_scale;
}

// Slice tears: horizontal slices, a share of them shifted by their own
// amount, plus a smaller offset shared by groups of neighbouring slices.
float slice_tear(float y, float share) {
	if (share <= 0.0) return 0.0;
	uint s = step_of(u_slice_rate_hz);
	float slice = floor(y / max(u_slice_px * u_scale, 1.0));
	float group = floor(slice / max(floor(u_slice_group), 1.0));
	float max_px = u_slice_shift_px * u_scale;
	float off = 0.0;
	if (rnd(cell(group), s, 41u) < share * 1.4)
		off += (rnd(cell(group), s, 42u) * 2.0 - 1.0) * 0.35 * max_px;
	if (rnd(cell(slice), s, 43u) < share) {
		float r = rnd(cell(slice), s, 44u);
		off += (r < 0.5 ? -1.0 : 1.0) * mix(0.15, 1.0, fract(r * 2.0)) * max_px;
	}
	return off;
}

// Block corruption: irregular row and column stripes (value noise over a
// threshold) crossed, then gated by coarse random cells. Returns the block's
// sideways shift, or 0 outside a block.
float block_shift(vec2 q, float a) {
	if (a <= 0.0) return 0.0;
	uint s = step_of(u_block_rate_hz);
	float size = max(u_block_size, 0.05) * u_scale;
	float hy = 11.0 * size, wx = 56.0 * size;
	// Column stripes are drawn anew for each coarse row group, so blocks
	// do not line up down the whole glass.
	uint gx = cell(q.x / (wx * 1.7)), gy = cell(q.y / (hy * 3.3));
	uint sy = s ^ (gy * 0x9e37u);
	float ny = 0.65 * vnoise(q.y / hy, s, 51u) + 0.35 * vnoise(q.y / (hy * 0.41), s, 52u);
	float nx = 0.65 * vnoise(q.x / wx, sy, 53u) + 0.35 * vnoise(q.x / (wx * 0.37), sy, 54u);
	if (ny < 1.0 - 0.55 * a || nx < 0.9 - 0.5 * a) return 0.0;
	if (rnd(gx, gy ^ s, 55u) > 0.3 + 0.7 * a) return 0.0;
	float r = rnd(cell(q.y / (hy * 1.3)), gx ^ s, 56u);
	return (r < 0.5 ? -1.0 : 1.0) * mix(0.4, 1.0, fract(r * 2.0)) * u_block_shift_px * u_scale;
}

// The noise band: one band of the screen, re-rolled every step, filled with
// coarse cells of near-binary noise. Returns its noise value and weight.
vec2 noise_band(vec2 q) {
	uint s = step_of(u_band_rate_hz);
	float h = mix(u_band_min, u_band_max, rnd(s, 61u, 0u)) * 2.0 * g_half.y;
	// Each branch keeps its whole expression, so a driver contracts the
	// region-free one exactly as before regions existed (byte-identical).
	float y;
	if (u_region_count > 0) // anywhere over glass and regions
		y = q.y - (g_span_top - (g_centre.y - g_half.y) + rnd(s, 62u, 0u) * (g_span_h - h));
	else
		y = q.y - rnd(s, 62u, 0u) * (2.0 * g_half.y - h);
	if (y < 0.0 || y > h) return vec2(0.0);
	float edge = smoothstep(0.0, 2.0 * u_scale, min(y, h - y));
	vec2 c = q / max(u_band_cell_px * u_scale, 1.0);
	float n = rnd(cell(c.x), cell(c.y), s ^ 63u);
	float row = 0.55 + 0.45 * rnd(cell(c.y), s, 64u); // rows of uneven gain
	return vec2(smoothstep(0.25, 0.75, n) * row, edge);
}

void main() {
	vec2 p = vec2(gl_FragCoord.x, u_size.y - gl_FragCoord.y);
	vec4 here = load(ivec2(p));
	vec2 hs = u_body.zw * 0.5;
	g_centre = u_body.xy + hs;
	g_half = max(hs - vec2(u_inset), vec2(1.0));
	float r = max(min(u_radius, min(hs.x, hs.y)) - u_inset, 0.0);
	float inside = clamp(0.5 - sd_round_box(p - g_centre, g_half, r), 0.0, 1.0);
	float master = clamp(u_strength, 0.0, 1.0);
	if (master <= 0.0) { o_color = here; return; }

	// The screen this fragment is on: the glass, else the first region that
	// covers it. Regions take no inset; the glass wins where they overlap.
	vec2 centre = g_centre, half_size = g_half;
	bool on_region = false;
	int count = clamp(u_region_count, 0, 4);
	if (inside <= 0.0) {
		for (int i = 0; i < 4; i++) {
			if (i >= count) break;
			vec2 rh = max(u_region[i].zw * 0.5, vec2(1.0));
			vec2 rc = u_region[i].xy + rh;
			float rr = clamp(u_region_radius[i], 0.0, min(rh.x, rh.y));
			float cover = clamp(0.5 - sd_round_box(p - rc, rh, rr), 0.0, 1.0);
			if (cover > 0.0) {
				inside = cover;
				on_region = true;
				centre = rc;
				half_size = rh;
				r = rr;
				break;
			}
		}
		if (inside <= 0.0) { o_color = here; return; }
	}
	g_span_top = g_centre.y - g_half.y;
	float span_bottom = g_centre.y + g_half.y;
	for (int i = 0; i < 4; i++) {
		if (i >= count) break;
		g_span_top = min(g_span_top, u_region[i].y);
		span_bottom = max(span_bottom, u_region[i].y + u_region[i].w);
	}
	g_span_h = span_bottom - g_span_top;

	// The row's horizontal extent on its screen, kept a margin from its edge,
	// so reads never leave the screen (a plate never pulls in panel content).
	float margin = 1.0 + u_scale;
	if (on_region) {
		// The region shrunk by the margin all round: every tap then lands on
		// fully covered pixels, even along the flat ends of a corner arc.
		// Rows outside it are not displaced.
		vec2 hb = half_size - vec2(margin);
		float rb = max(r - margin, 0.0);
		float dy = abs(p.y - centre.y) - (hb.y - rb);
		float half_w = hb.x;
		if (dy > 0.0) half_w = dy < rb ? hb.x - rb + sqrt(rb * rb - dy * dy) : -1.0;
		g_lo = centre.x - half_w;
		g_hi = centre.x + half_w;
	} else {
		float dy = abs(p.y - centre.y) - (half_size.y - r);
		float half_w = half_size.x;
		if (dy > 0.0) half_w = dy < r ? half_size.x - r + sqrt(r * r - dy * dy) : 0.0;
		g_lo = centre.x - half_w + margin;
		g_hi = centre.x + half_w - margin;
	}

	float e = clamp(max(ambient(), clamp(u_event, 0.0, 1.0)) * u_strength, 0.0, 1.0);
	// Shared screen coordinates, relative to the glass's top left (negative
	// above it), so regions follow the glass's rows.
	vec2 q = p - (g_centre - g_half);

	float dx = wobble(q.y, amount(u_wobble_rest, u_wobble_episode, master, e), e);
	float wob = abs(dx);
	dx += master * (tracking(p.y, u_tracking_b_hz, 0.61, 72u) - tracking(p.y, u_tracking_a_hz, 0.13, 71u));
	dx += slice_tear(q.y, amount(u_slice_rest, u_slice_episode, master, e));

	float shift = block_shift(q, amount(u_block_rest, u_block_episode, master, e));
	vec4 c;
	if (shift != 0.0) {
		c = fetch_split(p, p.x - dx - shift, u_block_split_px * u_scale);
		float b = u_block_brightness;
		c = vec4(c.rgb * (1.0 + b) + vec3(0.15 * b * c.a), c.a);
	} else {
		float split = master * (u_fringe_px * u_scale + u_fringe_growth * wob);
		c = fetch_split(p, p.x - dx, split);
	}

	// Slice flashes: a small share of slices a third (u_flash_gain) brighter.
	float flash = amount(u_flash_rest, u_flash_episode, master, e);
	if (flash > 0.0) {
		float slice = floor(q.y / max(u_slice_px * u_scale, 1.0));
		if (rnd(cell(slice), step_of(u_flash_rate_hz), 81u) < flash)
			c = vec4(c.rgb * (1.0 + u_flash_gain), c.a);
	}

	// Line pulse: a dense ripple near the pixel pitch, drifting.
	float pulse = amount(u_pulse_rest, u_pulse_episode, master, e);
	if (pulse > 0.0) {
		float y = p.y / u_scale + u_time * u_pulse_drift_px_s;
		float wave = cos(6.2831853 * y / max(u_pulse_pitch_px, 0.25));
		c = vec4(c.rgb * (1.0 + pulse * wave), c.a);
	}

	float band = amount(u_band_rest, u_band_episode, master, e);
	if (band > 0.0) {
		vec2 n = noise_band(q);
		c = vec4(mix(c.rgb, vec3(n.x * c.a), band * n.y), c.a);
	}

	// Static: fine white noise, premultiplied so clear pixels stay clear.
	float stat = amount(u_static_rest, u_static_episode, master, e);
	if (stat > 0.0) {
		float n = rnd(cell(p.x), cell(p.y), step_of(u_static_rate_hz) ^ 0x5eedu) - 0.5;
		c = vec4(c.rgb + vec3(2.0 * n * stat * c.a), c.a);
	}

	c = vec4(clamp(c.rgb, vec3(0.0), vec3(c.a)), clamp(c.a, 0.0, 1.0));
	o_color = mix(here, c, inside);
}
