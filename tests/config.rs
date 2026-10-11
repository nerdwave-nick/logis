//! The config at the binary's boundary: `--check-config`, `--config`, and
//! live reload in the daemon (spec §4.6, §4.9). Every test uses a scratch
//! `XDG_CONFIG_HOME` and `XDG_RUNTIME_DIR`, never the real ones.

mod support;

use std::io::{BufRead, BufReader, Read, Write};
use std::path::Path;
use std::process::{Command, Output, Stdio};
use std::sync::mpsc::{Receiver, channel};
use std::thread;
use std::time::{Duration, Instant};

use support::ScratchDir;
use support::rig::{Daemon, PATIENCE, Rig};

/// `logis` with no Wayland session and a scratch config home.
fn logis(dir: &ScratchDir) -> Command {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_logis"));
    cmd.env_remove("WAYLAND_SOCKET")
        .env_remove("WAYLAND_DISPLAY")
        .env("XDG_RUNTIME_DIR", dir.path())
        .env("XDG_CONFIG_HOME", dir.path().join("config"));
    cmd
}

fn write(path: &Path, text: &str) {
    std::fs::create_dir_all(path.parent().unwrap()).unwrap();
    std::fs::write(path, text).unwrap();
}

fn stdout(out: &Output) -> String {
    String::from_utf8_lossy(&out.stdout).into_owned()
}

fn stderr(out: &Output) -> String {
    String::from_utf8_lossy(&out.stderr).into_owned()
}

#[test]
fn check_config_accepts_a_sound_config_and_exits_0() {
    let dir = ScratchDir::new();
    let path = dir.path().join("mine.jsonc");
    write(
        &path,
        "{\n  // brass and fire\n  \"theme\": \"cogitator-phosphor\",\n  \"terminal\": [\"foot\", \"-e\"],\n}\n",
    );
    let out = logis(&dir)
        .arg("--check-config")
        .arg("--config")
        .arg(&path)
        .output()
        .unwrap();
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert!(stdout(&out).contains("is sound"), "{}", stdout(&out));
    assert!(
        stdout(&out).contains(&path.display().to_string()),
        "{}",
        stdout(&out)
    );
    assert!(out.stderr.is_empty(), "{}", stderr(&out));
}

#[test]
fn check_config_without_a_config_file_accepts_the_defaults() {
    let dir = ScratchDir::new();
    let out = logis(&dir).arg("--check-config").output().unwrap();
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    let expected = dir.path().join("config/logis/logis.jsonc");
    assert!(
        stdout(&out).contains(&expected.display().to_string()),
        "{}",
        stdout(&out)
    );
    assert!(stdout(&out).contains("defaults"), "{}", stdout(&out));
}

#[test]
fn check_config_reads_xdg_config_home_and_locates_an_unknown_key() {
    let dir = ScratchDir::new();
    let path = dir.path().join("config/logis/logis.jsonc");
    write(&path, "{\n  \"thme\": \"x\"\n}\n");
    let out = logis(&dir).arg("--check-config").output().unwrap();
    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty(), "{}", stdout(&out));
    let located = format!("{}:2:8: unknown field `thme`", path.display());
    assert!(stderr(&out).contains(&located), "{}", stderr(&out));
    assert!(
        stderr(&out).starts_with("logis: the config is heretical"),
        "{}",
        stderr(&out)
    );
}

#[test]
fn check_config_names_the_key_of_a_wrong_value() {
    let dir = ScratchDir::new();
    let path = dir.path().join("logis.jsonc");
    write(&path, "{\"focus-loss\": \"linger\"}");
    let out = logis(&dir)
        .args(["--config"])
        .arg(&path)
        .arg("--check-config")
        .output()
        .unwrap();
    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    let msg = stderr(&out);
    assert!(msg.contains(&format!("{}:1:", path.display())), "{msg}");
    assert!(
        msg.contains("`focus-loss`") && msg.contains("linger"),
        "{msg}"
    );
}

#[test]
fn check_config_with_a_missing_named_file_exits_2() {
    let dir = ScratchDir::new();
    let path = dir.path().join("nowhere.jsonc");
    let out = logis(&dir)
        .arg("--check-config")
        .arg("--config")
        .arg(&path)
        .output()
        .unwrap();
    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(stderr(&out).contains("no such file"), "{}", stderr(&out));
}

#[test]
fn a_daemon_refuses_to_start_on_an_invalid_config_and_its_client_exits_2() {
    let rig = Rig::new();
    write(&rig.config_file(), "{\"idle-timeout\": \"soon\"}");
    let out = rig.logis().arg("--dmenu").output().unwrap();
    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    let msg = stderr(&out);
    assert!(msg.contains("heretical"), "{msg}");
    assert!(
        msg.contains(&format!("{}:1:", rig.config_file().display())),
        "{msg}"
    );
    assert!(!rig.socket().exists(), "a daemon is listening anyway");
}

#[test]
fn a_client_hands_its_config_to_the_daemon_it_starts() {
    let rig = Rig::new();
    let dir = ScratchDir::new();
    let path = dir.path().join("named.jsonc");
    write(&path, "{\"heresy\": true}");
    let out = rig.logis().arg("--config").arg(&path).output().unwrap();
    assert_eq!(out.status.code(), Some(2), "stderr: {}", stderr(&out));
    assert!(
        stderr(&out).contains(&path.display().to_string()),
        "{}",
        stderr(&out)
    );
}

#[test]
fn a_running_daemon_keeps_its_config_whatever_a_caller_names() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let dir = ScratchDir::new();
    let path = dir.path().join("named.jsonc");
    write(&path, "{\"heresy\": true}");
    let mut caller = rig
        .logis()
        .arg("--config")
        .arg(&path)
        .arg("--dmenu")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    caller.stdin.take().unwrap().write_all(b">x\n").unwrap();
    let out = caller.wait_with_output().unwrap();
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert_eq!(out.stdout, b">x\n");
}

// Live reload in the daemon. The scripted resolver stands in for the window,
// so what the daemon applies is observed through its log (stderr) and through
// the setting that already acts: `idle-timeout`.

/// A daemon this test started, and its log lines as they are written.
struct Logged {
    daemon: Daemon,
    lines: Receiver<String>,
    seen: Vec<String>,
}

impl Logged {
    fn start(rig: &Rig, args: &[&str]) -> Self {
        let mut child = rig
            .logis()
            .arg("daemon")
            .args(args)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        let stderr = child.stderr.take().unwrap();
        let (tx, lines) = channel();
        thread::spawn(move || {
            for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                let _ = tx.send(line);
            }
        });
        let mut ready = [0];
        let n = child.stdout.as_mut().unwrap().read(&mut ready).unwrap();
        let mut logged = Self {
            daemon: Daemon::guard(child),
            lines,
            seen: Vec::new(),
        };
        assert_eq!(n, 1, "the daemon did not get ready: {:?}", logged.rest());
        logged
    }

    /// Waits for a log line containing every one of `parts`.
    fn line(&mut self, parts: &[&str]) -> String {
        let deadline = Instant::now() + PATIENCE;
        loop {
            let left = deadline.saturating_duration_since(Instant::now());
            match self.lines.recv_timeout(left) {
                Ok(line) if parts.iter().all(|p| line.contains(p)) => return line,
                Ok(line) => self.seen.push(line),
                Err(_) => panic!("no log line with {parts:?}; the log said {:?}", self.seen),
            }
        }
    }

    /// Whatever else was logged so far.
    fn rest(&mut self) -> Vec<String> {
        self.seen.extend(self.lines.try_iter());
        self.seen.clone()
    }
}

const HEEDS: &str = "heeds the amended config";

#[test]
fn the_config_idle_timeout_applies_from_the_start() {
    let rig = Rig::new();
    write(&rig.config_file(), "{\"idle-timeout\": 0.2}");
    let mut logged = Logged::start(&rig, &[]);
    let status = logged.daemon.wait_exit();
    assert!(status.success(), "{status}: {:?}", logged.rest());
}

#[test]
fn an_edit_in_place_is_applied_at_once() {
    let rig = Rig::new();
    write(&rig.config_file(), "{}");
    let mut logged = Logged::start(&rig, &[]);
    write(&rig.config_file(), "{\"idle-timeout\": 0.2} // rest soon");
    logged.line(&[HEEDS]);
    let status = logged.daemon.wait_exit();
    assert!(status.success(), "{status}: {:?}", logged.rest());
}

#[test]
fn an_atomically_replaced_config_is_applied() {
    let rig = Rig::new();
    write(&rig.config_file(), "{}");
    let mut logged = Logged::start(&rig, &[]);
    let tmp = rig.config_file().with_extension("jsonc.tmp");
    write(&tmp, "{\"idle-timeout\": 0.2}");
    std::fs::rename(&tmp, rig.config_file()).unwrap();
    logged.line(&[HEEDS]);
    let status = logged.daemon.wait_exit();
    assert!(status.success(), "{status}: {:?}", logged.rest());
}

#[test]
fn a_config_written_after_the_daemon_started_is_applied() {
    let rig = Rig::new();
    assert!(!rig.config_home().exists());
    let mut logged = Logged::start(&rig, &[]);
    write(&rig.config_file(), "{\"idle-timeout\": 0.2}");
    logged.line(&[HEEDS]);
    let status = logged.daemon.wait_exit();
    assert!(status.success(), "{status}: {:?}", logged.rest());
}

#[test]
fn an_invalid_edit_is_reported_and_the_last_sound_config_stays() {
    let rig = Rig::new();
    write(&rig.config_file(), "{\"idle-timeout\": 0.3}");
    let mut logged = Logged::start(&rig, &[]);
    // A connected client keeps the daemon from idling out meanwhile.
    let mut client = rig.connect();
    client.greet();

    write(&rig.config_file(), "{\n  \"idle-timeout\": \"soon\"\n}");
    let place = format!("{}:2:", rig.config_file().display());
    let line = logged.line(&["heretical", &place, "`idle-timeout`"]);
    assert!(line.contains("the last sound config stands"), "{line}");

    write(&rig.config_file(), "{\"idle-timout\": 0}");
    let place = format!("{}:1:", rig.config_file().display());
    logged.line(&["heretical", &place, "unknown field `idle-timout`"]);

    // Had an invalid edit replaced the config, the daemon would stay resident.
    drop(client);
    let status = logged.daemon.wait_exit();
    assert!(status.success(), "{status}: {:?}", logged.rest());
    assert!(
        !logged.rest().iter().any(|l| l.contains(HEEDS)),
        "an invalid edit was applied: {:?}",
        logged.rest()
    );
}

#[test]
fn a_sound_edit_after_an_invalid_one_is_applied() {
    let rig = Rig::new();
    write(&rig.config_file(), "{}");
    let mut logged = Logged::start(&rig, &[]);
    write(&rig.config_file(), "{\"focus-loss\": \"linger\"}");
    logged.line(&["heretical", "`focus-loss`"]);
    write(
        &rig.config_file(),
        "{\"focus-loss\": \"keep\", \"idle-timeout\": 0.2}",
    );
    logged.line(&[HEEDS]);
    let status = logged.daemon.wait_exit();
    assert!(status.success(), "{status}: {:?}", logged.rest());
}

#[test]
fn the_idle_timeout_flag_outranks_the_config() {
    let rig = Rig::new();
    write(&rig.config_file(), "{\"idle-timeout\": 0.1}");
    let mut logged = Logged::start(&rig, &["--idle-timeout", "0"]);
    write(&rig.config_file(), "{\"idle-timeout\": 0.2}");
    logged.line(&[HEEDS]);
    // Elapsed time is the behaviour under test: neither timeout may end it.
    thread::sleep(Duration::from_millis(500));
    assert!(logged.daemon.is_running(), "{:?}", logged.rest());
}

#[test]
fn a_daemon_started_by_a_client_reloads_after_the_client_is_gone() {
    let rig = Rig::new();
    write(&rig.config_file(), "{}");
    // The client starts a detached daemon and exits: the daemon's starter
    // stderr is closed from here on.
    let out = rig.logis().output().unwrap();
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    let pid = rig.connect().greet()["pid"].as_u64().unwrap();
    let alive = || Path::new(&format!("/proc/{pid}")).exists();

    let log = rig.socket().with_file_name("logis.log");
    let logged =
        |needle: &str| std::fs::read_to_string(&log).is_ok_and(|text| text.contains(needle));
    write(&rig.config_file(), "{\"theme\": \"../escape\"}");
    support::rig::wait_until("the heresy in logis.log", || logged("heretical"));
    assert!(alive(), "the daemon died reporting a heresy");

    write(&rig.config_file(), "{\"idle-timeout\": 0.2}");
    support::rig::wait_until("the reload in logis.log", || logged(HEEDS));
    support::rig::wait_until("the daemon to idle out", || !alive());
}
