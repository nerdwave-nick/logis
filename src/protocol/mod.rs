//! The client <-> daemon session protocol (spec §4.1): length-prefixed JSON
//! frames over the Unix stream socket `$XDG_RUNTIME_DIR/logis.sock`.
//!
//! One connection carries at most one session:
//!
//! 1. client → `hello`; daemon → `welcome` (or `error` and close).
//! 2. client → `open` (with the list's [`Options`], or for a provider
//!    session the [`Launch`] of its provider); daemon → `queued` and/or
//!    `visible`.
//! 3. dmenu sessions: client → any number of `items`, then `end`. A
//!    provider session's items come from its provider (docs/provider-protocol.md).
//! 4. daemon → `result`, then the daemon closes the connection.
//!
//! The client may send `cancel` at any time; disconnecting cancels too.
//! A protocol violation ends the connection with `error`.
//!
//! Connections are not ordered among themselves. A session opened just after
//! another client hung up may be `queued` until the daemon has handled that
//! hang-up, and then becomes `visible`; a `cancel` answered with its `result`
//! is handled by the time the result arrives.

pub mod base64;
pub mod frame;

use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

pub use logis::list::Options;

/// Bumped whenever a message changes incompatibly. 2: `open` carries the
/// list's options, items may carry their match text, and `typed` outcomes.
/// 3: provider sessions (`provider` mode, `open.provider`, `closed`).
pub const VERSION: u32 = 3;

/// The socket's file name inside `$XDG_RUNTIME_DIR`.
pub const SOCKET_NAME: &str = "logis.sock";

/// The daemon socket for this runtime directory, which must be absolute.
pub fn socket_path(runtime_dir: Option<&Path>) -> Result<PathBuf, String> {
    runtime_dir
        .filter(|d| d.is_absolute())
        .map(|d| d.join(SOCKET_NAME))
        .ok_or_else(|| "XDG_RUNTIME_DIR is not set or not absolute".to_owned())
}

/// What a session shows; it decides the concurrency policy.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Mode {
    /// Lines from the client's stdin; the choice goes back to the client.
    Dmenu,
    /// Desktop applications, launched by the daemon.
    Apps,
    /// servitor's rites and aspects.
    Servitor,
    /// An external provider process (`logis --provider`).
    Provider,
}

impl Mode {
    /// dmenu sessions queue behind a visible session; interactive ones replace it.
    pub fn is_interactive(self) -> bool {
        !matches!(self, Self::Dmenu)
    }
}

/// One selectable row: its original bytes, kept apart from its display text.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Item {
    /// The text shown, and matched unless `match` says otherwise.
    pub text: String,
    /// The text matched instead of `text` (`--match-nth`).
    #[serde(rename = "match", default, skip_serializing_if = "Option::is_none")]
    pub match_text: Option<String>,
    /// The exact input bytes, returned unchanged when the item is chosen.
    #[serde(with = "base64::serde_bytes")]
    pub raw: Vec<u8>,
}

impl Item {
    /// A dmenu line: the bytes as read, and their lossy UTF-8 for display.
    #[cfg(test)]
    pub fn from_raw(raw: Vec<u8>) -> Self {
        let text = String::from_utf8_lossy(&raw).into_owned();
        Self {
            text,
            match_text: None,
            raw,
        }
    }
}

/// Messages from a client to the daemon.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "lowercase")]
pub enum Request {
    Hello {
        version: u32,
    },
    Open {
        mode: Mode,
        #[serde(default)]
        options: Options,
        /// What provider mode runs.
        #[serde(default, skip_serializing_if = "Option::is_none")]
        provider: Option<Launch>,
        /// An app session: the client's environment (`NAME=VALUE` each),
        /// the session environment when no systemd user manager answers.
        #[serde(default, skip_serializing_if = "Vec::is_empty")]
        env: Vec<Bytes>,
    },
    Items {
        items: Vec<Item>,
    },
    End,
    Cancel,
}

/// How a session ended.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "lowercase")]
pub enum Outcome {
    /// The user chose the item at `index` (in the order the items arrived).
    Selected { index: u64, item: Item },
    /// The user accepted the typed text, which matched no item (dmenu's
    /// free text; never with `only-match`).
    Typed { text: String },
    /// The user dismissed the session.
    Dismissed,
    /// An interactive session took its place.
    Replaced,
    /// The client asked to cancel.
    Cancelled,
    /// The provider ended its flow (`close`).
    Closed,
}

/// Bytes on the wire, as base64 (paths, arguments and the environment need
/// not be UTF-8).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Bytes(#[serde(with = "base64::serde_bytes")] pub Vec<u8>);

/// How the daemon starts a provider: as if the client ran it, in the
/// client's working directory with the client's environment, never the
/// daemon's stale one.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Launch {
    /// The program, then its arguments.
    pub argv: Vec<Bytes>,
    /// The working directory.
    pub cwd: Bytes,
    /// The environment, `NAME=VALUE` each.
    pub env: Vec<Bytes>,
}

/// Messages from the daemon to a client.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "lowercase")]
pub enum Reply {
    Welcome {
        version: u32,
        pid: u32,
    },
    /// Another session is visible; this one waits its turn.
    Queued,
    /// The session is now the one shown.
    Visible,
    Result {
        outcome: Outcome,
    },
    Error {
        message: String,
    },
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{Value, json};

    fn wire<T: Serialize>(message: &T) -> Value {
        serde_json::to_value(message).unwrap()
    }

    #[test]
    fn requests_have_a_stable_wire_shape() {
        assert_eq!(
            wire(&Request::Hello { version: 1 }),
            json!({"type": "hello", "version": 1})
        );
        assert_eq!(VERSION, 3, "a changed protocol says so");
        assert_eq!(
            wire(&Reply::Result {
                outcome: Outcome::Typed { text: "x".into() }
            }),
            json!({"type": "result", "outcome": {"kind": "typed", "text": "x"}})
        );
        let open = |mode| Request::Open {
            mode,
            options: Options::default(),
            provider: None,
            env: Vec::new(),
        };
        let shape = wire(&open(Mode::Dmenu));
        assert_eq!(shape["type"], "open");
        assert_eq!(shape["mode"], "dmenu");
        assert_eq!(shape["options"]["prompt"], "> ");
        assert_eq!(shape["options"]["only-match"], false);
        // Every option has a default, so a bare open is still an open.
        let bare: Request =
            serde_json::from_value(json!({"type": "open", "mode": "apps"})).unwrap();
        assert_eq!(bare, open(Mode::Apps));
        assert_eq!(wire(&Request::End), json!({"type": "end"}));
        assert_eq!(wire(&Request::Cancel), json!({"type": "cancel"}));
    }

    #[test]
    fn an_item_carries_its_raw_bytes_apart_from_its_text() {
        let item = Item::from_raw(b"caf\xe9\tx".to_vec());
        assert_eq!(item.text, "caf\u{fffd}\tx");
        let message = Request::Items {
            items: vec![item.clone()],
        };
        assert_eq!(
            wire(&message),
            json!({"type": "items", "items": [{"text": "caf\u{fffd}\tx", "raw": "Y2Fm6Ql4"}]})
        );
        let back: Request = serde_json::from_value(wire(&message)).unwrap();
        assert_eq!(back, message);
        let matched = Item {
            match_text: Some("x".into()),
            ..item
        };
        assert_eq!(wire(&matched)["match"], "x");
        let back: Item = serde_json::from_value(wire(&matched)).unwrap();
        assert_eq!(back, matched);
    }

    #[test]
    fn replies_have_a_stable_wire_shape() {
        assert_eq!(
            wire(&Reply::Welcome { version: 1, pid: 7 }),
            json!({"type": "welcome", "version": 1, "pid": 7})
        );
        assert_eq!(
            wire(&Reply::Result {
                outcome: Outcome::Dismissed
            }),
            json!({"type": "result", "outcome": {"kind": "dismissed"}})
        );
        let selected = Outcome::Selected {
            index: 2,
            item: Item::from_raw(b"a".to_vec()),
        };
        assert_eq!(
            wire(&Reply::Result { outcome: selected }),
            json!({"type": "result", "outcome":
                {"kind": "selected", "index": 2, "item": {"text": "a", "raw": "YQ=="}}})
        );
    }

    #[test]
    fn only_dmenu_sessions_queue() {
        assert!(!Mode::Dmenu.is_interactive());
        assert!(Mode::Apps.is_interactive());
        assert!(Mode::Servitor.is_interactive());
        assert!(Mode::Provider.is_interactive());
    }

    #[test]
    fn a_provider_open_carries_its_launch_as_bytes() {
        let launch = Launch {
            argv: vec![Bytes(b"/bin/rite".to_vec()), Bytes(b"\xff".to_vec())],
            cwd: Bytes(b"/".to_vec()),
            env: vec![Bytes(b"A=1".to_vec())],
        };
        let open = Request::Open {
            mode: Mode::Provider,
            options: Options::default(),
            provider: Some(launch),
            env: Vec::new(),
        };
        let shape = wire(&open);
        assert_eq!(shape["mode"], "provider");
        assert_eq!(shape["provider"]["argv"], json!(["L2Jpbi9yaXRl", "/w=="]));
        assert_eq!(shape["provider"]["env"], json!(["QT0x"]));
        let back: Request = serde_json::from_value(shape).unwrap();
        assert_eq!(back, open);
        assert_eq!(
            wire(&Outcome::Closed),
            json!({"kind": "closed"}),
            "the provider ended its flow"
        );
    }

    #[test]
    fn the_socket_lives_in_an_absolute_runtime_dir() {
        assert_eq!(
            socket_path(Some(Path::new("/run/user/1000"))),
            Ok(PathBuf::from("/run/user/1000/logis.sock"))
        );
        assert!(socket_path(None).is_err());
        assert!(socket_path(Some(Path::new("run/user"))).is_err());
    }
}
