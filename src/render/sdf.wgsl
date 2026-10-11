// logis SDF shapes: rounded rectangles with fill, border and outer glow,
// each paint solid or a linear gradient of up to four evenly spaced stops,
// and a highlight that may stand on the border. One instance per shape,
// drawn as a quad covering the shape plus its glow. Inputs and output are
// premultiplied.

struct Frame {
    size: vec2f,   // target size in buffer pixels
    scale: f32,    // logical -> buffer pixels
    srgb: f32,     // 1 when the target encodes sRGB itself (store linear)
};
@group(0) @binding(0) var<uniform> frame: Frame;

struct Shape {
    @location(0) rect: vec4f,        // x, y, w, h (logical)
    @location(1) params: vec4f,      // radius, border width, glow radius, highlight angle
    @location(2) fill0: vec4f,
    @location(3) fill1: vec4f,
    @location(4) fill2: vec4f,
    @location(5) fill3: vec4f,
    @location(6) fill_line: vec4f,   // start xy, end xy (logical)
    @location(7) border0: vec4f,
    @location(8) border1: vec4f,
    @location(9) border2: vec4f,
    @location(10) border3: vec4f,
    @location(11) border_line: vec4f,
    @location(12) glow: vec4f,
    @location(13) highlight: vec4f,  // its colour
    @location(14) extra: vec4f,      // fill stops, border stops, highlight width (rad), -
};

// Everything in buffer pixels; flat because it is per shape. The stops
// travel as half floats, two colours per vec4<u32>, so that the varyings
// stay within the downlevel limit of 15.
struct Varyings {
    @builtin(position) pos: vec4f,
    @location(0) @interpolate(flat) rect: vec4f,     // centre xy, half extent xy
    @location(1) @interpolate(flat) params: vec4f,
    @location(2) @interpolate(flat) fill01: vec4<u32>,
    @location(3) @interpolate(flat) fill23: vec4<u32>,
    @location(4) @interpolate(flat) fill_line: vec4f,
    @location(5) @interpolate(flat) border01: vec4<u32>,
    @location(6) @interpolate(flat) border23: vec4<u32>,
    @location(7) @interpolate(flat) border_line: vec4f,
    @location(8) @interpolate(flat) glow: vec4f,
    @location(9) @interpolate(flat) highlight: vec4f,
    @location(10) @interpolate(flat) extra: vec4f,
};

fn pack_pair(a: vec4f, b: vec4f) -> vec4<u32> {
    return vec4<u32>(pack2x16float(a.xy), pack2x16float(a.zw), pack2x16float(b.xy), pack2x16float(b.zw));
}

fn first(p: vec4<u32>) -> vec4f {
    return vec4f(unpack2x16float(p.x), unpack2x16float(p.y));
}

fn second(p: vec4<u32>) -> vec4f {
    return vec4f(unpack2x16float(p.z), unpack2x16float(p.w));
}

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
    // Radius, border and glow scale; the highlight's angle does not.
    out.params = vec4f(shape.params.xyz * s, shape.params.w);
    out.fill01 = pack_pair(shape.fill0, shape.fill1);
    out.fill23 = pack_pair(shape.fill2, shape.fill3);
    out.fill_line = shape.fill_line * s;
    out.border01 = pack_pair(shape.border0, shape.border1);
    out.border23 = pack_pair(shape.border2, shape.border3);
    out.border_line = shape.border_line * s;
    out.glow = shape.glow;
    out.highlight = shape.highlight;
    out.extra = shape.extra;
    return out;
}

// Signed distance to a rounded box centred at the origin.
fn sd_round_box(p: vec2f, half: vec2f, r: f32) -> f32 {
    let q = abs(p) - half + vec2f(r);
    return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

// A paint at p: `count` premultiplied stops evenly spaced along the line.
fn paint(p: vec2f, s01: vec4<u32>, s23: vec4<u32>, count: f32, line: vec4f) -> vec4f {
    let s0 = first(s01);
    if (count < 1.5) {
        return s0;
    }
    let dir = line.zw - line.xy;
    let len2 = dot(dir, dir);
    var t = 0.0;
    if (len2 > 1e-6) {
        t = clamp(dot(p - line.xy, dir) / len2, 0.0, 1.0);
    }
    let x = t * (count - 1.0);
    if (x <= 1.0) {
        return mix(s0, second(s01), x);
    }
    if (x <= 2.0) {
        return mix(second(s01), first(s23), x - 1.0);
    }
    return mix(first(s23), second(s23), x - 2.0);
}

// How much of the highlight lies at p: 1 at its angle, fading to almost
// nothing at its angular width either side.
fn highlight_at(p: vec2f, centre: vec2f, half: vec2f, angle: f32, width: f32) -> f32 {
    let rel = (p - centre) / max(half, vec2f(1e-3));
    let a = atan2(rel.y, rel.x);
    var d = a - angle;
    d = d - 6.2831853 * round(d / 6.2831853);
    let x = d / max(width, 1e-4);
    return exp(-4.0 * x * x);
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
    let fill = paint(p, in.fill01, in.fill23, in.extra.x, in.fill_line) * inner;
    var border = paint(p, in.border01, in.border23, in.extra.y, in.border_line);
    if (in.highlight.a > 0.0) {
        let k = highlight_at(p, in.rect.xy, in.rect.zw, in.params.w, in.extra.z);
        border = border * (1.0 - k * in.highlight.a) + in.highlight * k;
    }
    var col = fill + border * (cover - inner);

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
