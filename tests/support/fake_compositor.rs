//! A fake Wayland compositor that speaks just enough of the wire protocol for
//! a client to read the registry: it answers `wl_display.get_registry` with the
//! globals it was given and `wl_display.sync` with `wl_callback.done`, and
//! ignores every other request. It serves every client on a thread of its
//! own; dropping the returned [`Compositor`] hangs up on all of them, as a
//! compositor that quits or crashes would.

use std::io::{Read, Write};
use std::net::Shutdown;
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::thread;

const DISPLAY_ID: u32 = 1;
const DISPLAY_SYNC: u16 = 0;
const DISPLAY_GET_REGISTRY: u16 = 1;
const DISPLAY_DELETE_ID: u16 = 1;
const REGISTRY_GLOBAL: u16 = 0;
const CALLBACK_DONE: u16 = 0;

/// The running fake compositor; dropping it hangs up on its clients.
#[must_use = "dropping the compositor hangs up at once"]
pub struct Compositor {
    /// The accepted clients, and whether we hung up already.
    clients: Arc<Mutex<(Vec<UnixStream>, bool)>>,
}

impl Drop for Compositor {
    /// Disconnects every client, now or as soon as it connects.
    fn drop(&mut self) {
        let mut clients = self.clients.lock().unwrap();
        clients.1 = true;
        for stream in &clients.0 {
            let _ = stream.shutdown(Shutdown::Both);
        }
    }
}

/// Listens on `<dir>/<name>` and serves clients on background threads.
pub fn serve(dir: &Path, name: &str, globals: &'static [(&'static str, u32)]) -> Compositor {
    let listener = UnixListener::bind(dir.join(name)).unwrap();
    let clients = Arc::new(Mutex::new((Vec::new(), false)));
    let shared = Arc::clone(&clients);
    thread::spawn(move || {
        for stream in listener.incoming() {
            let Ok(stream) = stream else { return };
            {
                let mut shared = shared.lock().unwrap();
                if shared.1 {
                    let _ = stream.shutdown(Shutdown::Both);
                }
                shared.0.extend(stream.try_clone());
            }
            thread::spawn(move || serve_client(stream, globals));
        }
    });
    Compositor { clients }
}

fn serve_client(mut stream: UnixStream, globals: &[(&str, u32)]) {
    let mut pending = Vec::new();
    let mut buf = [0u8; 4096];
    loop {
        let n = match stream.read(&mut buf) {
            Ok(0) | Err(_) => return,
            Ok(n) => n,
        };
        pending.extend_from_slice(&buf[..n]);
        while let Some((object, opcode, args, len)) = next_message(&pending) {
            let reply = respond(object, opcode, &args, globals);
            pending.drain(..len);
            if stream.write_all(&reply).is_err() {
                return;
            }
        }
    }
}

/// Splits one request off the front of `bytes`: (object, opcode, args, length).
fn next_message(bytes: &[u8]) -> Option<(u32, u16, Vec<u8>, usize)> {
    if bytes.len() < 8 {
        return None;
    }
    let object = word(bytes, 0);
    let header = word(bytes, 4);
    let len = (header >> 16) as usize;
    if len < 8 || bytes.len() < len {
        return None;
    }
    Some((
        object,
        (header & 0xffff) as u16,
        bytes[8..len].to_vec(),
        len,
    ))
}

fn respond(object: u32, opcode: u16, args: &[u8], globals: &[(&str, u32)]) -> Vec<u8> {
    let mut out = Vec::new();
    if object != DISPLAY_ID {
        return out;
    }
    let new_id = word(args, 0);
    match opcode {
        DISPLAY_GET_REGISTRY => {
            for (name, (interface, version)) in (1u32..).zip(globals) {
                let mut body = name.to_ne_bytes().to_vec();
                push_string(&mut body, interface);
                body.extend_from_slice(&version.to_ne_bytes());
                push_message(&mut out, new_id, REGISTRY_GLOBAL, &body);
            }
        }
        DISPLAY_SYNC => {
            push_message(&mut out, new_id, CALLBACK_DONE, &0u32.to_ne_bytes());
            push_message(
                &mut out,
                DISPLAY_ID,
                DISPLAY_DELETE_ID,
                &new_id.to_ne_bytes(),
            );
        }
        _ => {}
    }
    out
}

fn word(bytes: &[u8], at: usize) -> u32 {
    u32::from_ne_bytes(bytes[at..at + 4].try_into().unwrap())
}

fn push_message(out: &mut Vec<u8>, object: u32, opcode: u16, body: &[u8]) {
    let len = 8 + body.len() as u32;
    out.extend_from_slice(&object.to_ne_bytes());
    out.extend_from_slice(&((len << 16) | u32::from(opcode)).to_ne_bytes());
    out.extend_from_slice(body);
}

/// A wire string: length including the NUL terminator, bytes, NUL, padding.
fn push_string(out: &mut Vec<u8>, s: &str) {
    out.extend_from_slice(&(s.len() as u32 + 1).to_ne_bytes());
    out.extend_from_slice(s.as_bytes());
    out.push(0);
    while !out.len().is_multiple_of(4) {
        out.push(0);
    }
}
