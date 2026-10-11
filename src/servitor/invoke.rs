//! Launching `servitor invoke` so that it outlives logis (servitor caller
//! interface S7, codex passage `detached`; owner decision ae44412a).

//!
//! As fuzzel launches applications: fork and exec, with
//!
//! - stdin, stdout and stderr on /dev/null, never a pipe logis would leave;
//! - a session of its own (`setsid`), so no hangup of logis's terminal or
//!   signal to logis's process group reaches it;
//! - the errno of a failed exec reported back through a pipe closed on exec
//!   (std's spawn does exactly this once a `pre_exec` hook is set), so a
//!   servitor that cannot start is known at once.
//!
//! It runs in the caller's environment and working directory (this
//! provider's, which the daemon gave it from the client). logis never waits
//! for it, never reads its exit status and never signals it; once the
//! provider exits it is adopted by the session's subreaper or init. Known
//! limits (S7): SIGKILL, and a cgroup killed whole (stopping logis.service
//! or the caller's scope); a launch prefix such as `systemd-run --user
//! --scope` would set it apart, which this slice does not add.

use std::ffi::OsStr;
use std::os::unix::process::CommandExt;
use std::process::{Command, Stdio};

/// Starts `program args...` detached and returns once it runs.
pub fn detached(program: &OsStr, args: &[String]) -> Result<(), String> {
    let mut command = Command::new(program);
    command
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    // SAFETY: runs in the forked child before exec; setsid(2) is a single
    // async-signal-safe system call that touches no memory of the parent.
    unsafe {
        command.pre_exec(|| {
            rustix::process::setsid()
                .map(drop)
                .map_err(std::io::Error::from)
        });
    }
    // The child is never waited for: dropping it neither waits nor kills.
    command.spawn().map(drop).map_err(|e| e.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servitor::testing::{Scratch, fixture};
    use std::path::Path;
    use std::time::{Duration, Instant};

    fn record(dir: &Path, name: &str) -> String {
        std::fs::read_to_string(dir.join("invoke").join(name)).unwrap()
    }

    fn wait_for(path: &Path, patience: Duration) -> bool {
        let until = Instant::now() + patience;
        while Instant::now() < until {
            if path.exists() {
                return true;
            }
            std::thread::sleep(Duration::from_millis(10));
        }
        false
    }

    /// The fake servitor, made to labour `seconds` once invoked.
    fn slow_fake(dir: &Scratch, seconds: &str) -> std::path::PathBuf {
        let fake = dir.fake(&fixture("vigil.jsonl"));
        let script = std::fs::read_to_string(&fake).unwrap().replacen(
            "exec ",
            &format!("FAKE_INVOKE_SECONDS={seconds} exec "),
            1,
        );
        std::fs::write(&fake, script).unwrap();
        fake
    }

    #[test]
    fn invoke_runs_in_a_session_of_its_own_with_stdio_on_dev_null_and_unawaited() {
        let dir = Scratch::new("detached");
        let fake = slow_fake(&dir, "2");
        let since = Instant::now();
        let args = [
            "invoke",
            "dnd",
            "on",
            "--reason",
            "-a word that looks like a rune",
        ];
        let args: Vec<String> = args.map(str::to_owned).to_vec();
        detached(fake.as_os_str(), &args).unwrap();
        assert!(
            since.elapsed() < Duration::from_millis(1500),
            "logis waited for the invocation ({:?})",
            since.elapsed()
        );
        let recorded = dir.path().join("invoke/recorded");
        assert!(wait_for(&recorded, Duration::from_secs(5)), "never invoked");
        assert!(!dir.path().join("invoke/done").exists(), "not yet done");
        assert_eq!(
            record(dir.path(), "argv").lines().collect::<Vec<_>>(),
            &args[1..]
        );
        let pid = record(dir.path(), "pid").trim().to_owned();
        let ids: Vec<String> = record(dir.path(), "ids")
            .split_whitespace()
            .map(str::to_owned)
            .collect();
        assert_eq!(ids, [pid.clone(), pid], "its own session and process group");
        assert_eq!(
            record(dir.path(), "fds").lines().collect::<Vec<_>>(),
            ["/dev/null"; 3],
            "stdin, stdout and stderr on /dev/null"
        );
        assert!(wait_for(
            &dir.path().join("invoke/done"),
            Duration::from_secs(5)
        ));
    }

    #[test]
    fn a_servitor_that_cannot_be_started_is_an_error_at_once() {
        let args = vec!["invoke".to_owned()];
        let err = detached(OsStr::new("/nonexistent/servitor"), &args).unwrap_err();
        assert!(err.contains("No such file"), "{err}");
    }
}
