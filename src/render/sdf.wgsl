// logis SDF shapes: rounded rectangles with fill, border and outer glow, each
// paint solid or a linear gradient. One instance per shape, drawn as a quad
// covering the shape plus its glow. Inputs and output are premultiplied.

struct Frame {
    size: vec2f,   // target size in buffer pixels
    scale: f32,    // logical -> buffer pixels
    srgb: f32,     // 1 when the target encodes sRGB itself (store linear)
};
@group(0) @binding(0) var<uniform> frame: Frame;

struct Shape {
    @location(0) rect: vec4f,        // x, y, w, h (logical)
    @location(1) params: vec4f,      // radius, border width, glow radius, -
    @location(2) fill_from: vec4f,
    @location(3) fill_to: vec4f,
    @location(4) fill_line: vec4f,   // start xy, end xy (logical)
    @location(5) border_from: vec4f,
    @location(6) border_to: vec4f,
    @location(7) border_line: vec4f,
    @location(8) glow: vec4f,
};

// Everything in buffer pixels; flat because it is per shape.
struct Varyings {
    @builtin(position) pos: vec4f,
    @location(0) @interpolate(flat) rect: vec4f,     // centre xy, half extent xy
    @location(1) @interpolate(flat) params: vec4f,
    @location(2) @interpolate(flat) fill_from: vec4f,
    @location(3) @interpolate(flat) fill_to: vec4f,
    @location(4) @interpolate(flat) fill_line: vec4f,
    @location(5) @interpolate(flat) border_from: vec4f,
    @location(6) @interpolate(flat) border_to: vec4f,
    @location(7) @interpolate(flat) border_line: vec4f,
    @location(8) @interpolate(flat) glow: vec4f,
};

@vertex
fn vs(@builtin(vertex_index) vi: u32, shape: Shape) -> Varyings {
    let s = frame.scale;
    // Triangle strip corners (0,0) (1,0) (0,1) (1,1), pushed out by the glow
    // and one pixel of anti-aliasing.
    let corner = vec2f(f32(vi & 1u), f32((vi >> 1u) & 1u));
    let outset = shape.params.z * s + 1.0;
    let origin = shape.rect.xy * s - vec2f(outset);
    let extent = shape.rect.zw * s + vec2f(2.0 * outset);
    let p = origin + corner * extent;

    var out: Varyings;
    out.pos = vec4f(p.x / frame.size.x * 2.0 - 1.0, 1.0 - p.y / frame.size.y * 2.0, 0.0, 1.0);
    let half = shape.rect.zw * s * 0.5;
    out.rect = vec4f(shape.rect.xy * s + half, half);
    out.params = shape.params * s;
    out.fill_from = shape.fill_from;
    out.fill_to = shape.fill_to;
    out.fill_line = shape.fill_line * s;
    out.border_from = shape.border_from;
    out.border_to = shape.border_to;
    out.border_line = shape.border_line * s;
    out.glow = shape.glow;
    return out;
}

// Signed distance to a rounded box centred at the origin.
fn sd_round_box(p: vec2f, half: vec2f, r: f32) -> f32 {
    let q = abs(p) - half + vec2f(r);
    return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

// A paint at p: both colours premultiplied, interpolated along the line.
fn paint(p: vec2f, start_colour: vec4f, end_colour: vec4f, line: vec4f) -> vec4f {
    let dir = line.zw - line.xy;
    let len2 = dot(dir, dir);
    var t = 0.0;
    if (len2 > 1e-6) {
        t = clamp(dot(p - line.xy, dir) / len2, 0.0, 1.0);
    }
    return mix(start_colour, end_colour, t);
}

fn srgb_to_linear(c: vec3f) -> vec3f {
    let lo = c / 12.92;
    let hi = pow((c + vec3f(0.055)) / 1.055, vec3f(2.4));
    return select(hi, lo, c <= vec3f(0.04045));
}

@fragment
fn fs(in: Varyings) -> @location(0) vec4f {
    let p = in.pos.xy;
    let d = sd_round_box(p - in.rect.xy, in.rect.zw, in.params.x);
    // Coverage of the whole shape and of the interior inside the border,
    // anti-aliased over one buffer pixel.
    let cover = clamp(0.5 - d, 0.0, 1.0);
    let inner = clamp(0.5 - (d + in.params.y), 0.0, 1.0);
    let fill = paint(p, in.fill_from, in.fill_to, in.fill_line) * inner;
    let border = paint(p, in.border_from, in.border_to, in.border_line) * (cover - inner);
    var col = fill + border;

    let reach = in.params.z;
    if (reach > 0.0) {
        let t = clamp(max(d, 0.0) / reach, 0.0, 1.0);
        let falloff = exp(-3.0 * t) * (1.0 - t);
        col = col + in.glow * falloff * (1.0 - cover);
    }

    if (frame.srgb > 0.5 && col.a > 0.0) {
        col = vec4f(srgb_to_linear(col.rgb / col.a) * col.a, col.a);
    }
    return col;
}
