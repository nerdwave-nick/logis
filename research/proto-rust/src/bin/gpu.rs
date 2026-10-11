//! Research benchmark (disposable): cold-start cost of a GPU path —
//! wgpu (Vulkan backend) + vello — on a layer-shell surface. Draws the
//! gradient-bordered panel (no text) and reports milestones relative to $T0_NS.
//! RENDERER=wgpu → clear only (wgpu init cost); RENDERER=vello (default).
use std::ptr::NonNull;

use raw_window_handle::{RawDisplayHandle, RawWindowHandle, WaylandDisplayHandle, WaylandWindowHandle};
use smithay_client_toolkit::{
    compositor::{CompositorHandler, CompositorState},
    delegate_registry,
    output::{OutputHandler, OutputState},
    registry::{ProvidesRegistryState, RegistryState},
    registry_handlers,
    shell::{
        wlr_layer::{KeyboardInteractivity, Layer, LayerShell, LayerShellHandler, LayerSurface, LayerSurfaceConfigure},
        WaylandSurface,
    },
};
use vello::kurbo::{Affine, RoundedRect};
use vello::peniko::{color::palette, Color, Fill, Gradient};
use vello::{wgpu, AaConfig, RenderParams, Renderer, RendererOptions, Scene};
use wayland_client::{
    globals::registry_queue_init,
    protocol::{wl_output, wl_surface},
    Connection, Proxy, QueueHandle,
};

const W: u32 = 832;
const H: u32 = 351;

fn now_ns() -> i64 {
    let mut ts = libc::timespec { tv_sec: 0, tv_nsec: 0 };
    unsafe { libc::clock_gettime(libc::CLOCK_MONOTONIC, &mut ts) };
    ts.tv_sec * 1_000_000_000 + ts.tv_nsec
}

struct App {
    registry_state: RegistryState,
    output_state: OutputState,
    configured: bool,
    exit: bool,
}

impl CompositorHandler for App {
    fn scale_factor_changed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: i32) {}
    fn transform_changed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: wl_output::Transform) {}
    fn frame(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: u32) {}
    fn surface_enter(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: &wl_output::WlOutput) {}
    fn surface_leave(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: &wl_output::WlOutput) {}
}
impl OutputHandler for App {
    fn output_state(&mut self) -> &mut OutputState {
        &mut self.output_state
    }
    fn new_output(&mut self, _: &Connection, _: &QueueHandle<Self>, _: wl_output::WlOutput) {}
    fn update_output(&mut self, _: &Connection, _: &QueueHandle<Self>, _: wl_output::WlOutput) {}
    fn output_destroyed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: wl_output::WlOutput) {}
}
impl LayerShellHandler for App {
    fn closed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &LayerSurface) {
        self.exit = true;
    }
    fn configure(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &LayerSurface, _: LayerSurfaceConfigure, _: u32) {
        self.configured = true;
    }
}
delegate_registry!(App);
smithay_client_toolkit::delegate_dispatch2!(App);
impl ProvidesRegistryState for App {
    fn registry(&mut self) -> &mut RegistryState {
        &mut self.registry_state
    }
    registry_handlers![OutputState];
}

fn main() {
    let t0: i64 = std::env::var("T0_NS").ok().and_then(|v| v.parse().ok()).unwrap_or_else(now_ns);
    let mark = |what: &str| eprintln!("{:<24} {:8.2} ms", what, (now_ns() - t0) as f64 / 1e6);
    let use_vello = std::env::var("RENDERER").as_deref() != Ok("wgpu");
    mark("main()");
    let conn = Connection::connect_to_env().unwrap();
    let (globals, mut queue) = registry_queue_init(&conn).unwrap();
    let qh = queue.handle();
    let compositor = CompositorState::bind(&globals, &qh).unwrap();
    let layer_shell = LayerShell::bind(&globals, &qh).unwrap();
    let surface = compositor.create_surface(&qh);
    let layer = layer_shell.create_layer_surface(&qh, surface, Layer::Overlay, Some("logis-proto-gpu"), None);
    layer.set_size(W, H);
    layer.set_keyboard_interactivity(KeyboardInteractivity::None);
    layer.commit();
    let mut app = App { registry_state: RegistryState::new(&globals), output_state: OutputState::new(&globals, &qh), configured: false, exit: false };
    mark("globals");

    let instance = wgpu::Instance::new(wgpu::InstanceDescriptor { backends: wgpu::Backends::VULKAN, ..wgpu::InstanceDescriptor::new_without_display_handle() });
    mark("wgpu instance");
    let rdh = RawDisplayHandle::Wayland(WaylandDisplayHandle::new(NonNull::new(conn.backend().display_ptr() as *mut _).unwrap()));
    let rwh = RawWindowHandle::Wayland(WaylandWindowHandle::new(NonNull::new(layer.wl_surface().id().as_ptr() as *mut _).unwrap()));
    let surf = unsafe { instance.create_surface_unsafe(wgpu::SurfaceTargetUnsafe::RawHandle { raw_display_handle: Some(rdh), raw_window_handle: rwh }).unwrap() };
    let adapter = pollster::block_on(instance.request_adapter(&wgpu::RequestAdapterOptions { compatible_surface: Some(&surf), ..Default::default() })).expect("adapter");
    mark("wgpu adapter");
    let (device, gq) = pollster::block_on(adapter.request_device(&wgpu::DeviceDescriptor::default())).expect("device");
    mark("wgpu device");
    let caps = surf.get_capabilities(&adapter);
    let format = caps.formats.iter().copied().find(|f| !f.is_srgb()).unwrap_or(caps.formats[0]);
    let alpha = if caps.alpha_modes.contains(&wgpu::CompositeAlphaMode::PreMultiplied) { wgpu::CompositeAlphaMode::PreMultiplied } else { caps.alpha_modes[0] };
    while !app.configured {
        queue.blocking_dispatch(&mut app).unwrap();
    }
    mark("configured");
    surf.configure(&device, &wgpu::SurfaceConfiguration {
        usage: wgpu::TextureUsages::RENDER_ATTACHMENT,
        format,
        color_space: Default::default(),
        width: W,
        height: H,
        present_mode: wgpu::PresentMode::Mailbox,
        desired_maximum_frame_latency: 2,
        alpha_mode: alpha,
        view_formats: vec![],
    });
    mark("surface configured");

    let frame = match surf.get_current_texture() {
        wgpu::CurrentSurfaceTexture::Success(t) | wgpu::CurrentSurfaceTexture::Suboptimal(t) => t,
        _ => panic!("no surface texture"),
    };
    let view = frame.texture.create_view(&wgpu::TextureViewDescriptor::default());
    if use_vello {
        let mut renderer = Renderer::new(&device, RendererOptions { antialiasing_support: vello::AaSupport::area_only(), ..Default::default() }).expect("vello");
        mark("vello renderer (shaders)");
        let mut scene = Scene::new();
        let outer = RoundedRect::new(0.0, 0.0, W as f64, H as f64, 18.0);
        let grad = Gradient::new_linear((0.0, 0.0), (0.0, H as f64)).with_stops([Color::from_rgba8(0xb3, 0x1b, 0x1b, 0xff), Color::from_rgba8(0xd4, 0xa0, 0x17, 0xff)]);
        scene.fill(Fill::NonZero, Affine::IDENTITY, &grad, None, &outer);
        scene.fill(Fill::NonZero, Affine::IDENTITY, Color::from_rgba8(0x14, 0x0d, 0x0b, 0xf0), None, &RoundedRect::new(2.6, 2.6, W as f64 - 2.6, H as f64 - 2.6, 15.4));
        let target = device.create_texture(&wgpu::TextureDescriptor {
            label: None,
            size: wgpu::Extent3d { width: W, height: H, depth_or_array_layers: 1 },
            mip_level_count: 1,
            sample_count: 1,
            dimension: wgpu::TextureDimension::D2,
            format: wgpu::TextureFormat::Rgba8Unorm,
            usage: wgpu::TextureUsages::STORAGE_BINDING | wgpu::TextureUsages::TEXTURE_BINDING,
            view_formats: &[],
        });
        let tview = target.create_view(&wgpu::TextureViewDescriptor::default());
        renderer.render_to_texture(&device, &gq, &scene, &tview, &RenderParams { base_color: palette::css::TRANSPARENT, width: W, height: H, antialiasing_method: AaConfig::Area }).expect("render");
        let blitter = wgpu::util::TextureBlitter::new(&device, format);
        let mut enc = device.create_command_encoder(&wgpu::CommandEncoderDescriptor::default());
        blitter.copy(&device, &mut enc, &tview, &view);
        gq.submit([enc.finish()]);
        mark("vello rendered");
    } else {
        let mut enc = device.create_command_encoder(&wgpu::CommandEncoderDescriptor::default());
        {
            let _pass = enc.begin_render_pass(&wgpu::RenderPassDescriptor {
                label: None,
                color_attachments: &[Some(wgpu::RenderPassColorAttachment {
                    view: &view,
                    depth_slice: None,
                    resolve_target: None,
                    ops: wgpu::Operations { load: wgpu::LoadOp::Clear(wgpu::Color { r: 0.08, g: 0.05, b: 0.04, a: 0.94 }), store: wgpu::StoreOp::Store },
                })],
                ..Default::default()
            });
        }
        gq.submit([enc.finish()]);
    }
    gq.present(frame);
    mark("committed (present)");
    let _ = queue.roundtrip(&mut app);
    let _ = app.exit;
}
