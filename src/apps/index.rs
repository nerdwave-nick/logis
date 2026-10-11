//! The app index: every desktop entry the launcher may show, read from the
//! `applications` directories of the XDG base directories (Desktop Entry
//! Specification, "Desktop File ID" and "Hidden"):
//!
//! - `$XDG_DATA_HOME/applications` (default `~/.local/share`) first, then
//!   `applications` in each of `$XDG_DATA_DIRS` (default
//!   `/usr/local/share:/usr/share`), in order; relative paths are ignored;
//! - a file's desktop-file ID is its path below `applications` with `/`
//!   turned into `-` (`forge/litany.desktop` is `forge-litany.desktop`);
//! - the first file with an ID wins: earlier directories override later
//!   ones, and an override with `Hidden=true` deletes the entry. A file that
//!   is not a usable application entry (malformed, a link …) is skipped and
//!   overrides nothing;
//! - shown are entries without `NoDisplay`, whose `OnlyShowIn` names one of
//!   the current desktops (`$XDG_CURRENT_DESKTOP`, colon-separated) if
//!   given, whose `NotShowIn` names none of them, and whose `TryExec`
//!   program exists and is executable (an absolute path, or found in
//!   `$PATH`).

use std::collections::HashSet;
use std::path::{Path, PathBuf};

use super::entry::DesktopEntry;
use super::locale::Locale;

/// The `applications` directories, highest precedence first.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Dirs(pub Vec<PathBuf>);

/// What decides how entries read and which show: the user's locale, the
/// current desktops and the program search path.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Context {
    pub locale: Locale,
    /// `$XDG_CURRENT_DESKTOP`, split at `:`.
    pub desktops: Vec<String>,
    /// `$PATH`, split.
    pub path: Vec<PathBuf>,
}

/// One application the launcher shows.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct App {
    /// Its desktop-file ID, such as `org.gnome.Nautilus.desktop`.
    pub id: String,
    /// The file it was read from (for `%k`).
    pub location: PathBuf,
    pub entry: DesktopEntry,
}

impl App {
    /// The ID without `.desktop`: the application ID (D-Bus name, app_id).
    pub fn app_id(&self) -> &str {
        self.id.strip_suffix(".desktop").unwrap_or(&self.id)
    }
}

/// The applications to show, by name, and the directories they came from.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Index {
    apps: Vec<App>,
    /// Every directory read (the `applications` directories and those below).
    pub dirs: Vec<PathBuf>,
}

impl Dirs {
    /// From the variables of an environment (see the module docs).
    pub fn from_vars(get: impl Fn(&str) -> Option<String>) -> Self {
        let absolute = |p: &PathBuf| p.is_absolute();
        let home = get("HOME").map(PathBuf::from).filter(absolute);
        let data_home = get("XDG_DATA_HOME")
            .map(PathBuf::from)
            .filter(absolute)
            .or_else(|| home.map(|h| h.join(".local/share")));
        let mut data_dirs: Vec<PathBuf> = get("XDG_DATA_DIRS")
            .map(|v| std::env::split_paths(&v).filter(absolute).collect())
            .unwrap_or_default();
        if data_dirs.is_empty() {
            data_dirs = vec!["/usr/local/share".into(), "/usr/share".into()];
        }
        let mut dirs: Vec<PathBuf> = Vec::new();
        for dir in data_home.into_iter().chain(data_dirs) {
            let apps = dir.join("applications");
            if !dirs.contains(&apps) {
                dirs.push(apps);
            }
        }
        Self(dirs)
    }

    /// From the daemon's own environment.
    pub fn from_env() -> Self {
        Self::from_vars(|name| std::env::var(name).ok())
    }
}

impl Context {
    /// From the variables of an environment.
    pub fn from_vars(get: impl Fn(&str) -> Option<String>) -> Self {
        Self {
            desktops: get("XDG_CURRENT_DESKTOP")
                .map(|v| {
                    v.split(':')
                        .filter(|d| !d.is_empty())
                        .map(str::to_owned)
                        .collect()
                })
                .unwrap_or_default(),
            path: get("PATH")
                .map(|v| std::env::split_paths(&v).collect())
                .unwrap_or_default(),
            locale: Locale::from_vars(&get),
        }
    }

    /// From the daemon's own environment.
    pub fn from_env() -> Self {
        Self::from_vars(|name| std::env::var(name).ok())
    }

    /// Whether an entry shows (see the module docs).
    pub fn shows(&self, entry: &DesktopEntry) -> bool {
        let on = |list: &[String]| list.iter().any(|d| self.desktops.contains(d));
        !entry.hidden
            && !entry.no_display
            && (entry.only_show_in.is_empty() || on(&entry.only_show_in))
            && !on(&entry.not_show_in)
            && entry
                .try_exec
                .as_deref()
                .is_none_or(|program| self.finds(program))
    }

    /// Whether `program` is an executable file: at its absolute path, or
    /// in one of the search path's directories.
    pub fn finds(&self, program: &str) -> bool {
        if program.contains('/') {
            return Path::new(program).is_absolute() && executable(Path::new(program));
        }
        self.path
            .iter()
            .filter(|dir| dir.is_absolute())
            .any(|dir| executable(&dir.join(program)))
    }
}

fn executable(path: &Path) -> bool {
    path.is_file() && rustix::fs::access(path, rustix::fs::Access::EXEC_OK).is_ok()
}

/// How deep below an `applications` directory entries are looked for.
const MAX_DEPTH: usize = 8;

impl Index {
    /// Reads every entry of `dirs`.
    pub fn scan(dirs: &Dirs, cx: &Context) -> Self {
        let mut seen = HashSet::new();
        let mut index = Self::default();
        for dir in &dirs.0 {
            index.walk(dir, String::new(), 0, &mut seen, cx);
        }
        index
            .apps
            .sort_by_cached_key(|a| (a.entry.name.to_lowercase(), a.id.clone()));
        index
    }

    /// Reads the files below `dir`, whose IDs start with `prefix`.
    fn walk(
        &mut self,
        dir: &Path,
        prefix: String,
        depth: usize,
        seen: &mut HashSet<String>,
        cx: &Context,
    ) {
        let Ok(read) = std::fs::read_dir(dir) else {
            return;
        };
        self.dirs.push(dir.to_owned());
        let mut names: Vec<_> = read.flatten().map(|e| e.file_name()).collect();
        names.sort();
        for name in names {
            let Some(name) = name.to_str() else { continue };
            let path = dir.join(name);
            if path.is_dir() {
                if depth < MAX_DEPTH {
                    self.walk(&path, format!("{prefix}{name}-"), depth + 1, seen, cx);
                }
                continue;
            }
            if !name.ends_with(".desktop") {
                continue;
            }
            let id = format!("{prefix}{name}");
            if seen.contains(&id) {
                continue;
            }
            let Ok(text) = std::fs::read_to_string(&path) else {
                continue;
            };
            // A file that is not a usable entry overrides nothing.
            let Ok(entry) = super::entry::parse(&text, &cx.locale) else {
                continue;
            };
            seen.insert(id.clone());
            if cx.shows(&entry) {
                self.apps.push(App {
                    id,
                    location: path,
                    entry,
                });
            }
        }
    }

    /// The applications, by name (ignoring case), then ID.
    pub fn apps(&self) -> &[App] {
        &self.apps
    }

    /// The application with this desktop-file ID.
    pub fn get(&self, id: &str) -> Option<&App> {
        self.apps.iter().find(|a| a.id == id)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixtures() -> PathBuf {
        Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/apps")
    }

    fn vars(pairs: Vec<(&'static str, String)>) -> impl Fn(&str) -> Option<String> {
        move |name| {
            pairs
                .iter()
                .find(|(n, _)| *n == name)
                .map(|(_, v)| v.clone())
        }
    }

    fn context() -> Context {
        Context {
            locale: Locale::default(),
            desktops: vec!["niri".into()],
            path: vec![fixtures().join("bin")],
        }
    }

    fn dirs() -> Dirs {
        let f = fixtures();
        Dirs(vec![
            f.join("home/applications"),
            f.join("system/applications"),
            f.join("vendor/applications"),
        ])
    }

    fn ids(index: &Index) -> Vec<&str> {
        index.apps().iter().map(|a| a.id.as_str()).collect()
    }

    #[test]
    fn data_home_comes_before_the_data_dirs_and_relative_paths_are_ignored() {
        let dirs = Dirs::from_vars(vars(vec![
            ("HOME", "/home/adept".into()),
            ("XDG_DATA_DIRS", "/opt/share:relative:/usr/share".into()),
        ]));
        assert_eq!(
            dirs.0,
            [
                PathBuf::from("/home/adept/.local/share/applications"),
                "/opt/share/applications".into(),
                "/usr/share/applications".into(),
            ]
        );
        let dirs = Dirs::from_vars(vars(vec![("XDG_DATA_HOME", "/data".into())]));
        assert_eq!(
            dirs.0,
            [
                PathBuf::from("/data/applications"),
                "/usr/local/share/applications".into(),
                "/usr/share/applications".into(),
            ]
        );
    }

    #[test]
    fn the_context_reads_desktops_path_and_locale() {
        let cx = Context::from_vars(vars(vec![
            ("XDG_CURRENT_DESKTOP", "niri:wlroots".into()),
            ("PATH", "/usr/bin:/bin".into()),
            ("LANG", "de_DE.UTF-8".into()),
        ]));
        assert_eq!(cx.desktops, ["niri", "wlroots"]);
        assert_eq!(cx.path, [PathBuf::from("/usr/bin"), "/bin".into()]);
        assert_eq!(cx.locale, Locale::parse("de_DE"));
    }

    #[test]
    fn ids_follow_the_path_below_applications_and_the_first_file_wins() {
        let index = Index::scan(&dirs(), &context());
        let oil = index.get("forge-oil.desktop").unwrap();
        assert_eq!(oil.entry.name, "Oil (home)");
        assert!(
            oil.location
                .ends_with("home/applications/forge-oil.desktop")
        );
        let litany = index.get("forge-litany.desktop").unwrap();
        assert!(
            litany
                .location
                .ends_with("home/applications/forge/litany.desktop")
        );
        assert_eq!(litany.app_id(), "forge-litany");
        assert!(
            index
                .dirs
                .contains(&fixtures().join("home/applications/forge"))
        );
    }

    #[test]
    fn hidden_deletes_and_a_broken_override_overrides_nothing() {
        let index = Index::scan(&dirs(), &context());
        assert!(index.get("forge-deleted.desktop").is_none());
        assert_eq!(
            index.get("forge-broken.desktop").unwrap().entry.name,
            "Broken in home"
        );
    }

    #[test]
    fn only_shown_applications_are_indexed_by_name() {
        let index = Index::scan(&dirs(), &context());
        assert_eq!(
            ids(&index),
            [
                "forge-anoint.desktop",
                "forge-broken.desktop",
                "org.forge.Bus.desktop",
                "forge-terminal.desktop",
                "forge-litany.desktop",
                "forge-oil.desktop",
                "forge-only-niri.desktop",
                "forge-tryexec-present.desktop",
            ]
        );
    }

    #[test]
    fn show_in_rules_follow_the_current_desktops() {
        let gnome = Context {
            desktops: vec!["GNOME".into()],
            ..context()
        };
        let index = Index::scan(&dirs(), &gnome);
        let shown = ids(&index);
        assert!(shown.contains(&"forge-only-gnome.desktop"));
        assert!(shown.contains(&"forge-not-niri.desktop"));
        assert!(
            shown.contains(&"forge-only-niri.desktop"),
            "GNOME is listed too"
        );
        let nowhere = Context {
            desktops: Vec::new(),
            ..context()
        };
        let index = Index::scan(&dirs(), &nowhere);
        let shown = ids(&index);
        assert!(
            !shown.contains(&"forge-only-niri.desktop"),
            "OnlyShowIn needs a desktop"
        );
        assert!(shown.contains(&"forge-not-niri.desktop"));
    }

    #[test]
    fn try_exec_needs_an_executable_on_the_path_or_at_its_absolute_path() {
        let entry = |try_exec: &str| DesktopEntry {
            try_exec: Some(try_exec.into()),
            ..DesktopEntry::default()
        };
        let cx = context();
        assert!(cx.shows(&entry("anoint")));
        assert!(!cx.shows(&entry("forge-no-such-program")));
        let anoint = fixtures().join("bin/anoint");
        assert!(cx.shows(&entry(anoint.to_str().unwrap())));
        let not_executable = fixtures().join("system/applications/forge-oil.desktop");
        assert!(!cx.shows(&entry(not_executable.to_str().unwrap())));
        assert!(!cx.shows(&entry("/no/such/forge")));
    }
}
