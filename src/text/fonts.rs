//! The font book: a cosmic-text `FontSystem` that holds only the faces
//! fontconfig resolved. The default family is loaded up front; a fallback
//! face is loaded the first time some text needs one of its characters.

use std::collections::HashSet;
use std::fmt;
use std::thread::JoinHandle;

use glyphon::FontSystem;
use glyphon::cosmic_text::{Fallback, fontdb};
use unicode_script::Script;

use super::fc::{Config, FontFile, FontSet};

/// The family logis asks for when nothing else is configured.
pub const DEFAULT_FAMILY: &str = "sans-serif";

/// A face to resolve: families in order of preference (fontconfig picks
/// the first installed one, generic families through the user's aliases)
/// at a weight.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct Face {
    /// Family names, the most wanted first.
    pub families: Vec<String>,
    /// The OpenType weight (400 regular, 600 semibold, 700 bold).
    pub weight: u16,
}

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
    /// The faces texts name by index: family name and weight as loaded.
    faces: Vec<(String, u16)>,
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
        let (name, _) = identity(&system, id).ok_or_else(|| FontError::Unreadable(file.clone()))?;
        Ok(Self {
            config,
            requested: family.into(),
            family: name,
            system,
            fallbacks: None,
            loaded: vec![file],
            seen: HashSet::new(),
            faces: Vec::new(),
        })
    }

    /// The faces loaded so far, the default family's first.
    pub fn loaded(&self) -> &[FontFile] {
        &self.loaded
    }

    /// The default face's family name.
    pub fn family(&self) -> &str {
        &self.family
    }

    /// Resolves `faces` through fontconfig, loading each resolved file
    /// once, and makes them the faces texts name by index. A face that
    /// resolves to nothing readable is the default family instead, and the
    /// first such failure is returned once all are resolved.
    pub fn set_faces(&mut self, faces: &[Face]) -> Result<(), FontError> {
        let mut first_error = None;
        let mut resolved = Vec::with_capacity(faces.len());
        for face in faces {
            match self.resolve_face(face) {
                Ok(identity) => resolved.push(identity),
                Err(e) => {
                    resolved.push((self.family.clone(), 400));
                    first_error.get_or_insert(e);
                }
            }
        }
        self.faces = resolved;
        first_error.map_or(Ok(()), Err)
    }

    fn resolve_face(&mut self, face: &Face) -> Result<(String, u16), FontError> {
        let families: Vec<&str> = face.families.iter().map(String::as_str).collect();
        let (file, _) = self
            .config
            .best_of(&families, Some(face.weight))
            .ok_or_else(|| FontError::NoFace(face.families.join(", ")))?;
        let known = self
            .system
            .db()
            .faces()
            .find(|f| same_file(f, &file))
            .map(|f| f.id);
        let id = match known {
            Some(id) => id,
            None => {
                let id = load(&mut self.system, &file)
                    .ok_or_else(|| FontError::Unreadable(file.clone()))?;
                self.loaded.push(file.clone());
                id
            }
        };
        identity(&self.system, id).ok_or(FontError::Unreadable(file))
    }

    /// The family name and weight of face `index` as the shaper sees it:
    /// the first face for an index out of range, the default family when
    /// there are no faces.
    pub fn face(&self, index: usize) -> (&str, u16) {
        match self.faces.get(index).or(self.faces.first()) {
            Some((family, weight)) => (family, *weight),
            None => (&self.family, 400),
        }
    }

    /// Makes sure every character of `text` has a face, if fontconfig knows
    /// one: for each character not seen before, the first face in
    /// fontconfig's fallback order that covers it is loaded (once).
    pub fn ensure(&mut self, text: &str) {
        for c in text.chars() {
            if ignorable(c) || !self.seen.insert(c) {
                continue;
            }
            self.sort_fallbacks();
            let Some(file) = self.fallbacks.as_ref().and_then(|s| s.first_covering(c)) else {
                continue;
            };
            if !self.loaded.contains(&file) && load(&mut self.system, &file).is_some() {
                self.loaded.push(file);
            }
        }
    }

    /// Sorts fontconfig's fallback order for the family, once.
    fn sort_fallbacks(&mut self) {
        if self.fallbacks.is_none() {
            self.fallbacks = self.config.fallbacks(&self.requested);
        }
    }

    /// The shaping and rasterising state.
    pub(crate) fn system_mut(&mut self) -> &mut FontSystem {
        &mut self.system
    }
}

/// Fonts being resolved on a thread of their own, so that fontconfig's
/// start-up overlaps the compositor's and the GPU's: loading its
/// configuration and caches, matching the family against every installed
/// face and sorting the fallback order each cost milliseconds per thousand
/// faces, and none of them needs the GPU.
pub struct PendingFonts(JoinHandle<Result<Fonts, FontError>>);

impl PendingFonts {
    /// Starts resolving `family` from the configuration `config` makes: the
    /// default face is loaded and the fallback order sorted, so the first
    /// text only loads the fallback faces it needs.
    pub fn resolve(
        config: impl FnOnce() -> Result<FontConfig, FontError> + Send + 'static,
        family: &str,
    ) -> Self {
        let family = family.to_owned();
        Self(std::thread::spawn(move || {
            let mut fonts = Fonts::new(config()?, &family)?;
            fonts.sort_fallbacks();
            Ok(fonts)
        }))
    }

    /// The user's fontconfig and its [`DEFAULT_FAMILY`].
    pub fn system() -> Self {
        Self::resolve(FontConfig::system, DEFAULT_FAMILY)
    }

    /// The user's fontconfig with `faces` resolved as the faces texts name
    /// by index (a theme's). A face that cannot be resolved is the default
    /// family; that is no error here, the theme still shows.
    pub fn system_with(faces: Vec<Face>) -> Self {
        Self(std::thread::spawn(move || {
            let mut fonts = Fonts::new(FontConfig::system()?, DEFAULT_FAMILY)?;
            let _ = fonts.set_faces(&faces);
            fonts.sort_fallbacks();
            Ok(fonts)
        }))
    }

    /// The resolved fonts, once they are ready. A resolver that panicked is
    /// an error, not a panic of the caller.
    pub fn wait(self) -> Result<Fonts, FontError> {
        self.0
            .join()
            .unwrap_or_else(|_| Err(FontError::Config("the font thread perished".into())))
    }
}

/// A loaded face's first family name and its weight.
fn identity(system: &FontSystem, id: fontdb::ID) -> Option<(String, u16)> {
    let face = system.db().face(id)?;
    let (name, _) = face.families.first()?;
    Some((name.clone(), face.weight.0))
}

/// Whether the loaded face `info` is the one `file` names.
fn same_file(info: &fontdb::FaceInfo, file: &FontFile) -> bool {
    info.index == file.index
        && matches!(&info.source, fontdb::Source::File(path) if *path == file.path)
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
