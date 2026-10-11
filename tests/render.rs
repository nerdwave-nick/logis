//! The renderer's offscreen path, read back pixel by pixel: SDF coverage,
//! borders, gradients, glows, premultiplied alpha, scales 1.0/1.3/2.0 and
//! each backend of the fallback chain.
//!
//! These tests need a GPU adapter. Without one they print `SKIPPED` and pass,
//! unless the backend is listed in `LOGIS_TEST_REQUIRE_BACKENDS` (comma
//! separated: auto, vulkan, gl, software), which turns a missing adapter into
//! a failure. CI installs Mesa lavapipe and requires `auto,vulkan,software`.
//! `LOGIS_RENDER_DUMP=<dir>` writes every rendered image there as PAM.

use logis::render::{
    BackendChoice, Border, Glow, Gpu, Image, OFFSCREEN_FORMAT, Paint, Renderer, Rgba, RoundedRect,
    Scale, Scene, render_offscreen,
};

const LOGICAL: (u32, u32) = (100, 80);
const BLUE: Rgba = Rgba::hex(0x40a0ff);

fn required(choice: BackendChoice) -> bool {
    std::env::var("LOGIS_TEST_REQUIRE_BACKENDS")
        .unwrap_or_default()
        .split(',')
        .any(|name| name.trim() == choice.name())
}

/// The GPU for `choice`, or `None` (after saying so) when there is none.
fn gpu(choice: BackendChoice) -> Option<Gpu> {
    match Gpu::headless(choice) {
        Ok(gpu) => Some(gpu),
        Err(e) if required(choice) => panic!("backend '{choice}' is required: {e}"),
        Err(e) => {
            eprintln!(
                "SKIPPED: {e}. List '{choice}' in LOGIS_TEST_REQUIRE_BACKENDS to make this fail."
            );
            None
        }
    }
}

fn render_with(gpu: &Gpu, name: &str, scene: &Scene, scale: Scale) -> Image {
    render_format(gpu, name, scene, scale, OFFSCREEN_FORMAT)
}

fn render_format(
    gpu: &Gpu,
    name: &str,
    scene: &Scene,
    scale: Scale,
    format: wgpu::TextureFormat,
) -> Image {
    let mut renderer = Renderer::new(gpu);
    let image = render_offscreen(gpu, &mut renderer, scene, LOGICAL, scale, format).unwrap();
    if let Some(dir) = std::env::var_os("LOGIS_RENDER_DUMP") {
        let path = std::path::Path::new(&dir).join(format!("{name}.pam"));
        std::fs::write(&path, pam(&image)).unwrap();
    }
    image
}

/// Netpbm PAM (RGB_ALPHA): the simplest lossless format that keeps alpha.
fn pam(image: &Image) -> Vec<u8> {
    let mut out = format!(
        "P7\nWIDTH {}\nHEIGHT {}\nDEPTH 4\nMAXVAL 255\nTUPLTYPE RGB_ALPHA\nENDHDR\n",
        image.width, image.height
    )
    .into_bytes();
    out.extend_from_slice(&image.pixels);
    out
}

fn shape(rect: [f32; 4], radius: f32, fill: Paint) -> RoundedRect {
    RoundedRect {
        rect,
        radius,
        fill,
        border: None,
        glow: None,
    }
}

fn scene(shapes: Vec<RoundedRect>) -> Scene {
    Scene {
        clear: None,
        shapes,
        ..Scene::default()
    }
}

#[track_caller]
fn assert_near(actual: [u8; 4], expected: [u8; 4], tolerance: u8) {
    let close = actual
        .iter()
        .zip(expected)
        .all(|(a, e)| a.abs_diff(e) <= tolerance);
    assert!(
        close,
        "pixel {actual:?} is not within {tolerance} of {expected:?}"
    );
}

#[track_caller]
fn assert_premultiplied(image: &Image) {
    for (i, px) in image.pixels.chunks(4).enumerate() {
        let (x, y) = (i as u32 % image.width, i as u32 / image.width);
        assert!(
            px[0] <= px[3] && px[1] <= px[3] && px[2] <= px[3],
            "pixel ({x}, {y}) = {px:?} is not premultiplied"
        );
    }
}

/// The reference scene: an opaque blue rounded rectangle, radius 20.
fn reference() -> Scene {
    scene(vec![shape(
        [10.0, 10.0, 80.0, 60.0],
        20.0,
        Paint::Solid(BLUE),
    )])
}

/// What the reference scene must look like at `scale`.
#[track_caller]
fn assert_reference(image: &Image, scale: Scale) {
    assert_reference_colours(image, scale);
    assert_premultiplied(image);
}

/// The reference scene's colours at known points, whatever the encoding.
#[track_caller]
fn assert_reference_colours(image: &Image, scale: Scale) {
    let s = scale.factor();
    let at = |x: f32, y: f32| image.pixel((x * s) as u32, (y * s) as u32);
    assert_eq!((image.width, image.height), scale.physical(LOGICAL));
    assert_near(at(50.0, 40.0), [0x40, 0xa0, 0xff, 255], 1); // interior
    assert_near(at(30.0, 12.0), [0x40, 0xa0, 0xff, 255], 1); // straight top edge
    assert_eq!(at(12.0, 12.0), [0, 0, 0, 0]); // outside the corner's arc
    assert_eq!(at(5.0, 40.0), [0, 0, 0, 0]); // left of the shape
    assert_eq!(at(50.0, 75.0), [0, 0, 0, 0]); // below the shape
}

#[test]
fn a_rounded_rect_covers_its_interior_and_spares_its_corners() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    let image = render_with(&gpu, "reference-1.0", &reference(), Scale::ONE);
    assert_reference(&image, Scale::ONE);
}

#[test]
fn scales_one_one_point_three_and_two_size_and_place_the_shape() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    for (numerator, name) in [(120, "1.0"), (156, "1.3"), (240, "2.0")] {
        let scale = Scale::from_120ths(numerator);
        let image = render_with(&gpu, &format!("reference-{name}"), &reference(), scale);
        assert_reference(&image, scale);
        // The right edge sits at 90 logical pixels at every scale.
        let edge = 90.0 * scale.factor();
        let y = (40.0 * scale.factor()) as u32;
        assert_eq!(image.pixel(edge as u32 - 2, y)[3], 255, "scale {name}");
        assert_eq!(image.pixel(edge as u32 + 2, y)[3], 0, "scale {name}");
    }
}

#[test]
fn a_translucent_fill_is_premultiplied() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    let red = Rgba::new(1.0, 0.0, 0.0, 0.5);
    let image = render_with(
        &gpu,
        "translucent",
        &scene(vec![shape(
            [10.0, 10.0, 80.0, 60.0],
            8.0,
            Paint::Solid(red),
        )]),
        Scale::ONE,
    );
    assert_near(image.pixel(50, 40), [128, 0, 0, 128], 1);
    assert_premultiplied(&image);
}

#[test]
fn an_edge_between_pixels_is_half_covered() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    // The left edge runs through the middle of pixel column 10.
    let rect = shape(
        [10.5, 10.0, 60.0, 60.0],
        0.0,
        Paint::Solid(Rgba::hex(0xffffff)),
    );
    let image = render_with(&gpu, "half-pixel-edge", &scene(vec![rect]), Scale::ONE);
    assert_near(image.pixel(10, 40), [128, 128, 128, 128], 2);
    assert_eq!(image.pixel(9, 40), [0, 0, 0, 0]);
    assert_eq!(image.pixel(11, 40), [255, 255, 255, 255]);
}

#[test]
fn a_border_paints_the_ring_and_the_fill_the_interior() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    let mut framed = shape(
        [10.0, 10.0, 80.0, 60.0],
        0.0,
        Paint::Solid(Rgba::hex(0x000000)),
    );
    framed.border = Some(Border {
        width: 4.0,
        paint: Paint::Solid(Rgba::hex(0xd4a017)),
    });
    let image = render_with(&gpu, "border", &scene(vec![framed]), Scale::ONE);
    assert_near(image.pixel(11, 40), [0xd4, 0xa0, 0x17, 255], 1); // in the ring
    assert_near(image.pixel(13, 40), [0xd4, 0xa0, 0x17, 255], 1);
    assert_near(image.pixel(15, 40), [0, 0, 0, 255], 1); // inside it
    assert_near(image.pixel(50, 12), [0xd4, 0xa0, 0x17, 255], 1); // top ring
}

#[test]
fn a_linear_gradient_runs_from_its_start_to_its_end() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    let gradient = Paint::Linear {
        start: [0.0, 0.0],
        end: [100.0, 0.0],
        from: Rgba::hex(0xff0000),
        to: Rgba::hex(0x0000ff),
    };
    let image = render_with(
        &gpu,
        "gradient",
        &scene(vec![shape([0.0, 0.0, 100.0, 80.0], 0.0, gradient)]),
        Scale::ONE,
    );
    assert_near(image.pixel(0, 40), [255, 0, 0, 255], 2);
    assert_near(image.pixel(50, 40), [127, 0, 128, 255], 2);
    assert_near(image.pixel(99, 40), [0, 0, 255, 255], 2);
}

#[test]
fn a_glow_fades_outside_the_edge_and_stays_out_of_the_interior() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    let mut lit = shape(
        [40.0, 30.0, 20.0, 20.0],
        4.0,
        Paint::Solid(Rgba::hex(0x000000)),
    );
    lit.glow = Some(Glow {
        radius: 20.0,
        color: Rgba::hex(0xb30f0d),
    });
    let image = render_with(&gpu, "glow", &scene(vec![lit]), Scale::ONE);
    let alpha_right_of_edge = |gap: u32| image.pixel(60 + gap, 40)[3];
    assert!(alpha_right_of_edge(2) > alpha_right_of_edge(10));
    assert!(alpha_right_of_edge(10) > 0);
    assert_eq!(alpha_right_of_edge(21), 0);
    assert_near(image.pixel(50, 40), [0, 0, 0, 255], 1); // opaque interior
    assert_premultiplied(&image);
}

#[test]
fn later_shapes_paint_over_earlier_ones_and_the_clear_colour_shows_through() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    let mut layered = scene(vec![
        shape(
            [0.0, 0.0, 60.0, 80.0],
            0.0,
            Paint::Solid(Rgba::hex(0xff0000)),
        ),
        shape(
            [40.0, 0.0, 60.0, 80.0],
            0.0,
            Paint::Solid(Rgba::hex(0x00ff00)),
        ),
    ]);
    layered.shapes[1].rect[3] = 40.0;
    layered.clear = Some(Rgba::hex(0x101010));
    let image = render_with(&gpu, "layers", &layered, Scale::ONE);
    assert_near(image.pixel(50, 20), [0, 255, 0, 255], 1); // overlap: top shape
    assert_near(image.pixel(50, 60), [255, 0, 0, 255], 1); // only the first
    assert_near(image.pixel(80, 60), [16, 16, 16, 255], 1); // clear colour
}

/// An sRGB target blends in linear light and stores encoded bytes, so only
/// its colours at known points are compared, not the byte-level
/// premultiplication of its anti-aliased edges.
#[test]
fn an_srgb_target_stores_the_same_colours() {
    let Some(gpu) = gpu(BackendChoice::Auto) else {
        return;
    };
    let srgb = render_format(
        &gpu,
        "reference-srgb",
        &reference(),
        Scale::ONE,
        wgpu::TextureFormat::Rgba8UnormSrgb,
    );
    assert_reference_colours(&srgb, Scale::ONE);
}

/// One test per backend of the chain: the reference scene must come out the
/// same whichever backend is forced.
fn forced(choice: BackendChoice, expect: impl Fn(&wgpu::AdapterInfo) -> bool) {
    let Some(gpu) = gpu(choice) else { return };
    let info = gpu.info();
    eprintln!(
        "{choice}: {} ({:?}, {})",
        info.name, info.backend, info.driver
    );
    assert!(expect(&info), "'{choice}' woke {info:?}");
    let image = render_with(&gpu, &format!("forced-{choice}"), &reference(), Scale::ONE);
    assert_reference(&image, Scale::ONE);
}

#[test]
fn forced_vulkan_renders_the_reference_scene() {
    forced(BackendChoice::Vulkan, |i| {
        i.backend == wgpu::Backend::Vulkan
    });
}

#[test]
fn forced_gl_renders_the_reference_scene() {
    forced(BackendChoice::Gl, |i| i.backend == wgpu::Backend::Gl);
}

#[test]
fn forced_software_renders_the_reference_scene() {
    forced(BackendChoice::Software, |i| {
        i.device_type == wgpu::DeviceType::Cpu
    });
}
