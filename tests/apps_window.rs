//! The app launcher end to end (spec §3 stories 5–7): a real daemon in the
//! test compositor (nested niri locally, headless sway on CI) reading the
//! fixture applications tests/fixtures/apps/e2e with icons from the fixture
//! theme, `logis --apps`, keys from the harness's virtual keyboard, and the
//! fixture program tests/fixtures/apps/bin/anoint recording how it was
//! started. The daemon finds no session bus (its runtime dir is scratch),
//! so apps start in the client's environment, outside any scope. Runs only
//! with `LOGIS_E2E=niri|sway`; otherwise each test prints `SKIPPED`.

mod support;

use std::ffi::OsStr;
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use logis::list::{Options, scene};
use logis::render::Image;
use logis::theme::Theme;
use support::ScratchDir;
use support::e2e::cogitator::Cogitator;
use support::e2e::keyboard::{Modifier, code};
use support::e2e::screencopy::crop_centred;
use support::e2e::wire::Harness;
use support::golden::{Goldens, Tolerance};

fn fixtures() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures")
}

/// A daemon over the E2E fixtures, its state in `dir`.
fn start(dir: &Path) -> Option<Cogitator> {
    let data_dirs =
        std::env::join_paths([fixtures().join("apps/e2e"), fixtures().join("icons/share")])
            .unwrap();
    let (home, state) = (dir.join("data-home"), dir.join("state"));
    let config = std::fs::read_to_string(fixtures().join("apps/e2e/logis.jsonc")).unwrap();
    let env = [
        ("XDG_DATA_DIRS", data_dirs.to_str().unwrap()),
        ("XDG_DATA_HOME", home.to_str().unwrap()),
        ("XDG_STATE_HOME", state.to_str().unwrap()),
    ];
    Cogitator::start_with(&env, Some(&config))
}

/// The client's environment: the fixture program on the PATH, where it
/// records itself, and the file that keeps it running.
fn client_env(dir: &Path) -> Vec<(&'static str, std::ffi::OsString)> {
    let path = format!("{}:/usr/bin:/bin", fixtures().join("apps/bin").display());
    vec![
        ("PATH", path.into()),
        ("HOME", dir.into()),
        ("ANOINT_RECORD", dir.join("record").into()),
        ("ANOINT_HOLD", dir.join("hold").into()),
    ]
}

fn heard(dir: &Path) -> Vec<(String, String)> {
    let record = dir.join("record");
    let deadline = Instant::now() + Duration::from_secs(5);
    while !record.exists() {
        assert!(Instant::now() < deadline, "the app never ran");
        std::thread::sleep(Duration::from_millis(10));
    }
    std::fs::read_to_string(record)
        .unwrap()
        .lines()
        .map(|l| {
            let (k, v) = l.split_once(' ').unwrap_or((l, ""));
            (k.to_owned(), v.to_owned())
        })
        .collect()
}

fn values(heard: &[(String, String)], key: &str) -> Vec<String> {
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
fn return_launches_the_app_detached_with_an_activation_token() {
    let dir = ScratchDir::new();
    let Some(mut c) = start(dir.path()) else {
        return;
    };
    std::fs::write(dir.path().join("hold"), b"").unwrap();
    let env = client_env(dir.path());
    let env: Vec<(&str, &OsStr)> = env.iter().map(|(k, v)| (*k, v.as_os_str())).collect();
    let client = c.apps(&env);
    let focused = c.shown(2);
    // The app launcher opens in the theme's own window.
    let size = scene::fit(&Options::default(), &Theme::bundled_default()).0;
    check_window(&mut c.harness, "apps", size);
    c.type_text(&focused, "unguent");
    assert_eq!(c.next("select"), "0", "matched by its keywords");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "0");
    c.next("token");
    let ended = client.ended();
    assert_eq!(ended.code, Some(0));
    c.next("hide");

    let heard = heard(dir.path());
    assert_eq!(values(&heard, "arg"), ["--e2e", "--name=Anoint the vessel"]);
    let token = values(&heard, "token").remove(0);
    assert!(!token.is_empty(), "the compositor's activation token");
    assert_eq!(values(&heard, "startup"), [token]);
    let pid: i32 = values(&heard, "pid")[0].parse().unwrap();
    assert_eq!(
        values(&heard, "sid"),
        [pid.to_string()],
        "a session of its own"
    );
    let daemon_pid = c.daemon.pid();
    assert_ne!(values(&heard, "ppid"), ["0".to_owned()]);

    // It outlives the daemon (and so logis).
    let _ = std::process::Command::new("kill")
        .arg("-9")
        .arg(daemon_pid.to_string())
        .status();
    std::thread::sleep(Duration::from_millis(200));
    assert!(alive(pid), "the app died with the daemon");
    std::fs::remove_file(dir.path().join("hold")).unwrap();
    let deadline = Instant::now() + Duration::from_secs(5);
    while alive(pid) {
        assert!(Instant::now() < deadline, "the app never ended");
        std::thread::sleep(Duration::from_millis(10));
    }
}

#[test]
fn shift_return_opens_the_desktop_actions_as_a_second_stage() {
    let dir = ScratchDir::new();
    let Some(mut c) = start(dir.path()) else {
        return;
    };
    let env = client_env(dir.path());
    let env: Vec<(&str, &OsStr)> = env.iter().map(|(k, v)| (*k, v.as_os_str())).collect();
    let client = c.apps(&env);
    let focused = c.shown(2);
    c.harness.hold(&mut c.kb, &focused, Modifier::Shift);
    c.tap(&focused, code::ENTER);
    c.harness.release(&mut c.kb, &focused, Modifier::Shift);
    assert_eq!(c.next("descend"), "0");
    c.next("stage");
    assert_eq!(c.next("items"), "2", "the app itself and its one action");
    c.tap(&focused, code::DOWN);
    assert_eq!(c.next("select"), "1");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "1");
    assert_eq!(client.ended().code, Some(0));
    let heard = heard(dir.path());
    assert_eq!(values(&heard, "arg"), ["--purge"]);
    let frecency = dir.path().join("state/logis/frecency.json");
    let deadline = Instant::now() + Duration::from_secs(5);
    while !std::fs::read_to_string(&frecency).is_ok_and(|t| t.contains("forge-anoint.desktop")) {
        assert!(Instant::now() < deadline, "frecency was never kept");
        std::thread::sleep(Duration::from_millis(10));
    }
}

/// Captures until two consecutive captures agree, then compares the window
/// cropped from the output with golden `e2e/<name>`.
fn check_window(harness: &mut Harness, name: &str, size: (u32, u32)) {
    let mut last = harness.capture();
    let capture = loop {
        let next = harness.capture();
        if next == last {
            break next;
        }
        last = next;
    };
    let goldens = Goldens::checked_in();
    let reference: Option<Image> = (!goldens.updating())
        .then(|| goldens.load("e2e", name).ok())
        .flatten();
    let window = crop_centred(&capture, size, reference.as_ref());
    let tolerance = Tolerance {
        channel: 4,
        outliers: 0.001,
    };
    let verdict = goldens.assert("e2e", name, &window, tolerance);
    eprintln!("e2e/{name}: {verdict:?}");
}

/// Plain `logis` is the app launcher (as in fuzzel); dismissing it launches
/// nothing and exits 1 with an empty stdout.
#[test]
fn plain_logis_opens_the_launcher_and_escape_exits_1() {
    let dir = ScratchDir::new();
    let Some(mut c) = start(dir.path()) else {
        return;
    };
    let mut command = c.compositor.logis();
    command
        .envs(client_env(dir.path()))
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped());
    let client = support::e2e::cogitator::Client {
        child: command.spawn().expect("logis starts"),
        stdin: None,
    };
    let focused = c.shown(2);
    c.tap(&focused, code::ESC);
    c.next("dismiss");
    let ended = client.ended();
    assert_eq!((ended.code, ended.stdout.as_slice()), (Some(1), &b""[..]));
    assert!(!dir.path().join("record").exists(), "nothing was launched");
}
