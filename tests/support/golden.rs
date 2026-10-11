//! The golden-image harness (spec §5 "offscreen golden renders"), shared by
//! every test crate: render a scene through `render::render_offscreen` (or
//! take any [`Image`], such as an E2E screen capture) and compare it with a
//! checked-in PNG under a [`Tolerance`], or write it in update mode.
//!
//! - **Files:** `tests/goldens/<set>/<name>.png`, 8-bit RGBA, the image bytes
//!   as they are (the renderer's are premultiplied; give golden scenes an
//!   opaque clear colour so the files also look right in a viewer). Scaled
//!   variants are named `<name>@<scale>`, e.g. `reference@1.3`.
//! - **Tolerance:** a pixel is an outlier when any channel differs by more
//!   than `channel`; a check passes when outliers are at most `outliers`
//!   (a fraction of all pixels). [`Tolerance::OFFSCREEN`] (2, none) absorbs
//!   blending and edge-coverage rounding between GPUs (Intel Arc against
//!   lavapipe); E2E captures pass a looser one for the compositor's own
//!   composition.
//! - **Update mode:** `LOGIS_UPDATE_GOLDENS=1 cargo test` writes the current
//!   images as the goldens instead of comparing; review them before
//!   committing.
//! - **Failures** name the golden and leave `<name>.actual.png` and
//!   `<name>.diff.png` (outliers magenta over the dimmed golden) under
//!   `$CARGO_TARGET_TMPDIR/golden-failures/<set>/`.
//!
//! Adoption: `mod support;` then `support::golden::check(set, name, &image)`
//! (the call the text and icon slices' local helpers already make), or
//! [`check_scene`] to render and check a scene at several scales.
#![allow(dead_code)] // shared by test crates that each use only part of it

use std::ffi::OsStr;
use std::io::{BufReader, BufWriter};
use std::path::{Path, PathBuf};

use logis::render::{
    BackendChoice, Gpu, Image, OFFSCREEN_FORMAT, Renderer, Scale, Scene, render_offscreen,
};

/// The variable that switches to update mode.
pub const UPDATE: &str = "LOGIS_UPDATE_GOLDENS";

/// How far an image may stray from its golden.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Tolerance {
    /// The largest per-channel difference a matching pixel may have.
    pub channel: u8,
    /// The fraction of pixels (0.0 to 1.0) allowed beyond `channel`.
    pub outliers: f64,
}

impl Tolerance {
    /// For offscreen renders: rounding differences between GPUs only.
    pub const OFFSCREEN: Self = Self {
        channel: 2,
        outliers: 0.0,
    };
}

/// How an image compares with its golden.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Comparison {
    /// Pixels with a channel beyond the tolerance.
    pub outliers: usize,
    /// The largest channel difference of any pixel.
    pub worst: u8,
    /// All pixels.
    pub total: usize,
    /// The channel tolerance the outliers were counted against.
    pub channel: u8,
}

impl Comparison {
    /// Whether the image is within `tolerance` of its golden.
    /// Whether the image is within `tolerance` of its golden; the
    /// comparison must have counted outliers against its channel.
    pub fn passes(&self, tolerance: Tolerance) -> bool {
        assert_eq!(
            self.channel, tolerance.channel,
            "compare with compare_with(.., {}) to judge this tolerance",
            tolerance.channel
        );
        self.outliers as f64 <= tolerance.outliers * self.total as f64
    }
}

/// What a check did.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Verdict {
    /// Compared and within tolerance.
    Matched(Comparison),
    /// Update mode: the golden was (re)written.
    Updated,
}

/// The largest channel difference of each pixel pair.
fn pixel_differences<'a>(actual: &'a Image, golden: &'a Image) -> impl Iterator<Item = u8> + 'a {
    actual
        .pixels
        .chunks(4)
        .zip(golden.pixels.chunks(4))
        .map(|(a, g)| {
            a.iter()
                .zip(g)
                .map(|(a, g)| a.abs_diff(*g))
                .max()
                .unwrap_or(0)
        })
}

/// Compares two images of the same size; outliers are counted against the
/// offscreen channel tolerance, use [`compare_with`] for another.
pub fn compare(actual: &Image, golden: &Image) -> Result<Comparison, String> {
    compare_with(actual, golden, Tolerance::OFFSCREEN.channel)
}

/// Compares two images of the same size, counting pixels beyond `channel`.
pub fn compare_with(actual: &Image, golden: &Image, channel: u8) -> Result<Comparison, String> {
    if (actual.width, actual.height) != (golden.width, golden.height) {
        return Err(format!(
            "the image is {}x{} but its golden is {}x{}",
            actual.width, actual.height, golden.width, golden.height
        ));
    }
    let (mut outliers, mut worst) = (0, 0);
    for d in pixel_differences(actual, golden) {
        worst = worst.max(d);
        outliers += usize::from(d > channel);
    }
    Ok(Comparison {
        outliers,
        worst,
        total: (actual.width * actual.height) as usize,
        channel,
    })
}

/// The golden dimmed to grey, with every outlier beyond `channel` magenta.
pub fn diff_image(actual: &Image, golden: &Image, channel: u8) -> Image {
    let mut pixels = Vec::with_capacity(golden.pixels.len());
    for (d, g) in pixel_differences(actual, golden).zip(golden.pixels.chunks(4)) {
        if d > channel {
            pixels.extend_from_slice(&[255, 0, 255, 255]);
        } else {
            let grey = ((u16::from(g[0]) + u16::from(g[1]) + u16::from(g[2])) / 12) as u8;
            pixels.extend_from_slice(&[grey, grey, grey, 255]);
        }
    }
    Image {
        width: golden.width,
        height: golden.height,
        pixels,
    }
}

/// Whether a value of [`UPDATE`] asks for update mode: set, non-empty, not 0.
pub fn update_requested(value: Option<&OsStr>) -> bool {
    value.is_some_and(|v| !v.is_empty() && v != "0")
}

/// Writes `image` as an 8-bit RGBA PNG.
pub fn write_png(path: &Path, image: &Image) -> std::io::Result<()> {
    let file = BufWriter::new(std::fs::File::create(path)?);
    let mut encoder = png::Encoder::new(file, image.width, image.height);
    encoder.set_color(png::ColorType::Rgba);
    encoder.set_depth(png::BitDepth::Eight);
    encoder.set_compression(png::Compression::High);
    let mut writer = encoder.write_header().map_err(std::io::Error::other)?;
    writer
        .write_image_data(&image.pixels)
        .map_err(std::io::Error::other)?;
    writer.finish().map_err(std::io::Error::other)
}

/// Reads an 8-bit RGBA PNG.
pub fn read_png(path: &Path) -> std::io::Result<Image> {
    let file = BufReader::new(std::fs::File::open(path)?);
    let mut reader = png::Decoder::new(file)
        .read_info()
        .map_err(std::io::Error::other)?;
    let size = reader.output_buffer_size().ok_or_else(|| {
        std::io::Error::other(format!("{} is too large to decode", path.display()))
    })?;
    let mut pixels = vec![0; size];
    let info = reader
        .next_frame(&mut pixels)
        .map_err(std::io::Error::other)?;
    if (info.color_type, info.bit_depth) != (png::ColorType::Rgba, png::BitDepth::Eight) {
        return Err(std::io::Error::other(format!(
            "{} is {:?} {:?}, not 8-bit RGBA",
            path.display(),
            info.color_type,
            info.bit_depth
        )));
    }
    pixels.truncate(info.buffer_size());
    Ok(Image {
        width: info.width,
        height: info.height,
        pixels,
    })
}

/// A directory of goldens and where failure artefacts go.
pub struct Goldens {
    root: PathBuf,
    failures: PathBuf,
    update: bool,
}

impl Goldens {
    /// The checked-in goldens under `tests/goldens/`, in update mode when
    /// [`UPDATE`] asks for it.
    pub fn checked_in() -> Self {
        let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/goldens");
        let failures = Path::new(env!("CARGO_TARGET_TMPDIR")).join("golden-failures");
        Self::at(root, failures).with_update(update_requested(std::env::var_os(UPDATE).as_deref()))
    }

    /// Goldens under `root`, failure artefacts under `failures`.
    pub fn at(root: PathBuf, failures: PathBuf) -> Self {
        Self {
            root,
            failures,
            update: false,
        }
    }

    /// Writes instead of comparing when `update` is true.
    pub fn with_update(mut self, update: bool) -> Self {
        self.update = update;
        self
    }

    /// Whether checks write goldens instead of comparing.
    pub fn updating(&self) -> bool {
        self.update
    }

    /// The file of golden `set/name`.
    pub fn path(&self, set: &str, name: &str) -> PathBuf {
        self.root.join(set).join(format!("{name}.png"))
    }

    /// The golden `set/name`, if it exists.
    pub fn load(&self, set: &str, name: &str) -> Result<Image, String> {
        let path = self.path(set, name);
        read_png(&path).map_err(|e| {
            format!(
                "golden {} cannot be read ({e}); render it with {UPDATE}=1 and review it",
                path.display()
            )
        })
    }

    /// Compares `image` with golden `set/name` (or writes it in update
    /// mode). A mismatch leaves the image and a difference image under the
    /// failures directory and names them in the error.
    pub fn check(
        &self,
        set: &str,
        name: &str,
        image: &Image,
        tolerance: Tolerance,
    ) -> Result<Verdict, String> {
        let path = self.path(set, name);
        if self.update {
            std::fs::create_dir_all(path.parent().expect("goldens live in a set"))
                .and_then(|()| write_png(&path, image))
                .map_err(|e| format!("cannot write golden {}: {e}", path.display()))?;
            eprintln!("updated golden {}", path.display());
            return Ok(Verdict::Updated);
        }
        let golden = self.load(set, name)?;
        let comparison = compare_with(image, &golden, tolerance.channel)
            .map_err(|e| format!("{} ({})", e, path.display()))?;
        if comparison.passes(tolerance) {
            return Ok(Verdict::Matched(comparison));
        }
        let out = self.failures.join(set);
        let actual = out.join(format!("{name}.actual.png"));
        let marked = out.join(format!("{name}.diff.png"));
        std::fs::create_dir_all(&out)
            .and_then(|()| write_png(&actual, image))
            .and_then(|()| write_png(&marked, &diff_image(image, &golden, tolerance.channel)))
            .map_err(|e| format!("cannot write failure artefacts in {}: {e}", out.display()))?;
        Err(format!(
            "{} of {} pixels differ from golden {} by more than {} (worst {}, allowed {:.3}%); \
             see {} and {}",
            comparison.outliers,
            comparison.total,
            path.display(),
            tolerance.channel,
            comparison.worst,
            tolerance.outliers * 100.0,
            actual.display(),
            marked.display()
        ))
    }

    /// [`Self::check`], panicking on a mismatch.
    #[track_caller]
    pub fn assert(&self, set: &str, name: &str, image: &Image, tolerance: Tolerance) -> Verdict {
        self.check(set, name, image, tolerance)
            .unwrap_or_else(|e| panic!("{e}"))
    }
}

/// Checks `image` against the checked-in golden `set/name` with the
/// offscreen tolerance; panics on a mismatch.
#[track_caller]
pub fn check(set: &str, name: &str, image: &Image) {
    Goldens::checked_in().assert(set, name, image, Tolerance::OFFSCREEN);
}

/// Whether `choice` is listed in `LOGIS_TEST_REQUIRE_BACKENDS`.
pub fn required(choice: BackendChoice) -> bool {
    std::env::var("LOGIS_TEST_REQUIRE_BACKENDS")
        .unwrap_or_default()
        .split(',')
        .any(|name| name.trim() == choice.name())
}

/// A headless GPU for `choice`, or `None` (after printing `SKIPPED`) when
/// there is none and the backend is not required.
pub fn gpu(choice: BackendChoice) -> Option<Gpu> {
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

/// A headless GPU for the backend `LOGIS_GPU_BACKEND` names (auto when
/// unset), as [`gpu`] does.
pub fn env_gpu() -> Option<Gpu> {
    gpu(BackendChoice::from_env().unwrap_or_else(|e| panic!("{e}")))
}

/// Renders `scene` at `logical` size and `scale` through
/// `render::render_offscreen` in the default offscreen format.
pub fn render(gpu: &Gpu, scene: &Scene, logical: (u32, u32), scale: Scale) -> Image {
    let mut renderer = Renderer::new(gpu);
    render_offscreen(gpu, &mut renderer, scene, logical, scale, OFFSCREEN_FORMAT)
        .unwrap_or_else(|e| panic!("offscreen render failed: {e}"))
}

/// The file-name suffix of a scale: `1.0`, `1.3`, `2.0`, `1.25` …
pub fn scale_suffix(scale: Scale) -> String {
    let n = scale.as_120ths();
    let (whole, rest) = (n / 120, n % 120);
    let mut fraction = format!("{:.3}", f64::from(rest) / 120.0);
    while fraction.ends_with('0') && fraction.len() > 3 {
        fraction.pop();
    }
    format!("{whole}{}", &fraction[1..])
}

/// Renders `scene` at each of `scales` and checks it against the
/// checked-in goldens `set/name@<scale>`; reports every failing scale.
#[track_caller]
pub fn check_scene(
    gpu: &Gpu,
    set: &str,
    name: &str,
    scene: &Scene,
    logical: (u32, u32),
    scales: &[Scale],
) {
    let goldens = Goldens::checked_in();
    let failures: Vec<String> = scales
        .iter()
        .filter_map(|&scale| {
            let image = render(gpu, scene, logical, scale);
            let name = format!("{name}@{}", scale_suffix(scale));
            goldens
                .check(set, &name, &image, Tolerance::OFFSCREEN)
                .err()
        })
        .collect();
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}
