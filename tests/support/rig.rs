//! A test rig for the daemon: a scratch `XDG_RUNTIME_DIR` and
//! `XDG_CONFIG_HOME` (never the real ones), a fake compositor, the scripted resolver, and a raw protocol client
//! that speaks the wire format independently of logis's own code.
#![allow(dead_code)] // shared by test crates that each use only part of it

use std::io::{Read, Write};
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::process::{Child, Command, ExitStatus, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use serde_json::{Value, json};

use super::ScratchDir;
use super::fake_compositor::{self, Compositor};

/// Globals of a compositor offering layer shell (niri/sway-like).
pub const WITH_LAYER_SHELL: &[(&str, u32)] = &[
    ("wl_compositor", 6),
    ("wl_shm", 1),
    ("zwlr_layer_shell_v1", 4),
    ("xdg_wm_base", 6),
];

/// How long any single wait in these tests may take before it fails.
pub const PATIENCE: Duration = Duration::from_secs(5);

pub struct Rig {
    // Field order matters: hang up on the daemon before removing its directory.
    compositor: Option<Compositor>,
    dir: ScratchDir,
}

impl Rig {
    pub fn new() -> Self {
        let dir = ScratchDir::new();
        let compositor = Some(fake_compositor::serve(
            dir.path(),
            "wayland-test",
            WITH_LAYER_SHELL,
        ));
        Self { compositor, dir }
    }

    pub fn socket(&self) -> PathBuf {
        self.dir.path().join("logis.sock")
    }

    /// The scratch `XDG_CONFIG_HOME`; it does not exist until a test makes it.
    pub fn config_home(&self) -> PathBuf {
        self.dir.path().join("config")
    }

    /// Where the daemon looks for its config by default.
    pub fn config_file(&self) -> PathBuf {
        self.config_home().join("logis").join("logis.jsonc")
    }

    /// `logis` in this rig's environment, answering sessions by script.
    pub fn logis(&self) -> Command {
        self.command(env!("CARGO_BIN_EXE_logis"))
    }

    /// Any program in this rig's environment.
    pub fn command(&self, program: &str) -> Command {
        let mut cmd = Command::new(program);
        cmd.env_remove("WAYLAND_SOCKET")
            .env_remove("LISTEN_PID")
            .env_remove("LISTEN_FDS")
            .env("WAYLAND_DISPLAY", "wayland-test")
            .env("XDG_RUNTIME_DIR", self.dir.path())
            .env("XDG_CONFIG_HOME", self.config_home())
            .env("LOGIS_RESOLVER", "scripted");
        cmd
    }

    /// Starts `logis daemon ARGS` and waits until it reports readiness.
    pub fn start_daemon(&self, args: &[&str]) -> Daemon {
        let child = self
            .logis()
            .arg("daemon")
            .args(args)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        let mut daemon = Daemon { child };
        let mut byte = [0];
        let ready = daemon
            .child
            .stdout
            .as_mut()
            .unwrap()
            .read(&mut byte)
            .unwrap();
        assert_eq!(
            ready,
            1,
            "the daemon did not get ready: {}",
            daemon.stderr()
        );
        daemon
    }

    /// The compositor quits: its client connection is cut.
    pub fn hang_up_compositor(&mut self) {
        self.compositor = None;
    }

    pub fn connect(&self) -> Raw {
        Raw::connect(&self.socket())
    }

    /// Opens a raw session of `mode`; returns it and the daemon's first answer.
    pub fn open(&self, mode: &str) -> (Raw, String) {
        let mut raw = self.connect();
        raw.greet();
        raw.send(json!({"type": "open", "mode": mode}));
        let state = raw.recv_type();
        (raw, state)
    }

    /// Opens a dmenu session only to see whether it would be shown at once
    /// ("visible") or wait ("queued"), then cancels it and waits for the
    /// cancellation. Merely dropping it would not do: the daemon handles each
    /// connection on its own thread, so another connection's next request may
    /// be served before the dropped probe's hang-up, and see it still there.
    /// A queued probe may also be shown before its cancel lands, when the
    /// session ahead of it ends meanwhile.
    pub fn probe(&self) -> String {
        let (mut probe, state) = self.open("dmenu");
        probe.send(json!({"type": "cancel"}));
        let mut message = probe.recv().expect("the daemon hung up on a probe");
        if state == "queued" && message["type"] == "visible" {
            message = probe.recv().expect("the daemon hung up on a probe");
        }
        assert_eq!(message["type"], "result", "{message}");
        assert_eq!(message["outcome"], json!({"kind": "cancelled"}));
        assert_eq!(probe.recv(), None, "the daemon kept the probe open");
        state
    }

    /// Waits until some other session is visible: a probe then queues.
    pub fn wait_for_a_visible_session(&self) {
        wait_until("a session to become visible", || self.probe() == "queued");
    }

    /// Waits until no session is visible or queued: a probe is then shown.
    pub fn wait_until_no_session_remains(&self) {
        wait_until("every session to end", || self.probe() == "visible");
    }
}

/// A daemon this test started; killed when dropped.
pub struct Daemon {
    child: Child,
}

impl Daemon {
    /// Guards a process that is (or will exec into) a daemon.
    pub fn guard(child: Child) -> Self {
        Self { child }
    }

    /// Waits for the daemon to exit on its own.
    pub fn wait_exit(&mut self) -> ExitStatus {
        let deadline = Instant::now() + PATIENCE;
        loop {
            if let Some(status) = self.child.try_wait().unwrap() {
                return status;
            }
            assert!(Instant::now() < deadline, "the daemon did not exit");
            thread::sleep(Duration::from_millis(10));
        }
    }

    pub fn pid(&self) -> u32 {
        self.child.id()
    }

    pub fn is_running(&mut self) -> bool {
        self.child.try_wait().unwrap().is_none()
    }

    /// Everything the daemon wrote to stderr; only complete after it exited.
    pub fn stderr(&mut self) -> String {
        let mut text = String::new();
        if let (Some(_), Some(stderr)) = (self.child.try_wait().unwrap(), &mut self.child.stderr) {
            let _ = stderr.read_to_string(&mut text);
        }
        text
    }
}

impl Drop for Daemon {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// A protocol client written against the wire format, not logis's types.
pub struct Raw {
    stream: UnixStream,
}

impl Raw {
    pub fn connect(path: &std::path::Path) -> Self {
        let stream = UnixStream::connect(path).unwrap();
        stream.set_read_timeout(Some(PATIENCE)).unwrap();
        Self { stream }
    }

    pub fn send(&mut self, message: Value) {
        let payload = serde_json::to_vec(&message).unwrap();
        let mut frame = (payload.len() as u32).to_be_bytes().to_vec();
        frame.extend_from_slice(&payload);
        self.stream.write_all(&frame).unwrap();
    }

    /// The next message, or `None` once the daemon closed the connection.
    pub fn recv(&mut self) -> Option<Value> {
        let mut header = [0; 4];
        match self.stream.read_exact(&mut header) {
            Ok(()) => {}
            Err(e) if e.kind() == std::io::ErrorKind::UnexpectedEof => return None,
            Err(e) => panic!("no message from the daemon: {e}"),
        }
        let mut payload = vec![0; u32::from_be_bytes(header) as usize];
        self.stream.read_exact(&mut payload).unwrap();
        Some(serde_json::from_slice(&payload).unwrap())
    }

    /// The `type` of the next message.
    pub fn recv_type(&mut self) -> String {
        let message = self.recv().expect("the daemon hung up");
        message["type"].as_str().unwrap().to_owned()
    }

    /// Says hello; returns the welcome.
    pub fn greet(&mut self) -> Value {
        self.send(json!({"type": "hello", "version": 2}));
        let welcome = self.recv().expect("no welcome");
        assert_eq!(welcome["type"], "welcome", "{welcome}");
        welcome
    }

    /// Sends these lines as items, then `end`.
    pub fn items_then_end(&mut self, lines: &[&str]) {
        let items: Vec<Value> = lines
            .iter()
            .map(|l| json!({"text": l, "raw": base64(l.as_bytes())}))
            .collect();
        self.send(json!({"type": "items", "items": items}));
        self.send(json!({"type": "end"}));
    }

    /// The outcome of the session's `result`; then the daemon hangs up.
    pub fn outcome(&mut self) -> Value {
        let message = self.recv().expect("the daemon hung up without a result");
        assert_eq!(message["type"], "result", "{message}");
        assert_eq!(self.recv(), None, "the daemon kept the connection open");
        message["outcome"].clone()
    }
}

/// Waits until `condition` holds, polling every 10 ms.
pub fn wait_until(what: &str, mut condition: impl FnMut() -> bool) {
    let deadline = Instant::now() + PATIENCE;
    while !condition() {
        assert!(Instant::now() < deadline, "timed out waiting for {what}");
        thread::sleep(Duration::from_millis(10));
    }
}

/// Standard padded base64, written here so the tests check the wire format.
pub fn base64(bytes: &[u8]) -> String {
    const ABC: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut out = String::new();
    for chunk in bytes.chunks(3) {
        let n = chunk
            .iter()
            .enumerate()
            .fold(0u32, |n, (i, &b)| n | u32::from(b) << (16 - 8 * i));
        for i in 0..4 {
            out.push(if i <= chunk.len() {
                ABC[(n >> (18 - 6 * i) & 63) as usize] as char
            } else {
                '='
            });
        }
    }
    out
}
