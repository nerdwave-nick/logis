//! Research spike (disposable): the candidate GPU foundation for logis.
//! sctk layer surface + wp_fractional_scale/viewporter + wgpu (Vulkan) +
//! a single WGSL panel shader + glyphon (cosmic-text) text.
//!
//! SPIKE=anim    (default) open once, animate SPIKE_FRAMES frames, report per-frame cost.
//! SPIKE=daemon  stay resident; each datagram on $XDG_RUNTIME_DIR/logis-spike.sock
//!               (payload: client CLOCK_MONOTONIC ns) opens the panel for 700 ms and
//!               reports request→committed latency. "quit" exits.
//! SPIKE_FULL=1  fullscreen layer surface with a dimmed backdrop (worst-case fill cost).
use std::{os::fd::AsRawFd, os::unix::net::UnixDatagram, ptr::NonNull, time::Duration};

use glyphon::{Attrs, Buffer, Cache, Color, Family, FontSystem, Metrics, Resolution, Shaping, SwashCache, TextArea, TextAtlas, TextBounds, TextRenderer, Viewport};
use raw_window_handle::{RawDisplayHandle, RawWindowHandle, WaylandDisplayHandle, WaylandWindowHandle};
use smithay_client_toolkit::{
    compositor::{CompositorHandler, CompositorState, FrameCallbackData},
    delegate_registry,
    output::{OutputHandler, OutputState},
    registry::{ProvidesRegistryState, RegistryState},
    registry_handlers,
    shell::{
        wlr_layer::{Anchor, KeyboardInteractivity, Layer, LayerShell, LayerShellHandler, LayerSurface, LayerSurfaceConfigure},
        WaylandSurface,
    },
};
use vello::wgpu;
use wayland_client::{
    globals::{registry_queue_init, GlobalList},
    protocol::{wl_output, wl_region, wl_surface},
    Connection, Dispatch, Proxy, QueueHandle,
};
use wayland_protocols::ext::background_effect::v1::client::{
    ext_background_effect_manager_v1::ExtBackgroundEffectManagerV1, ext_background_effect_surface_v1::ExtBackgroundEffectSurfaceV1,
};
use wayland_protocols::wp::{
    fractional_scale::v1::client::{wp_fractional_scale_manager_v1::WpFractionalScaleManagerV1, wp_fractional_scale_v1::{self, WpFractionalScaleV1}},
    viewporter::client::{wp_viewport::WpViewport, wp_viewporter::WpViewporter},
};

fn now_ns() -> i64 {
    let mut ts = libc::timespec { tv_sec: 0, tv_nsec: 0 };
    unsafe { libc::clock_gettime(libc::CLOCK_MONOTONIC, &mut ts) };
    ts.tv_sec * 1_000_000_000 + ts.tv_nsec
}
fn ms(ns: i64) -> f64 {
    ns as f64 / 1e6
}

const LOGICAL: (u32, u32) = (640, 360);
const ROWS: [(&str, &str, &str); 7] = [
    ("theme", "porpl", "The visage of the machine"),
    ("mouse-autohide-toggle", "off", "Veil the cursor when idle"),
    ("do-not-disturb", "on", "Silence the vox of the desktop"),
    ("keyboard-layout", "us", "The tongue of the keys"),
    ("power-profile", "balanced", "The hunger of the machine spirit"),
    ("night-light", "off", "Dim the light of the screens"),
    ("vox", "auto", "Who hears the machine"),
];

#[repr(C)]
#[derive(Clone, Copy, bytemuck::Pod, bytemuck::Zeroable)]
struct Uniforms {
    size: [f32; 2],
    panel: [f32; 2],
    time: f32,
    scale: f32,
    sel_y0: f32,
    sel_y1: f32,
    dim: f32,
    _pad: [f32; 3],
}

struct Gpu {
    instance: wgpu::Instance,
    adapter: Option<wgpu::Adapter>,
    device: Option<(wgpu::Device, wgpu::Queue)>,
    pipeline: Option<wgpu::RenderPipeline>,
    ubuf: Option<wgpu::Buffer>,
    bind: Option<wgpu::BindGroup>,
    format: wgpu::TextureFormat,
    alpha: wgpu::CompositeAlphaMode,
    fonts: FontSystem,
    swash: SwashCache,
    text: Option<(Cache, TextAtlas, Viewport, TextRenderer)>,
    cols: Vec<(f32, Color, Buffer)>,
}

// Fields drop in declaration order: the wgpu surface must go before the
// wl_surface (owned by the layer surface) whose raw handle it holds.
struct Win {
    surface: wgpu::Surface<'static>,
    layer: LayerSurface,
    viewport: WpViewport,
    _frac: WpFractionalScaleV1,
    _blur: Option<ExtBackgroundEffectSurfaceV1>,
    logical: (u32, u32),
    configured_surface: bool,
    t_req: i64,
    opened_at: i64,
    frames: u32,
}

struct App {
    registry_state: RegistryState,
    output_state: OutputState,
    conn: Connection,
    compositor: CompositorState,
    layer_shell: LayerShell,
    frac_mgr: WpFractionalScaleManagerV1,
    viewporter: WpViewporter,
    blur: Option<ExtBackgroundEffectManagerV1>,
    scale120: u32,
    gpu: Gpu,
    win: Option<Win>,
    full: bool,
    anim_frames: u32,
    daemon: bool,
    t_start: i64,
    stats: Vec<(f64, f64, f64)>, // cpu encode, gpu wait, frame interval (ms)
    last_frame: i64,
    exit: bool,
}

fn load_fonts() -> FontSystem {
    let out = std::process::Command::new("fc-match").args(["--format=%{file}", "monospace"]).output().expect("fc-match");
    let mut db = glyphon::fontdb::Database::new();
    db.load_font_file(String::from_utf8_lossy(&out.stdout).to_string()).expect("font");
    let family = db.faces().next().map(|f| f.families[0].0.clone()).unwrap_or_default();
    db.set_monospace_family(family);
    FontSystem::new_with_locale_and_db("en-US".into(), db)
}

impl App {
    fn scale(&self) -> f32 {
        self.scale120 as f32 / 120.0
    }

    fn open(&mut self, qh: &QueueHandle<Self>, t_req: i64) {
        let surface = self.compositor.create_surface(qh);
        let frac = self.frac_mgr.get_fractional_scale(&surface, qh, ());
        let viewport = self.viewporter.get_viewport(&surface, qh, ());
        let blur = self.blur.as_ref().map(|m| m.get_background_effect(&surface, qh, ()));
        let layer = self.layer_shell.create_layer_surface(qh, surface, Layer::Overlay, Some("logis-spike"), None);
        if self.full {
            layer.set_anchor(Anchor::TOP | Anchor::BOTTOM | Anchor::LEFT | Anchor::RIGHT);
            layer.set_size(0, 0);
        } else {
            layer.set_size(LOGICAL.0 + 48, LOGICAL.1 + 48); // room for the glow
        }
        layer.set_keyboard_interactivity(KeyboardInteractivity::None);
        layer.commit();
        let rdh = RawDisplayHandle::Wayland(WaylandDisplayHandle::new(NonNull::new(self.conn.backend().display_ptr() as *mut _).unwrap()));
        let rwh = RawWindowHandle::Wayland(WaylandWindowHandle::new(NonNull::new(layer.wl_surface().id().as_ptr() as *mut _).unwrap()));
        let wsurf = unsafe { self.gpu.instance.create_surface_unsafe(wgpu::SurfaceTargetUnsafe::RawHandle { raw_display_handle: Some(rdh), raw_window_handle: rwh }).unwrap() };
        self.win = Some(Win { surface: wsurf, layer, viewport, _frac: frac, _blur: blur, logical: (0, 0), configured_surface: false, t_req, opened_at: 0, frames: 0 });
    }

    fn close(&mut self) {
        self.win = None; // drops the wgpu surface first, then the layer surface
    }

    fn ensure_device(&mut self) {
        if self.gpu.device.is_some() {
            return;
        }
        let win = self.win.as_ref().unwrap();
        let adapter = pollster::block_on(self.gpu.instance.request_adapter(&wgpu::RequestAdapterOptions { compatible_surface: Some(&win.surface), ..Default::default() })).expect("adapter");
        let (device, queue) = pollster::block_on(adapter.request_device(&wgpu::DeviceDescriptor::default())).expect("device");
        let caps = win.surface.get_capabilities(&adapter);
        self.gpu.format = caps.formats.iter().copied().find(|f| !f.is_srgb()).unwrap_or(caps.formats[0]);
        self.gpu.alpha = if caps.alpha_modes.contains(&wgpu::CompositeAlphaMode::PreMultiplied) { wgpu::CompositeAlphaMode::PreMultiplied } else { caps.alpha_modes[0] };
        let shader = device.create_shader_module(wgpu::ShaderModuleDescriptor { label: Some("panel"), source: wgpu::ShaderSource::Wgsl(include_str!("panel.wgsl").into()) });
        let ubuf = device.create_buffer(&wgpu::BufferDescriptor { label: None, size: std::mem::size_of::<Uniforms>() as u64, usage: wgpu::BufferUsages::UNIFORM | wgpu::BufferUsages::COPY_DST, mapped_at_creation: false });
        let bgl = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
            label: None,
            entries: &[wgpu::BindGroupLayoutEntry { binding: 0, visibility: wgpu::ShaderStages::FRAGMENT, ty: wgpu::BindingType::Buffer { ty: wgpu::BufferBindingType::Uniform, has_dynamic_offset: false, min_binding_size: None }, count: None }],
        });
        let bind = device.create_bind_group(&wgpu::BindGroupDescriptor { label: None, layout: &bgl, entries: &[wgpu::BindGroupEntry { binding: 0, resource: ubuf.as_entire_binding() }] });
        let layout = device.create_pipeline_layout(&wgpu::PipelineLayoutDescriptor { label: None, bind_group_layouts: &[Some(&bgl)], immediate_size: 0 });
        let pipeline = device.create_render_pipeline(&wgpu::RenderPipelineDescriptor {
            label: Some("panel"),
            layout: Some(&layout),
            vertex: wgpu::VertexState { module: &shader, entry_point: Some("vs"), buffers: &[], compilation_options: Default::default() },
            fragment: Some(wgpu::FragmentState { module: &shader, entry_point: Some("fs"), targets: &[Some(wgpu::ColorTargetState { format: self.gpu.format, blend: None, write_mask: wgpu::ColorWrites::ALL })], compilation_options: Default::default() }),
            primitive: Default::default(),
            depth_stencil: None,
            multisample: Default::default(),
            multiview_mask: None,
            cache: None,
        });
        let cache = Cache::new(&device);
        let mut atlas = TextAtlas::with_color_mode(&device, &queue, &cache, self.gpu.format, glyphon::ColorMode::Web);
        let viewport = Viewport::new(&device, &cache);
        let renderer = TextRenderer::new(&mut atlas, &device, wgpu::MultisampleState::default(), None);
        self.gpu.text = Some((cache, atlas, viewport, renderer));
        self.gpu.adapter = Some(adapter);
        self.gpu.device = Some((device, queue));
        self.gpu.pipeline = Some(pipeline);
        self.gpu.ubuf = Some(ubuf);
        self.gpu.bind = Some(bind);
    }

    fn layout_text(&mut self) {
        let s = self.scale();
        let metrics = Metrics::new(15.0 * s, 26.0 * s);
        let cols: [(f32, Color, Vec<&str>); 3] = [
            (22.0, Color::rgb(0xe8, 0xdc, 0xc8), std::iter::once("+ ").chain(ROWS.iter().map(|r| r.0)).collect()),
            (300.0, Color::rgb(0xd4, 0xa0, 0x17), std::iter::once("").chain(ROWS.iter().map(|r| r.1)).collect()),
            (400.0, Color::rgb(0x8a, 0x7a, 0x66), std::iter::once("").chain(ROWS.iter().map(|r| r.2)).collect()),
        ];
        self.gpu.cols.clear();
        for (x, color, lines) in cols {
            let mut buf = Buffer::new(&mut self.gpu.fonts, metrics);
            buf.set_wrap(glyphon::Wrap::None);
            buf.set_size(Some((LOGICAL.0 as f32 - x - 12.0) * s), Some(LOGICAL.1 as f32 * s));
            // first line is the prompt row; leave a gap below it for the rule
            let text = format!("{}\n\n{}", lines[0], lines[1..].join("\n"));
            buf.set_text(&text, &Attrs::new().family(Family::Monospace), Shaping::Advanced, None);
            buf.shape_until_scroll(&mut self.gpu.fonts, false);
            self.gpu.cols.push((x, color, buf));
        }
    }

    fn render(&mut self, qh: &QueueHandle<Self>) {
        let s = self.scale();
        let t_frame = now_ns();
        let Some(win) = self.win.as_mut() else { return };
        let (lw, lh) = win.logical;
        let (bw, bh) = (((lw as f32) * s).round() as u32, ((lh as f32) * s).round() as u32);
        let (device, queue) = self.gpu.device.as_ref().unwrap();
        if !win.configured_surface {
            win.surface.configure(device, &wgpu::SurfaceConfiguration {
                usage: wgpu::TextureUsages::RENDER_ATTACHMENT,
                format: self.gpu.format,
                color_space: Default::default(),
                width: bw,
                height: bh,
                present_mode: wgpu::PresentMode::Mailbox,
                desired_maximum_frame_latency: 2,
                alpha_mode: self.gpu.alpha,
                view_formats: vec![],
            });
            win.viewport.set_destination(lw as i32, lh as i32);
            win.configured_surface = true;
        }
        let frame = match win.surface.get_current_texture() {
            wgpu::CurrentSurfaceTexture::Success(t) | wgpu::CurrentSurfaceTexture::Suboptimal(t) => t,
            _ => return,
        };
        let view = frame.texture.create_view(&wgpu::TextureViewDescriptor::default());
        let time = ms(now_ns() - self.t_start) as f32 / 1000.0;
        let panel = [LOGICAL.0 as f32 * s, LOGICAL.1 as f32 * s];
        let u = Uniforms { size: [bw as f32, bh as f32], panel, time, scale: s, sel_y0: 66.0 * s, sel_y1: 92.0 * s, dim: if self.full { 0.45 } else { 0.0 }, _pad: [0.0; 3] };
        queue.write_buffer(self.gpu.ubuf.as_ref().unwrap(), 0, bytemuck::bytes_of(&u));
        let (_, atlas, viewport, renderer) = self.gpu.text.as_mut().unwrap();
        viewport.update(queue, Resolution { width: bw, height: bh });
        let ox = (bw as f32 - panel[0]) / 2.0;
        let oy = (bh as f32 - panel[1]) / 2.0;
        let areas = self.gpu.cols.iter().map(|(x, color, buf)| TextArea {
            buffer: buf,
            left: ox + x * s,
            top: oy + 14.0 * s,
            scale: 1.0,
            bounds: TextBounds { left: 0, top: 0, right: bw as i32, bottom: bh as i32 },
            default_color: *color,
            custom_glyphs: &[],
        });
        renderer.prepare(device, queue, &mut self.gpu.fonts, atlas, viewport, areas, &mut self.gpu.swash).unwrap();
        let mut enc = device.create_command_encoder(&wgpu::CommandEncoderDescriptor::default());
        {
            let mut pass = enc.begin_render_pass(&wgpu::RenderPassDescriptor {
                label: None,
                color_attachments: &[Some(wgpu::RenderPassColorAttachment { view: &view, depth_slice: None, resolve_target: None, ops: wgpu::Operations { load: wgpu::LoadOp::Clear(wgpu::Color::TRANSPARENT), store: wgpu::StoreOp::Store } })],
                ..Default::default()
            });
            pass.set_pipeline(self.gpu.pipeline.as_ref().unwrap());
            pass.set_bind_group(0, self.gpu.bind.as_ref().unwrap(), &[]);
            pass.draw(0..3, 0..1);
            renderer.render(atlas, viewport, &mut pass).unwrap();
        }
        queue.submit([enc.finish()]);
        let t_cpu = now_ns();
        let _ = device.poll(wgpu::PollType::wait_indefinitely());
        let t_gpu = now_ns();
        win.layer.wl_surface().frame(qh, FrameCallbackData(win.layer.wl_surface().clone()));
        queue.present(frame);
        win.frames += 1;
        if win.frames == 1 {
            win.opened_at = now_ns();
            eprintln!("{:<26} {:8.2} ms  (buffer {}x{}, scale {:.3})", "request -> committed", ms(win.opened_at - win.t_req), bw, bh, s);
        } else if win.frames > 5 {
            self.stats.push((ms(t_cpu - t_frame), ms(t_gpu - t_cpu), ms(t_frame - self.last_frame)));
        }
        self.last_frame = t_frame;
    }
}

impl CompositorHandler for App {
    fn scale_factor_changed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: i32) {}
    fn transform_changed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: wl_output::Transform) {}
    fn frame(&mut self, _: &Connection, qh: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: u32) {
        let keep_animating = match &self.win {
            Some(w) if self.daemon => ms(now_ns() - w.opened_at) < 700.0,
            Some(w) => w.frames < self.anim_frames,
            None => false,
        };
        if keep_animating {
            self.render(qh);
        } else if self.win.is_some() {
            self.close();
            if !self.daemon {
                self.exit = true;
            }
        }
    }
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
        self.win = None;
    }
    fn configure(&mut self, _: &Connection, qh: &QueueHandle<Self>, _: &LayerSurface, cfg: LayerSurfaceConfigure, _: u32) {
        let Some(win) = self.win.as_mut() else { return };
        let first = win.logical == (0, 0);
        win.logical = cfg.new_size;
        if let Some(b) = &win._blur {
            let (lw, lh) = (cfg.new_size.0 as i32, cfg.new_size.1 as i32);
            let (pw, ph) = (LOGICAL.0 as i32, LOGICAL.1 as i32);
            let (x, y) = ((lw - pw) / 2, (lh - ph) / 2);
            let region = self.compositor.wl_compositor().create_region(qh, ());
            // a rounded rectangle approximated by three rectangles (corner radius 14)
            region.add(x + 14, y, pw - 28, ph);
            region.add(x, y + 14, pw, ph - 28);
            region.add(x + 4, y + 4, pw - 8, ph - 8);
            b.set_blur_region(Some(&region));
            region.destroy();
        }
        if first {
            self.ensure_device();
            if self.gpu.cols.is_empty() {
                self.layout_text();
            }
            self.render(qh);
        }
    }
}
impl Dispatch<WpFractionalScaleV1, ()> for App {
    fn event(app: &mut Self, _: &WpFractionalScaleV1, ev: wp_fractional_scale_v1::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {
        if let wp_fractional_scale_v1::Event::PreferredScale { scale } = ev {
            if scale != app.scale120 {
                app.scale120 = scale;
                app.gpu.cols.clear(); // re-layout at the new scale on next open
                if let Some(w) = app.win.as_mut() {
                    w.configured_surface = false;
                }
            }
        }
    }
}
impl Dispatch<WpFractionalScaleManagerV1, ()> for App {
    fn event(_: &mut Self, _: &WpFractionalScaleManagerV1, _: <WpFractionalScaleManagerV1 as Proxy>::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<ExtBackgroundEffectManagerV1, ()> for App {
    fn event(_: &mut Self, _: &ExtBackgroundEffectManagerV1, ev: <ExtBackgroundEffectManagerV1 as Proxy>::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {
        eprintln!("background effect capabilities: {ev:?}");
    }
}
impl Dispatch<ExtBackgroundEffectSurfaceV1, ()> for App {
    fn event(_: &mut Self, _: &ExtBackgroundEffectSurfaceV1, _: <ExtBackgroundEffectSurfaceV1 as Proxy>::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<wl_region::WlRegion, ()> for App {
    fn event(_: &mut Self, _: &wl_region::WlRegion, _: wl_region::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<WpViewporter, ()> for App {
    fn event(_: &mut Self, _: &WpViewporter, _: <WpViewporter as Proxy>::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<WpViewport, ()> for App {
    fn event(_: &mut Self, _: &WpViewport, _: <WpViewport as Proxy>::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
delegate_registry!(App);
smithay_client_toolkit::delegate_dispatch2!(App);
impl ProvidesRegistryState for App {
    fn registry(&mut self) -> &mut RegistryState {
        &mut self.registry_state
    }
    registry_handlers![OutputState];
}

fn bind_globals(globals: &GlobalList, qh: &QueueHandle<App>) -> (WpFractionalScaleManagerV1, WpViewporter) {
    (globals.bind(qh, 1..=1, ()).expect("fractional scale"), globals.bind(qh, 1..=1, ()).expect("viewporter"))
}

fn main() {
    let t_start = now_ns();
    let t0: i64 = std::env::var("T0_NS").ok().and_then(|v| v.parse().ok()).unwrap_or(t_start);
    let daemon = std::env::var("SPIKE").as_deref() == Ok("daemon");
    let full = std::env::var("SPIKE_FULL").is_ok();
    let anim_frames = std::env::var("SPIKE_FRAMES").ok().and_then(|v| v.parse().ok()).unwrap_or(240);
    let fonts = std::thread::spawn(load_fonts);
    let conn = Connection::connect_to_env().unwrap();
    let (globals, mut queue) = registry_queue_init(&conn).unwrap();
    let qh = queue.handle();
    let (frac_mgr, viewporter) = bind_globals(&globals, &qh);
    let instance = wgpu::Instance::new(wgpu::InstanceDescriptor { backends: wgpu::Backends::VULKAN, ..wgpu::InstanceDescriptor::new_without_display_handle() });
    let mut app = App {
        registry_state: RegistryState::new(&globals),
        output_state: OutputState::new(&globals, &qh),
        compositor: CompositorState::bind(&globals, &qh).unwrap(),
        layer_shell: LayerShell::bind(&globals, &qh).unwrap(),
        conn: conn.clone(),
        frac_mgr,
        viewporter,
        blur: if std::env::var("SPIKE_NOBLUR").is_ok() { None } else { globals.bind(&qh, 1..=1, ()).ok() },
        scale120: 120,
        gpu: Gpu { instance, adapter: None, device: None, pipeline: None, ubuf: None, bind: None, format: wgpu::TextureFormat::Bgra8Unorm, alpha: wgpu::CompositeAlphaMode::Auto, fonts: fonts.join().unwrap(), swash: SwashCache::new(), text: None, cols: vec![] },
        win: None,
        full,
        anim_frames,
        daemon,
        t_start,
        stats: vec![],
        last_frame: 0,
        exit: false,
    };
    // Learn the output's fractional scale with a throwaway surface first, so the
    // first real frame is already at the right size (a daemon does this once).
    {
        let probe = app.compositor.create_surface(&qh);
        let _f = app.frac_mgr.get_fractional_scale(&probe, &qh, ());
        let l = app.layer_shell.create_layer_surface(&qh, probe, Layer::Background, Some("logis-spike-probe"), None);
        l.set_size(1, 1);
        l.commit();
        for _ in 0..3 {
            queue.roundtrip(&mut app).unwrap();
        }
    }
    let sock_path = format!("{}/logis-spike.sock", std::env::var("XDG_RUNTIME_DIR").unwrap());
    let sock = if daemon {
        let _ = std::fs::remove_file(&sock_path);
        let s = UnixDatagram::bind(&sock_path).unwrap();
        s.set_nonblocking(true).unwrap();
        eprintln!("daemon ready ({:.1} ms after start)", ms(now_ns() - t0));
        Some(s)
    } else {
        app.open(&qh, t0);
        None
    };
    while !app.exit {
        queue.flush().unwrap();
        queue.dispatch_pending(&mut app).unwrap();
        let Some(guard) = queue.prepare_read() else { continue };
        let mut fds = vec![libc::pollfd { fd: guard.connection_fd().as_raw_fd(), events: libc::POLLIN, revents: 0 }];
        if let Some(s) = &sock {
            fds.push(libc::pollfd { fd: s.as_raw_fd(), events: libc::POLLIN, revents: 0 });
        }
        unsafe { libc::poll(fds.as_mut_ptr(), fds.len() as _, 1000) };
        if fds[0].revents & libc::POLLIN != 0 {
            guard.read().unwrap();
        } else {
            drop(guard);
        }
        if let Some(s) = &sock {
            let mut buf = [0u8; 64];
            if let Ok(n) = s.recv(&mut buf) {
                let msg = String::from_utf8_lossy(&buf[..n]).to_string();
                if msg == "quit" {
                    break;
                }
                if app.win.is_none() {
                    app.open(&qh, msg.trim().parse().unwrap_or_else(|_| now_ns()));
                }
            }
        }
        queue.dispatch_pending(&mut app).unwrap();
    }
    if !app.stats.is_empty() {
        let n = app.stats.len() as f64;
        let mut cpu: Vec<f64> = app.stats.iter().map(|s| s.0).collect();
        let mut gpu: Vec<f64> = app.stats.iter().map(|s| s.1).collect();
        let mut iv: Vec<f64> = app.stats.iter().map(|s| s.2).collect();
        for v in [&mut cpu, &mut gpu, &mut iv] {
            v.sort_by(|a, b| a.partial_cmp(b).unwrap());
        }
        let p = |v: &Vec<f64>, q: f64| v[((v.len() - 1) as f64 * q) as usize];
        eprintln!("frames {}: cpu encode median {:.3} p99 {:.3} ms | gpu wait median {:.3} p99 {:.3} ms | interval median {:.2} ms", n, p(&cpu, 0.5), p(&cpu, 0.99), p(&gpu, 0.5), p(&gpu, 0.99), p(&iv, 0.5));
    }
    let _ = std::fs::remove_file(&sock_path);
    std::thread::sleep(Duration::from_millis(10));
}
