// logis images: quads from the image atlas, one instance per image, drawn
// texel for texel: each pixel loads exactly one texel, no filtering. Atlas
// texels and the output are premultiplied, with sRGB-encoded channels like
// the SDF pipeline's colours.

struct Frame {
    size: vec2f,   // target size in buffer pixels
    srgb: f32,     // 1 when the target encodes sRGB itself (store linear)
    pad: f32,
};
@group(0) @binding(0) var<uniform> frame: Frame;
@group(0) @binding(1) var atlas: texture_2d<f32>;

struct Quad {
    @location(0) rect: vec4f,   // x, y, w, h in whole buffer pixels
    @location(1) texel: vec4f,  // the image's top-left texel in the atlas, -, -
};

struct Varyings {
    @builtin(position) pos: vec4f,
    // Atlas texel minus buffer pixel: add a pixel to get its texel.
    @location(0) @interpolate(flat) offset: vec2f,
};

@vertex
fn vs(@builtin(vertex_index) vi: u32, quad: Quad) -> Varyings {
    // Triangle strip corners (0,0) (1,0) (0,1) (1,1).
    let corner = vec2f(f32(vi & 1u), f32((vi >> 1u) & 1u));
    let p = quad.rect.xy + corner * quad.rect.zw;
    var out: Varyings;
    out.pos = vec4f(p.x / frame.size.x * 2.0 - 1.0, 1.0 - p.y / frame.size.y * 2.0, 0.0, 1.0);
    out.offset = quad.texel.xy - quad.rect.xy;
    return out;
}

fn srgb_to_linear(c: vec3f) -> vec3f {
    let lo = c / 12.92;
    let hi = pow((c + vec3f(0.055)) / 1.055, vec3f(2.4));
    return select(hi, lo, c <= vec3f(0.04045));
}

@fragment
fn fs(in: Varyings) -> @location(0) vec4f {
    // in.pos.xy is the pixel centre (x + 0.5); its floor is the pixel.
    var col = textureLoad(atlas, vec2i(floor(in.pos.xy + in.offset)), 0);
    if (frame.srgb > 0.5 && col.a > 0.0) {
        col = vec4f(srgb_to_linear(col.rgb / col.a) * col.a, col.a);
    }
    return col;
}
