//! One icon theme's `index.theme` (freedesktop Icon Theme Specification):
//! its parents and its directories with their size, scale and type, and the
//! spec's two rules for choosing among them.

/// How a theme directory's icons may be sized.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum DirKind {
    /// Only exactly `Size`.
    Fixed,
    /// Anything from `MinSize` to `MaxSize` (vector icons).
    Scalable {
        /// Smallest size, in icon units.
        min: u32,
        /// Largest size, in icon units.
        max: u32,
    },
    /// `Size` give or take `Threshold`.
    Threshold(u32),
}

/// One subdirectory listed in `Directories` or `ScaledDirectories`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct ThemeDir {
    /// The subdirectory, relative to the theme directory.
    pub path: String,
    /// Nominal size in icon units (logical pixels).
    pub size: u32,
    /// The integer scale its pixels are made for.
    pub scale: u32,
    /// How strictly `size` applies.
    pub kind: DirKind,
}

/// What `index.theme` says about a theme.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(crate) struct ThemeIndex {
    /// Parent theme names, in lookup order.
    pub inherits: Vec<String>,
    /// Directories in the order the theme lists them.
    pub dirs: Vec<ThemeDir>,
}

impl ThemeIndex {
    /// Parses an `index.theme`. Unknown keys, localised keys and comments are
    /// ignored; a directory without a group or a valid `Size` is skipped,
    /// because the spec makes `Size` mandatory.
    pub fn parse(text: &str) -> Self {
        let groups = groups(text);
        let header = groups.iter().find(|(name, _)| *name == "Icon Theme");
        let value = |key: &str| header.and_then(|(_, keys)| lookup(keys, key));
        let list = |key: &str| -> Vec<String> {
            value(key)
                .map(|v| {
                    v.split(',')
                        .map(str::trim)
                        .filter(|s| !s.is_empty())
                        .map(String::from)
                        .collect()
                })
                .unwrap_or_default()
        };
        let mut names = list("Directories");
        names.extend(list("ScaledDirectories"));
        let dirs = names
            .into_iter()
            .filter_map(|name| {
                let (_, keys) = groups.iter().find(|(group, _)| *group == name)?;
                dir(name, keys)
            })
            .collect();
        Self {
            inherits: list("Inherits"),
            dirs,
        }
    }
}

type Keys<'a> = Vec<(&'a str, &'a str)>;

/// Groups in file order with their `key=value` pairs.
fn groups(text: &str) -> Vec<(&str, Keys<'_>)> {
    let mut out: Vec<(&str, Keys<'_>)> = Vec::new();
    for line in text.lines().map(str::trim) {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some(name) = line.strip_prefix('[').and_then(|l| l.strip_suffix(']')) {
            out.push((name, Vec::new()));
        } else if let (Some((key, value)), Some((_, keys))) = (line.split_once('='), out.last_mut())
        {
            keys.push((key.trim(), value.trim()));
        }
    }
    out
}

fn lookup<'a>(keys: &Keys<'a>, key: &str) -> Option<&'a str> {
    keys.iter().find(|(k, _)| *k == key).map(|(_, v)| *v)
}

fn dir(path: String, keys: &Keys<'_>) -> Option<ThemeDir> {
    let number = |key: &str| lookup(keys, key).map(str::parse::<u32>);
    let size = number("Size")?.ok()?;
    let or = |key: &str, default: u32| match number(key) {
        Some(Ok(n)) => Some(n),
        Some(Err(_)) => None,
        None => Some(default),
    };
    let scale = or("Scale", 1)?.max(1);
    let kind = match lookup(keys, "Type").unwrap_or("Threshold") {
        "Fixed" => DirKind::Fixed,
        "Scalable" => DirKind::Scalable {
            min: or("MinSize", size)?,
            max: or("MaxSize", size)?,
        },
        _ => DirKind::Threshold(or("Threshold", 2)?),
    };
    Some(ThemeDir {
        path,
        size,
        scale,
        kind,
    })
}

impl ThemeDir {
    /// The spec's `DirectoryMatchesSize`: an exact fit for `size` at `scale`.
    pub fn matches(&self, size: u32, scale: u32) -> bool {
        if self.scale != scale {
            return false;
        }
        let (min, max) = self.range();
        (min..=max).contains(&size)
    }

    /// The spec's `DirectorySizeDistance`, in pixels: how far the directory's
    /// pixel sizes are from `size * scale`. Zero inside its range.
    pub fn distance(&self, size: u32, scale: u32) -> u32 {
        let wanted = size.saturating_mul(scale);
        let (min, max) = self.range();
        let (min, max) = (
            min.saturating_mul(self.scale),
            max.saturating_mul(self.scale),
        );
        if wanted < min {
            min - wanted
        } else {
            wanted.saturating_sub(max)
        }
    }

    /// The icon sizes this directory serves, inclusive.
    fn range(&self) -> (u32, u32) {
        match self.kind {
            DirKind::Fixed => (self.size, self.size),
            DirKind::Scalable { min, max } => (min, max),
            DirKind::Threshold(t) => (self.size.saturating_sub(t), self.size.saturating_add(t)),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const INDEX: &str = "\
# A comment
[Icon Theme]
Name=Forge
Name[de]=Schmiede
Inherits=parent-a, parent-b,
Directories=16x16/apps,48x48/apps,scalable/apps,orphan,sizeless
ScaledDirectories=16x16@2/apps

[16x16/apps]
Size=16
Type=Fixed

[16x16@2/apps]
Size = 16
Scale = 2
Type = Fixed

[48x48/apps]
Size=48

[scalable/apps]
Size=64
MinSize=8
MaxSize=256
Type=Scalable

[sizeless]
Type=Fixed

[unlisted]
Size=22
";

    fn dir(path: &str, size: u32, scale: u32, kind: DirKind) -> ThemeDir {
        ThemeDir {
            path: path.into(),
            size,
            scale,
            kind,
        }
    }

    #[test]
    fn parse_reads_parents_and_listed_directories_in_order() {
        let index = ThemeIndex::parse(INDEX);
        assert_eq!(index.inherits, ["parent-a", "parent-b"]);
        assert_eq!(
            index.dirs,
            [
                dir("16x16/apps", 16, 1, DirKind::Fixed),
                dir("48x48/apps", 48, 1, DirKind::Threshold(2)),
                dir(
                    "scalable/apps",
                    64,
                    1,
                    DirKind::Scalable { min: 8, max: 256 }
                ),
                dir("16x16@2/apps", 16, 2, DirKind::Fixed),
            ]
        );
    }

    #[test]
    fn parse_defaults_min_and_max_to_size_and_skips_bad_numbers() {
        let index = ThemeIndex::parse(
            "[Icon Theme]\nDirectories=a,b,c\n\
             [a]\nSize=32\nType=Scalable\n\
             [b]\nSize=32\nScale=two\n\
             [c]\nSize=24\nScale=0\nThreshold=5\n",
        );
        assert_eq!(
            index.dirs,
            [
                dir("a", 32, 1, DirKind::Scalable { min: 32, max: 32 }),
                dir("c", 24, 1, DirKind::Threshold(5)),
            ]
        );
    }

    #[test]
    fn parse_without_a_header_yields_nothing() {
        assert_eq!(
            ThemeIndex::parse("[16x16]\nSize=16\n"),
            ThemeIndex::default()
        );
    }

    #[test]
    fn matches_follow_type_size_and_scale() {
        let fixed = dir("f", 32, 1, DirKind::Fixed);
        assert!(fixed.matches(32, 1));
        assert!(!fixed.matches(31, 1));
        assert!(!fixed.matches(32, 2));
        let scalable = dir("s", 64, 2, DirKind::Scalable { min: 8, max: 256 });
        assert!(scalable.matches(8, 2) && scalable.matches(256, 2));
        assert!(!scalable.matches(257, 2) && !scalable.matches(64, 1));
        let threshold = dir("t", 48, 1, DirKind::Threshold(4));
        assert!(threshold.matches(44, 1) && threshold.matches(52, 1));
        assert!(!threshold.matches(43, 1) && !threshold.matches(53, 1));
    }

    #[test]
    fn distance_compares_pixel_sizes() {
        let fixed = dir("f", 32, 2, DirKind::Fixed); // 64 px
        assert_eq!(fixed.distance(32, 2), 0);
        assert_eq!(fixed.distance(48, 1), 16);
        assert_eq!(fixed.distance(40, 2), 16);
        let scalable = dir("s", 64, 1, DirKind::Scalable { min: 16, max: 128 });
        assert_eq!(scalable.distance(8, 1), 8);
        assert_eq!(scalable.distance(64, 2), 0);
        assert_eq!(scalable.distance(100, 2), 72);
        let threshold = dir("t", 48, 1, DirKind::Threshold(4)); // 44..=52
        assert_eq!(threshold.distance(40, 1), 4);
        assert_eq!(threshold.distance(50, 1), 0);
        assert_eq!(threshold.distance(32, 2), 12);
    }
}
