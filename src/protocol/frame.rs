//! Framing: each message is a 4-byte big-endian payload length followed by
//! that many bytes of UTF-8 JSON.

use std::fmt;
use std::io::{self, Read, Write};

use serde::Serialize;
use serde::de::DeserializeOwned;

/// The largest payload either side accepts. Item batches stay far below it.
pub const MAX_FRAME: usize = 16 << 20;

/// Why a frame could not be read or written.
#[derive(Debug)]
pub enum FrameError {
    Io(io::Error),
    /// The peer announced (or we tried to send) a payload above [`MAX_FRAME`].
    TooLarge(usize),
    /// The stream ended inside a frame.
    Truncated,
    /// The payload is not the JSON message expected.
    Malformed(serde_json::Error),
}

impl fmt::Display for FrameError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Io(e) => write!(f, "{e}"),
            Self::TooLarge(len) => write!(f, "a frame of {len} bytes exceeds {MAX_FRAME}"),
            Self::Truncated => f.write_str("the stream ended inside a frame"),
            Self::Malformed(e) => write!(f, "malformed message: {e}"),
        }
    }
}

/// Writes one message as a single frame.
pub fn write_frame<W: Write, T: Serialize>(writer: &mut W, message: &T) -> Result<(), FrameError> {
    // Length and payload go out in one write so frames never interleave.
    let mut frame = vec![0; 4];
    serde_json::to_writer(&mut frame, message).map_err(FrameError::Malformed)?;
    let len = frame.len() - 4;
    if len > MAX_FRAME {
        return Err(FrameError::TooLarge(len));
    }
    frame[..4].copy_from_slice(&(len as u32).to_be_bytes());
    writer.write_all(&frame).map_err(FrameError::Io)?;
    writer.flush().map_err(FrameError::Io)
}

/// Reads one message. `Ok(None)` is a clean end of stream between frames.
pub fn read_frame<R: Read, T: DeserializeOwned>(reader: &mut R) -> Result<Option<T>, FrameError> {
    let mut header = [0; 4];
    let got = read_full(reader, &mut header)?;
    if got == 0 {
        return Ok(None);
    }
    if got < header.len() {
        return Err(FrameError::Truncated);
    }
    let len = u32::from_be_bytes(header) as usize;
    if len > MAX_FRAME {
        return Err(FrameError::TooLarge(len));
    }
    let mut payload = vec![0; len];
    if read_full(reader, &mut payload)? < len {
        return Err(FrameError::Truncated);
    }
    serde_json::from_slice(&payload)
        .map(Some)
        .map_err(FrameError::Malformed)
}

/// Fills `buf` unless the stream ends first; returns how much was read.
fn read_full<R: Read>(reader: &mut R, buf: &mut [u8]) -> Result<usize, FrameError> {
    let mut filled = 0;
    while filled < buf.len() {
        match reader.read(&mut buf[filled..]) {
            Ok(0) => break,
            Ok(n) => filled += n,
            Err(e) if e.kind() == io::ErrorKind::Interrupted => {}
            Err(e) => return Err(FrameError::Io(e)),
        }
    }
    Ok(filled)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{Value, json};

    fn read(bytes: &[u8]) -> Result<Option<Value>, FrameError> {
        read_frame(&mut &bytes[..])
    }

    #[test]
    fn a_frame_is_a_big_endian_length_and_json() {
        let mut out = Vec::new();
        write_frame(&mut out, &json!({"a": 1})).unwrap();
        assert_eq!(out, b"\0\0\0\x07{\"a\":1}");
    }

    #[test]
    fn messages_round_trip_in_order_and_end_cleanly() {
        let mut stream = Vec::new();
        let messages = [
            json!({"type": "hello"}),
            json!([1, "two", null]),
            json!("é\n"),
        ];
        for m in &messages {
            write_frame(&mut stream, m).unwrap();
        }
        let mut reader = &stream[..];
        for m in &messages {
            assert_eq!(
                read_frame::<_, Value>(&mut reader).unwrap().as_ref(),
                Some(m)
            );
        }
        assert!(matches!(read_frame::<_, Value>(&mut reader), Ok(None)));
    }

    #[test]
    fn a_stream_ending_inside_a_frame_is_truncated() {
        assert!(matches!(read(b"\0\0"), Err(FrameError::Truncated)));
        assert!(matches!(
            read(b"\0\0\0\x07{\"a\""),
            Err(FrameError::Truncated)
        ));
    }

    #[test]
    fn an_oversized_length_is_refused_before_reading_the_payload() {
        let len = (MAX_FRAME as u32 + 1).to_be_bytes();
        assert!(matches!(read(&len), Err(FrameError::TooLarge(n)) if n == MAX_FRAME + 1));
    }

    #[test]
    fn an_oversized_message_is_not_sent() {
        let mut out = Vec::new();
        let big = "x".repeat(MAX_FRAME);
        assert!(matches!(
            write_frame(&mut out, &big),
            Err(FrameError::TooLarge(_))
        ));
        assert!(out.is_empty());
    }

    #[test]
    fn a_payload_that_is_not_json_is_malformed() {
        assert!(matches!(
            read(b"\0\0\0\x03{x}"),
            Err(FrameError::Malformed(_))
        ));
    }
}
