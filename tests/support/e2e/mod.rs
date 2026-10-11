//! The end-to-end harness (spec §5): a test compositor the harness starts
//! itself, logis clients inside it with the test trace on, a virtual
//! keyboard and screen capture, all confined to that compositor.
//!
//! - Locally: a **nested niri** (`LOGIS_E2E=niri`), opened as a window in
//!   the running session with its own generated config (never the owner's,
//!   never `--session`), scale 1, animations off, black background.
//! - On the CI runner: **sway with `WLR_BACKENDS=headless`**
//!   (`LOGIS_E2E=sway`), pixman renderer, no input devices, one 1280x720
//!   output at scale 1.
//!
//! Without `LOGIS_E2E` the E2E and latency tests print `SKIPPED` and pass;
//! with it, a compositor that cannot start fails them.
//!
//! Every compositor runs with a scratch `XDG_RUNTIME_DIR` of its own, so its
//! socket, logis's daemon socket and every client live there. The harness's
//! own Wayland connection ([`wire::Harness`]) is only made to that socket and
//! is refused unless the socket's server is the compositor process the
//! harness spawned, so keys can never reach the live session.
#![allow(dead_code)] // shared by test crates that each use only part of it

pub mod client;
pub mod keyboard;
pub mod screencopy;
pub mod wire;

use std::io::Write;
use std::os::unix::fs::{FileTypeExt, PermissionsExt};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::thread;
use std::time::{Duration, Instant};

use super::ScratchDir;

/// The variable that selects the test compositor.
pub const ENV: &str = "LOGIS_E2E";

/// How long a compositor may take to open its socket, a client to report
/// focus or a frame, or a capture to complete.
pub const PATIENCE: Duration = Duration::from_secs(15);

/// The compositors the harness can run.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    /// niri as a window in the running session (local runs).
    NestedNiri,
    /// sway on wlroots' headless backend (the CI runner).
    HeadlessSway,
}

impl Kind {
    /// Parses a value of [`ENV`]: `niri`, `sway`, or empty/unset for none.
    pub fn parse(value: Option<&str>) -> Result<Option<Self>, String> {
        match value.map(str::trim) {
            None | Some("") => Ok(None),
            Some("niri") => Ok(Some(Self::NestedNiri)),
            Some("sway") => Ok(Some(Self::HeadlessSway)),
            Some(other) => Err(format!("{ENV}={other}: expected niri or sway")),
        }
    }

    /// The compositor [`ENV`] asks for, or `None` after printing `SKIPPED`.
    pub fn requested() -> Option<Self> {
        let value = std::env::var(ENV).ok();
        let kind = Self::parse(value.as_deref()).unwrap_or_else(|e| panic!("{e}"));
        if kind.is_none() {
            eprintln!("SKIPPED: set {ENV}=niri (nested, local) or {ENV}=sway (headless, CI)");
        }
        kind
    }

    /// A short name for reports.
    pub fn name(self) -> &'static str {
        match self {
            Self::NestedNiri => "niri-nested",
            Self::HeadlessSway => "sway-headless",
        }
    }
}

/// The generated niri config: scale 1, no animations, no startup overlay,
/// no Xwayland, a black background so captures do not depend on it.
pub const NIRI_CONFIG: &str = r##"// logis test compositor (nested niri); generated, never the owner's config.
output "winit" {
    scale 1.0
}
hotkey-overlay {
    skip-at-startup
}
animations {
    off
}
xwayland-satellite {
    off
}
layout {
    background-color "#000000"
}
prefer-no-csd
"##;

/// The generated sway config: one headless output at 1280x720, scale 1, no
/// Xwayland, no bindings (injected keys must reach the client, not sway).
/// Without an `output … bg`, sway starts no swaybg and the background is
/// black.
pub const SWAY_CONFIG: &str = "# logis test compositor (headless sway); generated.
output HEADLESS-1 mode 1280x720@60Hz scale 1
xwayland disable
default_border none
focus_follows_mouse no
";

/// The fontconfig configuration of every client: only the pinned fonts in
/// tests/fonts/ (the text slice's OFL subsets), so text renders the same on
/// every machine; `{FONTS}` and `{CACHE}` are filled in.
const FONTS_CONF: &str = r#"<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<fontconfig>
  <dir>{FONTS}</dir>
  <cachedir>{CACHE}</cachedir>
  <alias binding="same">
    <family>sans-serif</family>
    <prefer><family>Noto Sans</family></prefer>
  </alias>
</fontconfig>
"#;

/// A compositor the harness started, with its scratch runtime directory.
pub struct TestCompositor {
    kind: Kind,
    // Field order: stop the compositor before removing its directory.
    child: Child,
    socket: PathBuf,
    dir: ScratchDir,
}

impl TestCompositor {
    /// Starts `kind` and waits for its Wayland socket.
    pub fn start(kind: Kind) -> Self {
        let dir = ScratchDir::new();
        std::fs::set_permissions(dir.path(), std::fs::Permissions::from_mode(0o700)).unwrap();
        let log = std::fs::File::create(dir.path().join("compositor.log")).unwrap();
        let fonts = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fonts");
        let fonts_conf = FONTS_CONF
            .replace("{FONTS}", &fonts.display().to_string())
            .replace(
                "{CACHE}",
                &dir.path().join("fontconfig").display().to_string(),
            );
        std::fs::write(dir.path().join("fonts.conf"), fonts_conf).unwrap();
        let mut command = match kind {
            Kind::NestedNiri => niri(dir.path()),
            Kind::HeadlessSway => sway(dir.path()),
        };
        command
            .env("XDG_RUNTIME_DIR", dir.path())
            .stdin(Stdio::null())
            .stdout(log.try_clone().unwrap())
            .stderr(log);
        let child = command
            .spawn()
            .unwrap_or_else(|e| panic!("cannot start the {} test compositor: {e}", kind.name()));
        let mut compositor = Self {
            kind,
            child,
            socket: PathBuf::new(),
            dir,
        };
        compositor.socket = compositor.wait_for_socket();
        compositor
    }

    fn wait_for_socket(&mut self) -> PathBuf {
        let deadline = Instant::now() + PATIENCE;
        loop {
            if let Some(socket) = find_socket(self.dir.path()) {
                return socket;
            }
            if let Ok(Some(status)) = self.child.try_wait() {
                panic!(
                    "the {} test compositor exited ({status}) before opening its socket:\n{}",
                    self.kind.name(),
                    self.log()
                );
            }
            assert!(
                Instant::now() < deadline,
                "the {} test compositor opened no socket in {PATIENCE:?}:\n{}",
                self.kind.name(),
                self.log()
            );
            thread::sleep(Duration::from_millis(10));
        }
    }

    /// Which compositor this is.
    pub fn kind(&self) -> Kind {
        self.kind
    }

    /// The compositor process (the server of [`Self::socket`]).
    pub fn pid(&self) -> u32 {
        self.child.id()
    }

    /// Its Wayland socket, inside [`Self::runtime_dir`].
    pub fn socket(&self) -> &Path {
        &self.socket
    }

    /// The scratch `XDG_RUNTIME_DIR` it and its clients use.
    pub fn runtime_dir(&self) -> &Path {
        self.dir.path()
    }

    /// Everything it logged so far.
    pub fn log(&self) -> String {
        std::fs::read_to_string(self.dir.path().join("compositor.log")).unwrap_or_default()
    }

    /// `program` as a client of this compositor: its runtime directory and
    /// socket, the pinned fonts, and nothing that points at another session.
    pub fn command(&self, program: impl AsRef<std::ffi::OsStr>) -> Command {
        let mut command = Command::new(program);
        for var in [
            "WAYLAND_SOCKET",
            "DISPLAY",
            "NIRI_SOCKET",
            "SWAYSOCK",
            "LISTEN_PID",
            "LISTEN_FDS",
        ] {
            command.env_remove(var);
        }
        command
            .env("XDG_RUNTIME_DIR", self.dir.path())
            .env("WAYLAND_DISPLAY", self.socket.file_name().unwrap())
            .env("FONTCONFIG_FILE", self.dir.path().join("fonts.conf"))
            .stdin(Stdio::null());
        command
    }

    /// `logis` as a client of this compositor.
    pub fn logis(&self) -> Command {
        self.command(env!("CARGO_BIN_EXE_logis"))
    }

    /// Writes `bytes` to a file in the runtime directory (keymaps, buffers).
    pub fn scratch_file(&self, name: &str, bytes: &[u8]) -> PathBuf {
        let path = self.dir.path().join(name);
        std::fs::File::create(&path)
            .and_then(|mut f| f.write_all(bytes))
            .unwrap();
        path
    }
}

impl Drop for TestCompositor {
    /// Stops the compositor; when a test is failing, its log goes to stderr
    /// first (the scratch directory holding it is removed next).
    fn drop(&mut self) {
        if thread::panicking() {
            eprintln!("{} test compositor log:\n{}", self.kind.name(), self.log());
        }
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

/// The compositor's `wayland-N` socket in `dir`, if it exists yet.
fn find_socket(dir: &Path) -> Option<PathBuf> {
    std::fs::read_dir(dir).ok()?.flatten().find_map(|entry| {
        let name = entry.file_name();
        let name = name.to_str()?;
        let numbered = name
            .strip_prefix("wayland-")
            .is_some_and(|n| !n.is_empty() && n.bytes().all(|b| b.is_ascii_digit()));
        let socket = entry.file_type().ok()?.is_socket();
        (numbered && socket).then(|| entry.path())
    })
}

/// The minimal environment of a test compositor: nothing of the session but
/// what it needs to run (and, for nested niri, to open its window).
fn base(program: &str) -> Command {
    let mut command = Command::new(program);
    command.env_clear();
    for var in [
        "PATH",
        "HOME",
        "LANG",
        "VK_DRIVER_FILES",
        "LIBGL_ALWAYS_SOFTWARE",
    ] {
        if let Some(value) = std::env::var_os(var) {
            command.env(var, value);
        }
    }
    command
}

fn niri(dir: &Path) -> Command {
    let config = dir.join("niri.kdl");
    std::fs::write(&config, NIRI_CONFIG).unwrap();
    let mut command = base("niri");
    // The nested window opens in the running session; its own socket goes
    // to the scratch runtime directory, so the parent is named absolutely.
    command
        .env("WAYLAND_DISPLAY", parent_socket())
        .env("RUST_LOG", "niri=warn")
        .arg("-c")
        .arg(config);
    command
}

/// The running session's socket, as an absolute path.
fn parent_socket() -> PathBuf {
    let display = std::env::var_os("WAYLAND_DISPLAY")
        .expect("nested niri opens a window, so it needs WAYLAND_DISPLAY of a running session");
    let display = PathBuf::from(display);
    if display.is_absolute() {
        return display;
    }
    let runtime = std::env::var_os("XDG_RUNTIME_DIR").expect("XDG_RUNTIME_DIR is set");
    PathBuf::from(runtime).join(display)
}

fn sway(dir: &Path) -> Command {
    let config = dir.join("sway.conf");
    std::fs::write(&config, SWAY_CONFIG).unwrap();
    let mut command = base("sway");
    command
        .env("WLR_BACKENDS", "headless")
        .env("WLR_HEADLESS_OUTPUTS", "1")
        .env("WLR_LIBINPUT_NO_DEVICES", "1")
        // Software composition without a GPU; WLR_RENDERER from the
        // caller's environment wins (e.g. gles2 on llvmpipe).
        .env(
            "WLR_RENDERER",
            std::env::var_os("WLR_RENDERER").unwrap_or_else(|| "pixman".into()),
        )
        .arg("--config")
        .arg(config);
    command
}
