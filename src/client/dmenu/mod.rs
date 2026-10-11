//! The client side of a dmenu session (spec §3 stories 1–4, §4.6): stdin
//! streams to the daemon as items while the session is open (the window
//! opens before the input ends), and the answer comes back on stdout:
//! the chosen item's original bytes (or its `--accept-nth` columns, or its
//! `--index`), or the typed text when it matched nothing. Exit 0 with a
//! choice, 1 with empty stdout when dismissed, 2 on error.

mod stream;

use std::fs::File;
use std::io::{self, BufRead, BufReader, Write};
use std::os::fd::AsFd;
use std::path::Path;
use std::process::ExitCode;
use std::thread;

use crate::cli::Dmenu;
use crate::fail;
use crate::protocol::{Mode, Outcome, Reply, Request};

/// Exit status of a session that ended without a choice.
const DISMISSED: u8 = 1;

/// Runs a dmenu session. `config` goes to a daemon this client has to start.
pub fn run(config: Option<&Path>, dmenu: Dmenu) -> ExitCode {
    let input = if dmenu.read_stdin {
        match stdin() {
            Ok(input) => Some(input),
            Err(e) => return fail(&format!("stdin cannot be read: {e}")),
        }
    } else {
        None
    };
    let mut input = input;
    if dmenu.no_run_if_empty
        && let Some(reader) = input.as_mut()
        && reader.fill_buf().map_or(true, |b| b.is_empty())
    {
        // Nothing to choose from: no window at all.
        return ExitCode::from(DISMISSED);
    }
    match session(config, input, &dmenu) {
        Ok(Some(outcome)) => match choice(outcome, &dmenu) {
            Some(bytes) => print(&bytes),
            None => ExitCode::from(DISMISSED),
        },
        Ok(None) => ExitCode::from(DISMISSED),
        Err(message) => fail(&message),
    }
}

fn stdin() -> io::Result<BufReader<File>> {
    let fd = io::stdin().as_fd().try_clone_to_owned()?;
    Ok(BufReader::with_capacity(64 << 10, File::from(fd)))
}

fn print(bytes: &[u8]) -> ExitCode {
    let mut out = io::stdout().lock();
    match out
        .write_all(bytes)
        .and_then(|()| out.write_all(b"\n"))
        .and_then(|()| out.flush())
    {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => fail(&format!("the choice could not be inscribed on stdout: {e}")),
    }
}

/// How the session ended, or `None` if the daemon never answered with a
/// result (it hung up).
fn session(
    config: Option<&Path>,
    input: Option<BufReader<File>>,
    dmenu: &Dmenu,
) -> Result<Option<Outcome>, String> {
    let mut connection = super::connect(config)?;
    connection.send(&Request::Open {
        mode: Mode::Dmenu,
        options: dmenu.view.clone(),
        provider: None,
    })?;
    let mut sender = connection.try_clone()?;
    match input {
        Some(input) => {
            let separator = dmenu.separator;
            let shape = Dmenu {
                view: Default::default(),
                with_nth: dmenu.with_nth.clone(),
                match_nth: dmenu.match_nth.clone(),
                delimiter: dmenu.delimiter,
                ..Dmenu::default()
            };
            // The daemon may answer before stdin ends; the process exit
            // stops this thread.
            thread::spawn(move || {
                stream::stream_items(
                    input,
                    separator,
                    |raw| stream::item(raw, &shape),
                    |request| sender.send(&request).is_ok(),
                );
            });
        }
        None => sender.send(&Request::End)?,
    }
    loop {
        match connection.recv()? {
            Some(Reply::Queued | Reply::Visible) => {}
            Some(Reply::Result { outcome }) => return Ok(Some(outcome)),
            Some(Reply::Error { message }) => return Err(message),
            Some(other) => return Err(format!("the machine spirit spoke out of turn: {other:?}")),
            None => return Err("the machine spirit fell silent before answering".to_owned()),
        }
    }
}

/// What a session's outcome prints: the item's index (`--index`), its
/// `--accept-nth` columns or its original bytes; typed text as typed (also
/// with `--index`, as fuzzel documents free text: "printed as is"). `None`
/// when nothing was chosen.
fn choice(outcome: Outcome, dmenu: &Dmenu) -> Option<Vec<u8>> {
    match outcome {
        Outcome::Selected { index, .. } if dmenu.index => Some(index.to_string().into_bytes()),
        Outcome::Selected { item, .. } => Some(match &dmenu.accept_nth {
            Some(nth) => nth.apply(&item.raw, dmenu.delimiter),
            None => item.raw,
        }),
        Outcome::Typed { text } => Some(text.into_bytes()),
        Outcome::Dismissed | Outcome::Replaced | Outcome::Cancelled | Outcome::Closed => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::nth::Nth;
    use crate::protocol::Item;

    fn selected(index: u64, raw: &[u8]) -> Outcome {
        Outcome::Selected {
            index,
            item: Item::from_raw(raw.to_vec()),
        }
    }

    #[test]
    fn a_chosen_item_prints_its_original_bytes() {
        let out = choice(selected(3, b"a\tb \xff"), &Dmenu::default());
        assert_eq!(out.as_deref(), Some(&b"a\tb \xff"[..]));
    }

    #[test]
    fn index_prints_the_position_in_the_input() {
        let dmenu = Dmenu {
            index: true,
            ..Dmenu::default()
        };
        assert_eq!(
            choice(selected(41, b"x"), &dmenu).as_deref(),
            Some(&b"41"[..])
        );
    }

    #[test]
    fn accept_nth_prints_the_chosen_columns() {
        let dmenu = Dmenu {
            accept_nth: Nth::parse("2").unwrap(),
            delimiter: b',',
            ..Dmenu::default()
        };
        assert_eq!(
            choice(selected(0, b"1,First"), &dmenu).as_deref(),
            Some(&b"First"[..])
        );
    }

    #[test]
    fn typed_text_prints_as_typed_whatever_the_output_runes() {
        let dmenu = Dmenu {
            index: true,
            accept_nth: Nth::parse("2").unwrap(),
            ..Dmenu::default()
        };
        let typed = Outcome::Typed {
            text: "free\ttext".into(),
        };
        assert_eq!(choice(typed, &dmenu).as_deref(), Some(&b"free\ttext"[..]));
    }

    #[test]
    fn every_other_ending_prints_nothing() {
        for outcome in [Outcome::Dismissed, Outcome::Replaced, Outcome::Cancelled] {
            assert_eq!(choice(outcome, &Dmenu::default()), None);
        }
    }
}
