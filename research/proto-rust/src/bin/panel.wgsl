// Research spike: grimdark panel drawn entirely in one fragment shader.
// SDF rounded panel, brass->crimson border with a travelling highlight,
// outer glow, scanlines, film grain, flicker, vignette, selected-row band.
// Output is premultiplied alpha.
struct U {
    size: vec2f,      // surface size in buffer pixels
    panel: vec2f,     // panel size in buffer pixels (centred)
    time: f32,
    scale: f32,
    sel_y0: f32,      // selected row band, buffer pixels from panel top
    sel_y1: f32,
    dim: f32,         // full-surface dim behind the panel (fullscreen mode)
    _pad0: f32,
    _pad1: f32,
    _pad2: f32,
};
@group(0) @binding(0) var<uniform> u: U;

@vertex
fn vs(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
    return vec4f(p * 2.0 - 1.0, 0.0, 1.0);
}

fn sd_round_box(p: vec2f, b: vec2f, r: f32) -> f32 {
    let q = abs(p) - b + vec2f(r);
    return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;
}

fn hash(p: vec2f) -> f32 {
    return fract(sin(dot(p, vec2f(12.9898, 78.233))) * 43758.5453);
}

fn over(dst: vec4f, src: vec4f) -> vec4f { // premultiplied
    return src + dst * (1.0 - src.a);
}

@fragment
fn fs(@builtin(position) pos: vec4f) -> @location(0) vec4f {
    let s = u.scale;
    let px = pos.xy;
    let c = u.size * 0.5;
    let p = px - c;
    let half = u.panel * 0.5;
    let r = 14.0 * s;
    let d = sd_round_box(p, half, r);
    let bw = 2.0 * s;

    var col = vec4f(0.0, 0.0, 0.0, u.dim);           // dim behind (premultiplied black)

    // Outer glow: crimson, exponential falloff, gently pulsing.
    let pulse = 0.85 + 0.15 * sin(u.time * 2.1);
    let glow = exp(-max(d, 0.0) / (9.0 * s)) * 0.55 * pulse * step(0.0, d);
    let crimson = vec3f(0.70, 0.06, 0.05);
    col = over(col, vec4f(crimson * glow, glow));

    let inside = clamp(0.5 - d, 0.0, 1.0);
    let inner = clamp(0.5 - (d + bw), 0.0, 1.0);

    // Body: soot-dark with a slight vertical gradient.
    let ty = clamp((px.y - (c.y - half.y)) / (2.0 * half.y), 0.0, 1.0);
    var body = mix(vec3f(0.085, 0.055, 0.045), vec3f(0.045, 0.030, 0.026), ty);
    // Selected row band with a soft glow edge.
    let ly = px.y - (c.y - half.y);
    let band = smoothstep(u.sel_y0 - 1.0, u.sel_y0 + 1.0, ly) * (1.0 - smoothstep(u.sel_y1 - 1.0, u.sel_y1 + 1.0, ly));
    let edge = exp(-abs(px.x - (c.x - half.x + 10.0 * s)) / (2.0 * s)) * band;
    body = mix(body, vec3f(0.26, 0.06, 0.05), band * 0.85) + vec3f(0.85, 0.55, 0.10) * edge * 0.9;
    // Scanlines, grain, flicker, vignette.
    let scan = 0.93 + 0.07 * sin(px.y * 3.14159 / (1.5 * s));
    let grain = (hash(floor(px / s) + fract(u.time * 7.0) * 113.0) - 0.5) * 0.05;
    let flicker = 0.985 + 0.015 * sin(u.time * 47.0);
    let vig = 1.0 - 0.35 * pow(length(p / half), 2.5);
    body = (body * scan * flicker + grain) * vig;
    let body_a = 0.78;
    col = over(col, vec4f(body * body_a, body_a) * inner);

    // Border: brass at the top to crimson at the bottom, with a highlight
    // that travels around the perimeter.
    let ang = atan2(p.y / half.y, p.x / half.x);
    let travel = pow(0.5 + 0.5 * cos(ang - u.time * 0.9), 24.0);
    let brass = vec3f(0.83, 0.63, 0.09);
    let bcol = mix(brass, crimson, ty) + vec3f(1.0, 0.85, 0.5) * travel * 0.8;
    let bmask = inside - inner;
    col = over(col, vec4f(bcol, 1.0) * bmask);

    // Brass rule under the prompt row.
    let rule_y = c.y - half.y + 53.0 * s;
    let rule = (1.0 - smoothstep(0.0, 1.2 * s, abs(px.y - rule_y))) * step(abs(p.x), half.x - 18.0 * s) * inner;
    col = over(col, vec4f(brass * 0.8, 0.8) * rule);
    return col;
}
