//! Watching the config file (or every theme in the themes directory) for
//! the daemon's live reload, with inotify through rustix (already in the
//! tree; no watcher crate, no async runtime).
//!
//! Two kinds of watch cover how editors save:
//! - the directory the files live in, for writes in place
//!   (`IN_CLOSE_WRITE`) and for atomic replacement by rename (`IN_MOVED_TO`)
//!   or deletion; while that directory does not exist yet, its nearest
//!   existing ancestor, for its creation;
//! - each watched file itself, followed through a symlink, for edits to a
//!   link's target (dotfile managers link the config from elsewhere).
//!
//! A burst of events (an editor's save is several) settles for a moment and
//! is reported once. The watch thread prints nothing: a daemon started by a
//! client outlives that client's stderr.

use std::ffi::{OsStr, OsString};
use std::io;
use std::mem::MaybeUninit;
use std::os::fd::OwnedFd;
use std::os::unix::ffi::OsStrExt;
use std::path::{Path, PathBuf};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use rustix::event::{PollFd, PollFlags, Timespec, poll};
use rustix::fs::inotify::{self, CreateFlags, ReadFlags, WatchFlags};
use rustix::io::Errno;

/// How long a burst of events must be quiet before it is reported.
const SETTLE: Duration = Duration::from_millis(30);
/// The longest a burst is held back, however busy the file is.
const MAX_SETTLE: Duration = Duration::from_millis(300);

/// Events on the watched directory (`ONLYDIR`: it must stay a directory).
const DIR_EVENTS: WatchFlags = WatchFlags::CLOSE_WRITE
    .union(WatchFlags::MOVED_TO)
    .union(WatchFlags::MOVED_FROM)
    .union(WatchFlags::CREATE)
    .union(WatchFlags::DELETE)
    .union(WatchFlags::DELETE_SELF)
    .union(WatchFlags::MOVE_SELF)
    .union(WatchFlags::ONLYDIR);

/// Events on the file (or a symlink's target).
const FILE_EVENTS: WatchFlags = WatchFlags::CLOSE_WRITE
    .union(WatchFlags::DELETE_SELF)
    .union(WatchFlags::MOVE_SELF);

/// Which files in the directory are watched.
enum Names {
    /// One file of this name.
    One(OsString),
    /// Every file with this extension, hidden ones aside.
    Extension(&'static str),
}

impl Names {
    fn matches(&self, name: &OsStr) -> bool {
        match self {
            Self::One(one) => name == one,
            Self::Extension(ext) => {
                let path = Path::new(name);
                !name.as_bytes().starts_with(b".")
                    && path.extension().is_some_and(|e| e == OsStr::new(ext))
            }
        }
    }
}

/// Watches one config file, or every theme in a directory.
pub struct Watcher {
    inotify: OwnedFd,
    /// The watched files' names in `dir`.
    names: Names,
    /// The directory the files live in.
    dir: PathBuf,
    /// The watched directory: `dir`, or its nearest existing ancestor.
    dir_watch: Option<(i32, PathBuf)>,
    file_watches: Vec<i32>,
}

impl Watcher {
    /// Starts watching `file`. Changes from now on are reported once
    /// [`Watcher::spawn`] runs, so read the file after this.
    pub fn new(file: &Path) -> io::Result<Self> {
        let (Some(dir), Some(name)) = (file.parent(), file.file_name()) else {
            return Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                format!("{} names no file in a directory", file.display()),
            ));
        };
        Self::start(dir, Names::One(name.to_owned()))
    }

    /// Starts watching every file in `dir` with the extension `extension`
    /// (the themes directory and its `*.jsonc`); `dir` may not exist yet.
    pub fn files_in(dir: &Path, extension: &'static str) -> io::Result<Self> {
        Self::start(dir, Names::Extension(extension))
    }

    fn start(dir: &Path, names: Names) -> io::Result<Self> {
        let inotify = inotify::init(CreateFlags::CLOEXEC)?;
        let mut watcher = Self {
            inotify,
            names,
            dir: dir.to_owned(),
            dir_watch: None,
            file_watches: Vec::new(),
        };
        watcher.arm();
        if watcher.dir_watch.is_none() {
            return Err(io::Error::new(
                io::ErrorKind::NotFound,
                format!(
                    "no directory on the way to {} can be watched",
                    dir.display()
                ),
            ));
        }
        Ok(watcher)
    }

    /// Calls `changed` on a thread of its own after every settled burst of
    /// changes to the file, until it returns `false`.
    pub fn spawn(mut self, mut changed: impl FnMut() -> bool + Send + 'static) -> JoinHandle<()> {
        thread::spawn(move || {
            let mut buf = [MaybeUninit::uninit(); 4096];
            while let Ok(burst) = self.next_burst(&mut buf) {
                self.arm();
                if burst && !changed() {
                    return;
                }
            }
        })
    }

    /// Waits for events until they settle; whether any concerned the file.
    fn next_burst(&self, buf: &mut [MaybeUninit<u8>]) -> Result<bool, Errno> {
        let mut relevant = self.read(buf)?;
        let start = Instant::now();
        while start.elapsed() < MAX_SETTLE && self.more_within(SETTLE)? {
            relevant |= self.read(buf)?;
        }
        Ok(relevant)
    }

    /// Reads what is pending (blocking until something is).
    fn read(&self, buf: &mut [MaybeUninit<u8>]) -> Result<bool, Errno> {
        let mut reader = inotify::Reader::new(&self.inotify, buf);
        let mut relevant = false;
        loop {
            match reader.next() {
                Ok(event) => relevant |= self.concerns_the_file(&event),
                Err(Errno::INTR) => continue,
                Err(e) => return Err(e),
            }
            if reader.is_buffer_empty() {
                return Ok(relevant);
            }
        }
    }

    fn more_within(&self, wait: Duration) -> Result<bool, Errno> {
        let mut fds = [PollFd::new(&self.inotify, PollFlags::IN)];
        let timeout = Timespec {
            tv_sec: 0,
            tv_nsec: wait.subsec_nanos().into(),
        };
        match poll(&mut fds, Some(&timeout)) {
            Ok(n) => Ok(n > 0),
            Err(Errno::INTR) => Ok(true),
            Err(e) => Err(e),
        }
    }

    fn concerns_the_file(&self, event: &inotify::Event<'_>) -> bool {
        let mask = event.events();
        if mask.contains(ReadFlags::QUEUE_OVERFLOW) || self.file_watches.contains(&event.wd()) {
            return true;
        }
        let Some((wd, watched)) = &self.dir_watch else {
            return false;
        };
        if event.wd() != *wd {
            return false;
        }
        if mask.intersects(ReadFlags::DELETE_SELF | ReadFlags::MOVE_SELF) {
            return true;
        }
        let Some(name) = event.file_name().map(|n| OsStr::from_bytes(n.to_bytes())) else {
            return false;
        };
        if *watched != self.dir {
            // An ancestor: the next directory on the way may have appeared.
            let next = self
                .dir
                .strip_prefix(watched)
                .ok()
                .and_then(|r| r.iter().next());
            return next == Some(name) && mask.intersects(ReadFlags::CREATE | ReadFlags::MOVED_TO);
        }
        if !self.names.matches(name) {
            return false;
        }
        let rewritten = ReadFlags::CLOSE_WRITE
            | ReadFlags::MOVED_TO
            | ReadFlags::MOVED_FROM
            | ReadFlags::DELETE;
        // A plain file just created is reported once written and closed; a
        // symlink is complete as soon as it exists.
        mask.intersects(rewritten)
            || (mask.contains(ReadFlags::CREATE) && self.dir.join(name).is_symlink())
    }

    /// (Re)places the watches: the file may be a new inode, and a directory
    /// on the way may have appeared or vanished.
    fn arm(&mut self) {
        let dir = self
            .dir
            .ancestors()
            .find(|d| d.is_dir())
            .map(Path::to_owned);
        let dir_watch = dir.and_then(|d| {
            let wd = inotify::add_watch(&self.inotify, &d, DIR_EVENTS).ok()?;
            Some((wd, d))
        });
        let old = std::mem::replace(&mut self.dir_watch, dir_watch);
        self.forget(
            old.map(|(wd, _)| wd),
            self.dir_watch.as_ref().map(|(wd, _)| *wd),
        );

        let files: Vec<PathBuf> = match &self.names {
            Names::One(name) => vec![self.dir.join(name)],
            Names::Extension(_) => std::fs::read_dir(&self.dir)
                .into_iter()
                .flatten()
                .flatten()
                .filter(|entry| self.names.matches(&entry.file_name()))
                .map(|entry| entry.path())
                .collect(),
        };
        let watches: Vec<i32> = files
            .iter()
            .filter_map(|f| inotify::add_watch(&self.inotify, f, FILE_EVENTS).ok())
            .collect();
        let old = std::mem::replace(&mut self.file_watches, watches);
        for wd in old {
            if !self.file_watches.contains(&wd) {
                self.forget(Some(wd), None);
            }
        }
    }

    /// Removes a watch that was replaced (one on a gone inode is already).
    fn forget(&self, old: Option<i32>, new: Option<i32>) {
        if let Some(old) = old.filter(|old| Some(*old) != new) {
            let _ = inotify::remove_watch(&self.inotify, old);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::mpsc::{Receiver, channel};

    /// How long a change may take to be reported.
    const PATIENCE: Duration = Duration::from_secs(5);
    /// How long to listen for a report that should not come.
    const QUIET: Duration = Duration::from_millis(300);

    struct Scratch(PathBuf);

    impl Scratch {
        fn new(name: &str) -> Self {
            let dir =
                std::env::temp_dir().join(format!("logis-watch-{}-{name}", std::process::id()));
            let _ = std::fs::remove_dir_all(&dir);
            std::fs::create_dir_all(&dir).unwrap();
            Self(dir)
        }
    }

    impl Drop for Scratch {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn watch(file: &Path) -> Receiver<()> {
        let (tx, rx) = channel();
        Watcher::new(file)
            .unwrap()
            .spawn(move || tx.send(()).is_ok());
        rx
    }

    fn reported(rx: &Receiver<()>) -> bool {
        let got = rx.recv_timeout(PATIENCE).is_ok();
        // Drain the rest of the burst, if it was reported in parts.
        while rx.recv_timeout(QUIET).is_ok() {}
        got
    }

    fn silent(rx: &Receiver<()>) -> bool {
        rx.recv_timeout(QUIET).is_err()
    }

    fn watch_dir(dir: &Path) -> Receiver<()> {
        let (tx, rx) = channel();
        Watcher::files_in(dir, "jsonc")
            .unwrap()
            .spawn(move || tx.send(()).is_ok());
        rx
    }

    #[test]
    fn every_theme_in_a_directory_is_watched_and_nothing_else() {
        let dir = Scratch::new("themes");
        std::fs::write(dir.0.join("forge.jsonc"), "{}").unwrap();
        let rx = watch_dir(&dir.0);
        std::fs::write(dir.0.join("forge.jsonc"), "{\"glow\": {}}").unwrap();
        assert!(reported(&rx), "an edit");
        std::fs::write(dir.0.join("amber.jsonc"), "{}").unwrap();
        assert!(reported(&rx), "a new theme");
        std::fs::write(dir.0.join("notes.txt"), "brass").unwrap();
        std::fs::write(dir.0.join(".forge.jsonc.swp"), "x").unwrap();
        assert!(silent(&rx), "other files");
        std::fs::remove_file(dir.0.join("amber.jsonc")).unwrap();
        assert!(reported(&rx), "a removed theme");
    }

    #[test]
    fn a_themes_directory_created_later_and_a_linked_theme_are_followed() {
        let root = Scratch::new("themes-later");
        let dir = root.0.join("logis/themes");
        let rx = watch_dir(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let target = root.0.join("dotfiles-mine.jsonc");
        std::fs::write(&target, "{}").unwrap();
        std::os::unix::fs::symlink(&target, dir.join("mine.jsonc")).unwrap();
        assert!(reported(&rx), "the linked theme appeared");
        std::fs::write(&target, "{\"glow\": {}}").unwrap();
        assert!(reported(&rx), "its target was edited");
    }

    #[test]
    fn a_write_in_place_is_reported_once() {
        let dir = Scratch::new("in-place");
        let file = dir.0.join("logis.jsonc");
        std::fs::write(&file, "{}").unwrap();
        let rx = watch(&file);
        std::fs::write(&file, "{\"theme\": \"x\"}").unwrap();
        assert!(rx.recv_timeout(PATIENCE).is_ok());
        assert!(silent(&rx), "one save was reported twice");
    }

    #[test]
    fn an_atomic_replacement_is_reported_and_still_watched_after() {
        let dir = Scratch::new("atomic");
        let file = dir.0.join("logis.jsonc");
        std::fs::write(&file, "{}").unwrap();
        let rx = watch(&file);
        for round in 0..2 {
            let tmp = dir.0.join(format!(".logis.jsonc.{round}"));
            std::fs::write(&tmp, "{\"theme\": \"y\"}").unwrap();
            std::fs::rename(&tmp, &file).unwrap();
            assert!(reported(&rx), "round {round}");
        }
    }

    #[test]
    fn a_deleted_file_is_reported() {
        let dir = Scratch::new("deleted");
        let file = dir.0.join("logis.jsonc");
        std::fs::write(&file, "{}").unwrap();
        let rx = watch(&file);
        std::fs::remove_file(&file).unwrap();
        assert!(reported(&rx));
    }

    #[test]
    fn a_file_created_later_is_reported() {
        let dir = Scratch::new("created");
        let file = dir.0.join("logis.jsonc");
        let rx = watch(&file);
        std::fs::write(&file, "{}").unwrap();
        assert!(reported(&rx));
        std::fs::write(&file, "{\"theme\": \"z\"}").unwrap();
        assert!(reported(&rx), "the new file is not watched");
    }

    #[test]
    fn a_directory_created_later_is_followed() {
        let dir = Scratch::new("dir-later");
        let file = dir.0.join("config/logis/logis.jsonc");
        let rx = watch(&file);
        std::fs::create_dir_all(file.parent().unwrap()).unwrap();
        std::fs::write(&file, "{}").unwrap();
        assert!(reported(&rx));
        std::fs::write(&file, "{\"theme\": \"z\"}").unwrap();
        assert!(reported(&rx), "the new directory is not watched");
    }

    #[test]
    fn other_files_beside_it_are_ignored() {
        let dir = Scratch::new("others");
        let file = dir.0.join("logis.jsonc");
        std::fs::write(&file, "{}").unwrap();
        let rx = watch(&file);
        std::fs::write(dir.0.join("frecency.json"), "{}").unwrap();
        std::fs::create_dir(dir.0.join("themes")).unwrap();
        assert!(silent(&rx));
    }

    #[test]
    fn an_edit_to_a_symlinked_files_target_is_reported() {
        let dir = Scratch::new("symlink");
        let target = dir.0.join("dotfiles/logis.jsonc");
        std::fs::create_dir_all(target.parent().unwrap()).unwrap();
        std::fs::write(&target, "{}").unwrap();
        let file = dir.0.join("config/logis.jsonc");
        std::fs::create_dir_all(file.parent().unwrap()).unwrap();
        std::os::unix::fs::symlink(&target, &file).unwrap();
        let rx = watch(&file);
        std::fs::write(&target, "{\"theme\": \"z\"}").unwrap();
        assert!(reported(&rx));
    }

    #[test]
    fn a_new_symlink_in_its_place_is_reported() {
        let dir = Scratch::new("new-symlink");
        let target = dir.0.join("elsewhere.jsonc");
        std::fs::write(&target, "{}").unwrap();
        let file = dir.0.join("logis.jsonc");
        let rx = watch(&file);
        std::os::unix::fs::symlink(&target, &file).unwrap();
        assert!(reported(&rx));
    }
}
