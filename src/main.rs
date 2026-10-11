//! logis: a grimdark Wayland launcher and dmenu-compatible picker.

mod cli;
mod client;
mod daemon;
mod nth;
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
        Ok(cli::Command::Run { config }) => match client::connect(config.as_deref()) {
            Ok(_) => ExitCode::SUCCESS,
            Err(e) => fail(&e),
        },
        Ok(cli::Command::Dmenu { config, dmenu }) => client::dmenu::run(config.as_deref(), *dmenu),
        Ok(cli::Command::CheckConfig { config }) => {
            let source = logis::config::Source::new(config.as_deref());
            match logis::config::check(&source) {
                Ok(verdict) => {
                    println!("logis: {verdict}");
                    ExitCode::SUCCESS
                }
                Err(e) => fail(&e),
            }
        }
        Ok(cli::Command::Daemon(options)) if options.debug_panel => {
            debug_panel(options.config.as_deref())
        }
        Ok(cli::Command::Daemon(options)) => daemon::run(options),
        Err(e) => fail(&e.to_string()),
    }
}

/// `logis daemon --debug-panel [--config PATH]`; `LOGIS_GPU_BACKEND` forces
/// a backend. It is not the session daemon: it neither listens nor serves
/// sessions, and reads the config once (for `focus-loss`).
fn debug_panel(config: Option<&std::path::Path>) -> ExitCode {
    let config = match logis::config::Source::new(config).load() {
        Ok(config) => config,
        Err(e) => return fail(&e.to_string()),
    };
    if let Err(e) = wayland::preflight() {
        return fail(&e.to_string());
    }
    let result = logis::render::BackendChoice::from_env()
        .map_err(|e| e.to_string())
        .and_then(|choice| {
            logis::window::run_debug_panel(choice, config.focus_loss).map_err(|e| e.to_string())
        });
    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => fail(&e),
    }
}

/// Reports an error on stderr and returns the error exit status. A closed
/// stderr (a daemon whose starter is gone) must not turn into a panic.
fn fail(message: &str) -> ExitCode {
    let _ = writeln!(std::io::stderr(), "logis: {message}");
    ExitCode::from(EXIT_ERROR)
}
