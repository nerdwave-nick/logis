//! servitor mode through a real daemon (spec §3 stories 8–10, §4.8):
//! `logis --servitor` with the scripted resolver standing in for the window
//! (it chooses the first settled row starting with `>` and types an
//! inscription's placeholder after its `>`; src/daemon/resolver.rs) and a
//! fake `servitor` on the caller's PATH (tests/fixtures/servitor/servitor)
//! telling census lines recorded from the real one and recording how it
//! was invoked.

mod support;

use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

use support::ScratchDir;
use support::rig::{Rig, wait_until};

fn fixture(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/servitor")
        .join(name)
}

/// A caller's world: a directory with `bin/servitor` (the fake) and the
/// fake's records.
struct World {
    dir: ScratchDir,
}

impl World {
    fn new() -> Self {
        let dir = ScratchDir::new();
        let bin = dir.path().join("bin");
        std::fs::create_dir_all(&bin).unwrap();
        std::os::unix::fs::symlink(fixture("servitor"), bin.join("servitor")).unwrap();
        Self { dir }
    }

    fn path(&self) -> &Path {
        self.dir.path()
    }

    /// `logis --servitor` as this caller, the fake telling `census`; the
    /// invocation labours `seconds`.
    fn servitor(&self, rig: &Rig, census: &str, seconds: &str) -> Command {
        let path = format!(
            "{}:{}",
            self.path().join("bin").display(),
            std::env::var("PATH").unwrap_or_default()
        );
        let mut command = rig.logis();
        command
            .arg("--servitor")
            .env("PATH", path)
            .env("FAKE_SERVITOR_DIR", self.path())
            .env("FAKE_SERVITOR_CENSUS", fixture(census))
            .env("FAKE_INVOKE_SECONDS", seconds)
            .env("FAKE_TOKEN", "the caller's own")
            .current_dir(self.path())
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        command
    }

    fn record(&self, name: &str) -> String {
        std::fs::read_to_string(self.path().join("invoke").join(name)).unwrap()
    }

    fn invoked(&self) -> bool {
        self.path().join("invoke/recorded").exists()
    }

    fn done(&self) -> bool {
        self.path().join("invoke/done").exists()
    }

    fn census_pid(&self) -> i32 {
        let path = self.path().join("census.pid");
        wait_until("the census to start", || {
            std::fs::read_to_string(&path).is_ok_and(|t| t.ends_with('\n'))
        });
        std::fs::read_to_string(path)
            .unwrap()
            .trim()
            .parse()
            .unwrap()
    }
}

/// Now, as the fake stamps it: nanoseconds of the wall clock.
fn stamp() -> u128 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_nanos()
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

fn wait_out(mut child: Child) -> (Option<i32>, Vec<u8>, String) {
    let deadline = Instant::now() + Duration::from_secs(10);
    while child.try_wait().unwrap().is_none() {
        assert!(Instant::now() < deadline, "the caller never ended");
        std::thread::sleep(Duration::from_millis(10));
    }
    let out = child.wait_with_output().unwrap();
    let stderr = String::from_utf8_lossy(&out.stderr).into_owned();
    (out.status.code(), out.stdout, stderr)
}

#[test]
fn servitor_mode_invokes_detached_once_settled_and_the_caller_does_not_wait() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let world = World::new();
    let client = world.servitor(&rig, "chosen.jsonl", "3").spawn().unwrap();

    // The chosen rite is still awaited: it is pending, so nothing is invoked
    // however long the auspex takes.
    world.census_pid();
    std::thread::sleep(Duration::from_millis(400));
    assert!(!world.invoked(), "a pending rite was invoked");

    // Its auspex settles; the rite, its aspect and inscriptions follow, and
    // servitor is invoked. The caller is answered at once, exit 0.
    let settled = stamp();
    std::fs::write(world.path().join("gate"), b"").unwrap();
    let (code, stdout, stderr) = wait_out(client);
    let answered = stamp();
    assert_eq!(code, Some(0), "stderr: {stderr}");
    assert!(stdout.is_empty());
    wait_until("the invocation to be recorded", || world.invoked());
    let started: u128 = world.record("start").trim().parse().unwrap();
    assert!(started > settled, "invoked before its rite settled");
    assert!(
        !world.done(),
        "logis waited for the invocation ({} ms after it began)",
        (answered - started) / 1_000_000
    );

    // How it was launched (servitor S7): the argv, a session and process
    // group of its own, stdio on /dev/null, the caller's environment and
    // working directory.
    assert_eq!(
        world.record("argv").lines().collect::<Vec<_>>(),
        [
            ">dnd",
            ">on",
            "--reason",
            "the forge demands focus",
            "--origin",
            "a hotkey of the adept"
        ]
    );
    let pid = world.record("pid").trim().to_owned();
    let ids: Vec<String> = world
        .record("ids")
        .split_whitespace()
        .map(str::to_owned)
        .collect();
    assert_eq!(ids, [pid.clone(), pid], "its own session and process group");
    assert_eq!(
        world.record("fds").lines().collect::<Vec<_>>(),
        ["/dev/null"; 3]
    );
    assert_eq!(world.record("token").trim(), "the caller's own");
    let cwd = world.path().canonicalize().unwrap();
    assert_eq!(Path::new(world.record("cwd").trim()), cwd);

    // It labours on, unwatched, to its end.
    let deadline = Instant::now() + Duration::from_secs(10);
    while !world.done() {
        assert!(Instant::now() < deadline, "the invocation never finished");
        std::thread::sleep(Duration::from_millis(20));
    }
}

#[test]
fn an_edict_is_invoked_without_an_aspect() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let world = World::new();
    let (code, _, stderr) = wait_out(world.servitor(&rig, "edict.jsonl", "0").spawn().unwrap());
    assert_eq!(code, Some(0), "stderr: {stderr}");
    wait_until("the invocation to be recorded", || world.invoked());
    assert_eq!(
        world.record("argv").lines().collect::<Vec<_>>(),
        [">screenshot", "--region", "whole"]
    );
}

#[test]
fn a_rite_that_never_settles_is_never_invoked_and_leaving_ends_the_census() {
    let rig = Rig::new();
    let _daemon = rig.start_daemon(&[]);
    let world = World::new();
    let mut client = world
        .servitor(&rig, "lingering.jsonl", "0")
        .spawn()
        .unwrap();
    let census = world.census_pid();
    std::thread::sleep(Duration::from_millis(500));
    // The caller leaves, as a dismissal ends a session: logis cancels it.
    client.kill().unwrap();
    client.wait().unwrap();
    wait_until("the census to end once its reader departed", || {
        !alive(census)
    });
    assert!(!world.invoked(), "a pending rite was invoked");
}

#[test]
fn no_dismissal_or_departure_of_logis_aborts_a_running_invocation() {
    let rig = Rig::new();
    let mut daemon = rig.start_daemon(&[]);
    let world = World::new();
    std::fs::write(world.path().join("gate"), b"").unwrap();
    let (code, _, stderr) = wait_out(world.servitor(&rig, "chosen.jsonl", "2").spawn().unwrap());
    assert_eq!(code, Some(0), "stderr: {stderr}");
    wait_until("the invocation to be recorded", || world.invoked());
    let invocation: i32 = world.record("pid").trim().parse().unwrap();

    // servitor mode opened again and dismissed (its caller leaves), then
    // the daemon itself goes: the invocation labours on.
    let again = World::new();
    let mut client = again
        .servitor(&rig, "lingering.jsonl", "0")
        .spawn()
        .unwrap();
    again.census_pid();
    client.kill().unwrap();
    client.wait().unwrap();
    let _ = Command::new("kill")
        .arg(daemon.pid().to_string())
        .status()
        .unwrap();
    daemon.wait_exit();
    assert!(
        alive(invocation) && !world.done(),
        "the invocation was cut short"
    );
    wait_until("the invocation to finish", || world.done());
}

#[test]
fn servitor_mode_runs_logis_itself_as_its_provider() {
    // `logis provide servitor` speaks the provider protocol on its own:
    // greeting, the rite stage at once, then the census as it is told.
    let world = World::new();
    let path = format!(
        "{}:{}",
        world.path().join("bin").display(),
        std::env::var("PATH").unwrap_or_default()
    );
    let mut provider = Command::new(env!("CARGO_BIN_EXE_logis"))
        .args(["provide", "servitor"])
        .env("PATH", path)
        .env("FAKE_SERVITOR_DIR", world.path())
        .env("FAKE_SERVITOR_CENSUS", fixture("edict.jsonl"))
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    let mut stdin = provider.stdin.take().unwrap();
    stdin
        .write_all(b"{\"type\":\"hello\",\"protocol\":1}\n")
        .unwrap();
    let mut lines = BufReader::new(provider.stdout.take().unwrap()).lines();
    let mut next =
        || -> serde_json::Value { serde_json::from_str(&lines.next().unwrap().unwrap()).unwrap() };
    assert_eq!(next()["type"], "hello");
    let stage = next();
    assert_eq!(
        (stage["type"].as_str(), stage["id"].as_str()),
        (Some("stage"), Some("rite"))
    );
    let items = next();
    assert_eq!(items["type"], "items");
    assert_eq!(items["items"][0]["id"], ">screenshot");
    stdin.write_all(b"{\"type\":\"cancel\"}\n").unwrap();
    drop(stdin);
    let status = provider.wait().unwrap();
    assert_eq!(status.code(), Some(0));
    assert!(!world.invoked());
}
