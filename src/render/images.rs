//! The image pass: draws a scene's [`ImageQuad`]s from the [`ImageAtlas`]
//! after the SDF shapes, in the same render pass, with the same
//! premultiplied blending. Nothing here waits on the GPU.

use super::Gpu;
use super::atlas::{ATLAS_FORMAT, ImageAtlas};
use super::pipeline::{Target, floats_to_bytes};
use super::scale::Scale;
use super::scene::ImageQuad;

/// Floats per image instance: rect, then atlas texel and opacity.
const QUAD_FLOATS: usize = 8;
const QUAD_BYTES: u64 = (QUAD_FLOATS * 4) as u64;

/// The atlas and everything needed to draw from it.
pub(crate) struct ImagePass {
    atlas: ImageAtlas,
    shader: wgpu::ShaderModule,
    bind_layout: wgpu::BindGroupLayout,
    layout: wgpu::PipelineLayout,
    frame: wgpu::Buffer,
    bind: wgpu::BindGroup,
    /// The atlas generation `bind` refers to.
    bound: u64,
    instances: wgpu::Buffer,
    /// Instances uploaded for the frame being encoded.
    count: u32,
    pipelines: Vec<(wgpu::TextureFormat, wgpu::RenderPipeline)>,
}

impl ImagePass {
    pub fn new(gpu: &Gpu) -> Self {
        let device = &gpu.device;
        let atlas = ImageAtlas::new(gpu);
        let shader = device.create_shader_module(wgpu::ShaderModuleDescriptor {
            label: Some("logis images"),
            source: wgpu::ShaderSource::Wgsl(include_str!("image.wgsl").into()),
        });
        let entry = |binding, ty| wgpu::BindGroupLayoutEntry {
            binding,
            visibility: wgpu::ShaderStages::VERTEX_FRAGMENT,
            ty,
            count: None,
        };
        let bind_layout = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
            label: Some("logis images"),
            entries: &[
                entry(
                    0,
                    wgpu::BindingType::Buffer {
                        ty: wgpu::BufferBindingType::Uniform,
                        has_dynamic_offset: false,
                        min_binding_size: None,
                    },
                ),
                entry(
                    1,
                    wgpu::BindingType::Texture {
                        sample_type: wgpu::TextureSampleType::Float { filterable: false },
                        view_dimension: wgpu::TextureViewDimension::D2,
                        multisampled: false,
                    },
                ),
            ],
        });
        let layout = device.create_pipeline_layout(&wgpu::PipelineLayoutDescriptor {
            label: Some("logis images"),
            bind_group_layouts: &[Some(&bind_layout)],
            immediate_size: 0,
        });
        let frame = device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("logis images frame"),
            size: 16,
            usage: wgpu::BufferUsages::UNIFORM | wgpu::BufferUsages::COPY_DST,
            mapped_at_creation: false,
        });
        let bind = bind_group(gpu, &bind_layout, &frame, &atlas);
        let bound = atlas.generation();
        Self {
            instances: instance_buffer(device, 16),
            atlas,
            shader,
            bind_layout,
            layout,
            frame,
            bind,
            bound,
            count: 0,
            pipelines: Vec::new(),
        }
    }

    pub fn atlas(&self) -> &ImageAtlas {
        &self.atlas
    }

    pub fn atlas_mut(&mut self) -> &mut ImageAtlas {
        &mut self.atlas
    }

    /// Builds the pipeline for `format` if it does not exist yet.
    pub fn prepare(&mut self, gpu: &Gpu, format: wgpu::TextureFormat) {
        if !self.pipelines.iter().any(|(f, _)| *f == format) {
            let pipeline = create_pipeline(gpu, &self.shader, &self.layout, format);
            self.pipelines.push((format, pipeline));
        }
    }

    /// Writes this frame's uniforms and instances; call before the render
    /// pass that [`ImagePass::draw`] records into. Images not in the atlas
    /// are skipped.
    pub fn upload(&mut self, gpu: &Gpu, target: &Target<'_>, quads: &[ImageQuad]) {
        self.prepare(gpu, target.format);
        if self.bound != self.atlas.generation() {
            self.bind = bind_group(gpu, &self.bind_layout, &self.frame, &self.atlas);
            self.bound = self.atlas.generation();
        }
        let srgb = if target.format.is_srgb() { 1.0 } else { 0.0 };
        let frame = [target.size.0 as f32, target.size.1 as f32, srgb, 0.0];
        gpu.queue
            .write_buffer(&self.frame, 0, &floats_to_bytes(&frame));

        let packed: Vec<f32> = quads
            .iter()
            .filter(|q| q.opacity > 0.0)
            .filter_map(|q| {
                let rect = self.atlas.rect(q.image)?;
                Some(pack(q.origin, rect, target.scale, q.opacity))
            })
            .flatten()
            .collect();
        self.count = (packed.len() / QUAD_FLOATS) as u32;
        if self.count > 0 {
            if self.instances.size() < QUAD_BYTES * u64::from(self.count) {
                let capacity = (self.count as usize).next_power_of_two();
                self.instances = instance_buffer(&gpu.device, capacity);
            }
            gpu.queue
                .write_buffer(&self.instances, 0, &floats_to_bytes(&packed));
        }
    }

    /// Records the uploaded images into `pass`, whose target is `format`.
    pub fn draw(&self, pass: &mut wgpu::RenderPass<'_>, format: wgpu::TextureFormat) {
        let Some((_, pipeline)) = self.pipelines.iter().find(|(f, _)| *f == format) else {
            return;
        };
        if self.count == 0 {
            return;
        }
        pass.set_pipeline(pipeline);
        pass.set_bind_group(0, &self.bind, &[]);
        pass.set_vertex_buffer(
            0,
            self.instances.slice(..QUAD_BYTES * u64::from(self.count)),
        );
        pass.draw(0..4, 0..self.count);
    }
}

/// One instance: the image's rect in buffer pixels (its corner snapped to
/// the pixel grid, its size its own pixel size), its top-left atlas texel
/// and its opacity.
fn pack(origin: [f32; 2], rect: [u32; 4], scale: Scale, opacity: f32) -> [f32; QUAD_FLOATS] {
    let s = scale.factor();
    let [ax, ay, w, h] = rect.map(|v| v as f32);
    [
        (origin[0] * s).round(),
        (origin[1] * s).round(),
        w,
        h,
        ax,
        ay,
        opacity.clamp(0.0, 1.0),
        0.0,
    ]
}

fn bind_group(
    gpu: &Gpu,
    layout: &wgpu::BindGroupLayout,
    frame: &wgpu::Buffer,
    atlas: &ImageAtlas,
) -> wgpu::BindGroup {
    gpu.device.create_bind_group(&wgpu::BindGroupDescriptor {
        label: Some("logis images"),
        layout,
        entries: &[
            wgpu::BindGroupEntry {
                binding: 0,
                resource: frame.as_entire_binding(),
            },
            wgpu::BindGroupEntry {
                binding: 1,
                resource: wgpu::BindingResource::TextureView(atlas.view()),
            },
        ],
    })
}

fn create_pipeline(
    gpu: &Gpu,
    shader: &wgpu::ShaderModule,
    layout: &wgpu::PipelineLayout,
    format: wgpu::TextureFormat,
) -> wgpu::RenderPipeline {
    const ATTRIBUTES: [wgpu::VertexAttribute; 2] =
        wgpu::vertex_attr_array![0 => Float32x4, 1 => Float32x4];
    debug_assert!(!ATLAS_FORMAT.is_srgb(), "the shader decodes sRGB itself");
    gpu.device
        .create_render_pipeline(&wgpu::RenderPipelineDescriptor {
            label: Some("logis images"),
            layout: Some(layout),
            vertex: wgpu::VertexState {
                module: shader,
                entry_point: Some("vs"),
                buffers: &[Some(wgpu::VertexBufferLayout {
                    array_stride: QUAD_BYTES,
                    step_mode: wgpu::VertexStepMode::Instance,
                    attributes: &ATTRIBUTES,
                })],
                compilation_options: Default::default(),
            },
            fragment: Some(wgpu::FragmentState {
                module: shader,
                entry_point: Some("fs"),
                targets: &[Some(wgpu::ColorTargetState {
                    format,
                    blend: Some(wgpu::BlendState::PREMULTIPLIED_ALPHA_BLENDING),
                    write_mask: wgpu::ColorWrites::ALL,
                })],
                compilation_options: Default::default(),
            }),
            primitive: wgpu::PrimitiveState {
                topology: wgpu::PrimitiveTopology::TriangleStrip,
                ..Default::default()
            },
            depth_stencil: None,
            multisample: Default::default(),
            multiview_mask: None,
            cache: None,
        })
}

fn instance_buffer(device: &wgpu::Device, quads: usize) -> wgpu::Buffer {
    device.create_buffer(&wgpu::BufferDescriptor {
        label: Some("logis images"),
        size: QUAD_BYTES * quads as u64,
        usage: wgpu::BufferUsages::VERTEX | wgpu::BufferUsages::COPY_DST,
        mapped_at_creation: false,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pack_snaps_the_corner_and_keeps_the_pixel_size() {
        let packed = pack([10.0, 7.0], [64, 32, 42, 40], Scale::from_120ths(156), 0.5);
        // 10 × 1.3 = 13, 7 × 1.3 = 9.1 → 9; the size stays 42×40 texels.
        assert_eq!(packed, [13.0, 9.0, 42.0, 40.0, 64.0, 32.0, 0.5, 0.0]);
    }

    #[test]
    fn pack_rounds_half_pixels_away_from_zero() {
        let packed = pack([2.5, 0.0], [0, 0, 4, 4], Scale::ONE, 1.0);
        assert_eq!(packed[0..2], [3.0, 0.0]);
    }
}
