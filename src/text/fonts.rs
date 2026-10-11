//! The font book: a cosmic-text `FontSystem` that holds only the faces
//! fontconfig resolved. The default family is loaded up front; a fallback
//! face is loaded the first time some text needs one of its characters.

use std::collections::HashSet;
use std::fmt;

use glyphon::FontSystem;
use glyphon::cosmic_text::{Fallback, fontdb};
use unicode_script::Script;

use super::fc::{Config, FontFile, FontSet};

/// The family logis asks for when nothing else is configured.
pub const DEFAULT_FAMILY: &str = "sans-serif";

/// Why the fonts could not be prepared.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum FontError {
    /// fontconfig could not load a configuration.
    Config(String),
    /// No face answers for the family.
    NoFace(String),
    /// The resolved file could not be read as a font.
    Unreadable(FontFile),
}

impl fmt::Display for FontError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Config(why) => write!(f, "the font litanies are corrupt: {why}"),
            Self::NoFace(family) => {
                write!(
                    f,
                    "no font answers the rite: fontconfig found no face for '{family}'"
                )
            }
            Self::Unreadable(file) => write!(
                f,
                "the font scroll {} (face {}) is unreadable",
                file.path.display(),
                file.index
            ),
        }
    }
}

impl std::error::Error for FontError {}

/// Where fonts come from: the user's fontconfig, or a pinned set of files.
pub struct FontConfig(Config);

impl FontConfig {
    /// The user's own fontconfig setup, aliases included.
    pub fn system() -> Result<Self, FontError> {
        Config::system()
            .map(Self)
            .ok_or_else(|| FontError::Config("fontconfig would not load".into()))
    }

    /// Only the rules in `xml` (a `<fontconfig>` document, for aliases) and
    /// only the given font files; nothing from the system. Used to pin fonts.
    pub fn isolated(xml: &str, files: &[&std::path::Path]) -> Result<Self, FontError> {
        Config::isolated(xml, files)
            .map(Self)
            .map_err(FontError::Config)
    }

    /// The face fontconfig picks for `family`, after aliases and defaults.
    pub fn resolve(&self, family: &str) -> Option<FontFile> {
        self.0.best(family).map(|(file, _)| file)
    }
}

/// The fonts text is shaped with. Only resolved files are ever loaded;
/// cosmic-text's full system scan is never used.
pub struct Fonts {
    config: Config,
    requested: String,
    family: String,
    system: FontSystem,
    fallbacks: Option<FontSet>,
    loaded: Vec<FontFile>,
    seen: HashSet<char>,
}

impl Fonts {
    /// Loads the face fontconfig resolves for `family` from `config`.
    pub fn new(config: FontConfig, family: &str) -> Result<Self, FontError> {
        let config = config.0;
        let (file, _) = config
            .best(family)
            .ok_or_else(|| FontError::NoFace(family.into()))?;
        let mut system = FontSystem::new_with_locale_and_db_and_fallback(
            "en-US".into(),
            fontdb::Database::new(),
            ResolvedOnly,
        );
        let id = load(&mut system, &file).ok_or_else(|| FontError::Unreadable(file.clone()))?;
        let name = system
            .db()
            .face(id)
            .and_then(|face| face.families.first())
            .map(|(name, _)| name.clone())
            .ok_or_else(|| FontError::Unreadable(file.clone()))?;
        Ok(Self {
            config,
            requested: family.into(),
            family: name,
            system,
            fallbacks: None,
            loaded: vec![file],
            seen: HashSet::new(),
        })
    }

    /// The user's fontconfig and its [`DEFAULT_FAMILY`].
    pub fn system() -> Result<Self, FontError> {
        Self::new(FontConfig::system()?, DEFAULT_FAMILY)
    }

    /// The faces loaded so far, the default family's first.
    pub fn loaded(&self) -> &[FontFile] {
        &self.loaded
    }

    /// The default face's family name.
    pub fn family(&self) -> &str {
        &self.family
    }

    /// Makes sure every character of `text` has a face, if fontconfig knows
    /// one: for each character not seen before, the first face in
    /// fontconfig's fallback order that covers it is loaded (once).
    pub fn ensure(&mut self, text: &str) {
        for c in text.chars() {
            if ignorable(c) || !self.seen.insert(c) {
                continue;
            }
            if self.fallbacks.is_none() {
                self.fallbacks = self.config.fallbacks(&self.requested);
            }
            let Some(file) = self.fallbacks.as_ref().and_then(|s| s.first_covering(c)) else {
                continue;
            };
            if !self.loaded.contains(&file) && load(&mut self.system, &file).is_some() {
                self.loaded.push(file);
            }
        }
    }

    /// The shaping and rasterising state.
    pub(crate) fn system_mut(&mut self) -> &mut FontSystem {
        &mut self.system
    }
}

/// Loads the one face `file` names and returns its id.
fn load(system: &mut FontSystem, file: &FontFile) -> Option<fontdb::ID> {
    let db = system.db_mut();
    let ids = db.load_font_source(fontdb::Source::File(file.path.clone()));
    let mut kept = None;
    for id in ids {
        if db.face(id).is_some_and(|face| face.index == file.index) {
            kept = Some(id);
        } else {
            db.remove_face(id);
        }
    }
    kept
}

/// Characters that never need a face of their own: controls, joiners,
/// variation selectors and other default-ignorables. Asking fontconfig for
/// them would load whichever font happens to list them.
fn ignorable(c: char) -> bool {
    c.is_control()
        || matches!(c,
            '\u{00AD}' | '\u{034F}' | '\u{061C}' | '\u{180B}'..='\u{180F}'
            | '\u{200B}'..='\u{200F}' | '\u{202A}'..='\u{202E}' | '\u{2060}'..='\u{206F}'
            | '\u{FE00}'..='\u{FE0F}' | '\u{FEFF}' | '\u{E0000}'..='\u{E0FFF}')
}

/// cosmic-text's fallback lists name fonts by family; logis has already
/// chosen its faces through fontconfig, so these lists stay empty and the
/// shaper falls back over the loaded faces only.
struct ResolvedOnly;

impl Fallback for ResolvedOnly {
    fn common_fallback(&self) -> &[&'static str] {
        &[]
    }

    fn forbidden_fallback(&self) -> &[&'static str] {
        &[]
    }

    fn script_fallback(&self, _: Script, _: &str) -> &[&'static str] {
        &[]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn joiners_selectors_and_controls_are_ignorable() {
        for c in ['\u{200D}', '\u{FE0F}', '\n', '\t', '\u{E0067}', '\u{202E}'] {
            assert!(ignorable(c), "{c:?}");
        }
        for c in ['a', ' ', '機', '🔥', 'ש', '\u{1F3FD}'] {
            assert!(!ignorable(c), "{c:?}");
        }
    }
}
