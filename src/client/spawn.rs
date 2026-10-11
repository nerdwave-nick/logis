//! Starting the daemon from a client (spec §4.1): `logis daemon` as a child
//! in its own process group, detached from the client's stdio. Its stdout is
//! one end of a socket pair: the daemon's readiness line arrives there, and
//! end of stream without it means the daemon gave up, its reason on stderr.

use std::io::Read;
use std::os::fd::OwnedFd;
use std::os::unix::net::UnixStream;
use std::os::unix::process::CommandExt;
use std::process::{Command, Stdio};
use std::time::Instant;

/// Starts the daemon and waits until it listens, at most until `deadline`.
pub fn start_daemon(deadline: Instant) -> Result<(), String> {
    let exe = std::env::current_exe()
        .map_err(|e| format!("cannot find its own binary to rouse the machine spirit: {e}"))?;
    let (mut ready, theirs) =
        UnixStream::pair().map_err(|e| format!("cannot rouse the machine spirit: {e}"))?;
    let mut child = Command::new(exe)
        .args(["daemon", "--detach"])
        .stdin(Stdio::null())
        .stdout(Stdio::from(OwnedFd::from(theirs)))
        .stderr(Stdio::piped())
        // Ctrl+C in the client's terminal must not reach the daemon.
        .process_group(0)
        .spawn()
        .map_err(|e| format!("cannot rouse the machine spirit: {e}"))?;

    let wait = deadline.saturating_duration_since(Instant::now());
    let _ = ready.set_read_timeout(Some(wait.max(std::time::Duration::from_millis(1))));
    let mut byte = [0];
    match ready.read(&mut byte) {
        Ok(1..) => Ok(()),
        Ok(0) => {
            let status = child.wait().map_err(|e| e.to_string())?;
            let mut reason = String::new();
            if let Some(mut stderr) = child.stderr.take() {
                let _ = stderr.read_to_string(&mut reason);
            }
            let reason = reason.trim();
            let reason = reason.strip_prefix("logis: ").unwrap_or(reason);
            Err(if reason.is_empty() {
                format!("the machine spirit could not be roused ({status})")
            } else {
                format!("the machine spirit could not be roused: {reason}")
            })
        }
        Err(e) => {
            let _ = child.kill();
            let _ = child.wait();
            Err(format!(
                "the machine spirit did not wake within {:.1} s ({e})",
                wait.as_secs_f64()
            ))
        }
    }
}
