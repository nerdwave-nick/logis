//! Observable behaviour of the `logis` binary at its process boundary.

use std::process::Command;

fn logis() -> Command {
    Command::new(env!("CARGO_BIN_EXE_logis"))
}

#[test]
fn version_flag_prints_name_and_version_and_succeeds() {
    let out = logis().arg("--version").output().unwrap();

    assert_eq!(out.status.code(), Some(0));
    assert_eq!(
        String::from_utf8_lossy(&out.stdout),
        format!("logis {}\n", env!("CARGO_PKG_VERSION"))
    );
    assert!(out.stderr.is_empty());
}
