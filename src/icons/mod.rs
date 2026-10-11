//! Icons (spec §4.7): freedesktop icon-theme lookup for the configured
//! theme, its parents, hicolor and the pixmaps directories; SVG and PNG
//! rasterisation; and a cache that puts each icon into the renderer's image
//! atlas once per pixel size.

mod cache;
mod lookup;
mod raster;
mod search;
mod theme;

pub use cache::{FrameIcons, IconCache, IconMiss, IconSource, NoIcons};
pub use lookup::{HICOLOR, IconThemes};
pub use raster::{MAX_ICON_PIXELS, RasterError, rasterise};
pub use search::SearchPaths;
