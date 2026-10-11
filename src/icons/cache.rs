//! Icons by name, ready to draw: looked up once per (name, size, scale),
//! rasterised and uploaded once per (file, pixel size), misses remembered.

use std::collections::HashMap;
use std::fmt;
use std::path::PathBuf;

use super::lookup::IconThemes;
use super::raster::{RasterError, rasterise};
use crate::render::{AtlasError, Gpu, ImageAtlas, ImageId, Scale};

/// Why an icon has no image. Each is remembered, so a missing or broken icon
/// costs one lookup, not one per frame.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum IconMiss {
    /// No theme, hicolor or pixmaps directory has the icon.
    NotFound {
        /// The icon name asked for.
        name: String,
    },
    /// The file was found but could not be rasterised.
    Raster(RasterError),
    /// The rasterised image did not enter the atlas.
    Atlas(AtlasError),
}

impl fmt::Display for IconMiss {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NotFound { name } => write!(
                f,
                "no icon theme, hicolor or pixmaps directory holds the icon '{name}'"
            ),
            Self::Raster(e) => e.fmt(f),
            Self::Atlas(e) => e.fmt(f),
        }
    }
}

impl std::error::Error for IconMiss {}

/// The icon cache. It owns the loaded theme chain; the atlas belongs to the
/// renderer and is passed in.
#[derive(Debug)]
pub struct IconCache {
    themes: IconThemes,
    /// (name, logical size, scale) → image.
    named: HashMap<(String, u32, Scale), Result<ImageId, IconMiss>>,
    /// (file, pixel size) → image, shared by names that resolve alike.
    files: HashMap<(PathBuf, u32), Result<ImageId, IconMiss>>,
}

impl IconCache {
    /// A cache over `themes`, empty.
    pub fn new(themes: IconThemes) -> Self {
        Self {
            themes,
            named: HashMap::new(),
            files: HashMap::new(),
        }
    }

    /// The theme chain lookups use.
    pub fn themes(&self) -> &IconThemes {
        &self.themes
    }

    /// The image for icon `name` at `size` logical pixels on an output of
    /// `scale`, rasterised at `scale.physical(size)` pixels square. Draw it
    /// with an [`crate::render::ImageQuad`] in frames of that scale.
    pub fn image(
        &mut self,
        gpu: &Gpu,
        atlas: &mut ImageAtlas,
        name: &str,
        size: u32,
        scale: Scale,
    ) -> Result<ImageId, IconMiss> {
        let key = (name.to_owned(), size, scale);
        if let Some(known) = self.named.get(&key) {
            return known.clone();
        }
        let result = match self.themes.find(name, size, scale) {
            None => Err(IconMiss::NotFound {
                name: name.to_owned(),
            }),
            Some(path) => {
                let pixels = scale.physical((size, size)).0;
                self.files
                    .entry((path, pixels))
                    .or_insert_with_key(|(path, pixels)| {
                        let image = rasterise(path, *pixels).map_err(IconMiss::Raster)?;
                        atlas.insert(gpu, &image).map_err(IconMiss::Atlas)
                    })
                    .clone()
            }
        };
        self.named.insert(key, result.clone());
        result
    }
}
