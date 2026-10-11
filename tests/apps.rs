//! The app launcher through a real daemon (spec §3 stories 5–7), with the
//! scripted resolver standing in for the window (it chooses the app whose
//! name starts with `>` and hands out a stand-in token, `scripted-<n>`;
//! see src/daemon/resolver.rs). The daemon reads fixture applications
//! directories only, keeps frecency in a scratch state dir, and finds no
//! session bus; the launched program is the fixture
//! tests/fixtures/apps/bin/anoint, which records how it was started.

mod support;

use std::ffi::OsStr;
use std::path::{Path, PathBuf};
use std::process::{Child, Stdio};
use std::time::{Duration, Instant};

use support::ScratchDir;
use support::rig::{Daemon, Rig, wait_until};

fn fixtures() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/apps")
}

struct Launcher {
    rig: Rig,
    dir: ScratchDir,
    daemon: Daemon,
}

impl Launcher {
    /// A daemon whose data dirs are `data_home` and `data_dirs` (the
    /// scripted fixtures by default).
    fn start(data_home: Option<&Path>, data_dirs: Option<&Path>) -> Self {
        let rig = Rig::new();
        let dir = ScratchDir::new();
        let state = dir.path().join("state");
        let home = data_home.map_or_else(|| dir.path().join("no-data-home"), Path::to_owned);
        let data_dirs = data_dirs.map_or_else(|| fixtures().join("scripted"), Path::to_owned);
        let env: [(&str, &OsStr); 4] = [
            ("XDG_DATA_HOME", home.as_os_str()),
            ("XDG_DATA_DIRS", data_dirs.as_os_str()),
            ("XDG_STATE_HOME", state.as_os_str()),
            ("PATH", OsStr::new("/usr/bin:/bin")),
        ];
        let daemon = rig.start_daemon_env(&[], &env);
        Self { rig, dir, daemon }
    }

    fn record(&self) -> PathBuf {
        self.dir.path().join("record")
    }

    fn hold(&self) -> PathBuf {
        self.dir.path().join("hold")
    }

    /// `logis --apps`, its environment the one an app gets without systemd.
    fn apps(&self) -> Child {
        std::fs::write(self.hold(), b"").unwrap();
        let path = format!("{}:/usr/bin:/bin", fixtures().join("bin").display());
        self.rig
            .logis()
            .arg("--apps")
            .env("PATH", path)
            .env("HOME", self.dir.path())
            .env("ANOINT_RECORD", self.record())
            .env("ANOINT_HOLD", self.hold())
            .env("ANOINT_MARKER", "client")
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap()
    }

    fn heard(&self) -> Vec<(String, String)> {
        wait_until("the app to record itself", || self.record().exists());
        std::fs::read_to_string(self.record())
            .unwrap()
            .lines()
            .map(|l| {
                let (k, v) = l.split_once(' ').unwrap_or((l, ""));
                (k.to_owned(), v.to_owned())
            })
            .collect()
    }
}

fn value(heard: &[(String, String)], key: &str) -> Vec<String> {
    heard
        .iter()
        .filter(|(k, _)| k == key)
        .map(|(_, v)| v.clone())
        .collect()
}

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

#[test]
fn the_chosen_app_is_launched_with_its_token_and_counts_for_frecency() {
    let mut l = Launcher::start(None, None);
    let out = l.apps().wait_with_output().unwrap();
    assert_eq!(
        out.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    assert!(out.stdout.is_empty());
    let heard = l.heard();
    assert_eq!(
        value(&heard, "arg"),
        ["--rite", "--name=>Rite of Anointing"]
    );
    assert_eq!(
        value(&heard, "marker"),
        ["client"],
        "the client's environment"
    );
    let token = value(&heard, "token").remove(0);
    assert!(token.starts_with("scripted-"), "{token}");
    let pid: i32 = value(&heard, "pid")[0].parse().unwrap();
    assert_eq!(
        value(&heard, "sid"),
        [pid.to_string()],
        "a session of its own"
    );

    let frecency = l.dir.path().join("state/logis/frecency.json");
    wait_until("frecency to be kept", || {
        std::fs::read_to_string(&frecency).is_ok_and(|t| t.contains("forge-rite.desktop"))
    });

    // The app outlives the daemon that launched it.
    let _ = std::process::Command::new("kill")
        .arg("-9")
        .arg(l.daemon.pid().to_string())
        .status();
    l.daemon.wait_exit();
    std::thread::sleep(Duration::from_millis(100));
    assert!(alive(pid), "the app died with the daemon");
    std::fs::remove_file(l.hold()).unwrap();
    wait_until("the app to end", || !alive(pid));
}

/// Plain `logis`, without arguments, is the app launcher, as in fuzzel.
#[test]
fn plain_logis_is_the_app_launcher() {
    let l = Launcher::start(None, None);
    std::fs::write(l.hold(), b"").unwrap();
    let path = format!("{}:/usr/bin:/bin", fixtures().join("bin").display());
    let out = l
        .rig
        .logis()
        .env("PATH", path)
        .env("HOME", l.dir.path())
        .env("ANOINT_RECORD", l.record())
        .stdin(Stdio::null())
        .output()
        .unwrap();
    assert_eq!(
        out.status.code(),
        Some(0),
        "{}",
        String::from_utf8_lossy(&out.stderr)
    );
    assert!(out.stdout.is_empty());
    assert_eq!(value(&l.heard(), "arg")[0], "--rite");
}

#[test]
fn an_app_installed_while_the_daemon_runs_is_found() {
    let data_home = ScratchDir::new();
    let nothing = ScratchDir::new();
    let apps = data_home.path().join("applications");
    let l = Launcher::start(Some(data_home.path()), Some(nothing.path()));
    // The first census (of nothing) is taken at once; install after it.
    std::thread::sleep(Duration::from_millis(300));
    // No marked app anywhere: a session now would wait for one. The
    // applications directory does not exist yet: it is watched through its
    // ancestor, and its new subdirectory through it.
    std::fs::create_dir_all(apps.join("forge")).unwrap();
    std::fs::copy(
        fixtures().join("scripted/applications/forge-rite.desktop"),
        apps.join("forge/late.desktop"),
    )
    .unwrap();
    let deadline = Instant::now() + Duration::from_secs(5);
    loop {
        let mut client = l.apps();
        let waited = Instant::now();
        let status = loop {
            if let Some(status) = client.try_wait().unwrap() {
                break Some(status);
            }
            if waited.elapsed() > Duration::from_millis(500) {
                let _ = client.kill();
                let _ = client.wait();
                break None;
            }
            std::thread::sleep(Duration::from_millis(10));
        };
        if let Some(status) = status {
            assert_eq!(status.code(), Some(0));
            break;
        }
        assert!(Instant::now() < deadline, "the new app was never indexed");
    }
    let heard = l.heard();
    assert_eq!(value(&heard, "arg")[0], "--rite");
    let _ = std::fs::remove_file(l.hold());
}
