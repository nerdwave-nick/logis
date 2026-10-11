//! Provider sessions end to end (spec §4.8): a real daemon in the test
//! compositor (nested niri locally, headless sway on CI), `logis --provider`
//! running the fixture provider tests/fixtures/providers/forge.sh, and keys
//! from the harness's virtual keyboard. Runs only with `LOGIS_E2E=niri|sway`;
//! otherwise each test prints `SKIPPED`. Waits follow the daemon's test
//! trace (see src/daemon/window/ui.rs).

mod support;

use std::ffi::OsStr;
use std::path::{Path, PathBuf};
use std::time::Duration;

use logis::list::{Options, scene};
use logis::render::Image;
use logis::theme::Theme;
use support::ScratchDir;
use support::e2e::cogitator::Cogitator;
use support::e2e::keyboard::code;
use support::e2e::screencopy::crop_centred;
use support::e2e::wire::Harness;
use support::golden::{Goldens, Tolerance};

fn forge() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/providers/forge.sh")
}

#[test]
fn a_two_stage_flow_with_a_pending_row_ends_in_a_toast() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let dir = ScratchDir::new();
    let gate = dir.path().join("gate");
    let forge = forge();
    let argv = [forge.as_os_str(), OsStr::new("flow")];
    let client = c.provider(&argv, &[("FORGE_DIR", dir.path()), ("FORGE_GATE", &gate)]);

    // The rite stage: its first row is still pending, so Return does
    // nothing. Down and Up after it prove the key was handled.
    let focused = c.shown(2);
    // The first stage sizes the window (forge.sh asks for 6 lines, 44 wide).
    let options = Options {
        lines: Some(6),
        width: Some(44),
        ..Options::default()
    };
    let size = scene::fit(&options, &Theme::bundled_default()).0;
    check_window(&mut c.harness, "provider-pending", size);
    c.tap(&focused, code::ENTER);
    c.tap(&focused, code::DOWN);
    assert_eq!(c.next("select"), "1");
    c.tap(&focused, code::UP);
    assert_eq!(c.next("select"), "0");
    let accepted = |c: &Cogitator| {
        c.daemon
            .events()
            .iter()
            .any(|e| e.what.starts_with("accept"))
    };
    assert!(!accepted(&c), "a pending row was accepted");

    // It settles; now Return takes it to the aspect stage, in the same window.
    std::fs::write(&gate, b"").unwrap();
    assert_eq!(c.next("update"), "0");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "0");
    c.next("stage");
    assert_eq!(c.next("items"), "2");

    // Choosing an aspect: the provider toasts and closes; the caller is
    // answered at once, the window lingers on the toast without the
    // keyboard, then goes.
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "0");
    assert_eq!(c.next("toast"), "\"the rite of oil is begun\"");
    c.next("linger");
    check_window(&mut c.harness, "provider-toast", size);
    let ended = client.ended();
    assert_eq!((ended.code, ended.stdout.as_slice()), (Some(0), &b""[..]));
    c.next("hide");
    let heard = std::fs::read_to_string(dir.path().join("heard")).unwrap();
    assert!(
        heard.contains(r#"{"type":"activate","stage":"aspect","id":"on"}"#),
        "{heard}"
    );
}

#[test]
fn escape_after_an_activation_never_kills_the_action_it_started() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let dir = ScratchDir::new();
    let forge = forge();
    let argv = [forge.as_os_str(), OsStr::new("act")];
    let client = c.provider(&argv, &[("FORGE_DIR", dir.path())]);
    let focused = c.shown(1);
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "0");
    let action = pid_in(dir.path(), "action");
    let provider = pid_in(dir.path(), "provider");
    c.tap(&focused, code::ESC);
    c.next("dismiss");
    assert_eq!(client.ended().code, Some(1));
    // Past both graces a provider that never acted would get.
    std::thread::sleep(Duration::from_millis(2500));
    let (provider_alive, action_alive) = (alive(provider), alive(action));
    for pid in [provider, action] {
        let _ = std::process::Command::new("kill")
            .arg("-9")
            .arg(pid.to_string())
            .status();
    }
    assert!(action_alive, "dismissal killed the action");
    assert!(
        provider_alive,
        "dismissal signalled the provider that started it"
    );
}

fn pid_in(dir: &Path, name: &str) -> i32 {
    let path = dir.join(name);
    for _ in 0..500 {
        if let Ok(text) = std::fs::read_to_string(&path)
            && text.ends_with('\n')
        {
            return text.trim().parse().unwrap();
        }
        std::thread::sleep(Duration::from_millis(10));
    }
    panic!("{name} was never written");
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
