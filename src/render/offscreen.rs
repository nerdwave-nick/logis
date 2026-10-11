//! Render-to-texture without a compositor, for golden tests and checks. This
//! path waits for the GPU to read pixels back; the frame loop never does.

use std::sync::mpsc;

use super::Gpu;
use super::pipeline::{Renderer, Target};
use super::scale::Scale;
use super::scene::Scene;

/// The default offscreen format: 8-bit RGBA, not sRGB-encoding, like the
/// swapchains logis prefers. `Rgba8UnormSrgb` is accepted too, to check the
/// path for compositors that only offer sRGB formats.
pub const OFFSCREEN_FORMAT: wgpu::TextureFormat = wgpu::TextureFormat::Rgba8Unorm;

/// A rendered frame: premultiplied RGBA8, rows top to bottom, no padding.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Image {
    /// Width in pixels.
    pub width: u32,
    /// Height in pixels.
    pub height: u32,
    /// `width * height * 4` bytes.
    pub pixels: Vec<u8>,
}

impl Image {
    /// The premultiplied RGBA of one pixel.
    pub fn pixel(&self, x: u32, y: u32) -> [u8; 4] {
        assert!(x < self.width && y < self.height, "({x}, {y}) is outside");
        let i = ((y * self.width + x) * 4) as usize;
        [
            self.pixels[i],
            self.pixels[i + 1],
            self.pixels[i + 2],
            self.pixels[i + 3],
        ]
    }
}

/// Why an offscreen render produced no image.
#[derive(Debug)]
pub struct ReadbackError(String);

impl std::fmt::Display for ReadbackError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "the GPU withheld the rendered image: {}", self.0)
    }
}

impl std::error::Error for ReadbackError {}

/// Renders `scene` at `logical` size and `scale` into a texture of `format`
/// (an RGBA8 format) and reads it back. The image is
/// `scale.physical(logical)` pixels.
pub fn render_offscreen(
    gpu: &Gpu,
    renderer: &mut Renderer,
    scene: &Scene,
    logical: (u32, u32),
    scale: Scale,
    format: wgpu::TextureFormat,
) -> Result<Image, ReadbackError> {
    use wgpu::TextureFormat::{Rgba8Unorm, Rgba8UnormSrgb};
    if !matches!(format, Rgba8Unorm | Rgba8UnormSrgb) {
        return Err(ReadbackError(format!("{format:?} is not an RGBA8 format")));
    }
    let (width, height) = scale.physical(logical);
    let texture = gpu.device.create_texture(&wgpu::TextureDescriptor {
        label: Some("logis offscreen"),
        size: wgpu::Extent3d {
            width,
            height,
            depth_or_array_layers: 1,
        },
        mip_level_count: 1,
        sample_count: 1,
        dimension: wgpu::TextureDimension::D2,
        format,
        usage: wgpu::TextureUsages::RENDER_ATTACHMENT | wgpu::TextureUsages::COPY_SRC,
        view_formats: &[],
    });
    let view = texture.create_view(&wgpu::TextureViewDescriptor::default());
    let row = width * 4;
    let padded_row =
        row.div_ceil(wgpu::COPY_BYTES_PER_ROW_ALIGNMENT) * wgpu::COPY_BYTES_PER_ROW_ALIGNMENT;
    let readback = gpu.device.create_buffer(&wgpu::BufferDescriptor {
        label: Some("logis readback"),
        size: u64::from(padded_row) * u64::from(height),
        usage: wgpu::BufferUsages::COPY_DST | wgpu::BufferUsages::MAP_READ,
        mapped_at_creation: false,
    });

    let mut encoder = gpu
        .device
        .create_command_encoder(&wgpu::CommandEncoderDescriptor::default());
    let target = Target {
        view: &view,
        format,
        size: (width, height),
        scale,
    };
    renderer.encode(gpu, &mut encoder, &target, scene);
    encoder.copy_texture_to_buffer(
        texture.as_image_copy(),
        wgpu::TexelCopyBufferInfo {
            buffer: &readback,
            layout: wgpu::TexelCopyBufferLayout {
                offset: 0,
                bytes_per_row: Some(padded_row),
                rows_per_image: Some(height),
            },
        },
        wgpu::Extent3d {
            width,
            height,
            depth_or_array_layers: 1,
        },
    );
    gpu.queue.submit([encoder.finish()]);

    let (sender, receiver) = mpsc::channel();
    readback
        .slice(..)
        .map_async(wgpu::MapMode::Read, move |result| {
            let _ = sender.send(result);
        });
    gpu.device
        .poll(wgpu::PollType::wait_indefinitely())
        .map_err(|e| ReadbackError(e.to_string()))?;
    receiver
        .recv()
        .map_err(|e| ReadbackError(e.to_string()))?
        .map_err(|e| ReadbackError(e.to_string()))?;

    let mapped = readback
        .slice(..)
        .get_mapped_range()
        .map_err(|e| ReadbackError(e.to_string()))?;
    let mut pixels = Vec::with_capacity((row * height) as usize);
    for line in mapped.chunks(padded_row as usize) {
        pixels.extend_from_slice(&line[..row as usize]);
    }
    drop(mapped);
    readback.unmap();
    Ok(Image {
        width,
        height,
        pixels,
    })
}
