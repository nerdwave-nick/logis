//! Icon files to pixels: SVG through resvg, PNG through tiny-skia's decoder,
//! both into a square, premultiplied RGBA8 image of the requested pixel
//! size, the content fitted with its aspect ratio kept and centred.

use std::fmt;
use std::path::{Path, PathBuf};

use resvg::tiny_skia::{FilterQuality, Pixmap, PixmapPaint, Transform};
use resvg::usvg;

use crate::render::Image;

/// The largest icon side logis rasterises, in pixels.
pub const MAX_ICON_PIXELS: u32 = 1024;

/// Why an icon file could not be rasterised.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RasterError {
    /// The icon file.
    pub path: PathBuf,
    /// What went wrong, in plain words.
    pub reason: String,
}

impl fmt::Display for RasterError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "the icon {} resists the rite of rasterisation: {}",
            self.path.display(),
            self.reason
        )
    }
}

impl std::error::Error for RasterError {}

/// Rasterises the PNG or SVG at `path` into a `pixels` × `pixels` image.
pub fn rasterise(path: &Path, pixels: u32) -> Result<Image, RasterError> {
    let fail = |reason: String| RasterError {
        path: path.to_owned(),
        reason,
    };
    if !(1..=MAX_ICON_PIXELS).contains(&pixels) {
        return Err(fail(format!(
            "{pixels} pixels is outside 1..={MAX_ICON_PIXELS}"
        )));
    }
    let extension = path
        .extension()
        .and_then(|e| e.to_str())
        .map(str::to_ascii_lowercase);
    let decode = match extension.as_deref() {
        Some("svg") => svg,
        Some("png") => png,
        _ => return Err(fail("only PNG and SVG icons are supported".into())),
    };
    let data = std::fs::read(path).map_err(|e| fail(e.to_string()))?;
    let pixmap = decode(&data, pixels).map_err(fail)?;
    Ok(Image {
        width: pixmap.width(),
        height: pixmap.height(),
        pixels: pixmap.take(),
    })
}

fn svg(data: &[u8], pixels: u32) -> Result<Pixmap, String> {
    let tree = usvg::Tree::from_data(data, &usvg::Options::default()).map_err(|e| e.to_string())?;
    let size = tree.size();
    let mut out = canvas(pixels)?;
    resvg::render(
        &tree,
        fit(size.width(), size.height(), pixels),
        &mut out.as_mut(),
    );
    Ok(out)
}

fn png(data: &[u8], pixels: u32) -> Result<Pixmap, String> {
    let mut source = Pixmap::decode_png(data).map_err(|e| e.to_string())?;
    if source.width() == pixels && source.height() == pixels {
        return Ok(source);
    }
    // Halve first while the source is at least twice too big: at scale 0.5
    // bilinear sampling averages each 2×2 block, so large downscales do not
    // skip source pixels. The last step is bicubic.
    while source.width().min(source.height()) >= pixels.saturating_mul(2) {
        let half = |side: u32| side.div_ceil(2);
        let mut smaller = Pixmap::new(half(source.width()), half(source.height()))
            .ok_or("the image vanished while halving")?;
        draw(
            &mut smaller,
            &source,
            Transform::from_scale(0.5, 0.5),
            FilterQuality::Bilinear,
        );
        source = smaller;
    }
    let mut out = canvas(pixels)?;
    let transform = fit(source.width() as f32, source.height() as f32, pixels);
    draw(&mut out, &source, transform, FilterQuality::Bicubic);
    Ok(out)
}

fn canvas(pixels: u32) -> Result<Pixmap, String> {
    Pixmap::new(pixels, pixels).ok_or_else(|| format!("no canvas of {pixels} pixels"))
}

fn draw(target: &mut Pixmap, source: &Pixmap, transform: Transform, quality: FilterQuality) {
    let paint = PixmapPaint {
        quality,
        ..PixmapPaint::default()
    };
    target.draw_pixmap(0, 0, source.as_ref(), &paint, transform, None);
}

/// Scales `width` × `height` to fit a `pixels` square and centres it.
fn fit(width: f32, height: f32, pixels: u32) -> Transform {
    let side = pixels as f32;
    let scale = (side / width).min(side / height);
    let (tx, ty) = ((side - width * scale) / 2.0, (side - height * scale) / 2.0);
    Transform::from_row(scale, 0.0, 0.0, scale, tx, ty)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture(path: &str) -> PathBuf {
        Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("tests/fixtures/icons")
            .join(path)
    }

    const FORGE: &str = "share/icons/logis-forge";

    fn premultiplied(image: &Image) -> bool {
        image
            .pixels
            .chunks(4)
            .all(|p| p[0] <= p[3] && p[1] <= p[3] && p[2] <= p[3])
    }

    #[test]
    fn a_png_at_its_own_size_keeps_its_pixels() {
        let image = rasterise(&fixture(&format!("{FORGE}/32x32/apps/forge-cog.png")), 32).unwrap();
        assert_eq!((image.width, image.height), (32, 32));
        assert_eq!(image.pixel(0, 0), [0, 0, 0, 0]);
        assert_eq!(image.pixel(10, 16), [220, 120, 20, 255]);
        assert!(premultiplied(&image));
    }

    #[test]
    fn a_png_is_resampled_to_the_requested_size() {
        let path = fixture(&format!("{FORGE}/32x32@2/apps/forge-cog.png"));
        for pixels in [42, 20, 100] {
            let image = rasterise(&path, pixels).unwrap();
            assert_eq!((image.width, image.height), (pixels, pixels));
            assert_eq!(image.pixel(0, 0), [0, 0, 0, 0]);
            let [r, g, b, a] = image.pixel(pixels / 2, pixels * 3 / 4);
            assert!(
                a == 255 && r > 200 && g > 170 && b < 80,
                "{:?}",
                [r, g, b, a]
            );
            assert!(premultiplied(&image));
        }
    }

    #[test]
    fn an_indexed_png_decodes() {
        let image = rasterise(&fixture("pixmaps/pixmap-only.png"), 48).unwrap();
        assert_eq!(image.pixel(0, 0)[3], 0);
        assert_eq!(image.pixel(24, 36)[3], 255);
    }

    #[test]
    fn an_svg_renders_at_the_requested_size() {
        let path = fixture(&format!("{FORGE}/scalable/apps/forge-cog.svg"));
        for pixels in [32, 42] {
            let image = rasterise(&path, pixels).unwrap();
            assert_eq!((image.width, image.height), (pixels, pixels));
            // The dark hub at the centre, nothing in the corners.
            let [r, g, b, a] = image.pixel(pixels / 2, pixels / 2);
            assert!(a == 255 && r == 0x20 && g == 0x08 && b == 0x08);
            assert_eq!(image.pixel(0, 0), [0, 0, 0, 0]);
            assert!(premultiplied(&image));
        }
    }

    #[test]
    fn a_wide_svg_is_fitted_and_centred() {
        let path = fixture(&format!("{FORGE}/scalable/apps/forge-banner.svg"));
        let image = rasterise(&path, 32).unwrap();
        // 64×32 fits as 32×16 at y 8..24.
        for x in 0..32 {
            assert_eq!(image.pixel(x, 6)[3], 0, "above the banner at x {x}");
            assert_eq!(image.pixel(x, 25)[3], 0, "below the banner at x {x}");
        }
        assert_eq!(image.pixel(28, 16), [0xa0, 0x18, 0x18, 255]);
    }

    #[test]
    fn broken_unsupported_missing_and_empty_requests_fail() {
        let reason = |path: &str, pixels| rasterise(&fixture(path), pixels).unwrap_err().reason;
        assert!(!reason("pixmaps/broken.png", 32).is_empty());
        assert!(!reason("pixmaps/broken.svg", 32).is_empty());
        assert_eq!(
            reason("pixmaps/xpm-only.xpm", 32),
            "only PNG and SVG icons are supported"
        );
        assert!(!reason("pixmaps/missing.png", 32).is_empty());
        assert!(reason("pixmaps/pixmap-only.png", 0).contains("outside"));
        assert!(reason("pixmaps/pixmap-only.png", 4096).contains("outside"));
        let error = rasterise(&fixture("pixmaps/broken.png"), 32).unwrap_err();
        assert!(error.to_string().contains("broken.png resists the rite"));
    }
}
