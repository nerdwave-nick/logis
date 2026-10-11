//! Icons on the GPU: the image atlas and image pass, read back pixel by
//! pixel; the icon cache over the fixture themes in `tests/fixtures/icons`;
//! and golden renders of fixture icons at scales 1.0 and 1.3.
//!
//! These tests need a GPU adapter. Without one they print `SKIPPED` and pass,
//! unless `auto` is listed in `LOGIS_TEST_REQUIRE_BACKENDS` (CI requires it,
//! on Mesa lavapipe).
//!
//! Goldens live in `tests/goldens/icons/` as PNG. A render matches when no
//! channel of any pixel differs by more than [`GOLDEN_TOLERANCE`]. On a
//! mismatch the render is written to `target/golden-failures/`.
//! `LOGIS_UPDATE_GOLDENS=1 cargo test --test icons` rewrites the goldens.
//! (A minimal harness local to this file; ticket e32f9347 consolidates.)

use std::path::{Path, PathBuf};

use logis::icons::{IconCache, IconMiss, IconThemes, SearchPaths};
use logis::render::{
    AtlasError, BackendChoice, Gpu, Image, ImageQuad, OFFSCREEN_FORMAT, Paint, Renderer, Rgba,
    RoundedRect, Scale, Scene, render_offscreen,
};
use resvg::tiny_skia::{IntSize, Pixmap};

const SCALE_1_0: Scale = Scale::ONE;

fn scale_1_3() -> Scale {
    Scale::from_120ths(156)
}

fn gpu() -> Option<Gpu> {
    let choice = BackendChoice::Auto;
    let required = std::env::var("LOGIS_TEST_REQUIRE_BACKENDS")
        .unwrap_or_default()
        .split(',')
        .any(|name| name.trim() == choice.name());
    match Gpu::headless(choice) {
        Ok(gpu) => Some(gpu),
        Err(e) if required => panic!("backend '{choice}' is required: {e}"),
        Err(e) => {
            eprintln!(
                "SKIPPED: {e}. List 'auto' in LOGIS_TEST_REQUIRE_BACKENDS to make this fail."
            );
            None
        }
    }
}

/// A `width` × `height` premultiplied image whose texels come from `texel`.
fn image(width: u32, height: u32, texel: impl Fn(u32, u32) -> [u8; 4]) -> Image {
    let pixels = (0..height)
        .flat_map(|y| (0..width).map(move |x| (x, y)))
        .flat_map(|(x, y)| texel(x, y))
        .collect();
    Image {
        width,
        height,
        pixels,
    }
}

/// Distinct, premultiplied texels, so a misplaced texel shows.
fn pattern(x: u32, y: u32) -> [u8; 4] {
    let a = 128 + (x * 16 + y * 8) as u8 % 128;
    [(x * 30) as u8 % a, (y * 40) as u8 % a, 7 % a, a]
}

fn images(quads: Vec<ImageQuad>) -> Scene {
    Scene {
        images: quads,
        ..Scene::default()
    }
}

fn render(
    gpu: &Gpu,
    renderer: &mut Renderer,
    scene: &Scene,
    logical: (u32, u32),
    scale: Scale,
) -> Image {
    render_offscreen(gpu, renderer, scene, logical, scale, OFFSCREEN_FORMAT).unwrap()
}

#[test]
fn an_image_is_drawn_texel_for_texel_at_its_snapped_corner() {
    let Some(gpu) = gpu() else { return };
    let source = image(7, 5, pattern);
    // 10.4 logical: 10 at 1.0, 13.52 → 14 at 1.3, 20.8 → 21 at 2.0.
    for (scale, corner) in [(120, 10), (156, 14), (240, 21)] {
        let mut renderer = Renderer::new(&gpu);
        let id = renderer.atlas_mut().insert(&gpu, &source).unwrap();
        let scene = images(vec![ImageQuad {
            image: id,
            origin: [10.4, 10.4],
        }]);
        let out = render(
            &gpu,
            &mut renderer,
            &scene,
            (40, 30),
            Scale::from_120ths(scale),
        );
        for y in 0..out.height {
            for x in 0..out.width {
                let inside = (corner..corner + 7).contains(&x) && (corner..corner + 5).contains(&y);
                let expected = if inside {
                    pattern(x - corner, y - corner)
                } else {
                    [0; 4]
                };
                assert_eq!(out.pixel(x, y), expected, "pixel ({x}, {y}) at {scale}/120");
            }
        }
    }
}

#[test]
fn images_paint_over_shapes_with_premultiplied_blending() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    let red_half = renderer
        .atlas_mut()
        .insert(&gpu, &image(4, 4, |_, _| [128, 0, 0, 128]))
        .unwrap();
    let scene = Scene {
        shapes: vec![RoundedRect {
            rect: [0.0, 0.0, 20.0, 20.0],
            radius: 0.0,
            fill: Paint::Solid(Rgba::new(0.0, 0.0, 1.0, 1.0)),
            border: None,
            glow: None,
        }],
        images: vec![ImageQuad {
            image: red_half,
            origin: [5.0, 5.0],
        }],
        ..Scene::default()
    };
    let out = render(&gpu, &mut renderer, &scene, (20, 20), SCALE_1_0);
    // src + dst × (1 − 128/255).
    let [r, g, b, a] = out.pixel(6, 6);
    assert_eq!((r, g, a), (128, 0, 255));
    assert!(b.abs_diff(127) <= 1, "blue {b}");
    assert_eq!(out.pixel(2, 2), [0, 0, 255, 255]);
}

#[test]
fn an_srgb_target_stores_images_linear() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    let grey = renderer
        .atlas_mut()
        .insert(&gpu, &image(4, 4, |_, _| [0x80, 0x80, 0x80, 0xff]))
        .unwrap();
    let scene = images(vec![ImageQuad {
        image: grey,
        origin: [0.0, 0.0],
    }]);
    let srgb = wgpu::TextureFormat::Rgba8UnormSrgb;
    let out = render_offscreen(&gpu, &mut renderer, &scene, (4, 4), SCALE_1_0, srgb).unwrap();
    // The texture encodes the stored linear value back to sRGB on read-out;
    // storing 0x80 unconverted would read back as about 0xbc.
    let [r, g, b, a] = out.pixel(1, 1);
    assert!(
        [r, g, b].iter().all(|c| c.abs_diff(0x80) <= 1),
        "{r} {g} {b}"
    );
    assert_eq!(a, 0xff);
}

#[test]
fn the_atlas_grows_and_keeps_its_images() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    let colours = [[255, 0, 0, 255], [0, 255, 0, 255], [0, 0, 255, 255]];
    let ids: Vec<_> = colours
        .iter()
        .map(|&c| {
            renderer
                .atlas_mut()
                .insert(&gpu, &image(300, 300, |_, _| c))
                .unwrap()
        })
        .collect();
    assert_eq!(renderer.atlas().side(), 1024);
    assert_eq!(renderer.atlas().len(), 3);
    for (id, colour) in ids.into_iter().zip(colours) {
        assert_eq!(renderer.atlas().size_of(id), Some((300, 300)));
        let scene = images(vec![ImageQuad {
            image: id,
            origin: [0.0, 0.0],
        }]);
        let out = render(&gpu, &mut renderer, &scene, (300, 300), SCALE_1_0);
        for (x, y) in [(0, 0), (299, 0), (150, 150), (0, 299), (299, 299)] {
            assert_eq!(out.pixel(x, y), colour, "({x}, {y})");
        }
    }
}

#[test]
fn the_atlas_refuses_malformed_and_oversized_images() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    let atlas = renderer.atlas_mut();
    assert_eq!(
        atlas.insert(&gpu, &image(0, 3, |_, _| [0; 4])),
        Err(AtlasError::Malformed {
            width: 0,
            height: 3
        })
    );
    let short = Image {
        width: 2,
        height: 2,
        pixels: vec![0; 15],
    };
    assert_eq!(
        atlas.insert(&gpu, &short),
        Err(AtlasError::Malformed {
            width: 2,
            height: 2
        })
    );
    let wide = image(20_000, 1, |_, _| [0; 4]);
    assert!(matches!(
        atlas.insert(&gpu, &wide),
        Err(AtlasError::TooLarge { width: 20_000, .. })
    ));
    assert!(atlas.is_empty());
    assert_eq!(atlas.side(), 512);
}

fn fixtures() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/icons")
}

/// The fixture search paths: a home and a share base dir, and pixmaps.
fn search_paths() -> SearchPaths {
    let root = fixtures();
    SearchPaths {
        icon_dirs: vec![root.join("home-icons"), root.join("share/icons")],
        pixmap_dirs: vec![root.join("pixmaps")],
    }
}

fn forge_cache() -> IconCache {
    IconCache::new(IconThemes::load(&search_paths(), "logis-forge"))
}

#[test]
fn the_cache_rasterises_each_file_once_per_pixel_size() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    let mut cache = forge_cache();
    let atlas = renderer.atlas_mut();
    let cog = cache
        .image(&gpu, atlas, "forge-cog", 32, SCALE_1_0)
        .unwrap();
    assert_eq!(
        cache.image(&gpu, atlas, "forge-cog", 32, SCALE_1_0),
        Ok(cog)
    );
    // Another name for the same file at the same pixel size shares it.
    assert_eq!(
        cache.image(&gpu, atlas, "forge-cog.png", 32, SCALE_1_0),
        Ok(cog)
    );
    assert_eq!(atlas.len(), 1);
    assert_eq!(atlas.size_of(cog), Some((32, 32)));

    // At 1.3 the same icon is 42 pixels: a second image.
    let cog_1_3 = cache
        .image(&gpu, atlas, "forge-cog", 32, scale_1_3())
        .unwrap();
    assert_ne!(cog_1_3, cog);
    assert_eq!(atlas.size_of(cog_1_3), Some((42, 42)));
    assert_eq!(
        cache.image(&gpu, atlas, "forge-cog", 32, scale_1_3()),
        Ok(cog_1_3)
    );
    assert_eq!(atlas.len(), 2);
}

#[test]
fn the_cache_remembers_misses() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    let mut cache = forge_cache();
    let atlas = renderer.atlas_mut();
    let missing = IconMiss::NotFound {
        name: "no-such-icon".into(),
    };
    assert_eq!(
        cache.image(&gpu, atlas, "no-such-icon", 32, SCALE_1_0),
        Err(missing.clone())
    );
    assert_eq!(
        cache.image(&gpu, atlas, "no-such-icon", 32, SCALE_1_0),
        Err(missing)
    );
    let broken = fixtures().join("pixmaps/broken.svg");
    let broken = broken.to_str().unwrap();
    let miss = cache.image(&gpu, atlas, broken, 32, SCALE_1_0).unwrap_err();
    assert!(
        matches!(&miss, IconMiss::Raster(e) if e.path.ends_with("broken.svg")),
        "{miss}"
    );
    assert!(atlas.is_empty());
    assert_eq!(
        cache.themes().names().collect::<Vec<_>>(),
        ["logis-forge", "logis-cogitator", "hicolor"]
    );
}

/// The largest per-channel difference a golden render may show, for GPU
/// differences in anti-aliasing of the SDF backdrop. Icons themselves are
/// rasterised on the CPU and copied texel for texel.
const GOLDEN_TOLERANCE: u8 = 3;

/// The golden scene: a backdrop panel and one icon from each lookup source,
/// 32 logical pixels each, 8 apart.
fn golden_scene(gpu: &Gpu, renderer: &mut Renderer, scale: Scale) -> (Scene, (u32, u32)) {
    const ICONS: [&str; 6] = [
        "forge-cog",       // PNG: 32x32 at 1.0, 32x32@2 at 1.3
        "forge-banner",    // SVG, wide, from the scalable directory
        "cogitator-skull", // SVG from the parent theme
        "home-sigil",      // PNG from the earlier base dir
        "hicolor-only",    // translucent PNG from hicolor
        "pixmap-only",     // indexed PNG from pixmaps, resampled
    ];
    let mut cache = forge_cache();
    let images = ICONS
        .iter()
        .enumerate()
        .map(|(i, name)| {
            let image = cache
                .image(gpu, renderer.atlas_mut(), name, 32, scale)
                .unwrap_or_else(|e| panic!("{name}: {e}"));
            ImageQuad {
                image,
                origin: [8.0 + 40.0 * i as f32, 8.0],
            }
        })
        .collect();
    let logical = (8 + 40 * ICONS.len() as u32, 48);
    let backdrop = RoundedRect {
        rect: [2.0, 2.0, logical.0 as f32 - 4.0, 44.0],
        radius: 8.0,
        fill: Paint::Solid(Rgba::hex(0x1a1410)),
        border: None,
        glow: None,
    };
    let scene = Scene {
        shapes: vec![backdrop],
        images,
        ..Scene::default()
    };
    (scene, logical)
}

fn golden_path(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/goldens/icons")
        .join(format!("{name}.png"))
}

fn to_png(image: &Image) -> Vec<u8> {
    let size = IntSize::from_wh(image.width, image.height).unwrap();
    let pixmap = Pixmap::from_vec(image.pixels.clone(), size).unwrap();
    pixmap.encode_png().unwrap()
}

/// Compares `image` with the golden `name`, or rewrites the golden when
/// `LOGIS_UPDATE_GOLDENS` is set.
#[track_caller]
fn assert_golden(name: &str, image: &Image) {
    let path = golden_path(name);
    if std::env::var_os("LOGIS_UPDATE_GOLDENS").is_some() {
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        std::fs::write(&path, to_png(image)).unwrap();
        eprintln!("updated golden {}", path.display());
        return;
    }
    let data = std::fs::read(&path).unwrap_or_else(|e| {
        panic!(
            "golden {} is unreadable ({e}); set LOGIS_UPDATE_GOLDENS=1 to write it",
            path.display()
        )
    });
    let golden = Pixmap::decode_png(&data).unwrap();
    let mismatch = |why: String| {
        let dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("target/golden-failures");
        std::fs::create_dir_all(&dir).unwrap();
        let actual = dir.join(format!("{name}.png"));
        std::fs::write(&actual, to_png(image)).unwrap();
        panic!("{name}: {why}; the render is at {}", actual.display());
    };
    if (golden.width(), golden.height()) != (image.width, image.height) {
        mismatch(format!(
            "size {}x{} differs from the golden's {}x{}",
            image.width,
            image.height,
            golden.width(),
            golden.height()
        ));
    }
    // Both sides are premultiplied; the golden went through PNG's straight
    // alpha and back, which the tolerance absorbs.
    let (mut worst, mut worst_at, mut over) = (0, 0, 0);
    for (i, (a, b)) in image
        .pixels
        .chunks(4)
        .zip(golden.data().chunks(4))
        .enumerate()
    {
        let diff = a.iter().zip(b).map(|(a, b)| a.abs_diff(*b)).max().unwrap();
        if diff > worst {
            (worst, worst_at) = (diff, i);
        }
        over += usize::from(diff > GOLDEN_TOLERANCE);
    }
    if worst > GOLDEN_TOLERANCE {
        let (x, y) = (worst_at as u32 % image.width, worst_at as u32 / image.width);
        mismatch(format!(
            "{over} pixels differ by more than {GOLDEN_TOLERANCE}; the worst, ({x}, {y}), by {worst}"
        ));
    }
}

#[test]
fn golden_icons_at_scale_1_0() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    let (scene, logical) = golden_scene(&gpu, &mut renderer, SCALE_1_0);
    let image = render(&gpu, &mut renderer, &scene, logical, SCALE_1_0);
    assert_eq!((image.width, image.height), (248, 48));
    assert_golden("icons-1.0", &image);
}

#[test]
fn golden_icons_at_scale_1_3() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    let (scene, logical) = golden_scene(&gpu, &mut renderer, scale_1_3());
    let image = render(&gpu, &mut renderer, &scene, logical, scale_1_3());
    assert_eq!((image.width, image.height), (322, 62));
    assert_golden("icons-1.3", &image);
}
