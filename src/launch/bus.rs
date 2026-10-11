//! A minimal D-Bus client on the session bus's Unix socket: the address
//! (`DBUS_SESSION_BUS_ADDRESS`, else `$XDG_RUNTIME_DIR/bus`), EXTERNAL
//! authentication by uid, `Hello`, and blocking method calls with a
//! timeout. Signals that arrive meanwhile are skipped. No crate: zbus would
//! bring an async runtime (spec §4.11), and logis makes only a few calls
//! per launch (see [`super`]).

use std::ffi::OsString;
use std::fmt;
use std::io::{self, Read, Write};
use std::os::unix::ffi::OsStringExt;
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::time::Duration;

use super::wire::{self, Call, METHOD_RETURN, Value, WireError};

/// How long one call may take before the bus counts as unresponsive.
pub const TIMEOUT: Duration = Duration::from_secs(2);

/// A connection to a bus.
pub struct Bus {
    stream: UnixStream,
    serial: u32,
}

/// Why a bus could not be used.
#[derive(Debug)]
pub enum BusError {
    /// No address names a bus logis can reach.
    NoAddress,
    /// The socket could not be reached, read or written.
    Io(io::Error),
    /// The bus refused the authentication.
    Auth(String),
    /// The bus spoke something unreadable.
    Wire(WireError),
    /// The call failed on the other side: the error's name and message.
    Remote { name: String, message: String },
}

impl fmt::Display for BusError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NoAddress => write!(f, "no session bus address"),
            Self::Io(e) => write!(f, "the session bus is unreachable: {e}"),
            Self::Auth(line) => write!(f, "the session bus refused us: {line}"),
            Self::Wire(e) => e.fmt(f),
            Self::Remote { name, message } => write!(f, "{name}: {message}"),
        }
    }
}

impl std::error::Error for BusError {}

impl From<io::Error> for BusError {
    fn from(e: io::Error) -> Self {
        Self::Io(e)
    }
}

impl From<WireError> for BusError {
    fn from(e: WireError) -> Self {
        Self::Wire(e)
    }
}

/// Where a socket address points.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Socket {
    /// `unix:path=…`
    Path(PathBuf),
    /// `unix:abstract=…`
    Abstract(Vec<u8>),
}

/// The session bus's address in an environment.
pub fn session_address(get: impl Fn(&str) -> Option<String>) -> Option<String> {
    get("DBUS_SESSION_BUS_ADDRESS")
        .filter(|a| !a.is_empty())
        .or_else(|| {
            get("XDG_RUNTIME_DIR")
                .filter(|d| d.starts_with('/'))
                .map(|d| format!("unix:path={d}/bus"))
        })
}

/// The Unix sockets an address names, in order (other transports skipped).
pub fn sockets(address: &str) -> Vec<Socket> {
    address
        .split(';')
        .filter_map(|one| one.strip_prefix("unix:"))
        .filter_map(|keys| {
            keys.split(',')
                .find_map(|pair| match pair.split_once('=')? {
                    ("path", v) => {
                        Some(Socket::Path(PathBuf::from(OsString::from_vec(unescape(v)))))
                    }
                    ("abstract", v) => Some(Socket::Abstract(unescape(v))),
                    _ => None,
                })
        })
        .collect()
}

/// Undoes an address value's `%XX` escapes.
fn unescape(value: &str) -> Vec<u8> {
    let bytes = value.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        let hex = bytes
            .get(i + 1..i + 3)
            .and_then(|h| std::str::from_utf8(h).ok())
            .and_then(|h| u8::from_str_radix(h, 16).ok());
        match (bytes[i], hex) {
            (b'%', Some(b)) => {
                out.push(b);
                i += 3;
            }
            (b, _) => {
                out.push(b);
                i += 1;
            }
        }
    }
    out
}

fn open(socket: &Socket) -> io::Result<UnixStream> {
    match socket {
        Socket::Path(path) => UnixStream::connect(path),
        Socket::Abstract(name) => {
            use std::os::linux::net::SocketAddrExt;
            let addr = std::os::unix::net::SocketAddr::from_abstract_name(name)?;
            UnixStream::connect_addr(&addr)
        }
    }
}

impl Bus {
    /// Connects to the first reachable socket of `address`, authenticates
    /// and says `Hello`.
    pub fn connect(address: &str) -> Result<Self, BusError> {
        let mut last = BusError::NoAddress;
        for socket in sockets(address) {
            match open(&socket) {
                Ok(stream) => return Self::greet(stream),
                Err(e) => last = BusError::Io(e),
            }
        }
        Err(last)
    }

    fn greet(stream: UnixStream) -> Result<Self, BusError> {
        stream.set_read_timeout(Some(TIMEOUT))?;
        stream.set_write_timeout(Some(TIMEOUT))?;
        let mut bus = Self { stream, serial: 0 };
        let uid = rustix::process::getuid().as_raw().to_string();
        let hex: String = uid.bytes().map(|b| format!("{b:02x}")).collect();
        bus.stream
            .write_all(format!("\0AUTH EXTERNAL {hex}\r\n").as_bytes())?;
        let line = bus.line()?;
        if !line.starts_with("OK ") {
            return Err(BusError::Auth(line));
        }
        bus.stream.write_all(b"BEGIN\r\n")?;
        bus.call(&Call {
            destination: "org.freedesktop.DBus",
            path: "/org/freedesktop/DBus",
            interface: "org.freedesktop.DBus",
            member: "Hello",
            body: Vec::new(),
            flags: 0,
        })?;
        Ok(bus)
    }

    /// One line of the authentication exchange, without its CRLF.
    fn line(&mut self) -> Result<String, BusError> {
        let mut line = Vec::new();
        let mut byte = [0];
        while !line.ends_with(b"\r\n") {
            if line.len() > 512 {
                return Err(BusError::Auth("an endless line".into()));
            }
            self.stream.read_exact(&mut byte)?;
            line.push(byte[0]);
        }
        line.truncate(line.len() - 2);
        Ok(String::from_utf8_lossy(&line).into_owned())
    }

    /// Sends a call; its serial.
    pub fn send(&mut self, call: &Call<'_>) -> Result<u32, BusError> {
        self.serial += 1;
        self.stream
            .write_all(&wire::encode_call(call, self.serial))?;
        Ok(self.serial)
    }

    /// Calls a method and waits for its reply's body.
    pub fn call(&mut self, call: &Call<'_>) -> Result<Vec<Value>, BusError> {
        let serial = self.send(call)?;
        loop {
            let message = self.receive()?;
            if message.reply_serial != Some(serial) {
                continue;
            }
            if message.kind == METHOD_RETURN {
                return Ok(message.body);
            }
            let text = message.body.first().and_then(Value::as_str).unwrap_or("");
            return Err(BusError::Remote {
                name: message.error_name.unwrap_or_default(),
                message: text.to_owned(),
            });
        }
    }

    fn receive(&mut self) -> Result<wire::Message, BusError> {
        let mut head = [0; 16];
        self.stream.read_exact(&mut head)?;
        let mut bytes = head.to_vec();
        bytes.resize(wire::total_len(&head)?, 0);
        self.stream.read_exact(&mut bytes[16..])?;
        Ok(wire::decode(&bytes)?)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::launch::fake_bus::{Answer, FakeBus};

    fn vars(pairs: &'static [(&'static str, &'static str)]) -> impl Fn(&str) -> Option<String> {
        move |n| {
            pairs
                .iter()
                .find(|(k, _)| *k == n)
                .map(|(_, v)| (*v).to_owned())
        }
    }

    #[test]
    fn the_session_address_falls_back_to_the_runtime_dir() {
        const GIVEN: &[(&str, &str)] = &[
            ("DBUS_SESSION_BUS_ADDRESS", "unix:path=/x"),
            ("XDG_RUNTIME_DIR", "/run/u"),
        ];
        assert_eq!(
            session_address(vars(GIVEN)).as_deref(),
            Some("unix:path=/x")
        );
        const FALLBACK: &[(&str, &str)] = &[("XDG_RUNTIME_DIR", "/run/u")];
        assert_eq!(
            session_address(vars(FALLBACK)).as_deref(),
            Some("unix:path=/run/u/bus")
        );
        assert_eq!(session_address(vars(&[("XDG_RUNTIME_DIR", "rel")])), None);
        assert_eq!(session_address(vars(&[])), None);
    }

    #[test]
    fn addresses_name_unix_sockets_with_escapes() {
        assert_eq!(
            sockets(
                "tcp:host=x,port=1;unix:path=/run/a%20b/bus,guid=00ff;unix:abstract=/tmp/dbus-X"
            ),
            [
                Socket::Path("/run/a b/bus".into()),
                Socket::Abstract(b"/tmp/dbus-X".to_vec())
            ]
        );
        assert!(sockets("unix:tmpdir=/tmp").is_empty());
    }

    #[test]
    fn a_call_gets_its_reply_past_the_signals_before_it() {
        let bus = FakeBus::start(|call| match call.member.as_deref() {
            Some("Ping") => Answer::Return(vec![Value::Str("pong".into())]),
            _ => Answer::Error("org.forge.Error.Heresy", "no such rite".into()),
        });
        let mut b = Bus::connect(&bus.address).unwrap();
        let ping = Call {
            destination: "org.forge",
            path: "/org/forge",
            interface: "org.forge.Rite",
            member: "Ping",
            body: vec![],
            flags: 0,
        };
        assert_eq!(b.call(&ping).unwrap(), [Value::Str("pong".into())]);
        let other = Call {
            member: "Other",
            ..ping.clone()
        };
        match b.call(&other) {
            Err(BusError::Remote { name, message }) => {
                assert_eq!(
                    (name.as_str(), message.as_str()),
                    ("org.forge.Error.Heresy", "no such rite")
                );
            }
            other => panic!("{other:?}"),
        }
        assert_eq!(
            b.call(&ping).unwrap(),
            [Value::Str("pong".into())],
            "still usable"
        );
    }

    #[test]
    fn a_missing_bus_is_an_error() {
        assert!(matches!(
            Bus::connect("unix:path=/nonexistent/logis-bus"),
            Err(BusError::Io(_))
        ));
        assert!(matches!(
            Bus::connect("tcp:host=x"),
            Err(BusError::NoAddress)
        ));
    }
}
