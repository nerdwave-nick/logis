//! A logis process with the test trace on (`LOGIS_TEST_TRACE=1`), read
//! line by line from its stderr: waiting for keyboard focus, key events and
//! frame commits, each stamped with the client's CLOCK_MONOTONIC.

use std::io::{BufRead, BufReader};
use std::process::{Child, Command, ExitStatus, Stdio};
use std::sync::mpsc::{self, Receiver, RecvTimeoutError};
use std::thread;
use std::time::Instant;

use logis::window::trace;

use super::PATIENCE;

/// One trace event: when (CLOCK_MONOTONIC ns) and what.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Event {
    pub ns: u64,
    pub what: String,
}

/// Parses a trace line, or `None` for any other stderr line.
pub fn parse(line: &str) -> Option<Event> {
    let rest = line.strip_prefix(trace::PREFIX)?.trim_start();
    let (ns, what) = rest.split_once(' ')?;
    Some(Event {
        ns: ns.parse().ok()?,
        what: what.to_owned(),
    })
}

/// Proof that the client reported keyboard focus: the virtual keyboard
/// types only with one, so no key is sent before the client can receive it.
#[derive(Debug)]
pub struct Focused(());

/// A traced logis process, killed on drop.
pub struct Traced {
    child: Child,
    lines: Receiver<String>,
    stderr: Vec<String>,
    events: Vec<Event>,
    cursor: usize,
}

impl Traced {
    /// Spawns `command` with the trace on and its stderr read.
    pub fn spawn(mut command: Command) -> Self {
        let mut child = command
            .env(trace::ENV, "1")
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .expect("logis starts");
        let stderr = child.stderr.take().unwrap();
        let (sender, lines) = mpsc::channel();
        thread::spawn(move || {
            for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                if sender.send(line).is_err() {
                    break;
                }
            }
        });
        Self {
            child,
            lines,
            stderr: Vec::new(),
            events: Vec::new(),
            cursor: 0,
        }
    }

    /// The process id.
    pub fn pid(&self) -> u32 {
        self.child.id()
    }

    /// Everything the process wrote to stderr so far.
    pub fn stderr(&mut self) -> String {
        while let Ok(line) = self.lines.try_recv() {
            self.take(line);
        }
        self.stderr.join("\n")
    }

    fn take(&mut self, line: String) {
        if let Some(event) = parse(&line) {
            self.events.push(event);
        }
        self.stderr.push(line);
    }

    /// The next event after the last one waited for that satisfies `want`,
    /// within [`PATIENCE`]; earlier non-matching events are passed over.
    /// Use it for sequences (a key, then its commit).
    pub fn wait_for(&mut self, what: &str, want: impl Fn(&Event) -> bool) -> Event {
        let i = self.wait_index(what, self.cursor, want);
        self.cursor = i + 1;
        self.events[i].clone()
    }

    /// The first event since the start that satisfies `want`, without
    /// moving the sequence cursor. Use it for facts whose order is not
    /// fixed (focus may arrive before or after the first commit).
    pub fn wait_seen(&mut self, what: &str, want: impl Fn(&Event) -> bool) -> Event {
        let i = self.wait_index(what, 0, want);
        self.events[i].clone()
    }

    fn wait_index(&mut self, what: &str, from: usize, want: impl Fn(&Event) -> bool) -> usize {
        let deadline = Instant::now() + PATIENCE;
        let mut from = from;
        loop {
            if let Some(i) = self.events[from..].iter().position(&want) {
                return from + i;
            }
            from = self.events.len();
            let left = deadline.saturating_duration_since(Instant::now());
            match self.lines.recv_timeout(left) {
                Ok(line) => self.take(line),
                Err(RecvTimeoutError::Timeout) => {
                    panic!("no {what} in {PATIENCE:?}; stderr:\n{}", self.stderr())
                }
                Err(RecvTimeoutError::Disconnected) => {
                    let status = self.child.wait().ok();
                    panic!(
                        "logis ended ({status:?}) before {what}; stderr:\n{}",
                        self.stderr()
                    )
                }
            }
        }
    }

    /// Waits until the client has reported keyboard focus on its panel.
    pub fn wait_for_focus(&mut self) -> Focused {
        self.wait_seen("keyboard focus", |e| e.what == "keyboard-enter");
        Focused(())
    }

    /// Waits for the next keyboard focus in sequence: a daemon shows one
    /// window per session, and each takes the focus anew.
    pub fn wait_for_next_focus(&mut self) -> Focused {
        self.wait_for("keyboard focus", |e| e.what == "keyboard-enter");
        Focused(())
    }

    /// Waits until the client has committed its first frame.
    pub fn wait_for_first_commit(&mut self) -> Event {
        self.wait_seen("a first frame commit", |e| e.what == "commit")
    }

    /// Waits for the next frame commit in sequence.
    pub fn wait_for_commit(&mut self) -> Event {
        self.wait_for("a frame commit", |e| e.what == "commit")
    }

    /// Waits for the next commit stamped at or after `ns`.
    pub fn wait_for_commit_after(&mut self, ns: u64) -> Event {
        self.wait_for("a frame commit", |e| e.what == "commit" && e.ns >= ns)
    }

    /// Every trace event seen so far.
    pub fn events(&self) -> &[Event] {
        &self.events
    }

    /// Waits for the process to exit within [`PATIENCE`].
    pub fn wait_exit(&mut self) -> ExitStatus {
        let deadline = Instant::now() + PATIENCE;
        loop {
            if let Some(status) = self.child.try_wait().unwrap() {
                return status;
            }
            assert!(
                Instant::now() < deadline,
                "logis did not exit; stderr:\n{}",
                self.stderr()
            );
            thread::sleep(std::time::Duration::from_millis(5));
        }
    }
}

impl Drop for Traced {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}
