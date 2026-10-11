//! A minimal golden-image check for the text tests, until the general
//! harness of ticket e32f9347 replaces it.
//!
//! Goldens are PNGs under tests/goldens/. Their bytes are the renderer's
//! output as is (premultiplied RGBA); golden scenes have an opaque
//! background, so the files also view correctly.
//!
//! Tolerance: a pixel matches when every channel is within [`TOLERANCE`].
//! Glyphs come from fixed font files, are rasterised on the CPU (swash) and
//! drawn as pixel-aligned quads sampled with nearest filtering, so text
//! itself is exact across GPUs; the remaining differences are rounding in
//! blending and in the SDF shapes' edge coverage. Measured: Intel Arc
//! (Vulkan and GL) against Mesa lavapipe, see the ticket handoff.
//!
//! `LOGIS_UPDATE_GOLDENS=1` writes the current renders as the new goldens
//! instead of comparing. A mismatch writes the render and a difference
//! image under the cargo target tmpdir and names them in the failure.

use std::path::{Path, PathBuf};

use logis::render::Image;

/// The largest per-channel difference a matching pixel may have.
pub const TOLERANCE: u8 = 2;

/// The variable that switches to update mode.
pub const UPDATE: &str = "LOGIS_UPDATE_GOLDENS";

fn golden_path(set: &str, name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/goldens")
        .join(set)
        .join(format!("{name}.png"))
}

/// Compares `image` with the golden `set/name`, or writes it in update mode.
#[track_caller]
pub fn check(set: &str, name: &str, image: &Image) {
    let path = golden_path(set, name);
    if std::env::var_os(UPDATE).is_some_and(|v| !v.is_empty() && v != "0") {
        std::fs::create_dir_all(path.parent().unwrap()).unwrap();
        write_png(&path, image);
        eprintln!("updated golden {}", path.display());
        return;
    }
    let Some(golden) = read_png(&path) else {
        panic!(
            "golden {} is missing; render it with {UPDATE}=1 and review it",
            path.display()
        );
    };
    assert_eq!(
        (image.width, image.height),
        (golden.width, golden.height),
        "golden {} has another size",
        path.display()
    );
    let mut diff = Image {
        width: image.width,
        height: image.height,
        pixels: vec![0; image.pixels.len()],
    };
    let (mut outliers, mut worst) = (0usize, 0u8);
    for (i, (a, g)) in image
        .pixels
        .chunks(4)
        .zip(golden.pixels.chunks(4))
        .enumerate()
    {
        let d = a.iter().zip(g).map(|(a, g)| a.abs_diff(*g)).max().unwrap();
        worst = worst.max(d);
        if d > TOLERANCE {
            outliers += 1;
            diff.pixels[i * 4..i * 4 + 4].copy_from_slice(&[255, 0, 255, 255]);
        } else {
            let grey = g[0] / 4;
            diff.pixels[i * 4..i * 4 + 4].copy_from_slice(&[grey, grey, grey, 255]);
        }
    }
    if outliers > 0 {
        let out = Path::new(env!("CARGO_TARGET_TMPDIR"))
            .join("golden-failures")
            .join(set);
        std::fs::create_dir_all(&out).unwrap();
        let actual = out.join(format!("{name}.actual.png"));
        let marked = out.join(format!("{name}.diff.png"));
        write_png(&actual, image);
        write_png(&marked, &diff);
        panic!(
            "{outliers} pixels differ from golden {} by more than {TOLERANCE} \
             (worst {worst}); see {} and {}",
            path.display(),
            actual.display(),
            marked.display()
        );
    }
}

fn write_png(path: &Path, image: &Image) {
    let file = std::io::BufWriter::new(std::fs::File::create(path).unwrap());
    let mut encoder = png::Encoder::new(file, image.width, image.height);
    encoder.set_color(png::ColorType::Rgba);
    encoder.set_depth(png::BitDepth::Eight);
    encoder.set_compression(png::Compression::High);
    let mut writer = encoder.write_header().unwrap();
    writer.write_image_data(&image.pixels).unwrap();
}

fn read_png(path: &Path) -> Option<Image> {
    let file = std::io::BufReader::new(std::fs::File::open(path).ok()?);
    let mut reader = png::Decoder::new(file).read_info().unwrap();
    let mut pixels = vec![0; reader.output_buffer_size().unwrap()];
    let info = reader.next_frame(&mut pixels).unwrap();
    assert_eq!(
        (info.color_type, info.bit_depth),
        (png::ColorType::Rgba, png::BitDepth::Eight),
        "golden {} is not 8-bit RGBA",
        path.display()
    );
    pixels.truncate(info.buffer_size());
    Some(Image {
        width: info.width,
        height: info.height,
        pixels,
    })
}
