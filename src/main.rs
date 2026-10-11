//! logis: a grimdark Wayland launcher and dmenu-compatible picker.

mod cli;
mod wayland;

use std::process::ExitCode;

/// Exit status for every error (spec §4.6): the message goes to stderr.
const EXIT_ERROR: u8 = 2;

fn main() -> ExitCode {
    match cli::parse(std::env::args_os().skip(1)) {
        Ok(cli::Command::Version) => {
            println!("logis {}", env!("CARGO_PKG_VERSION"));
            ExitCode::SUCCESS
        }
        Ok(cli::Command::Run) => match wayland::preflight() {
            Ok(()) => ExitCode::SUCCESS,
            Err(e) => fail(&e.to_string()),
        },
        Ok(cli::Command::DebugPanel) => debug_panel(),
        Err(arg) => fail(&format!(
            "the rune '{arg}' is unknown to this cogitator. It knows only --version."
        )),
    }
}

/// `logis daemon --debug-panel`; `LOGIS_GPU_BACKEND` forces a backend.
fn debug_panel() -> ExitCode {
    if let Err(e) = wayland::preflight() {
        return fail(&e.to_string());
    }
    let result = logis::render::BackendChoice::from_env()
        .map_err(|e| e.to_string())
        .and_then(|choice| logis::window::run_debug_panel(choice).map_err(|e| e.to_string()));
    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => fail(&e),
    }
}

fn fail(message: &str) -> ExitCode {
    eprintln!("logis: {message}");
    ExitCode::from(EXIT_ERROR)
}
