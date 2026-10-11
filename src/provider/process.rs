//! A provider process (spec §4.8): spawned in its own process group with the
//! caller's working directory and environment, spoken to through its stdin
//! and heard on its stdout, one JSON line at a time (see [`super::wire`]).
//!
//! - A reader thread turns stdout lines into callbacks, reads on to the end
//!   (so the provider never blocks on a full pipe), then reaps the process.
//! - Writes never block the daemon: stdin is non-blocking, and a provider
//!   that does not read its input is an error.
//! - Signals go through a pidfd, so they can never reach another process
//!   that reused the pid, and only to the provider itself: never to its
//!   process group or the children it started (its actions).

use std::ffi::OsString;
use std::io::{Read, Write};
use std::os::fd::OwnedFd;
use std::os::unix::ffi::OsStringExt;
use std::os::unix::process::CommandExt;
use std::path::PathBuf;
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};
use std::thread;

use rustix::event::{PollFd, PollFlags, poll};
use rustix::fs::{OFlags, fcntl_getfl, fcntl_setfl};
use rustix::io::Errno;
use rustix::process::{Pid, PidfdFlags, Signal, pidfd_open, pidfd_send_signal};

use super::wire::{self, Line, ToProvider};

/// The longest line a provider may write.
pub const MAX_LINE: u64 = 16 << 20;

/// What to run, where and with what environment (the caller's).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Launch {
    pub argv: Vec<OsString>,
    pub cwd: PathBuf,
    pub env: Vec<(OsString, OsString)>,
}

/// A provider as the client asked for it: bytes become OS strings.
impl From<&crate::protocol::Launch> for Launch {
    fn from(launch: &crate::protocol::Launch) -> Self {
        let os = |b: &crate::protocol::Bytes| OsString::from_vec(b.0.clone());
        Self {
            argv: launch.argv.iter().map(os).collect(),
            cwd: PathBuf::from(os(&launch.cwd)),
            env: environment(launch.env.iter().map(|b| b.0.clone())),
        }
    }
}

/// What the reader thread reports.
pub enum Heard {
    /// One line, read.
    Line(Result<Line, String>),
    /// The provider exited (after its stdout closed); how.
    Exited(String),
}

/// A running provider.
pub struct Process {
    pidfd: OwnedFd,
    stdin: Option<ChildStdin>,
}

impl Process {
    /// Starts the provider. `heard` gets each line until it returns `false`,
    /// then the exit; it runs on the reader thread.
    pub fn spawn(
        command: &Launch,
        mut heard: impl FnMut(Heard) -> bool + Send + 'static,
    ) -> Result<Self, String> {
        let (program, args) = command
            .argv
            .split_first()
            .ok_or("a provider needs a program to run")?;
        let cwd = Some(command.cwd.as_path())
            .filter(|d| d.is_dir())
            .unwrap_or("/".as_ref());
        let mut child = Command::new(program)
            .args(args)
            .env_clear()
            .envs(command.env.iter().map(|(k, v)| (k, v)))
            .current_dir(cwd)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            // Its own process group: signals for the daemon's group (a
            // terminal's Ctrl+C) never reach it or the actions it starts.
            .process_group(0)
            .spawn()
            .map_err(|e| format!("'{}' cannot be started: {e}", program.to_string_lossy()))?;
        let pid = child.id();
        // Opened before the reader thread can reap the child, so the pidfd
        // always names this process.
        let pidfd = Pid::from_raw(pid as i32)
            .ok_or_else(|| "the provider has no pid".to_owned())
            .and_then(|p| pidfd_open(p, PidfdFlags::empty()).map_err(|e| e.to_string()));
        let pidfd = match pidfd {
            Ok(fd) => fd,
            Err(e) => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(format!("the provider cannot be watched: {e}"));
            }
        };
        let stdin = child.stdin.take().expect("piped");
        if let Ok(flags) = fcntl_getfl(&stdin) {
            let _ = fcntl_setfl(&stdin, flags | OFlags::NONBLOCK);
        }
        let stdout = child.stdout.take().expect("piped");
        let exit = pidfd
            .try_clone()
            .map_err(|e| format!("the provider cannot be watched: {e}"))?;
        thread::Builder::new()
            .name("provider".into())
            .spawn(move || listen(stdout, child, exit, &mut heard))
            .map_err(|e| format!("the provider cannot be heard: {e}"))?;
        Ok(Self {
            pidfd,
            stdin: Some(stdin),
        })
    }

    /// Sends one line. Fails when the provider closed its input, or does
    /// not read it (the pipe is full).
    pub fn send(&mut self, message: &ToProvider) -> Result<(), String> {
        let stdin = self
            .stdin
            .as_mut()
            .ok_or("the provider's input is closed")?;
        stdin
            .write_all(&wire::line(message))
            .map_err(|e| format!("the provider does not take its input: {e}"))
    }

    /// Closes the provider's stdin: it reads end of file.
    pub fn close_input(&mut self) {
        self.stdin = None;
    }

    /// Sends `signal` to the provider alone. Harmless once it is gone.
    pub fn signal(&self, signal: Signal) {
        let _ = pidfd_send_signal(&self.pidfd, signal);
    }
}

/// The reader thread. It waits for output or the exit (the pidfd), so an
/// exit is seen even while a child the provider started still holds its
/// stdout. Lines go to `heard` while it wants them; the output is read on to
/// its end in any case, never left to fill the pipe nor closed under a child
/// that still writes to it.
fn listen(
    mut stdout: ChildStdout,
    mut child: Child,
    exit: OwnedFd,
    heard: &mut impl FnMut(Heard) -> bool,
) {
    let mut lines = Lines::default();
    let mut buf = vec![0; 64 << 10];
    let mut exited = false;
    loop {
        let readable = exited || {
            let mut fds = [
                PollFd::new(&stdout, PollFlags::IN),
                PollFd::new(&exit, PollFlags::IN),
            ];
            match poll(&mut fds, None) {
                Ok(_) => !fds[0].revents().is_empty(),
                Err(Errno::INTR) => continue,
                Err(_) => true,
            }
        };
        if !readable {
            // Gone, with nothing more to say for now: an unended last line
            // counts, and the rest is only drained.
            lines.finish(heard);
            heard(Heard::Exited(reap(&mut child)));
            exited = true;
            continue;
        }
        match stdout.read(&mut buf) {
            Ok(0) => break,
            Ok(n) => lines.feed(&buf[..n], heard),
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => {}
            Err(_) => break,
        }
    }
    if !exited {
        lines.finish(heard);
        heard(Heard::Exited(reap(&mut child)));
    }
}

fn reap(child: &mut Child) -> String {
    match child.wait() {
        Ok(status) => status.to_string(),
        Err(e) => format!("unknown ({e})"),
    }
}

/// Output split into lines, handed on while the listener wants them.
#[derive(Default)]
struct Lines {
    partial: Vec<u8>,
    deaf: bool,
}

impl Lines {
    fn feed(&mut self, mut bytes: &[u8], heard: &mut impl FnMut(Heard) -> bool) {
        while let Some(end) = bytes.iter().position(|&b| b == b'\n') {
            if self.partial.is_empty() {
                self.deliver(&bytes[..end], heard);
            } else {
                self.partial.extend_from_slice(&bytes[..end]);
                let line = std::mem::take(&mut self.partial);
                self.deliver(&line, heard);
            }
            bytes = &bytes[end + 1..];
        }
        if self.deaf {
            return;
        }
        self.partial.extend_from_slice(bytes);
        if self.partial.len() as u64 > MAX_LINE {
            self.partial = Vec::new();
            let e = format!("a line longer than {MAX_LINE} bytes");
            self.deaf = !heard(Heard::Line(Err(e)));
        }
    }

    /// The output ended (or the provider did): an unended line counts.
    fn finish(&mut self, heard: &mut impl FnMut(Heard) -> bool) {
        let line = std::mem::take(&mut self.partial);
        self.deliver(&line, heard);
    }

    fn deliver(&mut self, line: &[u8], heard: &mut impl FnMut(Heard) -> bool) {
        if self.deaf || line.iter().all(u8::is_ascii_whitespace) {
            return;
        }
        self.deaf = !heard(Heard::Line(wire::parse(line)));
    }
}

/// The environment as `(name, value)` pairs from `NAME=VALUE` bytes.
pub fn environment(pairs: impl IntoIterator<Item = Vec<u8>>) -> Vec<(OsString, OsString)> {
    pairs
        .into_iter()
        .filter_map(|mut pair| {
            let eq = pair.iter().position(|&b| b == b'=')?;
            let value = pair.split_off(eq + 1);
            pair.pop();
            Some((OsString::from_vec(pair), OsString::from_vec(value)))
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::provider::wire::FromProvider;
    use std::sync::mpsc::{Receiver, channel};
    use std::time::Duration;

    const PATIENCE: Duration = Duration::from_secs(5);

    fn sh(script: &str) -> Launch {
        Launch {
            argv: ["/bin/sh", "-c", script].map(OsString::from).to_vec(),
            cwd: PathBuf::from("/"),
            env: environment([b"PATH=/usr/bin:/bin".to_vec(), b"OMEN=a=b".to_vec()]),
        }
    }

    fn start(script: &str) -> (Process, Receiver<Heard>) {
        let (tx, rx) = channel();
        let process = Process::spawn(&sh(script), move |h| tx.send(h).is_ok()).unwrap();
        (process, rx)
    }

    fn next(rx: &Receiver<Heard>) -> Heard {
        rx.recv_timeout(PATIENCE)
            .expect("the provider said nothing")
    }

    fn message(rx: &Receiver<Heard>) -> FromProvider {
        match next(rx) {
            Heard::Line(Ok(Line::Message(m))) => m,
            Heard::Line(other) => panic!("{other:?}"),
            Heard::Exited(status) => panic!("exited: {status}"),
        }
    }

    fn exited(rx: &Receiver<Heard>) -> String {
        loop {
            if let Heard::Exited(status) = next(rx) {
                return status;
            }
        }
    }

    #[test]
    fn lines_are_heard_in_order_then_the_exit() {
        let (_p, rx) =
            start(r#"echo '{"type":"hello","protocol":1}'; echo; printf '{"type":"close"}'"#);
        assert_eq!(message(&rx), FromProvider::Hello { protocol: 1 });
        assert_eq!(
            message(&rx),
            FromProvider::Close,
            "blank lines skipped, last line unended"
        );
        assert_eq!(exited(&rx), "exit status: 0");
    }

    #[test]
    fn the_provider_hears_what_logis_sends() {
        let (mut p, rx) = start(r#"read line; echo "$line""#);
        p.send(&ToProvider::Hello { protocol: 1 }).unwrap();
        assert_eq!(message(&rx), FromProvider::Hello { protocol: 1 });
        exited(&rx);
        // Gone: its input is broken, and that is an error, not a hang.
        thread::sleep(Duration::from_millis(50));
        assert!(p.send(&ToProvider::Cancel).is_err());
    }

    #[test]
    fn it_runs_in_the_callers_directory_and_environment_and_its_own_group() {
        let script = r#"printf '{"type":"toast","text":"%s %s %s"}\n' "$(pwd)" "$OMEN" "$(ps -o pid=,pgid= $$ | tr -s ' ' | sed 's/^ //')""#;
        let (_p, rx) = start(script);
        let FromProvider::Toast { text, .. } = message(&rx) else {
            panic!("a toast");
        };
        let parts: Vec<&str> = text.split(' ').collect();
        assert_eq!(parts[..2], ["/", "a=b"]);
        assert_eq!(parts[2], parts[3], "its own process group: {text}");
        let ours = rustix::process::getpgid(None)
            .unwrap()
            .as_raw_nonzero()
            .get();
        assert_ne!(parts[3], ours.to_string());
    }

    #[test]
    fn closing_its_input_lets_it_end() {
        let (mut p, rx) = start("cat >/dev/null");
        p.close_input();
        assert_eq!(exited(&rx), "exit status: 0");
    }

    #[test]
    fn a_signal_reaches_the_provider_alone() {
        // The provider starts a child in its group, then waits.
        let (p, rx) = start(r#"sleep 30 & echo "{\"type\":\"toast\",\"text\":\"$!\"}"; wait"#);
        let FromProvider::Toast { text: child, .. } = message(&rx) else {
            panic!("a toast");
        };
        p.signal(Signal::TERM);
        assert!(exited(&rx).contains("SIGTERM"));
        let child = Pid::from_raw(child.parse().unwrap()).unwrap();
        assert!(
            rustix::process::test_kill_process(child).is_ok(),
            "its child lives on"
        );
        let _ = rustix::process::kill_process(child, Signal::KILL);
    }

    #[test]
    fn garbage_is_reported_and_unheard_output_is_drained() {
        let (tx, rx) = channel();
        let script = r#"echo nonsense; yes '{"type":"ready"}' | head -n 100000"#;
        let mut first = true;
        let _p = Process::spawn(&sh(script), move |h| {
            let go_on = !first;
            first = false;
            tx.send(h).is_ok() && go_on
        })
        .unwrap();
        let Heard::Line(Err(e)) = next(&rx) else {
            panic!("an error first");
        };
        assert!(e.contains("JSON"), "{e}");
        assert_eq!(exited(&rx), "exit status: 0", "not stuck on a full pipe");
    }

    #[test]
    fn a_program_that_does_not_exist_is_an_error() {
        let launch = Launch {
            argv: vec![OsString::from("/nonexistent/provider")],
            ..sh("")
        };
        let err = Process::spawn(&launch, |_| true).err().unwrap();
        assert!(err.contains("/nonexistent/provider"), "{err}");
        let empty = Launch {
            argv: vec![],
            ..sh("")
        };
        assert!(Process::spawn(&empty, |_| true).is_err());
    }

    #[test]
    fn environment_pairs_split_at_the_first_equals() {
        let env = environment([b"A=1=2".to_vec(), b"NOEQ".to_vec(), b"B=".to_vec()]);
        assert_eq!(env, [("A".into(), "1=2".into()), ("B".into(), "".into())]);
    }
}
