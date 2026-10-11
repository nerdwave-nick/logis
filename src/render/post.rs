//! The post-processing pass (`post.wgsl`): when a scene asks for effects,
//! the renderer draws it into an intermediate texture of the target's size
//! and format, then this pass draws that texture into the target with the
//! effects applied. The intermediate texture is kept while the target's
//! size and format stay; nothing here waits on the GPU.

use super::Gpu;
use super::pipeline::{Target, floats_to_bytes};
use super::scene::Post;

/// Floats in the uniform block (`Params` in post.wgsl).
const PARAMS_FLOATS: usize = 20;

/// The frame drawn before the effects.
struct Intermediate {
    format: wgpu::TextureFormat,
    size: (u32, u32),
    view: wgpu::TextureView,
    bind: wgpu::BindGroup,
}

/// The effects pipeline, one per target format, and its intermediate frame.
pub(crate) struct PostPass {
    shader: wgpu::ShaderModule,
    bind_layout: wgpu::BindGroupLayout,
    layout: wgpu::PipelineLayout,
    pipelines: Vec<(wgpu::TextureFormat, wgpu::RenderPipeline)>,
    params: wgpu::Buffer,
    intermediate: Option<Intermediate>,
}

impl PostPass {
    pub(crate) fn new(gpu: &Gpu) -> Self {
        let device = &gpu.device;
        let shader = device.create_shader_module(wgpu::ShaderModuleDescriptor {
            label: Some("logis post"),
            source: wgpu::ShaderSource::Wgsl(include_str!("post.wgsl").into()),
        });
        let bind_layout = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
            label: Some("logis post"),
            entries: &[
                wgpu::BindGroupLayoutEntry {
                    binding: 0,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: wgpu::BindingType::Buffer {
                        ty: wgpu::BufferBindingType::Uniform,
                        has_dynamic_offset: false,
                        min_binding_size: None,
                    },
                    count: None,
                },
                wgpu::BindGroupLayoutEntry {
                    binding: 1,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: wgpu::BindingType::Texture {
                        sample_type: wgpu::TextureSampleType::Float { filterable: false },
                        view_dimension: wgpu::TextureViewDimension::D2,
                        multisampled: false,
                    },
                    count: None,
                },
            ],
        });
        let layout = device.create_pipeline_layout(&wgpu::PipelineLayoutDescriptor {
            label: Some("logis post"),
            bind_group_layouts: &[Some(&bind_layout)],
            immediate_size: 0,
        });
        let params = device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("logis post"),
            size: (PARAMS_FLOATS * 4) as u64,
            usage: wgpu::BufferUsages::UNIFORM | wgpu::BufferUsages::COPY_DST,
            mapped_at_creation: false,
        });
        Self {
            shader,
            bind_layout,
            layout,
            pipelines: Vec::new(),
            params,
            intermediate: None,
        }
    }

    /// The texture a frame for `target` is drawn into before the effects,
    /// made (or remade) to its size and format.
    pub(crate) fn source(&mut self, gpu: &Gpu, target: &Target<'_>) -> wgpu::TextureView {
        let fits = self
            .intermediate
            .as_ref()
            .is_some_and(|i| i.format == target.format && i.size == target.size);
        if !fits {
            self.intermediate = Some(self.intermediate(gpu, target));
        }
        self.intermediate.as_ref().expect("made above").view.clone()
    }

    fn intermediate(&self, gpu: &Gpu, target: &Target<'_>) -> Intermediate {
        let (width, height) = target.size;
        let texture = gpu.device.create_texture(&wgpu::TextureDescriptor {
            label: Some("logis post source"),
            size: wgpu::Extent3d {
                width,
                height,
                depth_or_array_layers: 1,
            },
            mip_level_count: 1,
            sample_count: 1,
            dimension: wgpu::TextureDimension::D2,
            format: target.format,
            usage: wgpu::TextureUsages::RENDER_ATTACHMENT | wgpu::TextureUsages::TEXTURE_BINDING,
            view_formats: &[],
        });
        let view = texture.create_view(&wgpu::TextureViewDescriptor::default());
        let bind = gpu.device.create_bind_group(&wgpu::BindGroupDescriptor {
            label: Some("logis post"),
            layout: &self.bind_layout,
            entries: &[
                wgpu::BindGroupEntry {
                    binding: 0,
                    resource: self.params.as_entire_binding(),
                },
                wgpu::BindGroupEntry {
                    binding: 1,
                    resource: wgpu::BindingResource::TextureView(&view),
                },
            ],
        });
        Intermediate {
            format: target.format,
            size: target.size,
            view,
            bind,
        }
    }

    /// Draws the intermediate frame into `target` with `post`'s effects.
    /// Call [`Self::source`] for this target first.
    pub(crate) fn draw(
        &mut self,
        gpu: &Gpu,
        encoder: &mut wgpu::CommandEncoder,
        target: &Target<'_>,
        post: &Post,
    ) {
        gpu.queue
            .write_buffer(&self.params, 0, &floats_to_bytes(&params(target, post)));
        let pipeline = self.pipeline(gpu, target.format).clone();
        let Some(intermediate) = &self.intermediate else {
            return;
        };
        let mut pass = encoder.begin_render_pass(&wgpu::RenderPassDescriptor {
            label: Some("logis post"),
            color_attachments: &[Some(wgpu::RenderPassColorAttachment {
                view: target.view,
                depth_slice: None,
                resolve_target: None,
                ops: wgpu::Operations {
                    load: wgpu::LoadOp::Clear(wgpu::Color::TRANSPARENT),
                    store: wgpu::StoreOp::Store,
                },
            })],
            ..Default::default()
        });
        pass.set_pipeline(&pipeline);
        pass.set_bind_group(0, &intermediate.bind, &[]);
        pass.draw(0..3, 0..1);
    }

    /// Builds the pipeline for `format` now rather than on its first frame.
    pub(crate) fn prepare(&mut self, gpu: &Gpu, format: wgpu::TextureFormat) {
        self.pipeline(gpu, format);
    }

    fn pipeline(&mut self, gpu: &Gpu, format: wgpu::TextureFormat) -> &wgpu::RenderPipeline {
        let index = match self.pipelines.iter().position(|(f, _)| *f == format) {
            Some(index) => index,
            None => {
                let pipeline = gpu
                    .device
                    .create_render_pipeline(&wgpu::RenderPipelineDescriptor {
                        label: Some("logis post"),
                        layout: Some(&self.layout),
                        vertex: wgpu::VertexState {
                            module: &self.shader,
                            entry_point: Some("vs"),
                            buffers: &[],
                            compilation_options: Default::default(),
                        },
                        fragment: Some(wgpu::FragmentState {
                            module: &self.shader,
                            entry_point: Some("fs"),
                            targets: &[Some(wgpu::ColorTargetState {
                                format,
                                blend: None,
                                write_mask: wgpu::ColorWrites::ALL,
                            })],
                            compilation_options: Default::default(),
                        }),
                        primitive: wgpu::PrimitiveState::default(),
                        depth_stencil: None,
                        multisample: Default::default(),
                        multiview_mask: None,
                        cache: None,
                    });
                self.pipelines.push((format, pipeline));
                self.pipelines.len() - 1
            }
        };
        &self.pipelines[index].1
    }
}

/// The uniform block for `post` on `target`, lengths in buffer pixels.
fn params(target: &Target<'_>, post: &Post) -> [f32; PARAMS_FLOATS] {
    let s = target.scale.factor();
    let [x, y, w, h] = post.rect.map(|v| v * s);
    let (width, height) = target.size;
    let radius = (post.radius * s).clamp(0.0, w.min(h).max(0.0) / 2.0);
    let unit = |v: f32| v.clamp(0.0, 1.0);
    [
        width as f32,
        height as f32,
        s,
        post.time,
        x,
        y,
        w,
        h,
        radius,
        0.0,
        0.0,
        0.0,
        unit(post.scanlines),
        unit(post.grain),
        unit(post.flicker),
        unit(post.vignette),
        unit(post.bloom),
        unit(post.curvature),
        unit(post.aberration),
        0.0,
    ]
}
