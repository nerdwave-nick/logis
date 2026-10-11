//! The text pass: glyphon's glyph atlas and quad pipeline, drawn into the
//! renderer's pass after the shapes. The atlas colour mode follows the
//! target (spec §4.4): `Web` for a non-sRGB target, which receives sRGB
//! values as they are, `Accurate` for an sRGB target, which encodes them.

use glyphon::{
    Buffer, Cache, Color, ColorMode, PrepareError, Resolution, SwashCache, TextArea, TextAtlas,
    TextBounds, TextRenderer, Viewport,
};

use super::Gpu;
use super::pipeline::Target;
use super::scale::Scale;
use super::text::{Text, shape};
use crate::text::Fonts;

/// The glyphon colour mode that matches a target `format`.
pub(crate) fn color_mode(format: wgpu::TextureFormat) -> ColorMode {
    if format.is_srgb() {
        ColorMode::Accurate
    } else {
        ColorMode::Web
    }
}

/// A shaped line and what it was shaped from, reused while both stay equal.
struct Shaped {
    text: Text,
    scale: Scale,
    buffer: Buffer,
}

/// Fonts, glyph atlases (one per target format) and shaped lines.
pub(crate) struct TextPass {
    fonts: Fonts,
    cache: Cache,
    swash: SwashCache,
    viewport: Viewport,
    atlases: Vec<(wgpu::TextureFormat, TextAtlas, TextRenderer)>,
    shaped: Vec<Shaped>,
}

impl TextPass {
    pub(crate) fn new(gpu: &Gpu, fonts: Fonts) -> Self {
        let cache = Cache::new(&gpu.device);
        let viewport = Viewport::new(&gpu.device, &cache);
        Self {
            fonts,
            cache,
            swash: SwashCache::new(),
            viewport,
            atlases: Vec::new(),
            shaped: Vec::new(),
        }
    }

    /// Creates the atlas and pipeline for `format` now rather than on the
    /// first frame that draws text.
    pub(crate) fn prepare_format(&mut self, gpu: &Gpu, format: wgpu::TextureFormat) -> usize {
        if let Some(i) = self.atlases.iter().position(|(f, _, _)| *f == format) {
            return i;
        }
        let mut atlas = TextAtlas::with_color_mode(
            &gpu.device,
            &gpu.queue,
            &self.cache,
            format,
            color_mode(format),
        );
        let renderer = TextRenderer::new(
            &mut atlas,
            &gpu.device,
            wgpu::MultisampleState::default(),
            None,
        );
        self.atlases.push((format, atlas, renderer));
        self.atlases.len() - 1
    }

    /// Shapes `texts` (reusing unchanged lines), rasterises missing glyphs
    /// into the atlas and uploads the glyph quads for `target`.
    pub(crate) fn prepare(
        &mut self,
        gpu: &Gpu,
        target: &Target<'_>,
        texts: &[Text],
    ) -> Result<(), PrepareError> {
        let scale = target.scale;
        self.shaped.truncate(texts.len());
        for (i, text) in texts.iter().enumerate() {
            let fresh = self
                .shaped
                .get(i)
                .is_some_and(|s| s.scale == scale && s.text == *text);
            if !fresh {
                let shaped = Shaped {
                    text: text.clone(),
                    scale,
                    buffer: shape(&mut self.fonts, text, scale),
                };
                if i < self.shaped.len() {
                    self.shaped[i] = shaped;
                } else {
                    self.shaped.push(shaped);
                }
            }
        }

        let (width, height) = target.size;
        self.viewport
            .update(&gpu.queue, Resolution { width, height });
        let index = self.prepare_format(gpu, target.format);
        let (_, atlas, renderer) = &mut self.atlases[index];
        atlas.trim();
        let areas = self.shaped.iter().map(|s| area(s, scale, target.size));
        renderer.prepare(
            &gpu.device,
            &gpu.queue,
            self.fonts.system_mut(),
            atlas,
            &self.viewport,
            areas,
            &mut self.swash,
        )
    }

    /// Draws what [`Self::prepare`] uploaded for `format`.
    pub(crate) fn render(&self, format: wgpu::TextureFormat, pass: &mut wgpu::RenderPass<'_>) {
        let Some((_, atlas, renderer)) = self.atlases.iter().find(|(f, _, _)| *f == format) else {
            return;
        };
        if let Err(e) = renderer.render(atlas, &self.viewport, pass) {
            eprintln!("logis: the glyphs would not be inscribed: {e}");
        }
    }
}

/// Where a shaped line goes: its origin snapped to the pixel grid, clipped
/// to its column (horizontally) and the target.
fn area(shaped: &Shaped, scale: Scale, (width, height): (u32, u32)) -> TextArea<'_> {
    let text = &shaped.text;
    let f = scale.factor();
    let (left, right) = text
        .column(scale)
        .unwrap_or((0, i32::try_from(width).unwrap_or(i32::MAX)));
    let [r, g, b, a] = [text.color.r, text.color.g, text.color.b, text.color.a]
        .map(|c| (c.clamp(0.0, 1.0) * 255.0).round() as u8);
    TextArea {
        buffer: &shaped.buffer,
        left: (text.origin[0] * f).round(),
        top: (text.origin[1] * f).round(),
        scale: 1.0,
        bounds: TextBounds {
            left,
            top: 0,
            right,
            bottom: i32::try_from(height).unwrap_or(i32::MAX),
        },
        default_color: Color::rgba(r, g, b, a),
        custom_glyphs: &[],
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use wgpu::TextureFormat::{Bgra8Unorm, Bgra8UnormSrgb, Rgba8Unorm, Rgba8UnormSrgb};

    #[test]
    fn the_colour_mode_follows_the_target_encoding() {
        assert_eq!(color_mode(Bgra8Unorm), ColorMode::Web);
        assert_eq!(color_mode(Rgba8Unorm), ColorMode::Web);
        assert_eq!(color_mode(Bgra8UnormSrgb), ColorMode::Accurate);
        assert_eq!(color_mode(Rgba8UnormSrgb), ColorMode::Accurate);
    }
}
