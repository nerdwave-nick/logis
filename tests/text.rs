//! Text and fonts (spec §4.4): fontconfig resolution with pinned fonts,
//! per-character fallback loading, glyphon rendering with the colour mode
//! of the target, ellipsis per column, and golden images of the corpus at
//! scales 1.0, 1.3 and 2.0.
//!
//! Fonts are pinned: an isolated fontconfig configuration that knows only the
//! OFL subsets in tests/fonts/ (made by tests/fonts/subset.sh), so results do
//! not depend on the fonts installed on the machine. Rendering tests need a
//! GPU adapter and follow tests/render.rs: `SKIPPED` without one unless
//! `LOGIS_TEST_REQUIRE_BACKENDS` lists `auto`.

mod support;

use support::golden;

use std::path::{Path, PathBuf};

use logis::render::{
    BackendChoice, Gpu, Image, ImageQuad, OFFSCREEN_FORMAT, Overflow, Paint, Renderer, Rgba,
    RoundedRect, Scale, Scene, Text, render_offscreen,
};

use logis::text::{FontConfig, FontError, Fonts, PendingFonts};

fn font(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fonts")
        .join(name)
}

const FILES: [&str; 5] = [
    "NotoSans-Regular.ttf",
    "NotoSansHebrew-Regular.ttf",
    "NotoSansArabic-Regular.ttf",
    "NotoSansCJKjp-Regular.otf",
    "NotoColorEmoji.ttf",
];

/// Aliases as a user's fonts.conf would declare them.
const RULES: &str = r#"<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <alias binding="same">
    <family>sans-serif</family>
    <prefer><family>Noto Sans</family></prefer>
  </alias>
  <alias binding="same">
    <family>logis-test-alias</family>
    <prefer><family>Noto Sans Hebrew</family></prefer>
  </alias>
</fontconfig>
"#;

fn pinned() -> FontConfig {
    let files: Vec<PathBuf> = FILES.iter().map(|f| font(f)).collect();
    let files: Vec<&Path> = files.iter().map(PathBuf::as_path).collect();
    FontConfig::isolated(RULES, &files).expect("the pinned fonts load")
}

fn pinned_fonts() -> Fonts {
    Fonts::new(pinned(), "sans-serif").expect("sans-serif resolves")
}

fn loaded_names(fonts: &Fonts) -> Vec<String> {
    fonts
        .loaded()
        .iter()
        .map(|f| f.path.file_name().unwrap().to_string_lossy().into_owned())
        .collect()
}

// ---------------------------------------------------------------- fonts

#[test]
fn aliases_resolve_to_the_files_they_prefer() {
    let config = pinned();
    let file = config.resolve("logis-test-alias").unwrap();
    assert_eq!(file.path, font("NotoSansHebrew-Regular.ttf"));
    let file = config.resolve("sans-serif").unwrap();
    assert_eq!(file.path, font("NotoSans-Regular.ttf"));
    assert_eq!(file.index, 0);
}

#[test]
fn only_the_default_face_is_loaded_at_first() {
    let fonts = pinned_fonts();
    assert_eq!(fonts.family(), "Noto Sans");
    assert_eq!(loaded_names(&fonts), ["NotoSans-Regular.ttf"]);
}

#[test]
fn an_alias_selects_the_default_family() {
    let fonts = Fonts::new(pinned(), "logis-test-alias").unwrap();
    assert_eq!(fonts.family(), "Noto Sans Hebrew");
}

#[test]
fn fallback_faces_load_per_character_only_when_needed() {
    let mut fonts = pinned_fonts();
    fonts.ensure("The machine spirit wakes: Ça déjà vu");
    assert_eq!(loaded_names(&fonts), ["NotoSans-Regular.ttf"]);

    fonts.ensure("機械の精霊");
    assert_eq!(
        loaded_names(&fonts),
        ["NotoSans-Regular.ttf", "NotoSansCJKjp-Regular.otf"]
    );

    fonts.ensure("🔥 שָׁלוֹם 機械");
    assert_eq!(
        loaded_names(&fonts),
        [
            "NotoSans-Regular.ttf",
            "NotoSansCJKjp-Regular.otf",
            "NotoColorEmoji.ttf",
            "NotoSansHebrew-Regular.ttf",
        ]
    );
}

#[test]
fn joiners_and_selectors_load_no_face() {
    // The Hebrew and Arabic subsets both list U+200D; it must not pull either in.
    let mut fonts = pinned_fonts();
    fonts.ensure("a\u{200D}b\u{FE0F}\n");
    assert_eq!(loaded_names(&fonts), ["NotoSans-Regular.ttf"]);
}

#[test]
fn a_missing_font_file_is_reported() {
    let missing = font("no-such-font.ttf");
    let error = FontConfig::isolated(RULES, &[missing.as_path()])
        .err()
        .expect("a missing file is an error");
    assert!(error.to_string().contains("no-such-font.ttf"), "{error}");
}

#[test]
fn the_system_configuration_resolves_the_default_family() {
    let config = FontConfig::system().expect("fontconfig loads the system configuration");
    let file = config
        .resolve(logis::text::DEFAULT_FAMILY)
        .expect("the system has at least one font for sans-serif");
    assert!(file.path.is_file(), "{} is not a file", file.path.display());
}

#[test]
fn fonts_resolved_on_their_own_thread_behave_like_inline_ones() {
    let pending = PendingFonts::resolve(|| Ok(pinned()), "sans-serif");
    let mut fonts = pending
        .wait()
        .expect("the font thread resolves the pinned fonts");
    assert_eq!(fonts.family(), "Noto Sans");
    assert_eq!(loaded_names(&fonts), ["NotoSans-Regular.ttf"]);
    fonts.ensure("機械の精霊");
    assert_eq!(
        loaded_names(&fonts),
        ["NotoSans-Regular.ttf", "NotoSansCJKjp-Regular.otf"]
    );
}

#[test]
fn a_font_thread_reports_why_it_failed() {
    let failed = PendingFonts::resolve(|| Err(FontError::Config("no rules".into())), "sans-serif");
    assert_eq!(
        failed.wait().err(),
        Some(FontError::Config("no rules".into()))
    );
    let missing = PendingFonts::resolve(|| Ok(pinned()), "\0");
    assert_eq!(missing.wait().err(), Some(FontError::NoFace("\0".into())));
}

#[test]
fn a_font_thread_that_dies_is_an_error_not_a_panic() {
    let pending = PendingFonts::resolve(|| panic!("the font scrolls burn"), "sans-serif");
    let error = pending
        .wait()
        .err()
        .expect("a dead font thread is an error");
    assert!(error.to_string().contains("font"), "{error}");
}

// ------------------------------------------------------------ rendering

/// Scales 1.0, 1.3 and 2.0.
fn scales() -> [Scale; 3] {
    [Scale::ONE, Scale::from_120ths(156), Scale::from_integer(2)]
}

fn required() -> bool {
    std::env::var("LOGIS_TEST_REQUIRE_BACKENDS")
        .unwrap_or_default()
        .split(',')
        .any(|name| name.trim() == BackendChoice::Auto.name())
}

/// The GPU of the default chain, or `None` (after saying so) without one.
fn gpu() -> Option<Gpu> {
    match Gpu::headless(BackendChoice::Auto) {
        Ok(gpu) => {
            let info = gpu.info();
            eprintln!("GPU: {} ({:?})", info.name, info.backend);
            Some(gpu)
        }
        Err(e) if required() => panic!("a GPU is required: {e}"),
        Err(e) => {
            eprintln!(
                "SKIPPED: {e}. List 'auto' in LOGIS_TEST_REQUIRE_BACKENDS to make this fail."
            );
            None
        }
    }
}

fn render(gpu: &Gpu, scene: &Scene, logical: (u32, u32), scale: Scale) -> Image {
    render_format(gpu, scene, logical, scale, OFFSCREEN_FORMAT)
}

fn render_format(
    gpu: &Gpu,
    scene: &Scene,
    logical: (u32, u32),
    scale: Scale,
    format: wgpu::TextureFormat,
) -> Image {
    render_with(gpu, pinned_fonts(), scene, logical, scale, format)
}

fn render_with(
    gpu: &Gpu,
    fonts: Fonts,
    scene: &Scene,
    logical: (u32, u32),
    scale: Scale,
    format: wgpu::TextureFormat,
) -> Image {
    let mut renderer = Renderer::new(gpu);
    renderer.set_fonts(gpu, fonts);
    render_offscreen(gpu, &mut renderer, scene, logical, scale, format).unwrap()
}

fn texts(texts: Vec<Text>) -> Scene {
    Scene {
        clear: None,
        shapes: Vec::new(),
        images: Vec::new(),
        texts,
        post: None,
    }
}

const WHITE: Rgba = Rgba::hex(0xffffff);
const BLUE: Rgba = Rgba::hex(0x40a0ff);

/// Pixels with any coverage, as (x, y).
fn ink(image: &Image) -> impl Iterator<Item = (u32, u32)> + '_ {
    image
        .pixels
        .chunks(4)
        .enumerate()
        .filter(|(_, px)| px[3] > 0)
        .map(|(i, _)| (i as u32 % image.width, i as u32 / image.width))
}

#[test]
fn text_is_drawn_where_it_is_placed() {
    let Some(gpu) = gpu() else { return };
    for scale in scales() {
        let f = scale.factor();
        let scene = texts(vec![Text::new("Hail", [10.0, 8.0], 16.0, WHITE)]);
        let image = render(&gpu, &scene, (120, 40), scale);
        let ink: Vec<_> = ink(&image).collect();
        assert!(ink.len() > 40, "@{f}: only {} inked pixels", ink.len());
        let left = ink.iter().map(|p| p.0).min().unwrap() as f32;
        let top = ink.iter().map(|p| p.1).min().unwrap() as f32;
        let right = ink.iter().map(|p| p.0).max().unwrap() as f32;
        let bottom = ink.iter().map(|p| p.1).max().unwrap() as f32;
        assert!(
            left >= 10.0 * f - 1.0 && left <= 13.0 * f,
            "@{f}: left {left}"
        );
        assert!(top >= 8.0 * f && top <= 16.0 * f, "@{f}: top {top}");
        assert!(right <= 50.0 * f, "@{f}: right {right}");
        assert!(bottom <= 8.0 * f + 16.0 * 1.3 * f, "@{f}: bottom {bottom}");
    }
}

#[test]
fn text_keeps_its_colour_on_linear_and_srgb_targets() {
    let Some(gpu) = gpu() else { return };
    let scene = texts(vec![Text::new("IIII", [4.0, 4.0], 48.0, BLUE)]);
    for format in [
        wgpu::TextureFormat::Rgba8Unorm,
        wgpu::TextureFormat::Rgba8UnormSrgb,
    ] {
        let image = render_format(&gpu, &scene, (120, 80), Scale::ONE, format);
        let solid: Vec<_> = image.pixels.chunks(4).filter(|px| px[3] == 255).collect();
        assert!(
            solid.len() > 100,
            "{format:?}: only {} solid pixels",
            solid.len()
        );
        for px in solid {
            let close = px[..3]
                .iter()
                .zip([0x40u8, 0xa0, 0xff])
                .all(|(a, e)| a.abs_diff(e) <= 2);
            assert!(close, "{format:?}: solid glyph pixel {px:?} is not #40a0ff");
        }
    }
}

#[test]
fn colour_emoji_keep_their_own_colours() {
    let Some(gpu) = gpu() else { return };
    // A white mask would give grey pixels; the fire emoji is orange and red.
    let scene = texts(vec![Text::new("🔥", [4.0, 4.0], 40.0, WHITE)]);
    let image = render(&gpu, &scene, (64, 64), Scale::ONE);
    let coloured = image
        .pixels
        .chunks(4)
        .filter(|px| px[3] == 255 && px[0].abs_diff(px[2]) > 80)
        .count();
    assert!(coloured > 50, "only {coloured} coloured pixels");
}

fn long_line(overflow: Overflow) -> Scene {
    texts(vec![
        Text::new("MMMMMMMMMMMMMMMMMMMM", [10.0, 10.0], 16.0, WHITE).in_column(60.0, overflow),
    ])
}

/// Inked rows within the last two inked columns: the height of the last mark.
fn last_mark_height(image: &Image) -> u32 {
    let ink: Vec<_> = ink(image).collect();
    let right = ink.iter().map(|p| p.0).max().unwrap();
    let rows: Vec<u32> = ink
        .iter()
        .filter(|p| p.0 + 2 > right && image.pixel(p.0, p.1)[3] > 64)
        .map(|p| p.1)
        .collect();
    rows.iter().max().unwrap() - rows.iter().min().unwrap() + 1
}

#[test]
fn a_column_clips_its_text_at_its_edges() {
    let Some(gpu) = gpu() else { return };
    let line = || Text::new("MMMMMMMMMMMMMMMMMMMM", [10.0, 10.0], 16.0, WHITE);
    let free = texts(vec![line()]);
    for scale in scales() {
        let f = scale.factor();
        let whole = render(&gpu, &free, (120, 40), scale);
        let clipped = render(&gpu, &long_line(Overflow::Clip), (120, 40), scale);
        let (left, right) = ((10.0 * f).round() as u32, (70.0 * f).round() as u32);
        assert!(
            ink(&whole).any(|p| p.0 >= right),
            "@{f}: the line must overflow"
        );
        for y in 0..whole.height {
            for x in 0..whole.width {
                let expected = if (left..right).contains(&x) {
                    whole.pixel(x, y)
                } else {
                    [0; 4]
                };
                assert_eq!(clipped.pixel(x, y), expected, "@{f}: pixel ({x}, {y})");
            }
        }
    }
}

#[test]
fn an_ellipsis_ends_a_line_wider_than_its_column() {
    let Some(gpu) = gpu() else { return };
    for scale in scales() {
        let f = scale.factor();
        let image = render(&gpu, &long_line(Overflow::Ellipsis), (120, 40), scale);
        let edge = (70.0 * f).round() as u32;
        let right = ink(&image).map(|p| p.0).max().expect("the line is drawn");
        assert!(
            right < edge,
            "@{f}: ink at x={right}, column ends at {edge}"
        );
        // The last mark is a dot of the ellipsis, not part of an M.
        let height = last_mark_height(&image) as f32;
        assert!(
            height <= 3.0 * f,
            "@{f}: last mark is {height} px tall, not a dot"
        );
    }
}

#[test]
fn a_line_that_fits_its_column_is_unchanged() {
    let Some(gpu) = gpu() else { return };
    let free = texts(vec![Text::new("Ave", [10.0, 10.0], 16.0, WHITE)]);
    let column = texts(vec![
        Text::new("Ave", [10.0, 10.0], 16.0, WHITE).in_column(60.0, Overflow::Ellipsis),
    ]);
    for scale in scales() {
        let a = render(&gpu, &free, (120, 40), scale);
        let b = render(&gpu, &column, (120, 40), scale);
        assert_eq!(a, b, "@{}", scale.factor());
    }
}

#[test]
fn text_is_drawn_over_images() {
    let Some(gpu) = gpu() else { return };
    let mut renderer = Renderer::new(&gpu);
    renderer.set_fonts(&gpu, pinned_fonts());
    let red = Image {
        width: 60,
        height: 30,
        pixels: [255, 0, 0, 255].repeat(60 * 30),
    };
    let id = renderer.atlas_mut().insert(&gpu, &red).unwrap();
    let scene = Scene {
        images: vec![ImageQuad {
            image: id,
            origin: [0.0, 0.0],
            opacity: 1.0,
        }],
        ..texts(vec![Text::new("MMM", [4.0, 2.0], 20.0, WHITE)])
    };
    let image = render_offscreen(
        &gpu,
        &mut renderer,
        &scene,
        (60, 30),
        Scale::ONE,
        OFFSCREEN_FORMAT,
    )
    .unwrap();
    let white = image.pixels.chunks(4).filter(|px| *px == [255; 4]).count();
    assert!(
        white > 30,
        "only {white} white text pixels over the red image"
    );
}

// --------------------------------------------------------------- goldens

const CORPUS: &str = include_str!("fonts/corpus.txt");
const GOLDEN_SIZE: (u32, u32) = (330, 184);

/// The corpus on a panel, plus a clipped and an ellipsised column.
fn corpus_scene() -> Scene {
    let ink = Rgba::hex(0xe8d8b0);
    let mut texts: Vec<Text> = CORPUS
        .lines()
        .enumerate()
        .map(|(i, line)| Text::new(line, [8.0, 6.0 + 24.0 * i as f32], 16.0, ink))
        .collect();
    let latin = CORPUS.lines().next().unwrap();
    for (i, overflow) in [Overflow::Ellipsis, Overflow::Clip].into_iter().enumerate() {
        let y = 6.0 + 24.0 * (5 + i) as f32;
        texts.push(Text::new(latin, [8.0, y], 16.0, ink).in_column(150.0, overflow));
    }
    let column = |y: f32| RoundedRect {
        rect: [8.0, y, 150.0, 21.0],
        radius: 3.0,
        fill: Paint::Solid(Rgba::hex(0x3a2a20)),
        border: None,
        glow: None,
    };
    Scene {
        clear: Some(Rgba::hex(0x140c0a)),
        shapes: vec![column(126.0), column(150.0)],
        images: Vec::new(),
        texts,
        post: None,
    }
}

#[test]
fn the_corpus_matches_its_goldens_at_each_scale() {
    let Some(gpu) = gpu() else { return };
    for scale in scales() {
        let image = render(&gpu, &corpus_scene(), GOLDEN_SIZE, scale);
        assert_eq!((image.width, image.height), scale.physical(GOLDEN_SIZE));
        golden::check("text", &format!("corpus@{:.1}", scale.factor()), &image);
    }
}

#[test]
fn fonts_resolved_on_their_own_thread_draw_the_same_corpus() {
    let Some(gpu) = gpu() else { return };
    for scale in scales() {
        let fonts = PendingFonts::resolve(|| Ok(pinned()), "sans-serif")
            .wait()
            .expect("the font thread resolves the pinned fonts");
        let image = render_with(
            &gpu,
            fonts,
            &corpus_scene(),
            GOLDEN_SIZE,
            scale,
            OFFSCREEN_FORMAT,
        );
        golden::check("text", &format!("corpus@{:.1}", scale.factor()), &image);
    }
}

#[test]
fn text_output_stays_premultiplied() {
    let Some(gpu) = gpu() else { return };
    let mut scene = corpus_scene();
    scene.clear = None;
    scene.shapes.clear();
    for scale in scales() {
        let image = render(&gpu, &scene, GOLDEN_SIZE, scale);
        for px in image.pixels.chunks(4) {
            assert!(
                px[0] <= px[3] && px[1] <= px[3] && px[2] <= px[3],
                "@{}: {px:?} is not premultiplied",
                scale.factor()
            );
        }
    }
}

// ---------------------------------------------------------------- spans and alignment

/// The colour of each solid pixel, left half and right half of the ink.
fn solid_halves(image: &Image) -> (Vec<[u8; 3]>, Vec<[u8; 3]>) {
    let ink: Vec<_> = ink(image).collect();
    let left = ink.iter().map(|p| p.0).min().unwrap();
    let right = ink.iter().map(|p| p.0).max().unwrap();
    let middle = (left + right) / 2;
    let mut halves = (Vec::new(), Vec::new());
    for (x, y) in ink {
        let i = ((y * image.width + x) * 4) as usize;
        let px = &image.pixels[i..i + 4];
        if px[3] == 255 {
            let rgb = [px[0], px[1], px[2]];
            if x < middle {
                &mut halves.0
            } else {
                &mut halves.1
            }
            .push(rgb);
        }
    }
    halves
}

fn near(rgb: [u8; 3], want: [u8; 3]) -> bool {
    rgb.iter().zip(want).all(|(a, b)| a.abs_diff(b) <= 2)
}

#[test]
fn spans_paint_their_bytes_in_their_own_colour() {
    let Some(gpu) = gpu() else { return };
    let mut line = Text::new("IIIIIIII", [4.0, 4.0], 48.0, WHITE);
    line.spans = vec![(4..8, BLUE)];
    let image = render(&gpu, &texts(vec![line]), (240, 80), Scale::ONE);
    let (left, right) = solid_halves(&image);
    assert!(
        left.len() > 50 && right.len() > 50,
        "{} / {}",
        left.len(),
        right.len()
    );
    let off = |half: &[[u8; 3]], want| half.iter().filter(|&&p| !near(p, want)).count();
    assert_eq!(off(&left, [0xff; 3]), 0, "left half not white");
    assert_eq!(off(&right, [0x40, 0xa0, 0xff]), 0, "right half not #40a0ff");
}

#[test]
fn an_end_aligned_line_ends_at_its_column_edge() {
    let Some(gpu) = gpu() else { return };
    for scale in scales() {
        let f = scale.factor();
        let mut line =
            Text::new("12/345", [10.0, 8.0], 16.0, WHITE).in_column(200.0, Overflow::Clip);
        line.align_end = true;
        let image = render(&gpu, &texts(vec![line]), (240, 40), scale);
        let right = ink(&image).map(|p| p.0).max().expect("the line is drawn") as f32;
        let left = ink(&image).map(|p| p.0).min().unwrap() as f32;
        assert!(
            right > 200.0 * f && right <= 210.0 * f,
            "@{f}: right {right}"
        );
        assert!(left > 120.0 * f, "@{f}: left {left}");
    }
}
