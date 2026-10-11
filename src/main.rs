//! logis: a grimdark Wayland launcher and dmenu-compatible picker.

mod cli;
mod client;
mod daemon;
mod protocol;
mod wayland;

use std::io::Write;
use std::process::ExitCode;

/// Exit status for every error (spec §4.6): the message goes to stderr.
const EXIT_ERROR: u8 = 2;

fn main() -> ExitCode {
    match cli::parse(std::env::args_os().skip(1)) {
        Ok(cli::Command::Version) => {
            println!("logis {}", env!("CARGO_PKG_VERSION"));
            ExitCode::SUCCESS
        }
        Ok(cli::Command::Run) => match client::connect() {
            Ok(_) => ExitCode::SUCCESS,
            Err(e) => fail(&e),
        },
        Ok(cli::Command::Dmenu) => client::dmenu::run(),
        Ok(cli::Command::Daemon(options)) => daemon::run(options),
        Err(e) => fail(&e.to_string()),
    }
}

/// Reports an error on stderr and returns the error exit status. A closed
/// stderr (a daemon whose starter is gone) must not turn into a panic.
fn fail(message: &str) -> ExitCode {
    let _ = writeln!(std::io::stderr(), "logis: {message}");
    ExitCode::from(EXIT_ERROR)
}
