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

/// Globals of a compositor offering everything the window needs
/// (niri/sway-like): layer shell, fractional scale and the viewporter.
const WITH_LAYER_SHELL: &[(&str, u32)] = &[
    ("wl_compositor", 6),
    ("wl_shm", 1),
    ("zwlr_layer_shell_v1", 4),
    ("wp_fractional_scale_manager_v1", 1),
    ("wp_viewporter", 1),
    ("xdg_wm_base", 6),
];

/// Layer shell, but no fractional scaling: the window cannot be crisp.
const WITHOUT_FRACTIONAL_SCALE: &[(&str, u32)] = &[
    ("wl_compositor", 6),
    ("wl_shm", 1),
    ("zwlr_layer_shell_v1", 4),
    ("wp_viewporter", 1),
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
fn on_compositor_without_fractional_scale_exits_2_naming_the_protocol() {
    let dir = ScratchDir::new();
    let _compositor = fake_compositor::serve(dir.path(), "wayland-test", WITHOUT_FRACTIONAL_SCALE);
    let out = logis(&dir, Some("wayland-test")).output().unwrap();

    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    let msg = stderr(&out);
    assert!(msg.contains("wp_fractional_scale_manager_v1"), "{msg}");
}

/// What plain `logis` did before it became the app launcher: making sure a
/// daemon answers. `logis daemon --detach` exits 0 at once when one does.
#[test]
fn daemon_detach_exits_0_at_once_when_a_daemon_already_answers() {
    use std::io::Read;
    let dir = ScratchDir::new();
    let _compositor = fake_compositor::serve(dir.path(), "wayland-test", WITH_LAYER_SHELL);
    let mut first = logis(&dir, Some("wayland-test"))
        .args(["daemon", "--detach"])
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::null())
        .spawn()
        .unwrap();
    let mut ready = [0];
    let read = first.stdout.as_mut().unwrap().read(&mut ready).unwrap();
    assert_eq!(read, 1, "the first daemon never said it listens");
    let start = std::time::Instant::now();
    let out = logis(&dir, Some("wayland-test"))
        .args(["daemon", "--detach"])
        .output()
        .unwrap();
    let _ = first.kill();
    let _ = first.wait();
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert!(start.elapsed() < std::time::Duration::from_secs(5));
    assert!(String::from_utf8_lossy(&out.stdout).contains("already listens"));
}

/// The caller contract (README "Caller contract"): a `logis --dmenu` caller
/// learns that logis cannot serve it from exit 2 and an empty stdout, with
/// the plain reason on stderr, whatever stands in the way.
#[test]
fn a_dmenu_caller_without_a_usable_session_exits_2_naming_why() {
    let dir = ScratchDir::new();
    let _compositor = fake_compositor::serve(dir.path(), "wayland-test", WITHOUT_LAYER_SHELL);
    let no_session = logis(&dir, None);
    let no_layer_shell = logis(&dir, Some("wayland-test"));
    let mut no_runtime_dir = logis(&dir, Some("wayland-test"));
    no_runtime_dir.env_remove("XDG_RUNTIME_DIR");
    for (mut command, reason) in [
        (no_session, "WAYLAND_DISPLAY is not set"),
        (no_layer_shell, "zwlr_layer_shell_v1"),
        (no_runtime_dir, "XDG_RUNTIME_DIR is not set"),
    ] {
        let out = command
            .arg("--dmenu")
            .stdin(std::process::Stdio::null())
            .output()
            .unwrap();
        assert_eq!(out.status.code(), Some(2), "{reason}: {}", stderr(&out));
        assert!(out.stdout.is_empty(), "{reason}");
        assert!(stderr(&out).contains(reason), "{reason}: {}", stderr(&out));
    }
}

#[test]
fn unknown_argument_exits_2_naming_it() {
    let dir = ScratchDir::new();
    let out = logis(&dir, None).arg("--heresy").output().unwrap();

    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    assert!(stderr(&out).contains("--heresy"), "{}", stderr(&out));
}
