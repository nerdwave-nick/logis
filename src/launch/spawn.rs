//! Starting an application's process so that it outlives logis: in a
//! session of its own (`setsid`: no controlling terminal, not in the
//! daemon's process group, so nothing aimed at the daemon reaches it), with
//! stdin from and stdout/stderr to `/dev/null`, and reaped by a small thread
//! so it never lingers as a zombie while the daemon runs (after the daemon,
//! the subreaper or init inherits it).
//!
//! To go into a scope from its first instruction, a process is *held*: it
//! starts as `/bin/sh` waiting for a line on its stdin, is moved into the
//! scope by pid, then released and replaced by the application (`exec`,
//! same pid). std's spawn returns only after the exec, so holding inside
//! the child before it would block the daemon; the shell is the holder.

use std::ffi::OsString;
use std::io::{self, Write};
use std::os::unix::process::CommandExt;
use std::path::Path;
use std::process::{Child, ChildStdin, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

/// Waits on a line, then becomes the application (`"$@"`).
const HOLD: &str = "read -r _; exec \"$@\" </dev/null";

/// A started process: held, or already the application.
pub struct Started {
    child: Child,
    hold: Option<ChildStdin>,
}

/// Starts `argv` detached, in `env` and `cwd`; `held` keeps it waiting for
/// [`Started::release`].
pub fn start(
    argv: &[String],
    env: &[(OsString, OsString)],
    cwd: &Path,
    held: bool,
) -> io::Result<Started> {
    let mut command = if held {
        let mut sh = Command::new("/bin/sh");
        sh.arg("-c").arg(HOLD).arg("logis-launch").args(argv);
        sh.stdin(Stdio::piped());
        sh
    } else {
        let (program, args) = argv
            .split_first()
            .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidInput, "no program"))?;
        let mut direct = Command::new(program);
        direct.args(args).stdin(Stdio::null());
        direct
    };
    command
        .env_clear()
        .envs(env.iter().map(|(k, v)| (k, v)))
        .current_dir(cwd)
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    // SAFETY: setsid is async-signal-safe and touches no memory of ours.
    unsafe {
        command.pre_exec(|| rustix::process::setsid().map(drop).map_err(io::Error::from));
    }
    let mut child = command.spawn()?;
    let hold = child.stdin.take();
    Ok(Started { child, hold })
}

impl Started {
    /// The process's id (the application's, once released).
    pub fn pid(&self) -> u32 {
        self.child.id()
    }

    /// Lets a held process become the application, and leaves it to be
    /// reaped when it exits.
    pub fn release(mut self) {
        if let Some(mut hold) = self.hold.take() {
            let _ = hold.write_all(b"\n");
        }
        let mut child = self.child;
        let reaper = thread::Builder::new()
            .name("reaper".into())
            .stack_size(64 * 1024)
            .spawn(move || {
                let _ = child.wait();
            });
        if reaper.is_err() {
            // No thread: the process is reaped by whoever inherits it once
            // the daemon is gone; meanwhile it may linger as a zombie.
        }
    }
}

/// Waits (at most `patience`) until process `pid` is in `unit`'s cgroup.
pub fn in_unit(pid: u32, unit: &str, patience: Duration) -> bool {
    let deadline = Instant::now() + patience;
    let suffix = format!("/{unit}");
    loop {
        let cgroup = std::fs::read_to_string(format!("/proc/{pid}/cgroup")).unwrap_or_default();
        if cgroup.lines().any(|l| l.trim_end().ends_with(&suffix)) {
            return true;
        }
        if Instant::now() >= deadline {
            return false;
        }
        thread::sleep(Duration::from_millis(1));
    }
}
