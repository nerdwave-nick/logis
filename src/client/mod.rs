//! The client role: every user-facing invocation (spec §4.1). It connects to
//! `$XDG_RUNTIME_DIR/logis.sock`, starting the daemon itself when none
//! answers, and greets it before opening a session.
//!
//! The Wayland preflight is the daemon's: it owns the compositor connection.
//! A client whose daemon cannot start relays the daemon's reason and exits 2.

pub mod dmenu;
pub mod provider;
mod spawn;

use std::io;
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};
use std::thread;
use std::time::{Duration, Instant};

use crate::protocol::frame::{FrameError, read_frame, write_frame};
use crate::protocol::{self, Reply, Request};

/// How long a client waits for a daemon to start and greet it before giving
/// up. The first-open budget (spec §4.10) is a target for the whole path, not
/// this limit, which only bounds a daemon that hangs at start.
const START_PATIENCE: Duration = Duration::from_secs(5);

/// How often a daemon may hang up before greeting before the client gives up.
const MAX_HANG_UPS: u32 = 3;

/// A greeted connection to the daemon.
pub struct Connection {
    stream: UnixStream,
}

impl Connection {
    pub fn send(&mut self, request: &Request) -> Result<(), String> {
        write_frame(&mut self.stream, request).map_err(|e| self.silent(e))
    }

    /// The next reply; `None` when the daemon hung up.
    pub fn recv(&mut self) -> Result<Option<Reply>, String> {
        read_frame(&mut self.stream).map_err(|e| self.silent(e))
    }

    /// A second handle for sending from another thread.
    pub fn try_clone(&self) -> Result<Self, String> {
        let stream = self.stream.try_clone().map_err(|e| e.to_string())?;
        Ok(Self { stream })
    }

    fn silent(&self, e: FrameError) -> String {
        format!("the machine spirit's voice broke: {e}")
    }
}

/// Connects to the daemon, starting it if none answers, and greets it.
/// A daemon this client starts reads `config` (`--config`); one already
/// running keeps the config it was started with.
pub fn connect(config: Option<&Path>) -> Result<Connection, String> {
    let runtime_dir = std::env::var_os("XDG_RUNTIME_DIR").map(PathBuf::from);
    let path = protocol::socket_path(runtime_dir.as_deref())
        .map_err(|e| format!("the machine spirit has no dwelling: {e}"))?;
    let deadline = Instant::now() + START_PATIENCE;
    let mut started = false;
    let mut hang_ups = 0;
    loop {
        let retry = match UnixStream::connect(&path) {
            Ok(stream) => match greet(stream, deadline)? {
                Some(connection) => return Ok(connection),
                // The daemon left as we arrived (idle timeout): try again,
                // but not forever (a socket unit whose daemon keeps failing).
                None if hang_ups < MAX_HANG_UPS => {
                    hang_ups += 1;
                    true
                }
                None => {
                    return Err(format!(
                        "the machine spirit at {} hangs up before greeting; \
                         if systemd starts it, see journalctl --user -u logis",
                        path.display()
                    ));
                }
            },
            Err(e)
                if matches!(
                    e.kind(),
                    io::ErrorKind::NotFound | io::ErrorKind::ConnectionRefused
                ) =>
            {
                if !started {
                    spawn::start_daemon(deadline, config)?;
                    started = true;
                    continue;
                }
                // Another daemon won the race to start and is not listening yet.
                true
            }
            Err(e) => {
                return Err(format!(
                    "cannot reach the machine spirit at {}: {e}",
                    path.display()
                ));
            }
        };
        if retry && Instant::now() >= deadline {
            return Err(format!(
                "the machine spirit at {} did not answer within {} s",
                path.display(),
                START_PATIENCE.as_secs()
            ));
        }
        thread::sleep(Duration::from_millis(2));
    }
}

/// Says hello. `None` if the daemon hung up (or broke off) before welcoming
/// us, as one that is just exiting does.
fn greet(stream: UnixStream, deadline: Instant) -> Result<Option<Connection>, String> {
    let mut connection = Connection { stream };
    if connection
        .send(&Request::Hello {
            version: protocol::VERSION,
        })
        .is_err()
    {
        return Ok(None);
    }
    let wait = deadline
        .saturating_duration_since(Instant::now())
        .max(Duration::from_millis(1));
    let _ = connection.stream.set_read_timeout(Some(wait));
    match connection.recv() {
        Ok(Some(Reply::Welcome { .. })) => {}
        Ok(Some(Reply::Error { message })) => return Err(message),
        Ok(Some(other)) => return Err(format!("the machine spirit spoke out of turn: {other:?}")),
        Ok(None) | Err(_) => return Ok(None),
    }
    let _ = connection.stream.set_read_timeout(None);
    Ok(Some(connection))
}
