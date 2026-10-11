//! The daemon and the session protocol over a real Unix socket, with the
//! scripted resolver standing in for the window (spec §4.1).

mod support;

use std::io::Write;
use std::os::unix::net::UnixListener;
use std::process::{Child, Output, Stdio};
use std::thread;
use std::time::Duration;

use serde_json::json;
use support::rig::{Daemon, Rig, base64, wait_until};

/// Runs `logis --dmenu` with this stdin to completion.
fn dmenu(rig: &Rig, stdin: &[u8]) -> Output {
    let mut child = spawn_dmenu(rig);
    child.stdin.take().unwrap().write_all(stdin).unwrap();
    child.wait_with_output().unwrap()
}

/// Starts `logis --dmenu` with its stdin left open.
fn spawn_dmenu(rig: &Rig) -> Child {
    rig.logis()
        .arg("--dmenu")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap()
}

fn stderr(out: &Output) -> String {
    String::from_utf8_lossy(&out.stderr).into_owned()
}

#[test]
fn a_client_starts_the_daemon_when_none_answers_and_later_clients_reuse_it() {
    let rig = Rig::new();
    assert!(!rig.socket().exists());

    // `output()` also proves the daemon does not keep the client's stdout open.
    let out = dmenu(&rig, b"first\n>second\nthird\n");
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert_eq!(out.stdout, b">second\n");

    let pid = rig.connect().greet()["pid"].clone();
    let out = dmenu(&rig, b">again\n");
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert_eq!(out.stdout, b">again\n");
    assert_eq!(
        rig.connect().greet()["pid"],
        pid,
        "a second daemon was started"
    );
}

#[test]
fn a_client_whose_daemon_cannot_start_exits_2_with_the_reason() {
    let rig = Rig::new();
    let out = rig
        .logis()
        .arg("--dmenu")
        .env_remove("WAYLAND_DISPLAY")
        .output()
        .unwrap();
    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    assert!(
        stderr(&out).contains("noosphere is silent"),
        "{}",
        stderr(&out)
    );
}

#[test]
fn a_stale_socket_is_cleared_by_the_next_daemon() {
    let rig = Rig::new();
    drop(UnixListener::bind(rig.socket()).unwrap());
    assert!(rig.socket().exists());
    let out = dmenu(&rig, b">fresh\n");
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert_eq!(out.stdout, b">fresh\n");
}

#[test]
fn the_original_bytes_of_the_chosen_item_are_returned_exactly() {
    let rig = Rig::new();
    let out = dmenu(&rig, b"plain\n>\xff\xfe not utf-8\r\n\tlast");
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert_eq!(out.stdout, b">\xff\xfe not utf-8\r\n");
}

#[test]
fn a_dismissed_dmenu_session_exits_1_with_empty_stdout() {
    let rig = Rig::new();
    let out = dmenu(&rig, b"nothing\nmarked\n");
    assert_eq!(out.status.code(), Some(1), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
}

#[test]
fn dmenu_sessions_queue_and_each_caller_gets_its_own_answer_in_order() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let (mut first, state) = rig.open("dmenu");
    assert_eq!(state, "visible");
    let (mut second, state) = rig.open("dmenu");
    assert_eq!(state, "queued");
    let (mut third, state) = rig.open("dmenu");
    assert_eq!(state, "queued");
    third.items_then_end(&[">three"]);
    second.items_then_end(&["x", ">two"]);

    first.items_then_end(&[">one"]);
    assert_eq!(first.outcome()["item"]["raw"], base64(b">one"));
    assert_eq!(second.recv_type(), "visible");
    let two = second.outcome();
    assert_eq!(
        (two["index"].as_u64(), &two["item"]["text"]),
        (Some(1), &json!(">two"))
    );
    assert_eq!(third.recv_type(), "visible");
    assert_eq!(third.outcome()["item"]["text"], ">three");
}

#[test]
fn a_dmenu_caller_waits_in_the_queue_for_its_turn() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let (mut visible, _) = rig.open("dmenu");

    let mut waiting = spawn_dmenu(&rig);
    waiting.stdin.take().unwrap().write_all(b">mine\n").unwrap();
    thread::sleep(Duration::from_millis(200));
    assert!(
        waiting.try_wait().unwrap().is_none(),
        "it did not wait its turn"
    );

    visible.items_then_end(&[">theirs"]);
    assert_eq!(visible.outcome()["item"]["text"], ">theirs");
    let out = waiting.wait_with_output().unwrap();
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert_eq!(out.stdout, b">mine\n");
}

#[test]
fn an_interactive_session_replaces_the_visible_one() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let (mut dmenu_session, _) = rig.open("dmenu");
    let (mut apps, state) = rig.open("apps");
    assert_eq!(state, "visible");
    assert_eq!(dmenu_session.outcome(), json!({"kind": "replaced"}));

    let (mut servitor, state) = rig.open("servitor");
    assert_eq!(state, "visible");
    assert_eq!(apps.outcome(), json!({"kind": "replaced"}));

    servitor.send(json!({"type": "cancel"}));
    assert_eq!(servitor.outcome(), json!({"kind": "cancelled"}));
}

#[test]
fn a_replaced_dmenu_caller_exits_1_with_empty_stdout() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let mut caller = spawn_dmenu(&rig);
    let mut stdin = caller.stdin.take().unwrap();
    stdin.write_all(b">never chosen\n").unwrap();
    rig.wait_for_a_visible_session();

    let (_apps, state) = rig.open("apps");
    assert_eq!(state, "visible");
    let out = caller.wait_with_output().unwrap();
    assert_eq!(out.status.code(), Some(1), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
}

#[test]
fn a_client_disconnect_cancels_its_session_and_the_next_one_is_shown() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let mut caller = spawn_dmenu(&rig);
    let _stdin = caller.stdin.take().unwrap();
    rig.wait_for_a_visible_session();
    let (mut queued, state) = rig.open("dmenu");
    assert_eq!(state, "queued");

    caller.kill().unwrap();
    caller.wait().unwrap();
    assert_eq!(queued.recv_type(), "visible");
    drop(queued);
    let (_next, state) = rig.open("dmenu");
    assert_eq!(
        state, "visible",
        "the disconnected sessions were not cancelled"
    );
}

#[test]
fn with_an_idle_timeout_the_daemon_exits_once_no_client_is_connected() {
    let rig = Rig::new();
    let mut daemon = rig.start_daemon(&["--idle-timeout", "0.3"]);
    let client = rig.connect();
    thread::sleep(Duration::from_millis(600));
    assert!(daemon.is_running(), "it left while a client was connected");

    drop(client);
    let status = daemon.wait_exit();
    assert_eq!(status.code(), Some(0), "stderr: {}", daemon.stderr());
    assert!(!rig.socket().exists(), "the socket was left behind");
}

#[test]
fn without_an_idle_timeout_the_daemon_stays() {
    let rig = Rig::new();
    let mut daemon = rig.start_daemon(&[]);
    thread::sleep(Duration::from_millis(500));
    assert!(daemon.is_running());
}

#[test]
fn losing_the_wayland_connection_makes_the_daemon_exit_non_zero() {
    let mut rig = Rig::new();
    let mut daemon = rig.start_daemon(&[]);
    rig.hang_up_compositor();
    let status = daemon.wait_exit();
    assert_eq!(status.code(), Some(2));
    assert!(daemon.stderr().contains("noosphere"), "{}", daemon.stderr());
    wait_until("the socket to be removed", || !rig.socket().exists());
}

#[test]
fn an_auto_started_daemon_outlives_its_starter_and_logs_to_the_runtime_dir() {
    let mut rig = Rig::new();
    let out = dmenu(&rig, b">x\n");
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    let pid = rig.connect().greet()["pid"].as_u64().unwrap();
    let alive = || std::path::Path::new(&format!("/proc/{pid}")).exists();
    assert!(alive());

    // Its starter is gone, so its diagnostics must not depend on that stderr.
    rig.hang_up_compositor();
    wait_until("the daemon to exit", || !alive());
    let log = std::fs::read_to_string(rig.socket().with_file_name("logis.log")).unwrap();
    assert!(log.contains("noosphere fell silent"), "{log}");
}

#[test]
fn a_second_daemon_defers_to_the_one_already_listening() {
    let rig = Rig::new();
    let _first = rig.start_daemon(&[]);
    let pid = rig.connect().greet()["pid"].clone();
    let out = rig.logis().arg("daemon").output().unwrap();
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert_eq!(rig.connect().greet()["pid"], pid);
}

#[test]
fn a_greeting_in_another_protocol_version_is_refused() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let mut raw = rig.connect();
    raw.send(json!({"type": "hello", "version": 999}));
    let reply = raw.recv().unwrap();
    assert_eq!(reply["type"], "error");
    assert!(
        reply["message"].as_str().unwrap().contains("999"),
        "{reply}"
    );
}

#[test]
fn an_unknown_idle_timeout_value_is_an_error() {
    let rig = Rig::new();
    let out = rig
        .logis()
        .args(["daemon", "--idle-timeout", "soon"])
        .output()
        .unwrap();
    assert_eq!(out.status.code(), Some(2));
    assert!(stderr(&out).contains("soon"), "{}", stderr(&out));
}

/// systemd's socket activation as `logis.socket` does it, via the
/// `systemd-socket-activate` tool (skipped where it is not installed).
fn socket_activated(rig: &Rig, daemon_args: &[&str]) -> Option<Daemon> {
    let tool = "/usr/bin/systemd-socket-activate";
    if !std::path::Path::new(tool).exists() {
        eprintln!("skipped: {tool} is not installed");
        return None;
    }
    let socket = rig.socket();
    let child = rig
        .command(tool)
        .arg("--listen")
        .arg(&socket)
        // As in the user manager, where the compositor imports WAYLAND_DISPLAY.
        .args([
            "-E",
            "XDG_RUNTIME_DIR",
            "-E",
            "WAYLAND_DISPLAY",
            "-E",
            "LOGIS_RESOLVER",
        ])
        .args([env!("CARGO_BIN_EXE_logis"), "daemon"])
        .args(daemon_args)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    let guard = Daemon::guard(child);
    wait_until("the activation socket", || socket.exists());
    Some(guard)
}

#[test]
fn a_socket_activated_daemon_serves_the_socket_it_was_given() {
    let rig = Rig::new();
    let Some(activated) = socket_activated(&rig, &[]) else {
        return;
    };
    let out = dmenu(&rig, b">activated\n");
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert_eq!(out.stdout, b">activated\n");
    let pid = rig.connect().greet()["pid"].as_u64();
    assert_eq!(
        pid,
        Some(u64::from(activated.pid())),
        "a client-started daemon answered"
    );
}

#[test]
fn a_socket_activated_daemon_leaves_the_socket_to_systemd_when_idle() {
    let rig = Rig::new();
    let Some(mut activated) = socket_activated(&rig, &["--idle-timeout", "0.2"]) else {
        return;
    };
    drop(rig.connect());
    let status = activated.wait_exit();
    assert_eq!(status.code(), Some(0), "stderr: {}", activated.stderr());
    assert!(rig.socket().exists(), "the daemon removed systemd's socket");
}
