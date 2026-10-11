//! The daemon's event loop and when it ends: after `idle-timeout` without a
//! connected client (exit 0), or when the compositor connection is lost
//! (exit non-zero, spec §4.2). Whatever produces [`Event`]s (socket threads,
//! the Wayland connection, the resolver) feeds one channel.

use std::sync::mpsc::{Receiver, RecvTimeoutError};
use std::time::{Duration, Instant};

use super::core::{Core, Event};
use super::resolver::Resolver;

/// Why the daemon stopped.
#[derive(Debug, PartialEq, Eq)]
pub enum Exit {
    /// No client for the whole idle timeout.
    Idle,
    /// The Wayland connection is gone; the reason says how.
    WaylandLost(String),
    /// Every event source is gone; the daemon could hear nothing more.
    Deaf,
}

/// Feeds events to `core` until the daemon must stop.
pub fn serve<R: Resolver>(
    core: &mut Core<R>,
    events: &Receiver<Event>,
    idle_timeout: Option<Duration>,
) -> Exit {
    let mut idle_since = Instant::now();
    loop {
        let event = match idle_timeout.filter(|_| core.is_idle()) {
            Some(timeout) => {
                let left = timeout.saturating_sub(idle_since.elapsed());
                match events.recv_timeout(left) {
                    Ok(event) => event,
                    Err(RecvTimeoutError::Timeout) => return Exit::Idle,
                    Err(RecvTimeoutError::Disconnected) => return Exit::Deaf,
                }
            }
            None => match events.recv() {
                Ok(event) => event,
                Err(_) => return Exit::Deaf,
            },
        };
        let was_idle = core.is_idle();
        match event {
            Event::Connected { conn, peer } => core.connect(conn, peer),
            Event::Received { conn, request } => core.receive(conn, request),
            Event::Disconnected { conn } => core.disconnect(conn),
            Event::Resolved { session, outcome } => core.resolve(session, outcome),
            Event::WaylandLost { reason } => return Exit::WaylandLost(reason),
        }
        // Idleness is measured from the moment the last client left.
        if !(was_idle && core.is_idle()) {
            idle_since = Instant::now();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::daemon::resolver::Windowless;
    use crate::protocol::Reply;
    use std::sync::mpsc::channel;
    use std::thread;

    struct Mute;
    impl crate::daemon::core::Peer for Mute {
        fn send(&mut self, _: &Reply) {}
        fn close(&mut self) {}
    }

    fn core() -> Core<Windowless> {
        Core::new(Windowless, 1)
    }

    const SHORT: Duration = Duration::from_millis(50);

    #[test]
    fn an_idle_daemon_exits_after_the_timeout() {
        let (_tx, rx) = channel();
        let start = Instant::now();
        assert_eq!(serve(&mut core(), &rx, Some(SHORT)), Exit::Idle);
        assert!(start.elapsed() >= SHORT);
    }

    #[test]
    fn the_idle_timeout_waits_for_the_last_client_to_leave() {
        let (tx, rx) = channel();
        tx.send(Event::Connected {
            conn: 1,
            peer: Box::new(Mute),
        })
        .unwrap();
        let leave = thread::spawn(move || {
            thread::sleep(SHORT * 3);
            tx.send(Event::Disconnected { conn: 1 }).unwrap();
            tx // keep the channel open past the disconnect
        });
        let start = Instant::now();
        assert_eq!(serve(&mut core(), &rx, Some(SHORT)), Exit::Idle);
        assert!(
            start.elapsed() >= SHORT * 4,
            "left after {:?}",
            start.elapsed()
        );
        drop(leave.join());
    }

    #[test]
    fn a_lost_wayland_connection_ends_the_loop_at_once() {
        let (tx, rx) = channel();
        tx.send(Event::Connected {
            conn: 1,
            peer: Box::new(Mute),
        })
        .unwrap();
        tx.send(Event::WaylandLost {
            reason: "gone".into(),
        })
        .unwrap();
        assert_eq!(
            serve(&mut core(), &rx, None),
            Exit::WaylandLost("gone".into())
        );
    }

    #[test]
    fn without_any_event_source_the_daemon_is_deaf() {
        let (tx, rx) = channel::<Event>();
        drop(tx);
        assert_eq!(serve(&mut core(), &rx, None), Exit::Deaf);
    }
}
