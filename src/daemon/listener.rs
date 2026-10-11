//! The daemon socket: claiming it (one daemon per runtime directory), or
//! taking it from systemd socket activation, and turning each connection into
//! [`Event`]s on its own reader thread.

use std::fs::{File, OpenOptions, TryLockError};
use std::io;
use std::net::Shutdown;
use std::os::fd::FromRawFd;
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::{Path, PathBuf};
use std::sync::mpsc::Sender;
use std::thread;
use std::time::{Duration, Instant};

use super::core::{Event, Peer};
use crate::protocol::frame::{FrameError, read_frame, write_frame};
use crate::protocol::{Reply, Request, SOCKET_NAME};

/// The lock file beside the socket; whoever holds it owns the socket path.
const LOCK_NAME: &str = "logis.lock";

/// How long to wait for a daemon that holds the lock but does not answer yet
/// (it is starting or shutting down).
const CONTENDED: Duration = Duration::from_secs(2);

/// A client that stops reading must not stall the daemon.
const WRITE_TIMEOUT: Duration = Duration::from_secs(2);

/// The outcome of claiming the socket.
pub enum Claim {
    /// This daemon listens.
    Listening(Listening),
    /// Another daemon already answers on the socket.
    AlreadyServed,
}

/// The listening socket; removes its path on drop unless systemd owns it.
pub struct Listening {
    pub listener: UnixListener,
    pub path: PathBuf,
    owns_path: bool,
    _lock: Option<File>,
}

impl Drop for Listening {
    fn drop(&mut self) {
        if self.owns_path {
            let _ = std::fs::remove_file(&self.path);
        }
    }
}

/// Takes the socket from systemd, or claims `<runtime_dir>/logis.sock`.
pub fn claim(runtime_dir: &Path) -> io::Result<Claim> {
    let path = runtime_dir.join(SOCKET_NAME);
    if let Some(listener) = activated()? {
        let _lock = lock(runtime_dir).ok().flatten();
        return Ok(Claim::Listening(Listening {
            listener,
            path,
            owns_path: false,
            _lock,
        }));
    }
    let deadline = Instant::now() + CONTENDED;
    loop {
        if let Some(lock) = lock(runtime_dir)? {
            // Holding the lock, any socket file left there is stale.
            match std::fs::remove_file(&path) {
                Err(e) if e.kind() != io::ErrorKind::NotFound => return Err(e),
                _ => {}
            }
            let listener = UnixListener::bind(&path)?;
            let _lock = Some(lock);
            return Ok(Claim::Listening(Listening {
                listener,
                path,
                owns_path: true,
                _lock,
            }));
        }
        if UnixStream::connect(&path).is_ok() {
            return Ok(Claim::AlreadyServed);
        }
        if Instant::now() >= deadline {
            return Err(io::Error::other(format!(
                "another machine spirit holds {} but does not answer",
                runtime_dir.join(LOCK_NAME).display()
            )));
        }
        thread::sleep(Duration::from_millis(5));
    }
}

/// The exclusive lock on `<runtime_dir>/logis.lock`, or `None` if it is held.
fn lock(runtime_dir: &Path) -> io::Result<Option<File>> {
    let file = OpenOptions::new()
        .create(true)
        .truncate(false)
        .write(true)
        .open(runtime_dir.join(LOCK_NAME))?;
    match file.try_lock() {
        Ok(()) => Ok(Some(file)),
        Err(TryLockError::WouldBlock) => Ok(None),
        Err(TryLockError::Error(e)) => Err(e),
    }
}

/// The socket passed by systemd socket activation (`logis.socket`), if any.
fn activated() -> io::Result<Option<UnixListener>> {
    let var = |name| std::env::var(name).ok().and_then(|v| v.parse::<u32>().ok());
    if var("LISTEN_PID") != Some(std::process::id()) || var("LISTEN_FDS").unwrap_or(0) < 1 {
        return Ok(None);
    }
    // SAFETY: with LISTEN_PID naming this process, systemd passed its first
    // socket as fd 3 (SD_LISTEN_FDS_START) and nothing else here owns it.
    let inherited = unsafe { UnixListener::from_raw_fd(3) };
    // The duplicate is close-on-exec, so launched apps do not inherit it;
    // dropping `inherited` closes fd 3.
    inherited.try_clone().map(Some)
}

/// Accepts connections on a thread; each gets a reader thread of its own.
pub fn spawn_acceptor(listener: UnixListener, events: Sender<Event>) {
    thread::spawn(move || {
        for (conn, stream) in (1..).zip(listener.incoming()) {
            let Ok(stream) = stream else {
                // Out of descriptors or similar: do not spin.
                thread::sleep(Duration::from_millis(10));
                continue;
            };
            let Ok(reader) = stream.try_clone() else {
                continue;
            };
            let _ = stream.set_write_timeout(Some(WRITE_TIMEOUT));
            let peer = Box::new(StreamPeer(stream));
            if events.send(Event::Connected { conn, peer }).is_err() {
                return;
            }
            let events = events.clone();
            thread::spawn(move || read_requests(conn, reader, &events));
        }
    });
}

fn read_requests(conn: u64, mut stream: UnixStream, events: &Sender<Event>) {
    loop {
        let request = match read_frame::<_, Request>(&mut stream) {
            Ok(Some(request)) => Ok(request),
            Ok(None) | Err(FrameError::Io(_) | FrameError::Truncated) => break,
            Err(e) => Err(e.to_string()),
        };
        let garbled = request.is_err();
        if events.send(Event::Received { conn, request }).is_err() || garbled {
            break;
        }
    }
    let _ = events.send(Event::Disconnected { conn });
}

struct StreamPeer(UnixStream);

impl Peer for StreamPeer {
    fn send(&mut self, reply: &Reply) {
        // A failed write surfaces as the reader thread's disconnect.
        let _ = write_frame(&mut self.0, reply);
    }

    fn close(&mut self) {
        let _ = self.0.shutdown(Shutdown::Both);
    }
}
