//! The client side of a provider session (`logis --provider PROGRAM
//! [ARG]...`, spec §4.8): the daemon runs the provider as if this client
//! had, in its working directory and with its environment, and shows what
//! the provider says. The client waits for the end: exit 0 when the
//! provider closed its flow, 1 when the user dismissed it (or another
//! session replaced it), 2 on error (the provider could not start, broke
//! the protocol or departed early).

use std::ffi::OsString;
use std::os::unix::ffi::OsStringExt;
use std::path::Path;
use std::process::ExitCode;

use crate::fail;
use crate::protocol::{Bytes, Launch, Mode, Options, Outcome, Reply, Request};

/// Exit status of a session that ended without the provider's close.
const DISMISSED: u8 = 1;

/// Runs a provider session for `argv`. `config` goes to a daemon this
/// client has to start.
pub fn run(config: Option<&Path>, argv: Vec<OsString>) -> ExitCode {
    match session(config, launch(argv)) {
        Ok(Outcome::Dismissed | Outcome::Replaced | Outcome::Cancelled) => {
            ExitCode::from(DISMISSED)
        }
        Ok(_) => ExitCode::SUCCESS,
        Err(message) => fail(&message),
    }
}

/// The provider as this client would run it.
fn launch(argv: Vec<OsString>) -> Launch {
    let cwd =
        std::env::current_dir().map_or_else(|_| b"/".to_vec(), |d| d.into_os_string().into_vec());
    Launch {
        argv: argv.into_iter().map(|a| Bytes(a.into_vec())).collect(),
        cwd: Bytes(cwd),
        env: environment(),
    }
}

/// This client's environment, `NAME=VALUE` each.
pub(super) fn environment() -> Vec<Bytes> {
    std::env::vars_os()
        .map(|(name, value)| {
            let mut pair = name.into_vec();
            pair.push(b'=');
            pair.extend(value.into_vec());
            Bytes(pair)
        })
        .collect()
}

fn session(config: Option<&Path>, launch: Launch) -> Result<Outcome, String> {
    opened(
        config,
        Request::Open {
            mode: Mode::Provider,
            options: Options::default(),
            provider: Some(launch),
            env: Vec::new(),
        },
    )
}

/// Opens an interactive session with `open` and waits for its outcome.
pub(super) fn opened(config: Option<&Path>, open: Request) -> Result<Outcome, String> {
    let mut connection = super::connect(config)?;
    connection.send(&open)?;
    loop {
        match connection.recv()? {
            Some(Reply::Queued | Reply::Visible) => {}
            Some(Reply::Result { outcome }) => return Ok(outcome),
            Some(Reply::Error { message }) => return Err(message),
            Some(other) => return Err(format!("the machine spirit spoke out of turn: {other:?}")),
            None => return Err("the machine spirit fell silent before answering".to_owned()),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_launch_carries_argv_cwd_and_environment_as_bytes() {
        let launch = launch(vec![
            OsString::from("rite"),
            OsString::from_vec(b"\xff".to_vec()),
        ]);
        assert_eq!(
            launch.argv,
            [Bytes(b"rite".to_vec()), Bytes(b"\xff".to_vec())]
        );
        let cwd = std::env::current_dir().unwrap().into_os_string().into_vec();
        assert_eq!(launch.cwd, Bytes(cwd));
        let path = std::env::var_os("PATH").unwrap().into_vec();
        let mut want = b"PATH=".to_vec();
        want.extend(path);
        assert!(launch.env.contains(&Bytes(want)));
    }
}
