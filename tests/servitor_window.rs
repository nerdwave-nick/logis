//! servitor mode in the window (spec §3 stories 8–10): a real daemon in the
//! test compositor (nested niri locally, headless sway on CI), `logis
//! --servitor` with the fake servitor on its PATH
//! (tests/fixtures/servitor/servitor, census window.jsonl), keys from the
//! harness's virtual keyboard. Runs only with `LOGIS_E2E=niri|sway`;
//! otherwise it prints `SKIPPED`.

mod support;

use std::path::{Path, PathBuf};
use std::process::Stdio;

use logis::list::{Options, scene};
use logis::render::Image;
use support::ScratchDir;
use support::e2e::cogitator::{Client, Cogitator};
use support::e2e::keyboard::code;
use support::e2e::screencopy::crop_centred;
use support::e2e::wire::Harness;
use support::golden::{Goldens, Tolerance};

fn fixture(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/servitor")
        .join(name)
}

#[test]
fn rites_wait_pending_settle_live_and_lead_to_a_detached_invocation() {
    let Some(mut c) = Cogitator::start() else {
        return;
    };
    let dir = ScratchDir::new();
    let bin = dir.path().join("bin");
    std::fs::create_dir_all(&bin).unwrap();
    std::os::unix::fs::symlink(fixture("servitor"), bin.join("servitor")).unwrap();
    let path = format!(
        "{}:{}",
        bin.display(),
        std::env::var("PATH").unwrap_or_default()
    );
    let mut command = c.compositor.logis();
    command
        .arg("--servitor")
        .env("PATH", path)
        .env("FAKE_SERVITOR_DIR", dir.path())
        .env("FAKE_SERVITOR_CENSUS", fixture("window.jsonl"))
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    let client = Client {
        child: command.spawn().expect("logis --servitor starts"),
        stdin: None,
    };

    // Every rite at once; dnd's auspex is still awaited: its row is
    // pending, and Return on it does nothing.
    let focused = c.shown(3);
    let size = scene::size(&Options {
        lines: 10,
        width: 64,
        ..Options::default()
    });
    check_window(&mut c.harness, "servitor-pending", size);
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
    assert!(!accepted(&c), "a pending rite was accepted");

    // The auspex settles: the row is replaced in place, settled in "on".
    std::fs::write(dir.path().join("gate"), b"").unwrap();
    assert_eq!(c.next("update"), "0");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "0");

    // Its aspects, the current one marked and selected.
    c.next("stage");
    assert_eq!(c.next("items"), "2");
    check_window(&mut c.harness, "servitor-aspect", size);
    c.tap(&focused, code::DOWN);
    assert_eq!(c.next("select"), "1");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("accept"), "1");

    // The mandatory inscription takes a word; the next one comes with the
    // decree of the chosen aspect.
    c.next("stage");
    c.type_text(&focused, "focus");
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("typed"), "\"focus\"");
    c.next("stage");
    check_window(&mut c.harness, "servitor-inscription", size);
    c.tap(&focused, code::ENTER);
    assert_eq!(c.next("typed"), "\"the cogitator\"");

    // servitor is invoked; the caller is answered and the window goes,
    // with no toast.
    let ended = client.ended();
    assert_eq!((ended.code, ended.stdout.as_slice()), (Some(0), &b""[..]));
    c.next("hide");
    assert!(
        !c.daemon.events().iter().any(|e| e.what.starts_with("toast")),
        "logis showed an outcome"
    );
    let recorded = dir.path().join("invoke/recorded");
    for _ in 0..500 {
        if recorded.exists() {
            break;
        }
        std::thread::sleep(std::time::Duration::from_millis(10));
    }
    let argv = std::fs::read_to_string(dir.path().join("invoke/argv")).unwrap();
    assert_eq!(
        argv.lines().collect::<Vec<_>>(),
        ["dnd", "off", "--reason", "focus", "--origin", "the cogitator"]
    );
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
