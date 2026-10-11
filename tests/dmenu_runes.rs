//! The dmenu runes at the process boundary (spec §4.6): real `logis
//! --dmenu` clients and a real daemon over its socket, with the scripted
//! resolver standing in for the window (it picks the first item whose shown
//! text starts with `>`, or dismisses). No compositor window is needed, so
//! these run everywhere; tests/dmenu.rs covers the window itself.

mod support;

use std::io::Write;
use std::process::{Output, Stdio};
use std::time::{Duration, Instant};

use support::rig::Rig;

/// Runs `logis --dmenu args` with this stdin to completion.
fn dmenu(rig: &Rig, args: &[&str], stdin: &[u8]) -> Output {
    let mut child = rig
        .logis()
        .arg("--dmenu")
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    child.stdin.take().unwrap().write_all(stdin).unwrap();
    child.wait_with_output().unwrap()
}

fn stderr(out: &Output) -> String {
    String::from_utf8_lossy(&out.stderr).into_owned()
}

#[track_caller]
fn printed(out: &Output, stdout: &[u8]) {
    assert_eq!(out.status.code(), Some(0), "stderr: {}", stderr(out));
    assert_eq!(
        String::from_utf8_lossy(&out.stdout),
        String::from_utf8_lossy(stdout)
    );
}

#[test]
fn dmenu0_reads_nul_separated_items_with_newlines_inside() {
    let rig = Rig::new();
    let out = rig
        .logis()
        .arg("--dmenu0")
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .and_then(|mut child| {
            child
                .stdin
                .take()
                .unwrap()
                .write_all(b"x\0>two\nlines\0y\0")?;
            child.wait_with_output()
        })
        .unwrap();
    printed(&out, b">two\nlines\n");
}

#[test]
fn index_prints_the_position_of_the_choice() {
    let rig = Rig::new();
    printed(&dmenu(&rig, &["--index"], b"a\nb\n>c\n"), b"2\n");
}

#[test]
fn with_nth_shows_a_column_and_the_whole_line_comes_back() {
    let rig = Rig::new();
    let out = dmenu(&rig, &["--with-nth=2"], b"id-1\tplain\nid-2\t>marked\n");
    printed(&out, b"id-2\t>marked\n");
}

#[test]
fn accept_nth_prints_columns_split_at_the_nth_delimiter() {
    let rig = Rig::new();
    let args = [
        "--with-nth={2}",
        "--accept-nth={3}:{1}",
        "--nth-delimiter=,",
    ];
    let out = dmenu(&rig, &args, b"a,b,c\nkey,>shown,value\n");
    printed(&out, b"value:key\n");
}

#[test]
fn the_original_bytes_survive_columns_and_invalid_utf8() {
    let rig = Rig::new();
    let out = dmenu(&rig, &["--with-nth=2"], b"\xff\xfe\t>pick\r\n");
    printed(&out, b"\xff\xfe\t>pick\r\n");
}

#[test]
fn no_run_if_empty_exits_1_at_once_without_a_daemon() {
    let rig = Rig::new();
    let out = dmenu(&rig, &["--no-run-if-empty"], b"");
    assert_eq!(out.status.code(), Some(1), "stderr: {}", stderr(&out));
    assert!(out.stdout.is_empty());
    assert!(!rig.socket().exists(), "no daemon was summoned");
    // With input it runs as usual.
    printed(&dmenu(&rig, &["-R"], b">x\n"), b">x\n");
}

#[test]
fn prompt_only_never_waits_for_stdin() {
    let rig = Rig::new();
    let mut child = rig
        .logis()
        .args(["--dmenu", "--prompt-only", "pin: "])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .unwrap();
    // stdin stays open; the scripted resolver sees no items and dismisses.
    let _stdin = child.stdin.take();
    let deadline = Instant::now() + Duration::from_secs(10);
    while child.try_wait().unwrap().is_none() {
        assert!(Instant::now() < deadline, "the client waited for stdin");
        std::thread::sleep(Duration::from_millis(5));
    }
    assert_eq!(child.wait().unwrap().code(), Some(1));
}

#[test]
fn list_runes_without_dmenu_are_an_error() {
    let rig = Rig::new();
    let out = rig.logis().arg("--index").output().unwrap();
    assert_eq!(out.status.code(), Some(2));
    assert!(stderr(&out).contains("--index"), "{}", stderr(&out));
}
