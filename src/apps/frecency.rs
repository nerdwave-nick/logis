//! Frecency (spec §3 story 7): how often and how recently each application
//! was launched, persisted in `$XDG_STATE_HOME/logis/frecency.json`
//! (default `~/.local/state`) and turned into a boost for its match score.
//!
//! Each launch adds 1 to an application's rank and stamps the time. The
//! boost weighs the rank by how long ago the last launch was (×4 within an
//! hour, ×2 within a day, ×½ within a week, ×¼ after that) and grows with
//! its logarithm, at most [`MAX_BOOST`], so a much better match still wins
//! over a favourite. When the ranks add up to more than [`MAX_TOTAL`], all
//! of them age by a tenth and the faded ones are forgotten.
//!
//! The file is replaced atomically (written beside it, synced, renamed), so
//! a crash leaves the old or the new one, never half of either.

use std::collections::HashMap;
use std::io::{self, Write};
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

/// The largest boost frecency adds to a match score.
pub const MAX_BOOST: u32 = 48;
/// The ranks' sum above which they all age.
pub const MAX_TOTAL: f64 = 1000.0;

/// The launches of every application, by desktop-file ID.
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct Frecency {
    version: u32,
    apps: HashMap<String, Use>,
}

#[derive(Clone, Copy, Debug, PartialEq, Serialize, Deserialize)]
struct Use {
    rank: f64,
    /// Seconds since the Unix epoch.
    last: u64,
}

impl Frecency {
    /// The file format's version.
    const VERSION: u32 = 1;

    /// Where the frecency file lives in an environment.
    pub fn path_from_vars(get: impl Fn(&str) -> Option<String>) -> Option<PathBuf> {
        let absolute = |p: &PathBuf| p.is_absolute();
        get("XDG_STATE_HOME")
            .map(PathBuf::from)
            .filter(absolute)
            .or_else(|| {
                get("HOME")
                    .map(PathBuf::from)
                    .filter(absolute)
                    .map(|h| h.join(".local/state"))
            })
            .map(|state| state.join("logis/frecency.json"))
    }

    /// Reads the file; a missing one is no history yet.
    pub fn load(path: &Path) -> Result<Self, String> {
        match std::fs::read(path) {
            Ok(bytes) => serde_json::from_slice(&bytes)
                .map_err(|e| format!("{} is unreadable: {e}", path.display())),
            Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(Self::default()),
            Err(e) => Err(format!("{} is unreadable: {e}", path.display())),
        }
    }

    /// Writes the file atomically, creating its directory.
    pub fn save(&self, path: &Path) -> io::Result<()> {
        let mut doc = self.clone();
        doc.version = Self::VERSION;
        let bytes = serde_json::to_vec(&doc).map_err(io::Error::other)?;
        write_atomically(path, &bytes)
    }

    /// Notes a launch of `id` at `now` (seconds since the epoch).
    pub fn record(&mut self, id: &str, now: u64) {
        let used = self.apps.entry(id.to_owned()).or_insert(Use {
            rank: 0.0,
            last: now,
        });
        used.rank += 1.0;
        used.last = used.last.max(now);
        if self.apps.values().map(|u| u.rank).sum::<f64>() > MAX_TOTAL {
            for used in self.apps.values_mut() {
                used.rank *= 0.9;
            }
            self.apps.retain(|_, u| u.rank >= 1.0);
        }
    }

    /// How much `id`'s match score is raised at `now`.
    pub fn boost(&self, id: &str, now: u64) -> u32 {
        let Some(used) = self.apps.get(id) else {
            return 0;
        };
        let age = now.saturating_sub(used.last);
        let weight = match age {
            a if a < 3600 => 4.0,
            a if a < 24 * 3600 => 2.0,
            a if a < 7 * 24 * 3600 => 0.5,
            _ => 0.25,
        };
        let boost = (8.0 * (used.rank * weight).ln_1p()).round();
        (boost as u32).clamp(1, MAX_BOOST)
    }
}

/// Seconds since the Unix epoch, now.
pub fn now() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_or(0, |d| d.as_secs())
}

/// Replaces `path` with `bytes`: a temporary file beside it, synced, then
/// renamed over it.
fn write_atomically(path: &Path, bytes: &[u8]) -> io::Result<()> {
    let dir = path
        .parent()
        .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidInput, "no directory"))?;
    std::fs::create_dir_all(dir)?;
    let name = path.file_name().unwrap_or_default().to_string_lossy();
    let temporary = dir.join(format!(".{name}.{}.tmp", std::process::id()));
    let written = std::fs::File::create(&temporary).and_then(|mut file| {
        file.write_all(bytes)?;
        file.sync_all()
    });
    match written.and_then(|()| std::fs::rename(&temporary, path)) {
        Ok(()) => Ok(()),
        Err(e) => {
            let _ = std::fs::remove_file(&temporary);
            Err(e)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const NOW: u64 = 1_760_000_000;
    const HOUR: u64 = 3600;
    const DAY: u64 = 24 * HOUR;

    #[test]
    fn the_file_lives_in_the_state_home() {
        let get = |pairs: &'static [(&'static str, &'static str)]| {
            move |n: &str| {
                pairs
                    .iter()
                    .find(|(k, _)| *k == n)
                    .map(|(_, v)| (*v).to_owned())
            }
        };
        assert_eq!(
            Frecency::path_from_vars(get(&[("XDG_STATE_HOME", "/state"), ("HOME", "/h")])),
            Some(PathBuf::from("/state/logis/frecency.json"))
        );
        assert_eq!(
            Frecency::path_from_vars(get(&[("XDG_STATE_HOME", "rel"), ("HOME", "/h")])),
            Some(PathBuf::from("/h/.local/state/logis/frecency.json"))
        );
        assert_eq!(Frecency::path_from_vars(get(&[])), None);
    }

    #[test]
    fn launches_raise_the_boost_and_recent_ones_more() {
        let mut f = Frecency::default();
        assert_eq!(f.boost("oil.desktop", NOW), 0);
        f.record("oil.desktop", NOW);
        let once = f.boost("oil.desktop", NOW);
        assert!(once > 0);
        for _ in 0..9 {
            f.record("oil.desktop", NOW);
        }
        let often = f.boost("oil.desktop", NOW);
        assert!(often > once, "{often} > {once}");
        let later = |dt| f.boost("oil.desktop", NOW + dt);
        assert!(later(2 * HOUR) < often);
        assert!(later(2 * DAY) < later(2 * HOUR));
        assert!(later(30 * DAY) < later(2 * DAY));
        assert!(later(30 * DAY) > 0, "never forgotten by age alone");
        assert_eq!(f.boost("fire.desktop", NOW), 0);
    }

    #[test]
    fn the_boost_is_bounded() {
        let mut f = Frecency::default();
        for _ in 0..900 {
            f.record("oil.desktop", NOW);
        }
        assert_eq!(f.boost("oil.desktop", NOW), MAX_BOOST);
    }

    #[test]
    fn ranks_age_once_they_add_up_too_high() {
        let mut f = Frecency::default();
        f.record("once.desktop", NOW);
        for _ in 0..1000 {
            f.record("oil.desktop", NOW);
        }
        assert!(f.apps["oil.desktop"].rank < 1000.0, "aged");
        assert!(
            !f.apps.contains_key("once.desktop"),
            "faded ranks are forgotten"
        );
    }

    fn scratch(name: &str) -> PathBuf {
        let dir =
            std::env::temp_dir().join(format!("logis-frecency-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        dir
    }

    #[test]
    fn a_saved_history_loads_back_and_a_missing_file_is_empty() {
        let dir = scratch("roundtrip");
        let path = dir.join("logis/frecency.json");
        assert_eq!(Frecency::load(&path), Ok(Frecency::default()));
        let mut f = Frecency::default();
        f.record("oil.desktop", NOW);
        f.save(&path).unwrap();
        let back = Frecency::load(&path).unwrap();
        assert_eq!(back.boost("oil.desktop", NOW), f.boost("oil.desktop", NOW));
        let leftovers: Vec<_> = std::fs::read_dir(path.parent().unwrap())
            .unwrap()
            .flatten()
            .map(|e| e.file_name())
            .collect();
        assert_eq!(leftovers, ["frecency.json"], "no temporary file is left");
        std::fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn a_corrupt_file_is_an_error() {
        let dir = scratch("corrupt");
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("frecency.json");
        std::fs::write(&path, b"{ heresy").unwrap();
        assert!(Frecency::load(&path).is_err());
        std::fs::remove_dir_all(dir).unwrap();
    }
}
