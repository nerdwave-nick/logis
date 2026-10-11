//! What the renderer draws: a retained list of SDF shapes in logical pixels,
//! painted in order. Colours are sRGB with straight alpha here; the GPU sees
//! them premultiplied (spec §4.3).

use super::atlas::ImageId;

/// An sRGB colour with straight (not premultiplied) alpha, channels in 0..=1.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Rgba {
    /// Red.
    pub r: f32,
    /// Green.
    pub g: f32,
    /// Blue.
    pub b: f32,
    /// Opacity.
    pub a: f32,
}

impl Rgba {
    /// Fully transparent.
    pub const TRANSPARENT: Self = Self::new(0.0, 0.0, 0.0, 0.0);

    /// From channels in 0..=1.
    pub const fn new(r: f32, g: f32, b: f32, a: f32) -> Self {
        Self { r, g, b, a }
    }

    /// An opaque colour from `0xRRGGBB`.
    pub const fn hex(rgb: u32) -> Self {
        Self::new(
            ((rgb >> 16) & 0xff) as f32 / 255.0,
            ((rgb >> 8) & 0xff) as f32 / 255.0,
            (rgb & 0xff) as f32 / 255.0,
            1.0,
        )
    }

    /// The same colour with another opacity.
    pub const fn with_alpha(self, a: f32) -> Self {
        Self::new(self.r, self.g, self.b, a)
    }

    /// Premultiplied channels, clamped to 0..=1, as the shader consumes them.
    pub fn premultiplied(self) -> [f32; 4] {
        let a = self.a.clamp(0.0, 1.0);
        let c = |v: f32| v.clamp(0.0, 1.0) * a;
        [c(self.r), c(self.g), c(self.b), a]
    }
}

/// How an area is painted.
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Paint {
    /// One colour.
    Solid(Rgba),
    /// A linear gradient from `from` at `start` to `to` at `end` (logical
    /// pixels, surface coordinates), interpolated premultiplied and clamped
    /// beyond its ends.
    Linear {
        /// Where `from` is reached.
        start: [f32; 2],
        /// Where `to` is reached.
        end: [f32; 2],
        /// Colour at `start`.
        from: Rgba,
        /// Colour at `end`.
        to: Rgba,
    },
    /// A linear gradient through three or four evenly spaced stops.
    Ramp(Ramp),
}

impl Paint {
    /// A gradient from `start` to `end` through `stops`, evenly spaced (at
    /// most [`Ramp::MAX`]): one stop is [`Paint::Solid`], two are
    /// [`Paint::Linear`], more a [`Paint::Ramp`]. No stop is transparent.
    pub fn ramp(start: [f32; 2], end: [f32; 2], stops: &[Rgba]) -> Self {
        match stops {
            [] => Self::Solid(Rgba::TRANSPARENT),
            [only] => Self::Solid(*only),
            [from, to] => Self::Linear {
                start,
                end,
                from: *from,
                to: *to,
            },
            _ => Self::Ramp(Ramp::new(start, end, stops)),
        }
    }
}

/// A linear gradient through up to [`Ramp::MAX`] evenly spaced stops, from
/// `start` to `end` (logical pixels, surface coordinates), interpolated
/// premultiplied and clamped beyond its ends.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Ramp {
    /// Where the first stop is reached.
    pub start: [f32; 2],
    /// Where the last stop is reached.
    pub end: [f32; 2],
    stops: [Rgba; Ramp::MAX],
    len: usize,
}

impl Ramp {
    /// The most stops a ramp holds (what the shader takes).
    pub const MAX: usize = 4;

    /// The first [`Self::MAX`] of `stops` (at least one; none is
    /// transparent).
    pub fn new(start: [f32; 2], end: [f32; 2], stops: &[Rgba]) -> Self {
        let mut ramp = Self {
            start,
            end,
            stops: [Rgba::TRANSPARENT; Self::MAX],
            len: stops.len().clamp(1, Self::MAX),
        };
        for (slot, stop) in ramp.stops.iter_mut().zip(stops) {
            *slot = *stop;
        }
        ramp
    }

    /// Its stops, in order.
    pub fn stops(&self) -> &[Rgba] {
        &self.stops[..self.len]
    }

    /// The same ramp with every stop changed by `f` and its line moved by
    /// `shift`.
    pub fn map(mut self, f: impl Fn(Rgba) -> Rgba, shift: impl Fn([f32; 2]) -> [f32; 2]) -> Self {
        for stop in &mut self.stops[..self.len] {
            *stop = f(*stop);
        }
        self.start = shift(self.start);
        self.end = shift(self.end);
        self
    }
}

/// A border drawn inside the shape's edge.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Border {
    /// Width in logical pixels.
    pub width: f32,
    /// How the border is painted.
    pub paint: Paint,
    /// A bright spot on the border, if any.
    pub highlight: Option<Highlight>,
}

/// A bright spot on a border, at an angle around the shape's centre (the
/// travelling highlight: its owner turns the angle over time).
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Highlight {
    /// Where it stands, in radians: 0 is the right, turning clockwise (y
    /// points down), so -π/2 is the top. Measured on the shape's box
    /// squashed to a square, so it sweeps a wide box evenly.
    pub angle: f32,
    /// How far it reaches along the border either side of its centre, in
    /// logical pixels.
    pub reach: f32,
    /// Its colour at the centre, fading to nothing at its reach.
    pub color: Rgba,
}

/// A soft glow outside the shape's edge, fading to nothing at `radius`.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Glow {
    /// Reach in logical pixels.
    pub radius: f32,
    /// Colour at the edge; its alpha is the glow's strength.
    pub color: Rgba,
}

/// A rounded rectangle with optional border and glow, in logical pixels.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct RoundedRect {
    /// Left, top, width, height.
    pub rect: [f32; 4],
    /// Corner radius, clamped to half the shorter side.
    pub radius: f32,
    /// The interior.
    pub fill: Paint,
    /// An optional border.
    pub border: Option<Border>,
    /// An optional outer glow.
    pub glow: Option<Glow>,
}

/// An atlas image drawn texel for texel at the pixel size it was rasterised
/// for, its top-left corner snapped to the nearest buffer pixel so it stays
/// crisp at fractional scales.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct ImageQuad {
    /// The image, from the renderer's [`super::ImageAtlas`].
    pub image: ImageId,
    /// Top-left corner in logical pixels.
    pub origin: [f32; 2],
}

/// Everything one frame shows: a clear colour, then shapes, then images,
/// then text, each in paint order. Text is the top layer, so labels and
/// badges stay legible over icons.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Scene {
    /// The colour the frame starts from (transparent for a layer surface).
    pub clear: Option<Rgba>,
    /// Shapes, back to front.
    pub shapes: Vec<RoundedRect>,
    /// Images, back to front, over all shapes.
    pub images: Vec<ImageQuad>,
    /// Lines of text, over shapes and images, drawn when the renderer has
    /// fonts.
    pub texts: Vec<super::text::Text>,
}

/// Floats per shape instance: fifteen `vec4f` vertex attributes.
pub(crate) const INSTANCE_FLOATS: usize = 60;

/// Packs a shape into the instance layout of `sdf.wgsl`: rect; radius,
/// border width, glow radius, highlight angle; four fill stops and the fill
/// line; four border stops and the border line; glow; highlight colour;
/// fill stop count, border stop count, highlight width (radians).
pub(crate) fn pack(shape: &RoundedRect) -> [f32; INSTANCE_FLOATS] {
    let [x, y, w, h] = shape.rect;
    let (w, h) = (w.max(0.0), h.max(0.0));
    let half_short = w.min(h) / 2.0;
    let radius = shape.radius.clamp(0.0, half_short);
    let border = shape.border.unwrap_or(Border {
        width: 0.0,
        paint: Paint::Solid(Rgba::TRANSPARENT),
        highlight: None,
    });
    let border_width = border.width.clamp(0.0, half_short);
    let glow = shape.glow.unwrap_or(Glow {
        radius: 0.0,
        color: Rgba::TRANSPARENT,
    });
    let glow_radius = glow.radius.max(0.0);
    let (angle, width, highlight) = match border.highlight {
        // The reach along the edge as an angle on the squashed box: the
        // mean half extent stands in for the radius.
        Some(hl) => (
            hl.angle,
            hl.reach.max(0.0) / ((w + h) / 4.0).max(1.0),
            hl.color,
        ),
        None => (0.0, 1.0, Rgba::TRANSPARENT),
    };

    let mut out = [0.0; INSTANCE_FLOATS];
    out[0..4].copy_from_slice(&[x, y, w, h]);
    out[4..8].copy_from_slice(&[radius, border_width, glow_radius, angle]);
    let (fill, fill_count) = paint(shape.fill);
    out[8..28].copy_from_slice(&fill);
    let (edge, edge_count) = paint(border.paint);
    out[28..48].copy_from_slice(&edge);
    out[48..52].copy_from_slice(&glow.color.premultiplied());
    out[52..56].copy_from_slice(&highlight.premultiplied());
    out[56..60].copy_from_slice(&[fill_count, edge_count, width, 0.0]);
    out
}

/// A paint as four premultiplied stops and its line, and how many of the
/// stops it uses.
fn paint(paint: Paint) -> ([f32; 20], f32) {
    let (stops, line): (&[Rgba], [f32; 4]) = match &paint {
        Paint::Solid(c) => (std::slice::from_ref(c), [0.0; 4]),
        Paint::Linear {
            start,
            end,
            from,
            to,
        } => (&[*from, *to], [start[0], start[1], end[0], end[1]]),
        Paint::Ramp(r) => (r.stops(), [r.start[0], r.start[1], r.end[0], r.end[1]]),
    };
    let mut out = [0.0; 20];
    for (i, stop) in stops.iter().take(Ramp::MAX).enumerate() {
        out[i * 4..i * 4 + 4].copy_from_slice(&stop.premultiplied());
    }
    out[16..20].copy_from_slice(&line);
    (out, stops.len().min(Ramp::MAX) as f32)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rect(w: f32, h: f32) -> RoundedRect {
        RoundedRect {
            rect: [10.0, 20.0, w, h],
            radius: 8.0,
            fill: Paint::Solid(Rgba::new(1.0, 0.5, 0.0, 0.5)),
            border: None,
            glow: None,
        }
    }

    #[test]
    fn colours_premultiply_and_clamp() {
        assert_eq!(
            Rgba::new(1.0, 0.5, 0.0, 0.5).premultiplied(),
            [0.5, 0.25, 0.0, 0.5]
        );
        assert_eq!(
            Rgba::new(2.0, -1.0, 0.5, 1.5).premultiplied(),
            [1.0, 0.0, 0.5, 1.0]
        );
    }

    #[test]
    fn hex_colours_are_opaque_srgb_bytes() {
        assert_eq!(Rgba::hex(0xff8000), Rgba::new(1.0, 128.0 / 255.0, 0.0, 1.0));
    }

    #[test]
    fn a_shape_packs_rect_params_and_premultiplied_paint() {
        let packed = pack(&rect(100.0, 40.0));
        assert_eq!(packed[0..4], [10.0, 20.0, 100.0, 40.0]);
        assert_eq!(packed[4..8], [8.0, 0.0, 0.0, 0.0]);
        assert_eq!(packed[8..12], [0.5, 0.25, 0.0, 0.5]); // the one fill stop
        assert_eq!(packed[56..58], [1.0, 1.0], "one fill stop, one border stop");
        assert_eq!(packed[48..52], [0.0; 4]); // no glow
    }

    #[test]
    fn the_radius_and_border_are_clamped_to_half_the_short_side() {
        let mut shape = rect(100.0, 10.0);
        shape.radius = 50.0;
        shape.border = Some(Border {
            width: 30.0,
            paint: Paint::Solid(Rgba::hex(0xffffff)),
            highlight: None,
        });
        let packed = pack(&shape);
        assert_eq!(packed[4], 5.0);
        assert_eq!(packed[5], 5.0);
    }

    #[test]
    fn negative_sizes_and_reaches_become_zero() {
        let mut shape = rect(-5.0, 10.0);
        shape.radius = -3.0;
        shape.glow = Some(Glow {
            radius: -4.0,
            color: Rgba::hex(0xff0000),
        });
        let packed = pack(&shape);
        assert_eq!(packed[2], 0.0);
        assert_eq!(packed[4], 0.0);
        assert_eq!(packed[6], 0.0);
        assert_eq!(packed[48..52], [1.0, 0.0, 0.0, 1.0]);
    }

    #[test]
    fn a_gradient_packs_its_line_and_both_ends() {
        let mut shape = rect(100.0, 40.0);
        shape.fill = Paint::Linear {
            start: [10.0, 20.0],
            end: [110.0, 20.0],
            from: Rgba::hex(0xff0000),
            to: Rgba::hex(0x0000ff).with_alpha(0.5),
        };
        let packed = pack(&shape);
        assert_eq!(packed[8..12], [1.0, 0.0, 0.0, 1.0]);
        assert_eq!(packed[12..16], [0.0, 0.0, 0.5, 0.5]);
        assert_eq!(packed[24..28], [10.0, 20.0, 110.0, 20.0]);
        assert_eq!(packed[56], 2.0, "two stops");
    }

    #[test]
    fn a_ramp_packs_its_stops_and_a_highlight_its_angle_and_width() {
        let mut shape = rect(100.0, 40.0);
        let stops = [
            Rgba::hex(0xff0000),
            Rgba::hex(0x00ff00),
            Rgba::hex(0x0000ff),
        ];
        shape.border = Some(Border {
            width: 2.0,
            paint: Paint::ramp([0.0, 20.0], [0.0, 60.0], &stops),
            highlight: Some(Highlight {
                angle: -1.5,
                reach: 35.0,
                color: Rgba::hex(0xffffff),
            }),
        });
        let packed = pack(&shape);
        assert_eq!(packed[28..32], [1.0, 0.0, 0.0, 1.0]);
        assert_eq!(packed[36..40], [0.0, 0.0, 1.0, 1.0]);
        assert_eq!(packed[44..48], [0.0, 20.0, 0.0, 60.0]);
        assert_eq!(packed[57], 3.0, "three border stops");
        assert_eq!(packed[7], -1.5, "the angle");
        // 35 px along a box whose mean half extent is (100 + 40) / 4 = 35.
        assert_eq!(packed[58], 1.0);
        assert_eq!(packed[52..56], [1.0; 4]);
    }
}
