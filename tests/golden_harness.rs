//! The golden-image harness itself (tests/support/golden.rs): comparison
//! with a tolerance, PNG round trips, update mode, failure artefacts, and
//! one real use through `render::render_offscreen`: a reference scene at
//! scales 1.0, 1.3 and 2.0 against the checked-in goldens.

mod support;

use logis::render::{Border, Glow, Image, Paint, Rgba, RoundedRect, Scale, Scene};
use support::ScratchDir;
use support::golden::{self, Alpha, Goldens, Tolerance, Verdict};

fn solid(width: u32, height: u32, rgba: [u8; 4]) -> Image {
    Image {
        width,
        height,
        pixels: rgba.repeat((width * height) as usize),
    }
}

fn with_pixel(mut image: Image, x: u32, y: u32, rgba: [u8; 4]) -> Image {
    let i = ((y * image.width + x) * 4) as usize;
    image.pixels[i..i + 4].copy_from_slice(&rgba);
    image
}

// ---------------------------------------------------------------- compare

#[test]
fn identical_images_have_no_outliers() {
    let image = solid(4, 3, [10, 20, 30, 255]);
    let c = golden::compare(&image, &image).unwrap();
    assert_eq!((c.outliers, c.worst, c.total), (0, 0, 12));
    assert!(c.passes(Tolerance::OFFSCREEN));
}

#[test]
fn a_difference_within_the_channel_tolerance_is_no_outlier() {
    let golden = solid(4, 3, [10, 20, 30, 255]);
    let actual = with_pixel(golden.clone(), 1, 1, [12, 18, 30, 255]);
    let c = golden::compare(&actual, &golden).unwrap();
    assert_eq!((c.outliers, c.worst), (0, 2));
    assert!(c.passes(Tolerance::OFFSCREEN));
}

#[test]
fn a_difference_beyond_the_channel_tolerance_is_an_outlier() {
    let golden = solid(4, 3, [10, 20, 30, 255]);
    let actual = with_pixel(golden.clone(), 3, 2, [10, 20, 30, 250]);
    let c = golden::compare(&actual, &golden).unwrap();
    assert_eq!((c.outliers, c.worst), (1, 5));
    assert!(!c.passes(Tolerance::OFFSCREEN));
}

#[test]
fn an_outlier_fraction_allows_that_many_outliers_and_no_more() {
    let golden = solid(10, 10, [0, 0, 0, 255]);
    let one = with_pixel(golden.clone(), 0, 0, [90, 0, 0, 255]);
    let two = with_pixel(one.clone(), 1, 0, [90, 0, 0, 255]);
    let loose = Tolerance {
        channel: 2,
        outliers: 0.01,
    };
    assert!(golden::compare(&one, &golden).unwrap().passes(loose));
    assert!(!golden::compare(&two, &golden).unwrap().passes(loose));
}

#[test]
fn images_of_another_size_do_not_compare() {
    let e = golden::compare(&solid(4, 3, [0; 4]), &solid(3, 4, [0; 4])).unwrap_err();
    assert!(e.contains("4x3") && e.contains("3x4"), "{e}");
}

#[test]
fn the_difference_image_marks_outliers_in_magenta() {
    let golden = solid(2, 1, [200, 100, 40, 255]);
    let actual = with_pixel(golden.clone(), 1, 0, [0, 0, 0, 255]);
    let diff = golden::diff_image(&actual, &golden, 2);
    assert_eq!(diff.pixel(1, 0), [255, 0, 255, 255]);
    let [r, g, b, a] = diff.pixel(0, 0);
    assert!(r == g && g == b && a == 255, "matching pixels are grey");
}

// ---------------------------------------------------------------- files

#[test]
fn png_round_trips_rgba_exactly() {
    let dir = ScratchDir::new();
    let path = dir.path().join("x.png");
    let image = with_pixel(solid(3, 2, [1, 2, 3, 4]), 2, 1, [250, 128, 0, 255]);
    golden::write_png(&path, &image).unwrap();
    assert_eq!(golden::read_png(&path).unwrap(), image);
}

fn goldens(dir: &ScratchDir, update: bool) -> Goldens {
    Goldens::at(dir.path().join("goldens"), dir.path().join("failures")).with_update(update)
}

#[test]
fn update_mode_writes_the_golden_and_compare_mode_then_matches_it() {
    let dir = ScratchDir::new();
    let image = solid(5, 5, [9, 8, 7, 255]);
    let verdict = goldens(&dir, true)
        .check("set", "scene@1.0", &image, Tolerance::OFFSCREEN)
        .unwrap();
    assert_eq!(verdict, Verdict::Updated);
    assert!(dir.path().join("goldens/set/scene@1.0.png").is_file());
    let verdict = goldens(&dir, false)
        .check("set", "scene@1.0", &image, Tolerance::OFFSCREEN)
        .unwrap();
    assert!(matches!(verdict, Verdict::Matched(c) if c.outliers == 0));
}

#[test]
fn a_missing_golden_fails_and_says_how_to_make_it() {
    let dir = ScratchDir::new();
    let e = goldens(&dir, false)
        .check("set", "absent", &solid(1, 1, [0; 4]), Tolerance::OFFSCREEN)
        .unwrap_err();
    assert!(
        e.contains("absent.png") && e.contains(golden::UPDATE),
        "{e}"
    );
}

#[test]
fn a_mismatch_fails_and_leaves_the_render_and_a_difference_image() {
    let dir = ScratchDir::new();
    goldens(&dir, true)
        .check(
            "set",
            "s",
            &solid(4, 4, [0, 0, 0, 255]),
            Tolerance::OFFSCREEN,
        )
        .unwrap();
    let wrong = with_pixel(solid(4, 4, [0, 0, 0, 255]), 2, 2, [255, 255, 255, 255]);
    let e = goldens(&dir, false)
        .check("set", "s", &wrong, Tolerance::OFFSCREEN)
        .unwrap_err();
    let actual = dir.path().join("failures/set/s.actual.png");
    let diff = dir.path().join("failures/set/s.diff.png");
    assert!(e.contains("1 of 16 pixels"), "{e}");
    assert!(e.contains(&actual.display().to_string()), "{e}");
    assert_eq!(golden::read_png(&actual).unwrap(), wrong);
    assert_eq!(
        golden::read_png(&diff).unwrap().pixel(2, 2),
        [255, 0, 255, 255]
    );
}

#[test]
fn straight_alpha_goldens_are_premultiplied_when_read_and_demultiplied_when_written() {
    let dir = ScratchDir::new();
    // A straight-alpha file, as tiny-skia's encoder writes them.
    std::fs::create_dir_all(dir.path().join("goldens/set")).unwrap();
    let straight = solid(1, 1, [255, 64, 0, 128]);
    golden::write_png(&dir.path().join("goldens/set/s.png"), &straight).unwrap();
    let premultiplied = solid(1, 1, [128, 32, 0, 128]);
    let goldens = goldens(&dir, false).with_alpha(Alpha::Straight);
    let verdict = goldens
        .check("set", "s", &premultiplied, Tolerance::OFFSCREEN)
        .unwrap();
    assert!(
        matches!(verdict, Verdict::Matched(c) if c.worst <= 1),
        "{verdict:?}"
    );
    goldens
        .with_update(true)
        .check("set", "t", &premultiplied, Tolerance::OFFSCREEN)
        .unwrap();
    let written = golden::read_png(&dir.path().join("goldens/set/t.png")).unwrap();
    assert!(golden::compare(&written, &straight).unwrap().worst <= 1);
}

#[test]
fn update_mode_is_read_from_the_environment_value() {
    assert!(golden::update_requested(Some("1".as_ref())));
    assert!(golden::update_requested(Some("yes".as_ref())));
    assert!(!golden::update_requested(Some("0".as_ref())));
    assert!(!golden::update_requested(Some("".as_ref())));
    assert!(!golden::update_requested(None));
}

// ------------------------------------------------- through render_offscreen

/// A scene that touches every primitive the renderer has: a gradient body
/// with a border and glow, a solid inset and a translucent bar.
fn reference_scene() -> Scene {
    let brass = Rgba::new(0.83, 0.63, 0.09, 1.0);
    let crimson = Rgba::new(0.70, 0.06, 0.05, 1.0);
    let body = RoundedRect {
        rect: [12.0, 12.0, 96.0, 56.0],
        radius: 10.0,
        fill: Paint::Linear {
            start: [0.0, 12.0],
            end: [0.0, 68.0],
            from: Rgba::new(0.10, 0.06, 0.05, 1.0),
            to: Rgba::new(0.04, 0.03, 0.02, 1.0),
        },
        border: Some(Border {
            width: 2.0,
            paint: Paint::Linear {
                start: [12.0, 0.0],
                end: [108.0, 0.0],
                from: brass,
                to: crimson,
            },
        }),
        glow: Some(Glow {
            radius: 8.0,
            color: crimson.with_alpha(0.6),
        }),
    };
    let inset = RoundedRect {
        rect: [24.0, 26.0, 20.0, 20.0],
        radius: 4.0,
        fill: Paint::Solid(brass),
        border: None,
        glow: None,
    };
    let bar = RoundedRect {
        rect: [52.0, 32.0, 44.0, 8.0],
        radius: 4.0,
        fill: Paint::Solid(Rgba::new(0.54, 0.48, 0.40, 0.5)),
        border: None,
        glow: None,
    };
    Scene {
        clear: Some(Rgba::new(0.0, 0.0, 0.0, 1.0)),
        shapes: vec![body, inset, bar],
        images: Vec::new(),
        texts: Vec::new(),
    }
}

#[test]
fn the_reference_scene_matches_its_goldens_at_every_scale() {
    let Some(gpu) = golden::env_gpu() else {
        return;
    };
    golden::check_scene(
        &gpu,
        "harness",
        "reference",
        &reference_scene(),
        (120, 80),
        &[Scale::ONE, Scale::from_120ths(156), Scale::from_120ths(240)],
    );
}

#[test]
fn render_returns_the_scaled_image_through_the_offscreen_path() {
    let Some(gpu) = golden::env_gpu() else {
        return;
    };
    let image: Image = golden::render(&gpu, &reference_scene(), (120, 80), Scale::from_120ths(156));
    assert_eq!((image.width, image.height), (156, 104));
}
