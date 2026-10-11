//! The D-Bus wire format (D-Bus Specification, "Message Protocol"), as much
//! as launching needs: method calls out, replies and errors in.
//!
//! Calls are written little-endian with their header fields in the order
//! GLib writes them (path, interface, destination, signature, member), so
//! tests can compare them byte for byte with GLib's encoding
//! (tests/fixtures/dbus/glib-reference.py). Replies are read in either byte
//! order; a signal is recognised and skipped without reading its body.

use std::fmt;

mod codec;

use codec::{Dec, Enc, big_endian, first_type};

/// A D-Bus value, with enough type to write its signature.
#[derive(Clone, Debug, PartialEq)]
pub enum Value {
    /// `y`
    Byte(u8),
    /// `b`
    Bool(bool),
    /// `u`
    U32(u32),
    /// `n`, `q`, `i`, `x`, `t`, `h`, widened.
    Int(i64),
    /// `d`
    Double(f64),
    /// `s`
    Str(String),
    /// `o`
    Path(String),
    /// `g`
    Sig(String),
    /// `a…`: the element signature, then the elements.
    Array(String, Vec<Value>),
    /// `(…)`
    Struct(Vec<Value>),
    /// `{…}`: a dict entry, inside an array.
    Entry(Box<Value>, Box<Value>),
    /// `v`
    Variant(Box<Value>),
}

impl Value {
    /// The value's signature.
    pub fn signature(&self) -> String {
        match self {
            Self::Byte(_) => "y".into(),
            Self::Bool(_) => "b".into(),
            Self::U32(_) => "u".into(),
            Self::Int(_) => "x".into(),
            Self::Double(_) => "d".into(),
            Self::Str(_) => "s".into(),
            Self::Path(_) => "o".into(),
            Self::Sig(_) => "g".into(),
            Self::Array(element, _) => format!("a{element}"),
            Self::Struct(fields) => {
                let inner: String = fields.iter().map(Self::signature).collect();
                format!("({inner})")
            }
            Self::Entry(k, v) => format!("{{{}{}}}", k.signature(), v.signature()),
            Self::Variant(_) => "v".into(),
        }
    }

    /// A string value, if this is one (or a path or signature).
    pub fn as_str(&self) -> Option<&str> {
        match self {
            Self::Str(s) | Self::Path(s) | Self::Sig(s) => Some(s),
            _ => None,
        }
    }
}

/// Message types.
pub const METHOD_CALL: u8 = 1;
pub const METHOD_RETURN: u8 = 2;
pub const ERROR: u8 = 3;
pub const SIGNAL: u8 = 4;

/// The caller does not want a reply.
pub const NO_REPLY_EXPECTED: u8 = 0x1;

/// A method call to send.
#[derive(Clone, Debug, PartialEq)]
pub struct Call<'a> {
    pub destination: &'a str,
    pub path: &'a str,
    pub interface: &'a str,
    pub member: &'a str,
    pub body: Vec<Value>,
    pub flags: u8,
}

/// A message received.
#[derive(Clone, Debug, PartialEq)]
pub struct Message {
    pub kind: u8,
    pub serial: u32,
    /// The call this answers (replies and errors).
    pub reply_serial: Option<u32>,
    /// The error's name (errors).
    pub error_name: Option<String>,
    /// A call's (or signal's) member, interface, object path and destination.
    pub member: Option<String>,
    pub interface: Option<String>,
    pub path: Option<String>,
    pub destination: Option<String>,
    /// The body; empty for a signal, which is never read.
    pub body: Vec<Value>,
}

/// Why bytes are not a message logis can read.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WireError(pub String);

impl fmt::Display for WireError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "a malformed D-Bus message: {}", self.0)
    }
}

/// The longest message logis reads.
pub const MAX_MESSAGE: usize = 64 << 20;

/// A method call's bytes, numbered `serial`.
pub fn encode_call(call: &Call<'_>, serial: u32) -> Vec<u8> {
    let mut fields = vec![
        (1, Value::Path(call.path.into())),
        (2, Value::Str(call.interface.into())),
        (6, Value::Str(call.destination.into())),
    ];
    let signature: String = call.body.iter().map(Value::signature).collect();
    if !signature.is_empty() {
        fields.push((8, Value::Sig(signature)));
    }
    fields.push((3, Value::Str(call.member.into())));
    encode(METHOD_CALL, call.flags, serial, fields, &call.body)
}

/// The bytes of a reply to call `reply_serial`: a method return, or with
/// `error` (its name) an error.
pub fn encode_reply(
    serial: u32,
    reply_serial: u32,
    error: Option<&str>,
    body: &[Value],
) -> Vec<u8> {
    let mut fields = vec![(5, Value::U32(reply_serial))];
    if let Some(name) = error {
        fields.push((4, Value::Str(name.into())));
    }
    let signature: String = body.iter().map(Value::signature).collect();
    if !signature.is_empty() {
        fields.push((8, Value::Sig(signature)));
    }
    let kind = if error.is_some() {
        ERROR
    } else {
        METHOD_RETURN
    };
    encode(kind, 0, serial, fields, body)
}

/// A message of `kind` with header `fields` (code, value) and `body`.
fn encode(kind: u8, flags: u8, serial: u32, fields: Vec<(u8, Value)>, body: &[Value]) -> Vec<u8> {
    let mut coded = Enc::default();
    for value in body {
        coded.value(value);
    }
    let fields = fields
        .into_iter()
        .map(|(code, value)| {
            Value::Struct(vec![Value::Byte(code), Value::Variant(Box::new(value))])
        })
        .collect();
    let mut head = Enc::default();
    head.buf.extend([b'l', kind, flags, 1]);
    head.u32(coded.buf.len() as u32);
    head.u32(serial);
    head.value(&Value::Array("(yv)".into(), fields));
    head.pad(8);
    head.buf.extend(coded.buf);
    head.buf
}

/// The whole length of a message from its first 16 bytes.
pub fn total_len(head: &[u8; 16]) -> Result<usize, WireError> {
    let big = big_endian(head[0])?;
    let word = |at: usize| {
        let b: [u8; 4] = head[at..at + 4].try_into().expect("four bytes");
        if big {
            u32::from_be_bytes(b)
        } else {
            u32::from_le_bytes(b)
        }
    };
    let (body, fields) = (word(4) as usize, word(12) as usize);
    let total = (16 + fields).next_multiple_of(8) + body;
    if total > MAX_MESSAGE {
        return Err(WireError(format!("{total} bytes is too long")));
    }
    Ok(total)
}

/// Reads one whole message.
pub fn decode(bytes: &[u8]) -> Result<Message, WireError> {
    let head: &[u8; 16] = bytes
        .get(..16)
        .and_then(|h| h.try_into().ok())
        .ok_or_else(|| WireError("shorter than its header".into()))?;
    if total_len(head)? != bytes.len() {
        return Err(WireError("its length does not match its header".into()));
    }
    let mut dec = Dec {
        data: bytes,
        pos: 12,
        big: big_endian(bytes[0])?,
        depth: 0,
    };
    let Value::Array(_, fields) = dec.value("a(yv)")? else {
        unreachable!("an array signature reads an array");
    };
    let mut message = Message {
        kind: bytes[1],
        serial: dec.word_at(8),
        reply_serial: None,
        error_name: None,
        member: None,
        interface: None,
        path: None,
        destination: None,
        body: Vec::new(),
    };
    let mut signature = String::new();
    for field in fields {
        let Value::Struct(parts) = field else {
            continue;
        };
        let [Value::Byte(code), Value::Variant(value)] = &parts[..] else {
            continue;
        };
        match (code, value.as_ref()) {
            (1, Value::Path(path)) => message.path = Some(path.clone()),
            (2, Value::Str(name)) => message.interface = Some(name.clone()),
            (3, Value::Str(name)) => message.member = Some(name.clone()),
            (4, Value::Str(name)) => message.error_name = Some(name.clone()),
            (6, Value::Str(name)) => message.destination = Some(name.clone()),
            (5, Value::U32(serial)) => message.reply_serial = Some(*serial),
            (8, Value::Sig(sig)) => signature = sig.clone(),
            _ => {}
        }
    }
    dec.align(8)?;
    if message.kind == SIGNAL {
        return Ok(message);
    }
    let mut rest = signature.as_str();
    while !rest.is_empty() {
        let (one, more) = first_type(rest)?;
        message.body.push(dec.value(one)?);
        rest = more;
    }
    if dec.pos != bytes.len() {
        return Err(WireError("its body is longer than its signature".into()));
    }
    Ok(message)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn reference(line: usize) -> Vec<u8> {
        let text = include_str!("../../../tests/fixtures/dbus/glib-reference.hex");
        let hex = text.lines().nth(line).unwrap();
        (0..hex.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&hex[i..i + 2], 16).unwrap())
            .collect()
    }

    fn s(v: &str) -> Value {
        Value::Str(v.into())
    }

    fn variant(v: Value) -> Value {
        Value::Variant(Box::new(v))
    }

    fn property(name: &str, value: Value) -> Value {
        Value::Struct(vec![s(name), variant(value)])
    }

    fn entry(key: &str, value: Value) -> Value {
        Value::Entry(Box::new(s(key)), Box::new(variant(value)))
    }

    #[test]
    fn a_transient_unit_call_is_encoded_as_glib_encodes_it() {
        let call = Call {
            destination: "org.freedesktop.systemd1",
            path: "/org/freedesktop/systemd1",
            interface: "org.freedesktop.systemd1.Manager",
            member: "StartTransientUnit",
            body: vec![
                s("app-logis-forge\\x2danoint-4242.scope"),
                s("fail"),
                Value::Array(
                    "(sv)".into(),
                    vec![
                        property("PIDs", Value::Array("u".into(), vec![Value::U32(4242)])),
                        property("Slice", s("app.slice")),
                        property("Description", s("Anoint")),
                    ],
                ),
                Value::Array("(sa(sv))".into(), vec![]),
            ],
            flags: 0,
        };
        assert_eq!(encode_call(&call, 7), reference(0));
    }

    #[test]
    fn an_application_action_call_is_encoded_as_glib_encodes_it() {
        let call = Call {
            destination: "org.forge.Bus",
            path: "/org/forge/Bus",
            interface: "org.freedesktop.Application",
            member: "ActivateAction",
            body: vec![
                s("wake"),
                Value::Array("v".into(), vec![]),
                Value::Array(
                    "{sv}".into(),
                    vec![
                        entry("activation-token", s("tok")),
                        entry("desktop-startup-id", s("tok")),
                    ],
                ),
            ],
            flags: 0,
        };
        assert_eq!(encode_call(&call, 3), reference(1));
    }

    #[test]
    fn a_call_without_a_body_has_no_signature() {
        let call = Call {
            destination: "org.freedesktop.DBus",
            path: "/org/freedesktop/DBus",
            interface: "org.freedesktop.DBus",
            member: "Hello",
            body: vec![],
            flags: 0,
        };
        assert_eq!(encode_call(&call, 1), reference(2));
    }

    #[test]
    fn a_big_endian_reply_is_read_with_its_variant() {
        let bytes = reference(3);
        let head: [u8; 16] = bytes[..16].try_into().unwrap();
        assert_eq!(total_len(&head), Ok(bytes.len()));
        let reply = decode(&bytes).unwrap();
        assert_eq!(
            (reply.kind, reply.serial, reply.reply_serial),
            (METHOD_RETURN, 31, Some(9))
        );
        let env = Value::Array(
            "s".into(),
            vec![
                s("PATH=/usr/bin"),
                s("WAYLAND_DISPLAY=wayland-1"),
                s("LANG=de_DE.UTF-8"),
            ],
        );
        assert_eq!(reply.body, [variant(env)]);
    }

    #[test]
    fn an_error_is_read_with_its_name_and_message() {
        let error = decode(&reference(4)).unwrap();
        assert_eq!(error.kind, ERROR);
        assert_eq!(error.reply_serial, Some(9));
        assert_eq!(
            error.error_name.as_deref(),
            Some("org.freedesktop.DBus.Error.ServiceUnknown")
        );
        assert_eq!(error.body, [s("The name is not activatable")]);
    }

    #[test]
    fn our_own_calls_read_back() {
        let call = Call {
            destination: "d.e",
            path: "/p",
            interface: "i.f",
            member: "M",
            body: vec![
                Value::Byte(7),
                Value::Bool(true),
                Value::Struct(vec![Value::U32(1), Value::Path("/o".into())]),
                Value::Array("{sv}".into(), vec![entry("k", Value::Sig("as".into()))]),
            ],
            flags: NO_REPLY_EXPECTED,
        };
        let bytes = encode_call(&call, 5);
        let back = decode(&bytes).unwrap();
        assert_eq!((back.kind, back.serial), (METHOD_CALL, 5));
        let names = [&back.destination, &back.path, &back.interface, &back.member];
        assert_eq!(
            names.map(|n| n.as_deref()),
            [Some("d.e"), Some("/p"), Some("i.f"), Some("M")]
        );
        assert_eq!(back.body, call.body);
    }

    #[test]
    fn replies_and_errors_read_back() {
        let reply = decode(&encode_reply(4, 2, None, &[Value::Path("/job/1".into())])).unwrap();
        assert_eq!(
            (reply.kind, reply.serial, reply.reply_serial, reply.body),
            (
                METHOD_RETURN,
                4,
                Some(2),
                vec![Value::Path("/job/1".into())]
            )
        );
        let error = decode(&encode_reply(5, 3, Some("x.Failed"), &[s("why")])).unwrap();
        assert_eq!(
            (error.kind, error.error_name.as_deref(), error.body),
            (ERROR, Some("x.Failed"), vec![s("why")])
        );
    }

    #[test]
    fn truncated_or_absurd_messages_are_refused() {
        let bytes = reference(3);
        assert!(decode(&bytes[..bytes.len() - 3]).is_err());
        let mut huge: [u8; 16] = bytes[..16].try_into().unwrap();
        huge[4..8].copy_from_slice(&[0x7f, 0xff, 0xff, 0xff]);
        assert!(total_len(&huge).is_err());
        assert!(total_len(&[b'x'; 16]).is_err(), "no byte order");
    }
}
