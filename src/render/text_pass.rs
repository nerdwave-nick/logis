//! The text pass: glyphon's glyph atlas and quad pipeline, drawn into the
//! renderer's pass after the shapes. The atlas colour mode follows the
//! target (spec §4.4): `Web` for a non-sRGB target, which receives sRGB
//! values as they are, `Accurate` for an sRGB target, which encodes them.

use glyphon::{
    Buffer, Cache, ColorMode, PrepareError, Resolution, SwashCache, TextArea, TextAtlas,
    TextBounds, TextRenderer, Viewport,
};

use super::Gpu;
use super::pipeline::Target;
use super::scale::Scale;
use super::scene::{Paint, RoundedRect};
use super::text::{Text, shape, spans};
use crate::text::{Face, FontError, Fonts};

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

    /// Resolves the faces texts name by index; every line is shaped anew.
    pub(crate) fn set_faces(&mut self, faces: &[Face]) -> Result<(), FontError> {
        self.shaped.clear();
        self.fonts.set_faces(faces)
    }

    /// Underlines under the spans of the lines that ask for them, as
    /// shapes in logical pixels, from the lines [`Self::prepare`] shaped.
    pub(crate) fn underlines(&self) -> Vec<RoundedRect> {
        self.shaped
            .iter()
            .filter(|s| s.text.underline_spans && !s.text.spans.is_empty())
            .flat_map(underlines)
            .collect()
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

/// The underlines of one shaped line: one bar per run of consecutive
/// glyphs in the same span, just below the baseline, clipped to the
/// line's column.
fn underlines(shaped: &Shaped) -> Vec<RoundedRect> {
    let text = &shaped.text;
    let f = shaped.scale.factor();
    let line = text.text.replace(['\n', '\r'], " ");
    let pieces: Vec<_> = spans(&line, &text.spans)
        .filter_map(|(range, color)| color.map(|c| (range, c)))
        .collect();
    let size = (text.size * f).max(1.0);
    let thickness = (size * 0.06).max(1.0);
    let left = (text.origin[0] * f).round();
    let top = (text.origin[1] * f).round();
    let (clip_left, clip_right) = text
        .column(shaped.scale)
        .map_or((f32::MIN, f32::MAX), |(l, r)| (l as f32, r as f32));
    let mut bars: Vec<RoundedRect> = Vec::new();
    for run in shaped.buffer.layout_runs() {
        let y = top + run.line_y + (size * 0.12).max(1.0);
        let mut open: Option<(usize, f32, f32)> = None;
        let mut close = |open: &mut Option<(usize, f32, f32)>| {
            if let Some((piece, x0, x1)) = open.take() {
                let (x0, x1) = (x0.max(clip_left), x1.min(clip_right));
                if x1 > x0 {
                    bars.push(bar([x0, y, x1 - x0, thickness], f, pieces[piece].1));
                }
            }
        };
        for glyph in run.glyphs {
            let piece = pieces.iter().position(|(r, _)| r.contains(&glyph.start));
            let (x0, x1) = (left + glyph.x, left + glyph.x + glyph.w);
            match (piece, &mut open) {
                (Some(p), Some((q, a, b))) if p == *q => {
                    *a = a.min(x0);
                    *b = b.max(x1);
                }
                (Some(p), _) => {
                    close(&mut open);
                    open = Some((p, x0, x1));
                }
                (None, _) => close(&mut open),
            }
        }
        close(&mut open);
    }
    bars
}

/// A bar from buffer pixels to logical ones.
fn bar(rect: [f32; 4], f: f32, color: super::scene::Rgba) -> RoundedRect {
    RoundedRect {
        rect: rect.map(|v| v / f),
        radius: 0.0,
        fill: Paint::Solid(color),
        border: None,
        glow: None,
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
        default_color: super::text::glyph_color(text.color),
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
