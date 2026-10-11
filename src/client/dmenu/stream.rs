//! stdin to items: input lines (ended by newline, or NUL for `--dmenu0`)
//! stream to the daemon in batches as they arrive, each item keeping its
//! original bytes apart from the text it shows and the text it is matched by
//! (`--with-nth`, `--match-nth`).

use std::io::{BufRead, BufReader, Read};

use crate::cli::Dmenu;
use crate::protocol::{Item, Request};

/// At most this many items go into one frame.
const BATCH_ITEMS: usize = 1024;
/// And at most about this many bytes of item text.
const BATCH_BYTES: usize = 256 << 10;

/// One input line as an item: shown as its `--with-nth` columns (or whole),
/// matched by its `--match-nth` columns (or as shown), bytes kept as read.
pub fn item(raw: Vec<u8>, dmenu: &Dmenu) -> Item {
    let lossy = |bytes: &[u8]| String::from_utf8_lossy(bytes).into_owned();
    let columns = |nth: &crate::nth::Nth| lossy(&nth.apply(&raw, dmenu.delimiter));
    Item {
        text: dmenu.with_nth.as_ref().map_or_else(|| lossy(&raw), columns),
        match_text: dmenu.match_nth.as_ref().map(columns),
        raw,
    }
}

/// Reads lines ended by `separator` and sends them as `items` frames, then
/// `end`. A batch goes out whenever the input has nothing more buffered, so
/// items reach the daemon as they arrive. Stops early when `send` fails.
pub fn stream_items<R: Read>(
    mut input: BufReader<R>,
    separator: u8,
    make: impl Fn(Vec<u8>) -> Item,
    mut send: impl FnMut(Request) -> bool,
) {
    let mut batch = Vec::new();
    let mut bytes = 0;
    loop {
        let mut line = Vec::new();
        match input.read_until(separator, &mut line) {
            Ok(0) | Err(_) => break,
            Ok(_) => {}
        }
        if line.last() == Some(&separator) {
            line.pop();
        }
        bytes += line.len();
        batch.push(make(line));
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
    use std::io;

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
        frames_split(chunks, b'\n')
    }

    fn frames_split(chunks: Vec<&'static [u8]>, separator: u8) -> Vec<Vec<Vec<u8>>> {
        let mut sent = Vec::new();
        let input = BufReader::with_capacity(64 << 10, Chunks(chunks));
        stream_items(input, separator, Item::from_raw, |request| {
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
        let input = BufReader::new(Chunks(vec![b"a\n", b"b\n"]));
        stream_items(input, b'\n', Item::from_raw, |_| {
            calls += 1;
            false
        });
        assert_eq!(calls, 1);
    }

    #[test]
    fn items_display_their_lossy_text() {
        let item = item(b"caf\xc3\xa9 \xff".to_vec(), &Dmenu::default());
        assert_eq!(item.text, "café \u{fffd}");
        assert_eq!(item.match_text, None);
        assert_eq!(item.raw, b"caf\xc3\xa9 \xff");
    }

    #[test]
    fn nul_separated_input_keeps_newlines_inside_items() {
        let sent = frames_split(vec![b"a\nb\0c\0", b"d"], 0);
        assert_eq!(
            sent,
            [lines(&[b"a\nb", b"c"]), lines(&[b"d"]), lines(&[b"END"])]
        );
    }

    #[test]
    fn with_nth_shapes_the_text_and_match_nth_the_match_text() {
        let dmenu = Dmenu {
            with_nth: crate::nth::Nth::parse("2").unwrap(),
            match_nth: crate::nth::Nth::parse("{3} {1}").unwrap(),
            ..Dmenu::default()
        };
        let item = item(b"id\tShown \xff\tsecret".to_vec(), &dmenu);
        assert_eq!(item.text, "Shown \u{fffd}");
        assert_eq!(item.match_text.as_deref(), Some("secret id"));
        assert_eq!(item.raw, b"id\tShown \xff\tsecret", "the bytes stay whole");
    }
}
