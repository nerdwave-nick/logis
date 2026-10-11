//! The client side of a dmenu session, as far as this slice goes: stdin lines
//! stream to the daemon as items while the session is open, and the chosen
//! item's original bytes come back on stdout. The full flag set and NUL mode
//! belong to the dmenu slice.

use std::fs::File;
use std::io::{self, BufRead, BufReader, Read, Write};
use std::os::fd::AsFd;
use std::process::ExitCode;
use std::thread;

use crate::fail;
use crate::protocol::{Item, Mode, Outcome, Reply, Request};

/// At most this many items go into one frame.
const BATCH_ITEMS: usize = 1024;
/// And at most about this many bytes of item text.
const BATCH_BYTES: usize = 256 << 10;

/// Runs a dmenu session: exit 0 with the choice on stdout, 1 when dismissed.
pub fn run() -> ExitCode {
    match session() {
        Ok(Some(raw)) => {
            let mut out = io::stdout().lock();
            match out
                .write_all(&raw)
                .and_then(|()| out.write_all(b"\n"))
                .and_then(|()| out.flush())
            {
                Ok(()) => ExitCode::SUCCESS,
                Err(e) => fail(&format!("the choice could not be inscribed on stdout: {e}")),
            }
        }
        Ok(None) => ExitCode::from(1),
        Err(message) => fail(&message),
    }
}

/// The chosen item's original bytes, or `None` if the session ended without one.
fn session() -> Result<Option<Vec<u8>>, String> {
    let mut connection = super::connect()?;
    connection.send(&Request::Open { mode: Mode::Dmenu })?;
    let mut sender = connection.try_clone()?;
    let stdin = io::stdin()
        .as_fd()
        .try_clone_to_owned()
        .map_err(|e| e.to_string())?;
    // The daemon may answer before stdin ends; the process exit stops this thread.
    thread::spawn(move || {
        stream_items(File::from(stdin), |request| sender.send(&request).is_ok());
    });
    loop {
        match connection.recv()? {
            Some(Reply::Queued | Reply::Visible) => {}
            Some(Reply::Result {
                outcome: Outcome::Selected { item, .. },
            }) => return Ok(Some(item.raw)),
            Some(Reply::Result { .. }) => return Ok(None),
            Some(Reply::Error { message }) => return Err(message),
            Some(other) => return Err(format!("the machine spirit spoke out of turn: {other:?}")),
            None => return Err("the machine spirit fell silent before answering".to_owned()),
        }
    }
}

/// Reads newline-separated lines and sends them as `items` frames, then `end`.
/// A batch goes out whenever the input has nothing more buffered, so items
/// reach the daemon as they arrive. Stops early when `send` fails.
fn stream_items<R: Read>(input: R, mut send: impl FnMut(Request) -> bool) {
    let mut input = BufReader::with_capacity(64 << 10, input);
    let mut batch = Vec::new();
    let mut bytes = 0;
    loop {
        let mut line = Vec::new();
        match input.read_until(b'\n', &mut line) {
            Ok(0) | Err(_) => break,
            Ok(_) => {}
        }
        if line.last() == Some(&b'\n') {
            line.pop();
        }
        bytes += line.len();
        batch.push(Item::from_raw(line));
        if batch.len() >= BATCH_ITEMS || bytes >= BATCH_BYTES || input.buffer().is_empty() {
            bytes = 0;
            if !send(Request::Items {
                items: std::mem::take(&mut batch),
            }) {
                return;
            }
        }
    }
    if !batch.is_empty() && !send(Request::Items { items: batch }) {
        return;
    }
    send(Request::End);
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A reader that hands out its chunks one `read` at a time.
    struct Chunks(Vec<&'static [u8]>);

    impl Read for Chunks {
        fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
            if self.0.is_empty() {
                return Ok(0);
            }
            let chunk = self.0.remove(0);
            buf[..chunk.len()].copy_from_slice(chunk);
            Ok(chunk.len())
        }
    }

    /// The frames sent, each as the raw bytes of its items (or "END").
    fn frames(chunks: Vec<&'static [u8]>) -> Vec<Vec<Vec<u8>>> {
        let mut sent = Vec::new();
        stream_items(Chunks(chunks), |request| {
            sent.push(match request {
                Request::Items { items } => items.into_iter().map(|i| i.raw).collect(),
                Request::End => vec![b"END".to_vec()],
                other => panic!("unexpected {other:?}"),
            });
            true
        });
        sent
    }

    fn lines(lines: &[&[u8]]) -> Vec<Vec<u8>> {
        lines.iter().map(|l| l.to_vec()).collect()
    }

    #[test]
    fn lines_are_sent_as_they_arrive_then_end() {
        let sent = frames(vec![b"a\nb\n", b"c\n"]);
        assert_eq!(
            sent,
            [lines(&[b"a", b"b"]), lines(&[b"c"]), lines(&[b"END"])]
        );
    }

    #[test]
    fn original_bytes_are_kept_and_a_final_line_needs_no_newline() {
        let sent = frames(vec![b"\xff\r\n\n", b"tail"]);
        assert_eq!(
            sent,
            [
                lines(&[b"\xff\r", b""]),
                lines(&[b"tail"]),
                lines(&[b"END"])
            ]
        );
    }

    #[test]
    fn empty_input_sends_only_end() {
        assert_eq!(frames(vec![]), [lines(&[b"END"])]);
    }

    #[test]
    fn a_large_input_is_split_into_bounded_batches() {
        let input: &'static [u8] = "x\n".repeat(BATCH_ITEMS + 1).leak().as_bytes();
        let sent = frames(vec![input]);
        let sizes: Vec<usize> = sent.iter().map(Vec::len).collect();
        assert_eq!(sizes, [BATCH_ITEMS, 1, 1]);
    }

    #[test]
    fn streaming_stops_when_the_daemon_is_gone() {
        let mut calls = 0;
        stream_items(Chunks(vec![b"a\n", b"b\n"]), |_| {
            calls += 1;
            false
        });
        assert_eq!(calls, 1);
    }

    #[test]
    fn items_display_their_lossy_text() {
        let item = Item::from_raw(b"caf\xc3\xa9 \xff".to_vec());
        assert_eq!(item.text, "café \u{fffd}");
    }
}
