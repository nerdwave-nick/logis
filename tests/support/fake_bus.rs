//! A fake D-Bus session bus for tests: a Unix socket in a scratch directory
//! that takes EXTERNAL authentication, answers `Hello` (then announces
//! `NameAcquired`, as a real bus does), records every other call and answers
//! it as the test says. It never reaches a real bus or systemd.
//!
//! Shared by the lib's unit tests (included by path, which is why it names
//! the crate `logis`) and the integration tests.
#![allow(dead_code)] // shared by test crates that each use only part of it

use std::io::{BufRead, BufReader, Read, Write};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

use logis::launch::wire::{self, Message, Value};

/// How the fake bus answers a call.
pub enum Answer {
    /// A method return with this body.
    Return(Vec<Value>),
    /// An error of this name, with this message.
    Error(&'static str, String),
    /// Nothing (the call expects no reply, or the bus is slow).
    Silent,
}

type Handler = dyn Fn(&Message) -> Answer + Send + Sync;

/// A running fake bus; dropped, it stops answering and removes its socket.
pub struct FakeBus {
    /// The bus address, as `DBUS_SESSION_BUS_ADDRESS` would hold it.
    pub address: String,
    dir: PathBuf,
    calls: Arc<Mutex<Vec<Message>>>,
}

impl FakeBus {
    /// Starts a bus answering calls with `handler`.
    pub fn start(handler: impl Fn(&Message) -> Answer + Send + Sync + 'static) -> Self {
        static COUNT: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);
        let n = COUNT.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
        let dir = std::env::temp_dir().join(format!("logis-fakebus-{}-{n}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let socket = dir.join("bus");
        let listener = UnixListener::bind(&socket).unwrap();
        let calls = Arc::new(Mutex::new(Vec::new()));
        let handler: Arc<Handler> = Arc::new(handler);
        let recorded = Arc::clone(&calls);
        thread::spawn(move || {
            for stream in listener.incoming() {
                let Ok(stream) = stream else { return };
                let (calls, handler) = (Arc::clone(&recorded), Arc::clone(&handler));
                thread::spawn(move || serve(stream, &calls, &*handler));
            }
        });
        Self {
            address: format!("unix:path={}", socket.display()),
            dir,
            calls,
        }
    }

    /// The calls so far (not `Hello`), oldest first.
    pub fn calls(&self) -> Vec<Message> {
        self.calls.lock().unwrap().clone()
    }

    /// Waits until a call to `member` arrived, and returns it.
    pub fn wait_for(&self, member: &str) -> Message {
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            let found = self
                .calls()
                .into_iter()
                .find(|c| c.member.as_deref() == Some(member));
            if let Some(call) = found {
                return call;
            }
            assert!(
                Instant::now() < deadline,
                "no {member} call reached the bus"
            );
            thread::sleep(Duration::from_millis(5));
        }
    }
}

impl Drop for FakeBus {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.dir);
    }
}

fn serve(stream: UnixStream, calls: &Mutex<Vec<Message>>, handler: &Handler) {
    let mut reader = BufReader::new(stream.try_clone().unwrap());
    let mut writer = stream;
    let mut nul = [0];
    if reader.read_exact(&mut nul).is_err() || nul != [0] {
        return;
    }
    loop {
        let mut line = String::new();
        if reader.read_line(&mut line).unwrap_or(0) == 0 {
            return;
        }
        let reply: &[u8] = match line.trim_end() {
            l if l.starts_with("AUTH EXTERNAL ") => b"OK 0123456789abcdef0123456789abcdef\r\n",
            "BEGIN" => break,
            _ => b"ERROR\r\n",
        };
        if writer.write_all(reply).is_err() {
            return;
        }
    }
    let mut serial = 1000;
    loop {
        let mut head = [0; 16];
        if reader.read_exact(&mut head).is_err() {
            return;
        }
        let Ok(len) = wire::total_len(&head) else {
            return;
        };
        let mut bytes = head.to_vec();
        bytes.resize(len, 0);
        if reader.read_exact(&mut bytes[16..]).is_err() {
            return;
        }
        let Ok(call) = wire::decode(&bytes) else {
            return;
        };
        serial += 1;
        let answer = if call.member.as_deref() == Some("Hello") {
            Answer::Return(vec![Value::Str(":1.42".into())])
        } else {
            calls.lock().unwrap().push(call.clone());
            handler(&call)
        };
        let reply = match answer {
            Answer::Return(body) => wire::encode_reply(serial, call.serial, None, &body),
            Answer::Error(name, message) => {
                wire::encode_reply(serial, call.serial, Some(name), &[Value::Str(message)])
            }
            Answer::Silent => continue,
        };
        if writer.write_all(&reply).is_err() {
            return;
        }
        if call.member.as_deref() == Some("Hello") {
            serial += 1;
            if writer.write_all(&name_acquired(serial)).is_err() {
                return;
            }
        }
    }
}

/// The `NameAcquired` signal a bus sends after `Hello`: a client must skip it.
fn name_acquired(serial: u32) -> Vec<u8> {
    let call = wire::Call {
        destination: ":1.42",
        path: "/org/freedesktop/DBus",
        interface: "org.freedesktop.DBus",
        member: "NameAcquired",
        body: vec![Value::Str(":1.42".into())],
        flags: 0,
    };
    let mut bytes = wire::encode_call(&call, serial);
    bytes[1] = wire::SIGNAL;
    bytes
}
