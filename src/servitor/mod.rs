//! servitor mode (spec §3 stories 8–10, §4.8): a provider built into logis.
//!
//! `logis --servitor` opens a provider session whose provider is logis
//! itself, `logis provide servitor`: the daemon runs it as the caller would
//! have (the caller's working directory and environment, a process group of
//! its own) and speaks the provider protocol with it (docs/provider-protocol.md).
//! This module is that provider process. It keeps vigil over the census
//! (`servitor census --vigil`, [`census`]), leads the user through
//! [`flow`], launches `servitor invoke …` detached ([`invoke`]) and closes.
//! It uses servitor only through its CLI, found on the caller's `PATH`.

pub mod census;
pub mod flow;
pub mod invoke;
mod shown;
#[cfg(test)]
mod testing;

use std::collections::VecDeque;
use std::ffi::OsStr;
use std::io::{BufRead, Write};
use std::process::ExitCode;
use std::sync::mpsc::{Sender, channel};
use std::thread;

use crate::provider::wire::ToProvider;

use census::Told;
use flow::{Act, Flow};

/// The program servitor mode speaks to.
const SERVITOR: &str = "servitor";

enum Event {
    /// A line from logis; `None` once its input ended.
    Logis(Option<ToProvider>),
    Census(Told),
}

/// Runs the provider on stdin and stdout until its flow is over.
pub fn run() -> ExitCode {
    let program = OsStr::new(SERVITOR);
    let (tx, rx) = channel();
    listen_to_logis(tx.clone());
    let mut flow = Flow::default();
    let mut acts: VecDeque<Act> = flow.begin().into();
    let mut census_started = false;
    let mut out = std::io::stdout().lock();
    loop {
        while let Some(act) = acts.pop_front() {
            match act {
                Act::Say(message) => {
                    let mut line = serde_json::to_vec(&message).expect("serialisable");
                    line.push(b'\n');
                    if out.write_all(&line).and_then(|()| out.flush()).is_err() {
                        // logis is gone: nobody is left to answer.
                        return ExitCode::SUCCESS;
                    }
                }
                Act::Invoke(args) => {
                    let launched = invoke::detached(program, &args);
                    acts.extend(flow.launched(launched));
                }
                Act::Exit(code) => return ExitCode::from(code),
            }
        }
        // The census starts once the first stage is out, so the window
        // shows at once however the census fares.
        if !census_started {
            census_started = true;
            let census = tx.clone();
            if let Err(e) =
                census::keep_vigil(program, move |t| census.send(Event::Census(t)).is_ok())
            {
                acts.extend(flow.unheard(e));
            }
            continue;
        }
        acts.extend(match rx.recv() {
            Ok(Event::Logis(Some(message))) => flow.heard(message),
            Ok(Event::Census(told)) => flow.told(told),
            Ok(Event::Logis(None)) | Err(_) => return ExitCode::SUCCESS,
        });
    }
}

/// Hears logis's lines on stdin; lines of a type this version does not
/// know are passed over, as the protocol asks.
fn listen_to_logis(events: Sender<Event>) {
    thread::spawn(move || {
        for line in std::io::stdin().lock().split(b'\n') {
            let Ok(line) = line else { break };
            match heard(&line) {
                Ok(Some(message)) => {
                    if events.send(Event::Logis(Some(message))).is_err() {
                        return;
                    }
                }
                Ok(None) => {}
                Err(e) => eprintln!("logis servitor: {e}"),
            }
        }
        let _ = events.send(Event::Logis(None));
    });
}

/// One line from logis: a message, `None` for a blank line or a type yet
/// unknown, or an error.
fn heard(line: &[u8]) -> Result<Option<ToProvider>, String> {
    if line.iter().all(u8::is_ascii_whitespace) {
        return Ok(None);
    }
    let value: serde_json::Value =
        serde_json::from_slice(line).map_err(|e| format!("logis spoke no JSON line: {e}"))?;
    let known = ["hello", "activate", "submit", "cancel"];
    let kind = value.get("type").and_then(serde_json::Value::as_str);
    match kind.map(str::to_owned) {
        Some(kind) if known.contains(&kind.as_str()) => serde_json::from_value(value)
            .map(Some)
            .map_err(|e| format!("a malformed '{kind}' from logis: {e}")),
        _ => Ok(None),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn logis_lines_are_read_and_unknown_ones_passed_over() {
        assert_eq!(
            heard(br#"{"type":"activate","stage":"rite","id":"dnd","later":1}"#),
            Ok(Some(ToProvider::Activate {
                stage: "rite".into(),
                id: "dnd".into()
            }))
        );
        assert_eq!(heard(br#"{"type":"cancel"}"#), Ok(Some(ToProvider::Cancel)));
        assert_eq!(heard(br#"{"type":"omen"}"#), Ok(None));
        assert_eq!(heard(b"  "), Ok(None));
        assert!(heard(b"garbage").is_err());
        assert!(heard(br#"{"type":"submit","stage":1}"#).is_err());
    }
}
