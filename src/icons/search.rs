//! Where icon themes and pixmaps live: the Icon Theme Specification's base
//! directories, derived from `$HOME` and the XDG base directory variables.

use std::ffi::OsStr;
use std::path::{Path, PathBuf};

/// Base directories for icon lookup, in priority order.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct SearchPaths {
    /// Directories that hold icon themes (`<dir>/<theme>/index.theme`).
    pub icon_dirs: Vec<PathBuf>,
    /// Directories searched last, for unthemed icons directly inside them.
    pub pixmap_dirs: Vec<PathBuf>,
}

/// The pixmaps directory the spec names explicitly.
const PIXMAPS: &str = "/usr/share/pixmaps";

impl SearchPaths {
    /// From the process environment: see [`SearchPaths::from_vars`].
    pub fn from_env() -> Self {
        Self::from_vars(
            std::env::var_os("HOME").as_deref(),
            std::env::var_os("XDG_DATA_HOME").as_deref(),
            std::env::var_os("XDG_DATA_DIRS").as_deref(),
        )
    }

    /// `$HOME/.icons`, `$XDG_DATA_HOME/icons` (default `~/.local/share`), then
    /// `icons` in each of `$XDG_DATA_DIRS` (default `/usr/local/share:/usr/share`);
    /// pixmaps from `pixmaps` in each data dir, ending with `/usr/share/pixmaps`.
    /// Relative paths are ignored, as the XDG spec requires; duplicates are
    /// dropped.
    pub fn from_vars(
        home: Option<&OsStr>,
        data_home: Option<&OsStr>,
        data_dirs: Option<&OsStr>,
    ) -> Self {
        let home = home.map(Path::new).filter(|p| p.is_absolute());
        let data_home = data_home
            .map(PathBuf::from)
            .filter(|p| p.is_absolute())
            .or_else(|| home.map(|h| h.join(".local/share")));
        let mut dirs: Vec<PathBuf> = data_dirs
            .map(std::env::split_paths)
            .into_iter()
            .flatten()
            .filter(|p| p.is_absolute())
            .collect();
        if dirs.is_empty() {
            dirs = vec!["/usr/local/share".into(), "/usr/share".into()];
        }

        let mut icon_dirs = Vec::new();
        icon_dirs.extend(home.map(|h| h.join(".icons")));
        icon_dirs.extend(data_home.map(|d| d.join("icons")));
        icon_dirs.extend(dirs.iter().map(|d| d.join("icons")));
        let mut pixmap_dirs: Vec<PathBuf> = dirs.iter().map(|d| d.join("pixmaps")).collect();
        pixmap_dirs.push(PIXMAPS.into());
        Self {
            icon_dirs: dedup(icon_dirs),
            pixmap_dirs: dedup(pixmap_dirs),
        }
    }
}

fn dedup(paths: Vec<PathBuf>) -> Vec<PathBuf> {
    let mut out: Vec<PathBuf> = Vec::with_capacity(paths.len());
    for path in paths {
        if !out.contains(&path) {
            out.push(path);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn paths(list: &[&str]) -> Vec<PathBuf> {
        list.iter().map(PathBuf::from).collect()
    }

    #[test]
    fn from_vars_orders_home_data_home_then_data_dirs() {
        let search = SearchPaths::from_vars(
            Some(OsStr::new("/home/adept")),
            Some(OsStr::new("/home/adept/data")),
            Some(OsStr::new("/opt/share:/usr/share")),
        );
        assert_eq!(
            search.icon_dirs,
            paths(&[
                "/home/adept/.icons",
                "/home/adept/data/icons",
                "/opt/share/icons",
                "/usr/share/icons",
            ])
        );
        assert_eq!(
            search.pixmap_dirs,
            paths(&["/opt/share/pixmaps", "/usr/share/pixmaps"])
        );
    }

    #[test]
    fn from_vars_uses_the_xdg_defaults_when_unset_or_empty() {
        let search =
            SearchPaths::from_vars(Some(OsStr::new("/home/adept")), None, Some(OsStr::new("")));
        assert_eq!(
            search.icon_dirs,
            paths(&[
                "/home/adept/.icons",
                "/home/adept/.local/share/icons",
                "/usr/local/share/icons",
                "/usr/share/icons",
            ])
        );
        assert_eq!(
            search.pixmap_dirs,
            paths(&["/usr/local/share/pixmaps", "/usr/share/pixmaps"])
        );
    }

    #[test]
    fn from_vars_ignores_relative_paths_and_a_missing_home() {
        let search = SearchPaths::from_vars(
            None,
            Some(OsStr::new("relative")),
            Some(OsStr::new("also/relative:/srv/share")),
        );
        assert_eq!(search.icon_dirs, paths(&["/srv/share/icons"]));
        assert_eq!(
            search.pixmap_dirs,
            paths(&["/srv/share/pixmaps", "/usr/share/pixmaps"])
        );
    }
}
