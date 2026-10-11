//! A logis daemon in a test compositor with a virtual keyboard to work its
//! window, and `logis --dmenu` clients with piped stdin and stdout (used by
//! tests/dmenu.rs and the dmenu series of tests/latency.rs). The daemon
//! runs with the test trace on, so waits follow what the window did.

use std::io::{Read, Write};
use std::process::{Child, ChildStdin, Stdio};
use std::time::{Duration, Instant};

use super::client::{Focused, Traced};
use super::keyboard::VirtualKeyboard;
use super::wire::Harness;
use super::{Kind, PATIENCE, TestCompositor};

/// A daemon in a test compositor, and a keyboard to work its window.
pub struct Cogitator {
    // Drop order: the keyboard before the harness connection it lives on.
    pub kb: VirtualKeyboard,
    pub harness: Harness,
    pub daemon: Traced,
    pub compositor: TestCompositor,
}

/// A `logis --dmenu` client.
pub struct Client {
    pub child: Child,
    pub stdin: Option<ChildStdin>,
}

/// How a client ended: its exit code and stdout.
#[derive(Debug, PartialEq, Eq)]
pub struct Ended {
    pub code: Option<i32>,
    pub stdout: Vec<u8>,
}

impl Cogitator {
    /// Starts the compositor and the daemon, or `None` without `LOGIS_E2E`.
    pub fn start() -> Option<Self> {
        let kind = Kind::requested()?;
        let compositor = TestCompositor::start(kind);
        let mut harness = Harness::connect(&compositor);
        let kb = harness.virtual_keyboard();
        let mut command = compositor.logis();
        command.arg("daemon");
        let daemon = Traced::spawn(command);
        greet(&compositor.runtime_dir().join("logis.sock"));
        Some(Self {
            kb,
            harness,
            daemon,
            compositor,
        })
    }

    /// Starts `logis --dmenu args` with `input` on its stdin, left open.
    pub fn dmenu(&self, args: &[&str], input: &[u8]) -> Client {
        let mut command = self.compositor.logis();
        command
            .arg("--dmenu")
            .args(args)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        let mut child = command.spawn().expect("logis --dmenu starts");
        let mut stdin = child.stdin.take();
        stdin.as_mut().unwrap().write_all(input).unwrap();
        Client { child, stdin }
    }

    /// Like [`Self::dmenu`], with stdin closed after `input`.
    pub fn dmenu_all(&self, args: &[&str], input: &[u8]) -> Client {
        let mut client = self.dmenu(args, input);
        client.stdin = None;
        client
    }

    /// Waits until the next session is shown, focused, drawn and holding
    /// all `items`. Focus, items and the first frame may come in any order
    /// after `show`; the sequence cursor ends after the focus.
    pub fn shown(&mut self, items: usize) -> Focused {
        let show = self
            .daemon
            .wait_for("show", |e| e.what.starts_with("show "));
        let since = show.ns;
        let focused = self.daemon.wait_for_next_focus();
        let want = format!("items {items}");
        self.daemon
            .wait_seen(&want, |e| e.ns >= since && e.what == want);
        self.daemon
            .wait_seen("a first frame", |e| e.ns >= since && e.what == "commit");
        focused
    }

    /// The next report starting with `word ` (in sequence), without the word.
    pub fn next(&mut self, word: &str) -> String {
        let prefix = format!("{word} ");
        let e = self
            .daemon
            .wait_for(word, |e| e.what.starts_with(&prefix) || e.what == word);
        e.what.strip_prefix(&prefix).unwrap_or_default().to_owned()
    }

    pub fn tap(&mut self, focused: &Focused, key: u32) {
        self.harness.tap(&mut self.kb, focused, key);
    }

    pub fn type_text(&mut self, focused: &Focused, text: &str) {
        self.harness.type_text(&mut self.kb, focused, text);
        let quoted = serde_json::to_string(text).unwrap();
        let want = format!("query {quoted}");
        self.daemon.wait_for(&want, |e| e.what.ends_with(&quoted));
    }
}

/// Waits until the daemon at `socket` greets a client: it binds its socket
/// before the window has woken (GPU, fonts), but answers only once it has.
fn greet(socket: &std::path::Path) {
    use std::os::unix::net::UnixStream;
    let deadline = Instant::now() + PATIENCE;
    let hello = br#"{"type":"hello","version":2}"#;
    loop {
        assert!(Instant::now() < deadline, "the daemon never greeted");
        let Ok(mut stream) = UnixStream::connect(socket) else {
            std::thread::sleep(Duration::from_millis(10));
            continue;
        };
        let mut frame = (hello.len() as u32).to_be_bytes().to_vec();
        frame.extend_from_slice(hello);
        let mut header = [0; 4];
        if stream.write_all(&frame).is_ok() && stream.read_exact(&mut header).is_ok() {
            let mut reply = vec![0; u32::from_be_bytes(header) as usize];
            stream.read_exact(&mut reply).unwrap();
            let reply = String::from_utf8_lossy(&reply);
            assert!(reply.contains("welcome"), "the daemon answered {reply}");
            return;
        }
        std::thread::sleep(Duration::from_millis(10));
    }
}

impl Client {
    /// Waits for the client to exit and takes its stdout.
    pub fn ended(mut self) -> Ended {
        self.stdin = None;
        let deadline = Instant::now() + PATIENCE;
        let status = loop {
            if let Some(status) = self.child.try_wait().unwrap() {
                break status;
            }
            assert!(Instant::now() < deadline, "the dmenu client never ended");
            std::thread::sleep(Duration::from_millis(5));
        };
        let mut stdout = Vec::new();
        self.child
            .stdout
            .take()
            .unwrap()
            .read_to_end(&mut stdout)
            .unwrap();
        let mut stderr = String::new();
        self.child
            .stderr
            .take()
            .unwrap()
            .read_to_string(&mut stderr)
            .unwrap();
        eprintln!("client stderr: {stderr}");
        Ended {
            code: status.code(),
            stdout,
        }
    }
}

impl Drop for Client {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}
