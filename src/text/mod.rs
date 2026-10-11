//! Fonts for the renderer's text (spec §4.4): families and per-character
//! fallbacks resolved by libfontconfig in-process, honouring the user's
//! aliases, and only the resolved files loaded into cosmic-text. The full
//! system scan (`FontSystem::new()`) is forbidden by clippy.toml.

mod fc;
mod fonts;

pub use fc::FontFile;
pub use fonts::{DEFAULT_FAMILY, Face, FontConfig, FontError, Fonts, PendingFonts};
