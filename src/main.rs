//! logis: a grimdark Wayland launcher and dmenu-compatible picker.

use std::process::ExitCode;

fn main() -> ExitCode {
    if std::env::args_os().nth(1).is_some_and(|a| a == "--version") {
        println!("logis {}", env!("CARGO_PKG_VERSION"));
    }
    ExitCode::SUCCESS
}
