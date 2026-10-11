//! Watching the `applications` directories so the daemon's app index stays
//! current (spec §4.7), with inotify through rustix as the config watcher
//! ([`crate::config::watch`]) does: no watcher crate, no async runtime.
//!
//! - Every directory the last scan read is watched for files created,
//!   written, moved or deleted (inotify does not recurse: subdirectories are
//!   watched one by one, and a new one shows up as a change of its parent).
//! - An `applications` directory that does not exist yet is watched through
//!   its nearest existing ancestor, for the creation of the next directory
//!   on the way.
//!
//! A burst of events (a package manager writes many files) is reported once,
//! when it has been quiet for [`SETTLE`] (at most [`MAX_SETTLE`] after it
//! began).

use std::collections::HashMap;
use std::ffi::OsStr;
use std::io;
use std::mem::MaybeUninit;
use std::os::fd::OwnedFd;
use std::os::unix::ffi::OsStrExt;
use std::path::PathBuf;
use std::time::{Duration, Instant};

use rustix::event::{PollFd, PollFlags, Timespec, poll};
use rustix::fs::inotify::{self, CreateFlags, ReadFlags, WatchFlags};
use rustix::io::Errno;

/// How long a burst must be quiet before it is reported.
pub const SETTLE: Duration = Duration::from_millis(100);
/// The longest a burst is held back.
pub const MAX_SETTLE: Duration = Duration::from_secs(1);

const EVENTS: WatchFlags = WatchFlags::CLOSE_WRITE
    .union(WatchFlags::CREATE)
    .union(WatchFlags::DELETE)
    .union(WatchFlags::MOVED_FROM)
    .union(WatchFlags::MOVED_TO)
    .union(WatchFlags::DELETE_SELF)
    .union(WatchFlags::MOVE_SELF)
    .union(WatchFlags::ONLYDIR);

/// What a watch descriptor watches.
#[derive(Clone, Debug)]
enum Watched {
    /// A directory entries are read from.
    Read,
    /// An ancestor of a missing `applications` directory; the names of the
    /// next directories on the way to the missing ones.
    Ancestor(Vec<PathBuf>),
}

/// Watches the app index's directories.
pub struct Watcher {
    inotify: OwnedFd,
    watches: HashMap<i32, (PathBuf, Watched)>,
}

impl Watcher {
    /// A watcher with nothing watched yet.
    pub fn new() -> io::Result<Self> {
        Ok(Self {
            inotify: inotify::init(CreateFlags::CLOEXEC)?,
            watches: HashMap::new(),
        })
    }

    /// Watches `read` (the directories a scan read) and, for each of `roots`
    /// that does not exist, its nearest existing ancestor. Replaces what was
    /// watched before.
    pub fn arm(&mut self, read: &[PathBuf], roots: &[PathBuf]) {
        for wd in self.watches.keys() {
            let _ = inotify::remove_watch(&self.inotify, *wd);
        }
        self.watches.clear();
        for dir in read {
            if let Ok(wd) = inotify::add_watch(&self.inotify, dir, EVENTS) {
                self.watches.insert(wd, (dir.clone(), Watched::Read));
            }
        }
        for root in roots.iter().filter(|r| !r.is_dir()) {
            let Some(ancestor) = root.ancestors().skip(1).find(|d| d.is_dir()) else {
                continue;
            };
            let Ok(wd) = inotify::add_watch(&self.inotify, ancestor, EVENTS) else {
                continue;
            };
            let entry = self
                .watches
                .entry(wd)
                .or_insert_with(|| (ancestor.to_owned(), Watched::Ancestor(Vec::new())));
            if let (_, Watched::Ancestor(next)) = entry
                && let Some(name) = root
                    .strip_prefix(ancestor)
                    .ok()
                    .and_then(|r| r.iter().next())
            {
                next.push(PathBuf::from(name));
            }
        }
    }

    /// Waits for a settled burst of changes; `false` if `timeout` passed
    /// first (`None` waits for ever).
    pub fn wait(&mut self, timeout: Option<Duration>) -> io::Result<bool> {
        let deadline = timeout.map(|t| Instant::now() + t);
        let mut buf = [MaybeUninit::uninit(); 4096];
        loop {
            let left = deadline.map(|d| d.saturating_duration_since(Instant::now()));
            if !readable(&self.inotify, left)? {
                return Ok(false);
            }
            let mut relevant = self.read(&mut buf)?;
            let start = Instant::now();
            while start.elapsed() < MAX_SETTLE && readable(&self.inotify, Some(SETTLE))? {
                relevant |= self.read(&mut buf)?;
            }
            if relevant {
                return Ok(true);
            }
        }
    }

    /// Reads the pending events; whether any is a change of the index.
    fn read(&self, buf: &mut [MaybeUninit<u8>]) -> Result<bool, Errno> {
        let mut reader = inotify::Reader::new(&self.inotify, buf);
        let mut relevant = false;
        loop {
            match reader.next() {
                Ok(event) => relevant |= self.concerns(&event),
                Err(Errno::INTR) => continue,
                Err(Errno::AGAIN) => return Ok(relevant),
                Err(e) => return Err(e),
            }
            if reader.is_buffer_empty() {
                return Ok(relevant);
            }
        }
    }

    fn concerns(&self, event: &inotify::Event<'_>) -> bool {
        if event.events().contains(ReadFlags::QUEUE_OVERFLOW) {
            return true;
        }
        match self.watches.get(&event.wd()) {
            Some((_, Watched::Read)) => true,
            Some((_, Watched::Ancestor(next))) => {
                let created = ReadFlags::CREATE | ReadFlags::MOVED_TO;
                let name = event.file_name().map(|n| OsStr::from_bytes(n.to_bytes()));
                event
                    .events()
                    .intersects(ReadFlags::DELETE_SELF | ReadFlags::MOVE_SELF)
                    || (event.events().intersects(created)
                        && name.is_some_and(|n| next.iter().any(|p| p.as_os_str() == n)))
            }
            None => false,
        }
    }
}

/// Whether `fd` becomes readable within `wait` (`None`: for ever).
fn readable(fd: &OwnedFd, wait: Option<Duration>) -> Result<bool, Errno> {
    let mut fds = [PollFd::new(fd, PollFlags::IN)];
    let timeout = wait.map(|w| Timespec {
        tv_sec: w.as_secs() as i64,
        tv_nsec: w.subsec_nanos().into(),
    });
    match poll(&mut fds, timeout.as_ref()) {
        Ok(n) => Ok(n > 0),
        Err(Errno::INTR) => Ok(true),
        Err(e) => Err(e),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread;

    fn scratch(name: &str) -> PathBuf {
        let dir =
            std::env::temp_dir().join(format!("logis-appwatch-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    const SOON: Option<Duration> = Some(Duration::from_secs(3));
    const QUIET: Option<Duration> = Some(Duration::from_millis(200));

    /// Runs `change` on another thread a moment after the watcher waits.
    fn after(change: impl FnOnce() + Send + 'static) -> thread::JoinHandle<()> {
        thread::spawn(move || {
            thread::sleep(Duration::from_millis(50));
            change();
        })
    }

    #[test]
    fn a_written_or_removed_entry_is_a_change() {
        let dir = scratch("write");
        let apps = dir.join("applications");
        std::fs::create_dir_all(apps.join("sub")).unwrap();
        let mut w = Watcher::new().unwrap();
        w.arm(&[apps.clone(), apps.join("sub")], std::slice::from_ref(&apps));
        assert!(!w.wait(QUIET).unwrap(), "nothing changed yet");
        let file = apps.join("sub/oil.desktop");
        let writer = after({
            let file = file.clone();
            move || std::fs::write(file, b"[Desktop Entry]\n").unwrap()
        });
        assert!(w.wait(SOON).unwrap(), "a new file in a subdirectory");
        writer.join().unwrap();
        let remover = after(move || std::fs::remove_file(file).unwrap());
        assert!(w.wait(SOON).unwrap(), "a removed file");
        remover.join().unwrap();
        std::fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn a_missing_applications_directory_is_watched_through_its_ancestor() {
        let dir = scratch("missing");
        let apps = dir.join("share/applications");
        let mut w = Watcher::new().unwrap();
        w.arm(&[], std::slice::from_ref(&apps));
        let noise = after({
            let dir = dir.clone();
            move || std::fs::write(dir.join("unrelated"), b"x").unwrap()
        });
        assert!(
            !w.wait(QUIET).unwrap(),
            "other files in the ancestor are not changes"
        );
        noise.join().unwrap();
        let maker = after({
            let share = dir.join("share");
            move || std::fs::create_dir(share).unwrap()
        });
        assert!(
            w.wait(SOON).unwrap(),
            "the next directory on the way appeared"
        );
        maker.join().unwrap();
        std::fs::remove_dir_all(dir).unwrap();
    }
}
