//! The client side of the app launcher (`logis --apps`, spec §3 stories
//! 5–7): the daemon shows its app index and launches the chosen app; the
//! client waits for the end. Its environment goes with it: the session
//! environment for the launch when no systemd user manager answers.
//! Exit 0 when an app was launched, 1 when the user dismissed the window
//! (or another session replaced it), 2 on error.

use std::path::Path;
use std::process::ExitCode;

use crate::fail;
use crate::protocol::{Mode, Options, Outcome, Request};

/// Runs an app session. `config` goes to a daemon this client has to start.
pub fn run(config: Option<&Path>) -> ExitCode {
    let open = Request::Open {
        mode: Mode::Apps,
        options: Options::default(),
        provider: None,
        env: super::provider::environment(),
    };
    match super::provider::opened(config, open) {
        Ok(Outcome::Closed) => ExitCode::SUCCESS,
        Ok(_) => ExitCode::from(1),
        Err(message) => fail(&message),
    }
}
