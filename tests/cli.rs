//! Observable behaviour of the `logis` binary at its process boundary.

mod support;

use std::process::{Command, Output};

use support::{ScratchDir, fake_compositor};

/// Globals of a compositor without layer shell (GNOME/Mutter-like).
const WITHOUT_LAYER_SHELL: &[(&str, u32)] = &[
    ("wl_compositor", 6),
    ("wl_shm", 1),
    ("wl_seat", 9),
    ("xdg_wm_base", 6),
];

/// Globals of a compositor offering layer shell (niri/sway-like).
const WITH_LAYER_SHELL: &[(&str, u32)] = &[
    ("wl_compositor", 6),
    ("wl_shm", 1),
    ("zwlr_layer_shell_v1", 4),
    ("xdg_wm_base", 6),
];

/// `logis` with a Wayland environment that the test fully controls.
fn logis(runtime_dir: &ScratchDir, wayland_display: Option<&str>) -> Command {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_logis"));
    cmd.env_remove("WAYLAND_SOCKET")
        .env_remove("WAYLAND_DISPLAY")
        .env("XDG_RUNTIME_DIR", runtime_dir.path())
        .env("XDG_CONFIG_HOME", runtime_dir.path().join("config"));
    if let Some(display) = wayland_display {
        cmd.env("WAYLAND_DISPLAY", display);
    }
    cmd
}

fn stderr(out: &Output) -> String {
    String::from_utf8_lossy(&out.stderr).into_owned()
}

#[test]
fn version_flag_prints_name_and_version_and_succeeds() {
    let dir = ScratchDir::new();
    let out = logis(&dir, None).arg("--version").output().unwrap();

    assert_eq!(out.status.code(), Some(0));
    assert_eq!(
        String::from_utf8_lossy(&out.stdout),
        format!("logis {}\n", env!("CARGO_PKG_VERSION"))
    );
    assert!(out.stderr.is_empty());
}

#[test]
fn without_wayland_display_exits_2_with_flavoured_message() {
    let dir = ScratchDir::new();
    let out = logis(&dir, None).output().unwrap();

    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    let msg = stderr(&out);
    assert!(msg.contains("noosphere is silent"), "{msg}");
    assert!(msg.contains("WAYLAND_DISPLAY is not set"), "{msg}");
}

#[test]
fn with_nonexistent_wayland_socket_exits_2_naming_the_socket() {
    let dir = ScratchDir::new();
    let out = logis(&dir, Some("wayland-nowhere")).output().unwrap();

    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    let msg = stderr(&out);
    assert!(msg.contains("noosphere is silent"), "{msg}");
    let socket = dir.path().join("wayland-nowhere");
    assert!(msg.contains(&socket.display().to_string()), "{msg}");
}

#[test]
fn on_compositor_without_layer_shell_exits_2_naming_the_protocol() {
    let dir = ScratchDir::new();
    let _compositor = fake_compositor::serve(dir.path(), "wayland-test", WITHOUT_LAYER_SHELL);
    let out = logis(&dir, Some("wayland-test")).output().unwrap();

    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    let msg = stderr(&out);
    assert!(msg.contains("zwlr_layer_shell_v1"), "{msg}");
    assert!(msg.contains("no altar"), "{msg}");
}

#[test]
fn on_compositor_with_layer_shell_succeeds_without_output() {
    let dir = ScratchDir::new();
    let _compositor = fake_compositor::serve(dir.path(), "wayland-test", WITH_LAYER_SHELL);
    let out = logis(&dir, Some("wayland-test")).output().unwrap();

    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
}

#[test]
fn unknown_argument_exits_2_naming_it() {
    let dir = ScratchDir::new();
    let out = logis(&dir, None).arg("--heresy").output().unwrap();

    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    assert!(stderr(&out).contains("--heresy"), "{}", stderr(&out));
}
