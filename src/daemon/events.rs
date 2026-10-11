//! What the daemon's event loop hears, and how it answers a client.

use logis::config::{self, Config};

use super::sessions::SessionId;
use crate::protocol::{Reply, Request};
use crate::provider::Answer;
use crate::provider::process::Heard;

/// Everything the daemon's event loop reacts to.
pub enum Event {
    /// A client connected; its connection number is also its session's id.
    Connected {
        conn: SessionId,
        peer: Box<dyn Peer>,
    },
    /// A frame arrived, or could not be decoded (the error says why).
    Received {
        conn: SessionId,
        request: Result<Request, String>,
    },
    /// The client hung up.
    Disconnected { conn: SessionId },
    /// The user's answer for a visible session (from the resolver).
    Answered { session: SessionId, answer: Answer },
    /// The resolver could not show a session; its client gets the error.
    Failed { session: SessionId, message: String },
    /// A session's provider process spoke a line, or exited.
    Heard { session: SessionId, heard: Heard },
    /// The app index was (re)read.
    Indexed(std::sync::Arc<logis::apps::index::Index>),
    /// The compositor's activation token for a session's launch, or none.
    Activation {
        session: SessionId,
        token: Option<String>,
    },
    /// The compositor connection is gone (spec §4.2: exit non-zero).
    WaylandLost { reason: String },
    /// The config file changed and was read again.
    Reconfigured(Result<Config, config::Error>),
}

/// The sending half of one client connection.
pub trait Peer: Send {
    /// Sends one reply; a failure is treated like a hang-up later.
    fn send(&mut self, reply: &Reply);
    /// Ends the connection; the client sees end of stream.
    fn close(&mut self);
}
