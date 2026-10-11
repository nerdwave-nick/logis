//! Research benchmark (disposable): time-to-first-frame of a Rust layer-shell
//! picker built on smithay-client-toolkit + tiny-skia + cosmic-text, drawing
//! the same kind of frame as the servitor Go prototype (gradient-bordered
//! rounded panel, prompt, eight rows of text). Milestones are CLOCK_MONOTONIC
//! ms relative to $T0_NS (set by research/bench/launch.py before exec).
//!
//! FONTS=scan  → cosmic_text::FontSystem::new() (fontdb scans all system fonts)
//! FONTS=fc    → only the file fc-match names for "monospace" (default)
use std::{process::Command, sync::mpsc, thread};

use cosmic_text::{Attrs, Buffer, Color, Family, FontSystem, Metrics, Shaping, SwashCache};
use smithay_client_toolkit::{
    compositor::{CompositorHandler, CompositorState, FrameCallbackData},
    delegate_registry,
    output::{OutputHandler, OutputState},
    registry::{ProvidesRegistryState, RegistryState},
    registry_handlers,
    shell::{
        wlr_layer::{KeyboardInteractivity, Layer, LayerShell, LayerShellHandler, LayerSurface, LayerSurfaceConfigure},
        WaylandSurface,
    },
    shm::{slot::SlotPool, Shm, ShmHandler},
};
use tiny_skia::{Color as SkColor, FillRule, GradientStop, LinearGradient, Paint, PathBuilder, Pixmap, Point, Rect, SpreadMode, Transform};
use wayland_client::{
    globals::registry_queue_init,
    protocol::{wl_output, wl_shm, wl_surface},
    Connection, QueueHandle,
};

fn now_ns() -> i64 {
    let mut ts = libc::timespec { tv_sec: 0, tv_nsec: 0 };
    unsafe { libc::clock_gettime(libc::CLOCK_MONOTONIC, &mut ts) };
    ts.tv_sec * 1_000_000_000 + ts.tv_nsec
}

fn t0() -> i64 {
    std::env::var("T0_NS").ok().and_then(|v| v.parse().ok()).unwrap_or_else(now_ns)
}

fn mark(t0: i64, what: &str) {
    eprintln!("{:<24} {:8.2} ms", what, (now_ns() - t0) as f64 / 1e6);
}

const W: u32 = 832;
const H: u32 = 351;
const ROWS: [(&str, &str, &str); 7] = [
    ("theme", "porpl", "The visage of the machine"),
    ("mouse-autohide-toggle", "off", "Veil the cursor when idle"),
    ("do-not-disturb", "on", "Silence the vox of the desktop"),
    ("keyboard-layout", "us", "The tongue of the keys"),
    ("power-profile", "balanced", "The hunger of the machine spirit"),
    ("night-light", "off", "Dim the light of the screens"),
    ("vox", "auto", "Who hears the machine"),
];

fn load_fonts() -> FontSystem {
    if std::env::var("FONTS").as_deref() == Ok("scan") {
        return FontSystem::new();
    }
    let out = Command::new("fc-match").args(["--format=%{file}", "monospace"]).output().expect("fc-match");
    let path = String::from_utf8_lossy(&out.stdout).to_string();
    let mut db = cosmic_text::fontdb::Database::new();
    db.load_font_file(&path).expect("font file");
    let family = db.faces().next().map(|f| f.families[0].0.clone()).unwrap_or_default();
    db.set_monospace_family(family);
    FontSystem::new_with_locale_and_db("en-US".into(), db)
}

struct App {
    registry_state: RegistryState,
    output_state: OutputState,
    shm: Shm,
    pool: SlotPool,
    layer: LayerSurface,
    fonts_rx: Option<mpsc::Receiver<FontSystem>>,
    fonts: Option<FontSystem>,
    cache: SwashCache,
    drawn: bool,
    exit: bool,
    t0: i64,
}

fn rounded_rect(x: f32, y: f32, w: f32, h: f32, r: f32) -> tiny_skia::Path {
    let mut pb = PathBuilder::new();
    let k = 0.5523 * r;
    pb.move_to(x + r, y);
    pb.line_to(x + w - r, y);
    pb.cubic_to(x + w - r + k, y, x + w, y + r - k, x + w, y + r);
    pb.line_to(x + w, y + h - r);
    pb.cubic_to(x + w, y + h - r + k, x + w - r + k, y + h, x + w - r, y + h);
    pb.line_to(x + r, y + h);
    pb.cubic_to(x + r - k, y + h, x, y + h - r + k, x, y + h - r);
    pb.line_to(x, y + r);
    pb.cubic_to(x, y + r - k, x + r - k, y, x + r, y);
    pb.close();
    pb.finish().unwrap()
}

impl App {
    fn draw(&mut self, qh: &QueueHandle<Self>) {
        let fonts = self.fonts.get_or_insert_with(|| self.fonts_rx.take().unwrap().recv().unwrap());
        mark(self.t0, "fonts ready");
        let start = now_ns();
        let mut pm = Pixmap::new(W, H).unwrap();
        // Panel: gradient border, translucent body.
        let mut border = Paint::default();
        border.anti_alias = true;
        border.shader = LinearGradient::new(
            Point::from_xy(0.0, 0.0),
            Point::from_xy(0.0, H as f32),
            vec![GradientStop::new(0.0, SkColor::from_rgba8(0xb3, 0x1b, 0x1b, 0xff)), GradientStop::new(1.0, SkColor::from_rgba8(0xd4, 0xa0, 0x17, 0xff))],
            SpreadMode::Pad,
            Transform::identity(),
        )
        .unwrap();
        pm.fill_path(&rounded_rect(0.0, 0.0, W as f32, H as f32, 18.0), &border, FillRule::Winding, Transform::identity(), None);
        let mut body = Paint::default();
        body.anti_alias = true;
        body.set_color_rgba8(0x14, 0x0d, 0x0b, 0xf0);
        pm.fill_path(&rounded_rect(2.6, 2.6, W as f32 - 5.2, H as f32 - 5.2, 15.4), &body, FillRule::Winding, Transform::identity(), None);
        let mut sel = Paint::default();
        sel.set_color_rgba8(0x4a, 0x12, 0x10, 0xff);
        pm.fill_rect(Rect::from_xywh(13.0, 66.0, W as f32 - 26.0, 33.0).unwrap(), &sel, Transform::identity(), None);

        // Text: one buffer per column, cosmic-text shaping + swash rasterising.
        let metrics = Metrics::new(19.5, 33.0);
        let cols: [(f32, Color, Vec<&str>); 3] = [
            (21.0, Color::rgb(0xe8, 0xdc, 0xc8), std::iter::once("> ").chain(ROWS.iter().map(|r| r.0)).collect()),
            (370.0, Color::rgb(0xd4, 0xa0, 0x17), std::iter::once("").chain(ROWS.iter().map(|r| r.1)).collect()),
            (536.0, Color::rgb(0x8a, 0x7a, 0x66), std::iter::once("").chain(ROWS.iter().map(|r| r.2)).collect()),
        ];
        for (x, color, lines) in cols {
            let mut buf = Buffer::new(fonts, metrics);
            buf.set_size(Some(W as f32 - x - 16.0), Some(H as f32));
            buf.set_text(&lines.join("\n"), &Attrs::new().family(Family::Monospace), Shaping::Advanced, None);
            let stride = W as usize * 4;
            let data = pm.data_mut();
            buf.draw(fonts, &mut self.cache, color, |gx, gy, gw, gh, c| {
                let a = c.a() as u32;
                if a == 0 {
                    return;
                }
                for yy in gy..gy + gh as i32 {
                    for xx in gx..gx + gw as i32 {
                        let (px, py) = (xx + x as i32, yy + 18);
                        if px < 0 || py < 0 || px >= W as i32 || py >= H as i32 {
                            continue;
                        }
                        let i = py as usize * stride + px as usize * 4;
                        for (k, s) in [c.r(), c.g(), c.b()].iter().enumerate() {
                            data[i + k] = ((*s as u32 * a + data[i + k] as u32 * (255 - a)) / 255) as u8;
                        }
                        data[i + 3] = (a + data[i + 3] as u32 * (255 - a) / 255) as u8;
                    }
                }
            });
        }
        eprintln!("{:<24} {:8.2} ms", "  (paint took)", (now_ns() - start) as f64 / 1e6);

        let (buffer, canvas) = self.pool.create_buffer(W as i32, H as i32, W as i32 * 4, wl_shm::Format::Abgr8888).expect("buffer");
        canvas.copy_from_slice(pm.data());
        self.layer.wl_surface().damage_buffer(0, 0, W as i32, H as i32);
        self.layer.wl_surface().frame(qh, FrameCallbackData(self.layer.wl_surface().clone()));
        buffer.attach_to(self.layer.wl_surface()).unwrap();
        self.layer.commit();
        mark(self.t0, "committed");
        self.drawn = true;
    }
}

impl CompositorHandler for App {
    fn scale_factor_changed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: i32) {}
    fn transform_changed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: wl_output::Transform) {}
    fn frame(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &wl_surface::WlSurface, _: u32) {
        mark(self.t0, "frame callback");
        self.exit = true;
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
        self.exit = true;
    }
    fn configure(&mut self, _: &Connection, qh: &QueueHandle<Self>, _: &LayerSurface, _: LayerSurfaceConfigure, _: u32) {
        mark(self.t0, "configured");
        if !self.drawn {
            self.draw(qh);
        }
    }
}

impl ShmHandler for App {
    fn shm_state(&mut self) -> &mut Shm {
        &mut self.shm
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
    let t0 = t0();
    mark(t0, "main()");
    let (tx, rx) = mpsc::channel();
    thread::spawn(move || {
        let fs = load_fonts();
        tx.send(fs).unwrap();
    });
    let conn = Connection::connect_to_env().unwrap();
    let (globals, mut queue) = registry_queue_init(&conn).unwrap();
    let qh = queue.handle();
    mark(t0, "globals");
    let compositor = CompositorState::bind(&globals, &qh).unwrap();
    let layer_shell = LayerShell::bind(&globals, &qh).unwrap();
    let shm = Shm::bind(&globals, &qh).unwrap();
    let surface = compositor.create_surface(&qh);
    let layer = layer_shell.create_layer_surface(&qh, surface, Layer::Overlay, Some("logis-proto"), None);
    layer.set_size(W, H);
    layer.set_keyboard_interactivity(KeyboardInteractivity::None);
    layer.commit();
    let pool = SlotPool::new((W * H * 4) as usize, &shm).unwrap();
    let mut app = App {
        registry_state: RegistryState::new(&globals),
        output_state: OutputState::new(&globals, &qh),
        shm,
        pool,
        layer,
        fonts_rx: Some(rx),
        fonts: None,
        cache: SwashCache::new(),
        drawn: false,
        exit: false,
        t0,
    };
    while !app.exit {
        queue.blocking_dispatch(&mut app).unwrap();
    }
}
