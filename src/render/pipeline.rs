//! The SDF pipeline: uploads a scene's shapes as instances and encodes one
//! render pass into a caller's command encoder. Nothing here waits on the GPU.

use super::Gpu;
use super::atlas::ImageAtlas;
use super::images::ImagePass;
use super::post::PostPass;
use super::scale::Scale;
use super::scene::{INSTANCE_FLOATS, Post, Scene, pack};
use super::text_pass::TextPass;
use crate::text::Fonts;

/// Bytes per shape instance.
const INSTANCE_BYTES: u64 = (INSTANCE_FLOATS * 4) as u64;

/// Where one frame goes.
pub struct Target<'a> {
    /// The texture view to draw into.
    pub view: &'a wgpu::TextureView,
    /// Its format.
    pub format: wgpu::TextureFormat,
    /// Its size in buffer pixels.
    pub size: (u32, u32),
    /// Logical → buffer pixels.
    pub scale: Scale,
}

/// logis's own renderer: one pipeline per target format, shared buffers.
pub struct Renderer {
    shader: wgpu::ShaderModule,
    layout: wgpu::PipelineLayout,
    pipelines: Vec<(wgpu::TextureFormat, wgpu::RenderPipeline)>,
    frame: wgpu::Buffer,
    bind: wgpu::BindGroup,
    instances: wgpu::Buffer,
    images: ImagePass,
    text: Option<TextPass>,
    post: PostPass,
}

impl Renderer {
    /// Compiles the shader and creates the shared buffers.
    pub fn new(gpu: &Gpu) -> Self {
        let device = &gpu.device;
        let shader = device.create_shader_module(wgpu::ShaderModuleDescriptor {
            label: Some("logis sdf"),
            source: wgpu::ShaderSource::Wgsl(include_str!("sdf.wgsl").into()),
        });
        let bind_layout = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
            label: Some("logis frame"),
            entries: &[wgpu::BindGroupLayoutEntry {
                binding: 0,
                visibility: wgpu::ShaderStages::VERTEX_FRAGMENT,
                ty: wgpu::BindingType::Buffer {
                    ty: wgpu::BufferBindingType::Uniform,
                    has_dynamic_offset: false,
                    min_binding_size: None,
                },
                count: None,
            }],
        });
        let layout = device.create_pipeline_layout(&wgpu::PipelineLayoutDescriptor {
            label: Some("logis sdf"),
            bind_group_layouts: &[Some(&bind_layout)],
            immediate_size: 0,
        });
        let frame = device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("logis frame"),
            size: 16,
            usage: wgpu::BufferUsages::UNIFORM | wgpu::BufferUsages::COPY_DST,
            mapped_at_creation: false,
        });
        let bind = device.create_bind_group(&wgpu::BindGroupDescriptor {
            label: Some("logis frame"),
            layout: &bind_layout,
            entries: &[wgpu::BindGroupEntry {
                binding: 0,
                resource: frame.as_entire_binding(),
            }],
        });
        let instances = instance_buffer(device, 16);
        Self {
            images: ImagePass::new(gpu),
            post: PostPass::new(gpu),
            shader,
            layout,
            pipelines: Vec::new(),
            frame,
            bind,
            instances,
            text: None,
        }
    }

    /// Gives the renderer fonts, so that scenes' texts are drawn. Without
    /// fonts, texts are skipped.
    pub fn set_fonts(&mut self, gpu: &Gpu, fonts: Fonts) {
        self.text = Some(TextPass::new(gpu, fonts));
    }

    /// Resolves the faces texts name by index ([`crate::render::Text::face`]),
    /// replacing any earlier ones. Without fonts it does nothing.
    pub fn set_faces(&mut self, faces: &[crate::text::Face]) -> Result<(), crate::text::FontError> {
        match &mut self.text {
            Some(text) => text.set_faces(faces),
            None => Ok(()),
        }
    }

    /// Builds the pipeline for `format` now rather than on its first frame.
    pub fn prepare(&mut self, gpu: &Gpu, format: wgpu::TextureFormat) {
        self.pipeline(gpu, format);
        self.images.prepare(gpu, format);
        self.post.prepare(gpu, format);
        if let Some(text) = &mut self.text {
            text.prepare_format(gpu, format);
        }
    }

    /// The image atlas the scene's [`super::ImageQuad`]s refer to.
    pub fn atlas(&self) -> &ImageAtlas {
        self.images.atlas()
    }

    /// The image atlas, to add images.
    pub fn atlas_mut(&mut self) -> &mut ImageAtlas {
        self.images.atlas_mut()
    }

    /// Encodes `scene` into `target`: clears it, then paints every shape,
    /// then every image, then every line of text (the top layer), then
    /// the scene's post-processing effects, if any are on.
    /// Buffers are written through the queue, so submit `encoder` before
    /// encoding the next frame.
    pub fn encode(
        &mut self,
        gpu: &Gpu,
        encoder: &mut wgpu::CommandEncoder,
        target: &Target<'_>,
        scene: &Scene,
    ) {
        match scene.post.filter(Post::any) {
            Some(post) => {
                let view = self.post.source(gpu, target);
                let drawn = Target {
                    view: &view,
                    format: target.format,
                    size: target.size,
                    scale: target.scale,
                };
                self.encode_scene(gpu, encoder, &drawn, scene);
                self.post.draw(gpu, encoder, target, &post);
            }
            None => self.encode_scene(gpu, encoder, target, scene),
        }
    }

    fn encode_scene(
        &mut self,
        gpu: &Gpu,
        encoder: &mut wgpu::CommandEncoder,
        target: &Target<'_>,
        scene: &Scene,
    ) {
        let srgb = if target.format.is_srgb() { 1.0 } else { 0.0 };
        let frame = [
            target.size.0 as f32,
            target.size.1 as f32,
            target.scale.factor(),
            srgb,
        ];
        gpu.queue
            .write_buffer(&self.frame, 0, &floats_to_bytes(&frame));

        // Text is shaped first: its underlines and carets are drawn with
        // the shapes.
        let text = self.prepare_text(gpu, target, scene);
        let underlines = match self.text.as_ref().filter(|_| text) {
            Some(pass) => pass.decorations(),
            None => Vec::new(),
        };
        let count = scene.shapes.len() + underlines.len();
        if count > 0 {
            let needed = INSTANCE_BYTES * count as u64;
            if self.instances.size() < needed {
                self.instances = instance_buffer(&gpu.device, count.next_power_of_two());
            }
            let packed: Vec<f32> = scene
                .shapes
                .iter()
                .chain(&underlines)
                .flat_map(pack)
                .collect();
            gpu.queue
                .write_buffer(&self.instances, 0, &floats_to_bytes(&packed));
        }

        let clear = scene.clear.map_or([0.0; 4], |c| c.premultiplied());
        let clear = wgpu::Color {
            r: f64::from(clear[0]),
            g: f64::from(clear[1]),
            b: f64::from(clear[2]),
            a: f64::from(clear[3]),
        };
        self.images.upload(gpu, target, &scene.images);
        let pipeline = self.pipeline(gpu, target.format).clone();
        let mut pass = encoder.begin_render_pass(&wgpu::RenderPassDescriptor {
            label: Some("logis sdf"),
            color_attachments: &[Some(wgpu::RenderPassColorAttachment {
                view: target.view,
                depth_slice: None,
                resolve_target: None,
                ops: wgpu::Operations {
                    load: wgpu::LoadOp::Clear(clear),
                    store: wgpu::StoreOp::Store,
                },
            })],
            ..Default::default()
        });
        if count > 0 {
            pass.set_pipeline(&pipeline);
            pass.set_bind_group(0, &self.bind, &[]);
            pass.set_vertex_buffer(0, self.instances.slice(..INSTANCE_BYTES * count as u64));
            pass.draw(0..4, 0..count as u32);
        }
        self.images.draw(&mut pass, target.format);
        if let Some(text) = self.text.as_ref().filter(|_| text) {
            text.render(target.format, &mut pass);
        }
    }

    /// Shapes and uploads the scene's text. False when there is none to
    /// draw: no fonts, no text, or the atlas could not take it.
    fn prepare_text(&mut self, gpu: &Gpu, target: &Target<'_>, scene: &Scene) -> bool {
        let Some(text) = self.text.as_mut().filter(|_| !scene.texts.is_empty()) else {
            return false;
        };
        match text.prepare(gpu, target, &scene.texts) {
            Ok(()) => true,
            Err(e) => {
                eprintln!("logis: the glyphs would not be inscribed: {e}");
                false
            }
        }
    }

    fn pipeline(&mut self, gpu: &Gpu, format: wgpu::TextureFormat) -> &wgpu::RenderPipeline {
        let index = match self.pipelines.iter().position(|(f, _)| *f == format) {
            Some(index) => index,
            None => {
                let pipeline = create_pipeline(gpu, &self.shader, &self.layout, format);
                self.pipelines.push((format, pipeline));
                self.pipelines.len() - 1
            }
        };
        &self.pipelines[index].1
    }
}

fn create_pipeline(
    gpu: &Gpu,
    shader: &wgpu::ShaderModule,
    layout: &wgpu::PipelineLayout,
    format: wgpu::TextureFormat,
) -> wgpu::RenderPipeline {
    const ATTRIBUTES: [wgpu::VertexAttribute; 15] = wgpu::vertex_attr_array![
        0 => Float32x4, 1 => Float32x4, 2 => Float32x4, 3 => Float32x4, 4 => Float32x4,
        5 => Float32x4, 6 => Float32x4, 7 => Float32x4, 8 => Float32x4, 9 => Float32x4,
        10 => Float32x4, 11 => Float32x4, 12 => Float32x4, 13 => Float32x4, 14 => Float32x4,
    ];
    gpu.device
        .create_render_pipeline(&wgpu::RenderPipelineDescriptor {
            label: Some("logis sdf"),
            layout: Some(layout),
            vertex: wgpu::VertexState {
                module: shader,
                entry_point: Some("vs"),
                buffers: &[Some(wgpu::VertexBufferLayout {
                    array_stride: INSTANCE_BYTES,
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

fn instance_buffer(device: &wgpu::Device, shapes: usize) -> wgpu::Buffer {
    device.create_buffer(&wgpu::BufferDescriptor {
        label: Some("logis shapes"),
        size: INSTANCE_BYTES * shapes as u64,
        usage: wgpu::BufferUsages::VERTEX | wgpu::BufferUsages::COPY_DST,
        mapped_at_creation: false,
    })
}

/// Native-endian bytes of `f32`s, as the GPU reads them; no unsafe casts.
pub(super) fn floats_to_bytes(floats: &[f32]) -> Vec<u8> {
    floats.iter().flat_map(|f| f.to_ne_bytes()).collect()
}
