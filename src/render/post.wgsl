// logis post-processing: one full-screen pass over a finished frame. Within
// a rounded rectangle (the panel's body) it applies curvature, chromatic
// aberration, bloom, scanlines, flicker, grain and a vignette, each by its
// strength (0 is off); outside it the frame passes through (the glow).
// Pixels are read with textureLoad (no sampler) and every random term comes
// from an integer hash, so a frozen time gives the same image on any GPU.
// Premultiplied in, premultiplied out.

struct Params {
    size: vec2f,     // target size in buffer pixels
    scale: f32,      // logical -> buffer pixels
    time: f32,       // seconds
    body: vec4f,     // left, top, width, height in buffer pixels
    radius: f32,     // corner radius in buffer pixels
    pad0: f32,
    pad1: f32,
    pad2: f32,
    a: vec4f,        // scanlines, grain, flicker, vignette
    b: vec4f,        // bloom, curvature, aberration, -
};
@group(0) @binding(0) var<uniform> u: Params;

// Twelve directions, 30 degrees apart.
const RING = array<vec2f, 12>(
    vec2f(1.0, 0.0), vec2f(0.8660254, 0.5), vec2f(0.5, 0.8660254),
    vec2f(0.0, 1.0), vec2f(-0.5, 0.8660254), vec2f(-0.8660254, 0.5),
    vec2f(-1.0, 0.0), vec2f(-0.8660254, -0.5), vec2f(-0.5, -0.8660254),
    vec2f(0.0, -1.0), vec2f(0.5, -0.8660254), vec2f(0.8660254, -0.5),
);
@group(0) @binding(1) var frame_in: texture_2d<f32>;

@vertex
fn vs(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
    return vec4f(p * 2.0 - 1.0, 0.0, 1.0);
}

fn sd_round_box(p: vec2f, half: vec2f, r: f32) -> f32 {
    let q = abs(p) - half + vec2f(r);
    return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

fn load(p: vec2i) -> vec4f {
    let last = vec2i(u.size) - vec2i(1);
    return textureLoad(frame_in, clamp(p, vec2i(0), last), 0);
}

// Bilinear between the four pixels around p (pixel centres at .5).
fn sample(p: vec2f) -> vec4f {
    let q = p - vec2f(0.5);
    let i = vec2i(floor(q));
    let f = q - floor(q);
    let top = mix(load(i), load(i + vec2i(1, 0)), f.x);
    let bottom = mix(load(i + vec2i(0, 1)), load(i + vec2i(1, 1)), f.x);
    return mix(top, bottom, f.y);
}

// A PCG-style integer hash to 0..1.
fn hash(cell: vec2u, seed: u32) -> f32 {
    var h = cell.x * 747796405u + cell.y * 2891336453u + seed * 277803737u + 1u;
    h = ((h >> ((h >> 28u) + 4u)) ^ h) * 277803737u;
    h = (h >> 22u) ^ h;
    return f32(h & 0xffffu) / 65535.0;
}

// Black laid over c with opacity k (premultiplied "over"), only as far as
// c itself is there: a fading panel keeps its effects in proportion.
fn darken(c: vec4f, k: f32) -> vec4f {
    return vec4f(c.rgb * (1.0 - k), c.a + (1.0 - c.a) * k * c.a);
}

@fragment
fn fs(@builtin(position) pos: vec4f) -> @location(0) vec4f {
    let p = pos.xy;
    let here = load(vec2i(p));
    let half = u.body.zw * 0.5;
    let centre = u.body.xy + half;
    let inside = clamp(0.5 - sd_round_box(p - centre, half, u.radius), 0.0, 1.0);
    if (inside <= 0.0) {
        return here;
    }
    let uv = (p - centre) / max(half, vec2f(1.0));

    // Curved glass: each pixel shows what lies further out, more so
    // towards the edges, so straight lines bow.
    var q = p;
    let curvature = u.b.y;
    if (curvature > 0.0) {
        q = centre + uv * (1.0 + 0.25 * curvature * dot(uv, uv)) * half;
    }
    var c = here;
    let aberration = u.b.z;
    if (aberration > 0.0) {
        let off = vec2f(2.0 * aberration * u.scale, 0.0);
        let mid = sample(q);
        let red = sample(q + off);
        let blue = sample(q - off);
        c = vec4f(red.r, mid.g, blue.b, max(mid.a, max(red.a, blue.a)));
    } else if (curvature > 0.0) {
        c = sample(q);
    }

    // Bloom: light from bright pixels around, on two rings. The directions
    // are constants (no cos/sin), so every GPU reads the same pixels; on
    // curved glass the taps lie between pixels and are read bilinearly, so
    // a rounding difference moves a tap a little, never to another pixel.
    let bloom = u.b.x;
    if (bloom > 0.0) {
        var light = vec3f(0.0);
        for (var i = 0; i < 12; i++) {
            let dir = RING[i];
            for (var ring = 0; ring < 2; ring++) {
                let reach = (3.0 + 4.0 * f32(ring)) * u.scale;
                var s: vec4f;
                if (curvature > 0.0) {
                    s = sample(q + dir * reach);
                } else {
                    s = load(vec2i(floor(q + dir * reach)));
                }
                light += max(s.rgb - vec3f(0.35 * s.a), vec3f(0.0));
            }
        }
        c = vec4f(c.rgb + light * (bloom * 2.0 / 24.0), c.a);
    }

    // Scanlines: a soft dark line every three logical pixels.
    let scanlines = u.a.x;
    if (scanlines > 0.0) {
        let row = fract((p.y - u.body.y) / (3.0 * u.scale));
        let line = pow(0.5 + 0.5 * cos(row * 6.2831853), 2.0);
        c = darken(c, scanlines * line);
    }

    // Flicker: the brightness trembling with time.
    let flicker = u.a.z;
    if (flicker > 0.0) {
        let wave = 0.5 + 0.5 * sin(u.time * 47.0 + 0.7 * sin(u.time * 13.0));
        c = vec4f(c.rgb * (1.0 - flicker * wave), c.a);
    }

    // Grain: noise per buffer pixel (integer coordinates, the same on
    // every GPU), new 24 times a second.
    let grain = u.a.y;
    if (grain > 0.0) {
        let cell = vec2u(floor(p));
        let n = hash(cell, u32(floor(u.time * 24.0))) - 0.5;
        c = vec4f(c.rgb + vec3f(n * grain * 0.5 * c.a), c.a);
    }

    // Vignette: darker towards the corners.
    let vignette = u.a.w;
    if (vignette > 0.0) {
        let r = length(uv) * 0.70710678;
        c = darken(c, vignette * clamp((r - 0.55) / 0.45, 0.0, 1.0));
    }

    c = vec4f(clamp(c.rgb, vec3f(0.0), vec3f(c.a)), clamp(c.a, 0.0, 1.0));
    return mix(here, c, inside);
}
