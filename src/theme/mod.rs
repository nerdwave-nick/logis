//! Themes (spec §4.9, the theme model decided on 6217c721): three layers,
//! a **palette** of named colours with alpha, the **elements** of the
//! cogitator (window, glow, border, panel, decorations, prompt, query,
//! counter, rule, row, selection, match, badge, pending), which refer to
//! palette names, and **effects and motion** (one post-processing pass,
//! motion timings).
//!
//! A theme is `themes/<name>.jsonc` beside the config, or one of the
//! [`BUNDLED`] themes compiled in; a user file overrides a bundled theme of
//! the same name. It may `"extends"` one other theme by name; whatever it
//! does not give comes from the bundled default ([`DEFAULT`]). Its JSON
//! Schema is `schema/theme.schema.json`.

pub mod colour;
pub mod doc;
mod load;
mod model;
mod resolve;

pub use load::{BUNDLED, DEFAULT, ThemeError, Themes};
pub use model::*;

impl Theme {
    /// The bundled default theme.
    pub fn bundled_default() -> Self {
        Themes::default()
            .load(DEFAULT)
            .expect("the bundled default theme is sound")
    }
}

#[cfg(test)]
mod schema_tests;
#[cfg(test)]
mod tests;
