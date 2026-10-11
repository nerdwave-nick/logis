//! The Icon Theme Specification's lookup: the configured theme and its
//! parents (depth first, each once), then hicolor, then the pixmaps
//! directories. Only PNG and SVG are looked for, because only those are
//! rasterised; XPM files are never returned.

use std::collections::HashSet;
use std::path::{Path, PathBuf};

use super::search::SearchPaths;
use super::theme::{ThemeDir, ThemeIndex};
use crate::render::Scale;

/// The fallback theme every chain ends with.
pub const HICOLOR: &str = "hicolor";

/// File extensions in the spec's order of preference, minus XPM.
const EXTENSIONS: [&str; 2] = ["png", "svg"];

/// A theme chain loaded once: each theme's directories that exist on disk,
/// in lookup order. Lookups only test which files exist.
#[derive(Clone, Debug)]
pub struct IconThemes {
    chain: Vec<Theme>,
    pixmap_dirs: Vec<PathBuf>,
}

#[derive(Clone, Debug)]
struct Theme {
    name: String,
    /// Each listed directory with its existing copies, one per base dir.
    dirs: Vec<(ThemeDir, Vec<PathBuf>)>,
}

impl IconThemes {
    /// Loads `theme` (the configured theme name), its `Inherits` chain and
    /// hicolor from `paths`. Missing or invalid theme names are skipped, so a
    /// misconfigured theme still finds hicolor and pixmaps icons.
    pub fn load(paths: &SearchPaths, theme: &str) -> Self {
        let mut chain = Vec::new();
        let mut seen = HashSet::new();
        load_into(paths, theme, &mut seen, &mut chain);
        load_into(paths, HICOLOR, &mut seen, &mut chain);
        Self {
            chain,
            pixmap_dirs: paths.pixmap_dirs.clone(),
        }
    }

    /// The loaded themes in lookup order.
    pub fn names(&self) -> impl Iterator<Item = &str> {
        self.chain.iter().map(|t| t.name.as_str())
    }

    /// The file for icon `name` at `size` logical pixels on an output of
    /// `scale`. Theme directories carry integer scales, so a fractional
    /// scale looks for the next integer one (1.3 → 2) and the caller
    /// rasterises the file at the exact pixel size. An absolute path names
    /// the file itself, as desktop entries may; a trailing `.png`/`.svg`/
    /// `.xpm` on a name is ignored.
    pub fn find(&self, name: &str, size: u32, scale: Scale) -> Option<PathBuf> {
        if Path::new(name).is_absolute() {
            let path = PathBuf::from(name);
            return (supported(&path) && path.is_file()).then_some(path);
        }
        let name = strip_extension(name);
        if name.is_empty() || name.starts_with('.') || name.contains('/') {
            return None;
        }
        let scale = scale.as_120ths().div_ceil(120).max(1);
        self.chain
            .iter()
            .find_map(|theme| theme.find(name, size, scale))
            .or_else(|| first_file(self.pixmap_dirs.iter(), name))
    }
}

impl Theme {
    /// The spec's `LookupIcon`: an exactly matching directory first, then
    /// the directory whose sizes are closest.
    fn find(&self, name: &str, size: u32, scale: u32) -> Option<PathBuf> {
        let exact = self
            .dirs
            .iter()
            .filter(|(dir, _)| dir.matches(size, scale))
            .find_map(|(_, copies)| first_file(copies.iter(), name));
        exact.or_else(|| {
            self.dirs
                .iter()
                .filter_map(|(dir, copies)| {
                    let file = first_file(copies.iter(), name)?;
                    Some((dir.distance(size, scale), file))
                })
                .min_by_key(|(distance, _)| *distance)
                .map(|(_, file)| file)
        })
    }
}

fn load_into(paths: &SearchPaths, name: &str, seen: &mut HashSet<String>, chain: &mut Vec<Theme>) {
    if !valid_theme_name(name) || !seen.insert(name.to_owned()) {
        return;
    }
    let Some(index) = paths
        .icon_dirs
        .iter()
        .find_map(|base| std::fs::read_to_string(base.join(name).join("index.theme")).ok())
    else {
        return;
    };
    let index = ThemeIndex::parse(&index);
    let dirs = index
        .dirs
        .into_iter()
        .filter_map(|dir| {
            let copies: Vec<PathBuf> = paths
                .icon_dirs
                .iter()
                .map(|base| base.join(name).join(&dir.path))
                .filter(|p| p.is_dir())
                .collect();
            (!copies.is_empty()).then_some((dir, copies))
        })
        .collect();
    chain.push(Theme {
        name: name.to_owned(),
        dirs,
    });
    for parent in &index.inherits {
        load_into(paths, parent, seen, chain);
    }
}

/// A theme name is one directory name, never a path.
fn valid_theme_name(name: &str) -> bool {
    !name.is_empty() && name != "." && name != ".." && !name.contains('/')
}

/// The first `<dir>/<name>.<ext>` that is a file, extensions in order.
fn first_file<'a>(dirs: impl Iterator<Item = &'a PathBuf>, name: &str) -> Option<PathBuf> {
    dirs.flat_map(|dir| {
        EXTENSIONS
            .iter()
            .map(move |ext| dir.join(format!("{name}.{ext}")))
    })
    .find(|path| path.is_file())
}

fn supported(path: &Path) -> bool {
    path.extension()
        .and_then(|e| e.to_str())
        .is_some_and(|e| EXTENSIONS.iter().any(|x| e.eq_ignore_ascii_case(x)))
}

fn strip_extension(name: &str) -> &str {
    [".png", ".svg", ".xpm"]
        .iter()
        .find_map(|ext| name.strip_suffix(ext))
        .unwrap_or(name)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixtures() -> PathBuf {
        Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/icons")
    }

    fn paths() -> SearchPaths {
        let root = fixtures();
        SearchPaths {
            icon_dirs: vec![root.join("home-icons"), root.join("share/icons")],
            pixmap_dirs: vec![root.join("pixmaps")],
        }
    }

    fn forge() -> IconThemes {
        IconThemes::load(&paths(), "logis-forge")
    }

    /// The found file relative to the fixture root.
    fn find(themes: &IconThemes, name: &str, size: u32, scale: u32) -> Option<String> {
        let path = themes.find(name, size, Scale::from_120ths(scale))?;
        Some(
            path.strip_prefix(fixtures())
                .unwrap()
                .to_string_lossy()
                .into_owned(),
        )
    }

    const FORGE: &str = "share/icons/logis-forge";

    #[test]
    fn load_follows_inherits_once_then_hicolor() {
        assert_eq!(
            forge().names().collect::<Vec<_>>(),
            ["logis-forge", "logis-cogitator", "hicolor"]
        );
    }

    #[test]
    fn find_prefers_the_exactly_matching_size() {
        let themes = forge();
        let cog = |size| find(&themes, "forge-cog", size, 120);
        assert_eq!(cog(16), Some(format!("{FORGE}/16x16/apps/forge-cog.png")));
        assert_eq!(cog(32), Some(format!("{FORGE}/32x32/apps/forge-cog.png")));
        // Only the scalable directory spans 24.
        assert_eq!(
            cog(24),
            Some(format!("{FORGE}/scalable/apps/forge-cog.svg"))
        );
    }

    #[test]
    fn find_uses_the_next_integer_scale_for_fractional_outputs() {
        let themes = forge();
        let doubled = Some(format!("{FORGE}/32x32@2/apps/forge-cog.png"));
        assert_eq!(find(&themes, "forge-cog", 32, 240), doubled);
        assert_eq!(find(&themes, "forge-cog", 32, 156), doubled);
        // 16 at 1.3 → scale 2, no 16@2: the closest pixels are 32x32's.
        assert_eq!(
            find(&themes, "forge-cog", 16, 156),
            Some(format!("{FORGE}/32x32/apps/forge-cog.png"))
        );
    }

    #[test]
    fn find_falls_back_to_the_closest_size() {
        let themes = forge();
        let relic = Some(format!("{FORGE}/48x48/apps/forge-relic.png"));
        assert_eq!(find(&themes, "forge-relic", 50, 120), relic); // threshold
        assert_eq!(find(&themes, "forge-relic", 100, 120), relic); // closest
        // Beyond every range, scalable's 256 is closer than any fixed size.
        assert_eq!(
            find(&themes, "forge-cog", 300, 120),
            Some(format!("{FORGE}/scalable/apps/forge-cog.svg"))
        );
    }

    #[test]
    fn find_prefers_earlier_base_dirs_and_png_over_svg() {
        let themes = forge();
        assert_eq!(
            find(&themes, "home-sigil", 32, 120),
            Some("home-icons/logis-forge/32x32/apps/home-sigil.png".into())
        );
        assert_eq!(
            find(&themes, "both-formats", 32, 120),
            Some(format!("{FORGE}/32x32/apps/both-formats.png"))
        );
    }

    #[test]
    fn find_searches_parents_hicolor_then_pixmaps() {
        let themes = forge();
        assert_eq!(
            find(&themes, "cogitator-skull", 32, 120),
            Some("share/icons/logis-cogitator/scalable/apps/cogitator-skull.svg".into())
        );
        assert_eq!(
            find(&themes, "hicolor-only", 32, 120),
            Some("share/icons/hicolor/32x32/apps/hicolor-only.png".into())
        );
        assert_eq!(
            find(&themes, "pixmap-only", 32, 120),
            Some("pixmaps/pixmap-only.png".into())
        );
    }

    #[test]
    fn find_misses_unsupported_unrelated_and_unknown_icons() {
        let themes = forge();
        assert_eq!(find(&themes, "xpm-only", 32, 120), None);
        assert_eq!(find(&themes, "stranger-only", 32, 120), None);
        assert_eq!(find(&themes, "no-such-icon", 32, 120), None);
    }

    #[test]
    fn an_unknown_or_invalid_theme_still_finds_hicolor() {
        for theme in ["no-such-theme", "../icons", ""] {
            let themes = IconThemes::load(&paths(), theme);
            assert_eq!(themes.names().collect::<Vec<_>>(), [HICOLOR]);
            assert_eq!(find(&themes, "forge-cog", 32, 120), None);
            assert!(find(&themes, "hicolor-only", 32, 120).is_some());
        }
    }

    #[test]
    fn names_may_be_absolute_paths_or_carry_an_extension() {
        let themes = forge();
        let file = fixtures().join("pixmaps/pixmap-only.png");
        assert_eq!(
            themes.find(file.to_str().unwrap(), 32, Scale::ONE),
            Some(file)
        );
        let xpm = fixtures().join("pixmaps/xpm-only.xpm");
        assert_eq!(themes.find(xpm.to_str().unwrap(), 32, Scale::ONE), None);
        assert_eq!(
            find(&themes, "forge-cog.png", 32, 120),
            Some(format!("{FORGE}/32x32/apps/forge-cog.png"))
        );
        assert_eq!(find(&themes, "apps/forge-cog", 32, 120), None);
        assert_eq!(find(&themes, "", 32, 120), None);
    }
}
