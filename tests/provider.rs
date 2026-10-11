//! Provider sessions through a real daemon and real provider processes
//! (spec §4.8), with the scripted resolver standing in for the window (it
//! chooses the first settled item marked `>`; see src/daemon/resolver.rs).
//! The fixture provider is tests/fixtures/providers/forge.sh.

mod support;

use std::path::{Path, PathBuf};
use std::process::{Child, Output, Stdio};
use std::time::{Duration, Instant};

use support::ScratchDir;
use support::rig::{Rig, wait_until};

fn forge() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/providers/forge.sh")
}

/// `logis --provider forge.sh MODE` in this rig, its fixture files in `dir`.
fn provider(rig: &Rig, dir: &Path, mode: &str) -> Child {
    rig.logis()
        .arg("--provider")
        .arg(forge())
        .arg(mode)
        .env("FORGE_DIR", dir)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap()
}

fn stderr(out: &Output) -> String {
    String::from_utf8_lossy(&out.stderr).into_owned()
}

fn heard(dir: &Path) -> String {
    std::fs::read_to_string(dir.join("heard")).unwrap_or_default()
}

fn pid_in(dir: &Path, name: &str) -> i32 {
    let path = dir.join(name);
    wait_until(&format!("{name} to be written"), || {
        std::fs::read_to_string(&path).is_ok_and(|t| t.ends_with('\n'))
    });
    std::fs::read_to_string(path)
        .unwrap()
        .trim()
        .parse()
        .unwrap()
}

/// Whether process `pid` still runs (a zombie counts as gone).
fn alive(pid: i32) -> bool {
    std::fs::read_to_string(format!("/proc/{pid}/stat")).is_ok_and(|stat| {
        !stat
            .rsplit(')')
            .next()
            .unwrap_or("")
            .trim_start()
            .starts_with('Z')
    })
}

fn end(pid: i32) {
    let _ = std::process::Command::new("kill")
        .arg("-9")
        .arg(pid.to_string())
        .status();
}

#[test]
fn a_two_stage_flow_runs_to_the_providers_close() {
    let rig = Rig::new();
    let dir = ScratchDir::new();
    let mut command = rig.logis();
    command
        .arg("--provider")
        .arg(forge())
        .arg("flow")
        .env("FORGE_DIR", dir.path())
        .env("FORGE_TOKEN", "the caller's own")
        .current_dir(dir.path());
    let out = command.output().unwrap();
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    let heard = heard(dir.path());
    let cwd = dir.path().canonicalize().unwrap();
    assert!(
        heard.starts_with(&format!("cwd {}\ntoken the caller's own\n", cwd.display())),
        "{heard}"
    );
    let lines: Vec<&str> = heard.lines().skip(2).collect();
    assert_eq!(
        lines,
        [
            r#"{"type":"hello","protocol":1}"#,
            r#"{"type":"activate","stage":"rite","id":"oil"}"#,
            r#"{"type":"activate","stage":"aspect","id":"on"}"#,
        ],
        "the pending rite was chosen only once settled, then its aspect"
    );
}

#[test]
fn a_dismissed_provider_that_acted_is_never_signalled_nor_its_action() {
    let rig = Rig::new();
    let dir = ScratchDir::new();
    let mut client = provider(&rig, dir.path(), "act");
    let action = pid_in(dir.path(), "action");
    let provider = pid_in(dir.path(), "provider");
    // The caller leaves (as a dismissal would end it): the session is over.
    client.kill().unwrap();
    client.wait().unwrap();
    rig.wait_until_no_session_remains();
    // Past both graces a provider that never acted would get.
    std::thread::sleep(Duration::from_millis(2500));
    let (provider_alive, action_alive) = (alive(provider), alive(action));
    end(provider);
    end(action);
    assert!(action_alive, "the action was killed");
    assert!(
        provider_alive,
        "the provider that started an action was signalled"
    );
}

#[test]
fn a_provider_that_never_acted_and_lingers_after_cancel_is_ended() {
    let rig = Rig::new();
    let dir = ScratchDir::new();
    let mut client = provider(&rig, dir.path(), "stubborn");
    let provider = pid_in(dir.path(), "provider");
    let since = Instant::now();
    client.kill().unwrap();
    client.wait().unwrap();
    wait_until("the stubborn provider to be ended", || !alive(provider));
    assert!(
        since.elapsed() >= Duration::from_millis(1800),
        "ended after {:?}: before its graces (it ignores SIGTERM)",
        since.elapsed()
    );
}

#[test]
fn a_provider_that_shows_nothing_in_time_fails_its_session() {
    let rig = Rig::new();
    let dir = ScratchDir::new();
    let since = Instant::now();
    let out = provider(&rig, dir.path(), "mute")
        .wait_with_output()
        .unwrap();
    assert_eq!(out.status.code(), Some(2));
    assert!(
        stderr(&out).contains("showed nothing within 5 s"),
        "{}",
        stderr(&out)
    );
    assert!(since.elapsed() >= Duration::from_secs(5));
}

#[test]
fn a_provider_that_breaks_the_protocol_or_departs_fails_its_session() {
    let rig = Rig::new();
    let dir = ScratchDir::new();
    let out = provider(&rig, dir.path(), "garbage")
        .wait_with_output()
        .unwrap();
    assert_eq!(out.status.code(), Some(2));
    assert!(stderr(&out).contains("not a JSON line"), "{}", stderr(&out));
    let out = provider(&rig, dir.path(), "departs")
        .wait_with_output()
        .unwrap();
    assert_eq!(out.status.code(), Some(2));
    assert!(stderr(&out).contains("exit status: 3"), "{}", stderr(&out));
}

#[test]
fn a_provider_that_cannot_start_is_an_error() {
    let rig = Rig::new();
    let out = rig
        .logis()
        .args(["--provider", "/nonexistent/rite"])
        .output()
        .unwrap();
    assert_eq!(out.status.code(), Some(2));
    assert!(
        stderr(&out).contains("/nonexistent/rite"),
        "{}",
        stderr(&out)
    );
}
