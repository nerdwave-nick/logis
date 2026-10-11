//! What the renderer draws: a retained list of SDF shapes in logical pixels,
//! painted in order. Colours are sRGB with straight alpha here; the GPU sees
//! them premultiplied (spec §4.3).

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
}

/// A border drawn inside the shape's edge.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Border {
    /// Width in logical pixels.
    pub width: f32,
    /// How the border is painted.
    pub paint: Paint,
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

/// Everything one frame shows: a clear colour, shapes in paint order, then
/// text above them.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Scene {
    /// The colour the frame starts from (transparent for a layer surface).
    pub clear: Option<Rgba>,
    /// Shapes, back to front.
    pub shapes: Vec<RoundedRect>,
    /// Lines of text, drawn over the shapes when the renderer has fonts.
    pub texts: Vec<super::text::Text>,
}

/// Floats per shape instance: nine `vec4f` vertex attributes.
pub(crate) const INSTANCE_FLOATS: usize = 36;

/// Packs a shape into the instance layout of `sdf.wgsl`: rect; radius,
/// border width, glow radius; fill from/to/line; border from/to/line; glow.
pub(crate) fn pack(shape: &RoundedRect) -> [f32; INSTANCE_FLOATS] {
    let [x, y, w, h] = shape.rect;
    let (w, h) = (w.max(0.0), h.max(0.0));
    let half_short = w.min(h) / 2.0;
    let radius = shape.radius.clamp(0.0, half_short);
    let border = shape.border.unwrap_or(Border {
        width: 0.0,
        paint: Paint::Solid(Rgba::TRANSPARENT),
    });
    let border_width = border.width.clamp(0.0, half_short);
    let glow = shape.glow.unwrap_or(Glow {
        radius: 0.0,
        color: Rgba::TRANSPARENT,
    });
    let glow_radius = glow.radius.max(0.0);

    let mut out = [0.0; INSTANCE_FLOATS];
    out[0..4].copy_from_slice(&[x, y, w, h]);
    out[4..8].copy_from_slice(&[radius, border_width, glow_radius, 0.0]);
    out[8..20].copy_from_slice(&paint(shape.fill));
    out[20..32].copy_from_slice(&paint(border.paint));
    out[32..36].copy_from_slice(&glow.color.premultiplied());
    out
}

/// A paint as from, to (premultiplied) and line (start, end).
fn paint(paint: Paint) -> [f32; 12] {
    let (from, to, line) = match paint {
        Paint::Solid(c) => (c, c, [0.0; 4]),
        Paint::Linear {
            start,
            end,
            from,
            to,
        } => (from, to, [start[0], start[1], end[0], end[1]]),
    };
    let mut out = [0.0; 12];
    out[0..4].copy_from_slice(&from.premultiplied());
    out[4..8].copy_from_slice(&to.premultiplied());
    out[8..12].copy_from_slice(&line);
    out
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
        assert_eq!(packed[8..12], [0.5, 0.25, 0.0, 0.5]); // fill from
        assert_eq!(packed[12..16], [0.5, 0.25, 0.0, 0.5]); // fill to
        assert_eq!(packed[32..36], [0.0; 4]); // no glow
    }

    #[test]
    fn the_radius_and_border_are_clamped_to_half_the_short_side() {
        let mut shape = rect(100.0, 10.0);
        shape.radius = 50.0;
        shape.border = Some(Border {
            width: 30.0,
            paint: Paint::Solid(Rgba::hex(0xffffff)),
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
        assert_eq!(packed[32..36], [1.0, 0.0, 0.0, 1.0]);
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
        assert_eq!(packed[16..20], [10.0, 20.0, 110.0, 20.0]);
    }
}
