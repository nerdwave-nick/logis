//! The daemon's event loop and when it ends: after `idle-timeout` without a
//! connected client (exit 0), or when the compositor connection is lost
//! (exit non-zero, spec §4.2). Whatever produces [`Event`]s (socket threads,
//! the Wayland connection, the resolver) feeds one channel.

use std::sync::mpsc::{Receiver, RecvTimeoutError};
use std::time::Instant;

use logis::config::Config;
use logis::config::live::{Live, Reload};

use super::core::{Core, Event};
use super::resolver::{Resolver, Toast};

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

/// Feeds events to `core` until the daemon must stop. `config` holds the
/// config in force and decides the idle timeout.
pub fn serve<R: Resolver>(core: &mut Core<R>, events: &Receiver<Event>, config: &mut Live) -> Exit {
    let mut idle_since = Instant::now();
    loop {
        let event = match config.idle_timeout().filter(|_| core.is_idle()) {
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
            Event::Reconfigured(loaded) => reconfigure(core, config, loaded),
        }
        // Idleness is measured from the moment the last client left.
        if !(was_idle && core.is_idle()) {
            idle_since = Instant::now();
        }
    }
}

/// Applies a reread config, or keeps the last sound one and says why.
fn reconfigure<R: Resolver>(
    core: &mut Core<R>,
    config: &mut Live,
    loaded: Result<Config, logis::config::Error>,
) {
    match config.reload(loaded) {
        Reload::Applied => {
            super::log("the machine spirit heeds the amended config");
            core.configure(config.config());
        }
        Reload::Restored => super::log("the config is sound once more; nothing has changed"),
        Reload::Unchanged => {}
        Reload::Rejected(error) => {
            super::log(&format!(
                "{}; the last sound config stands",
                logis::config::heresy_text(&error)
            ));
            core.toast(Toast::ConfigRejected { error });
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::daemon::resolver::{Toast, Windowless};
    use crate::daemon::sessions::SessionId;
    use crate::protocol::{Item, Mode, Reply};
    use logis::config::{Config, Error, Invalid, Problem};
    use std::sync::mpsc::channel;
    use std::sync::{Arc, Mutex};
    use std::thread;
    use std::time::Duration;

    struct Mute;
    impl crate::daemon::core::Peer for Mute {
        fn send(&mut self, _: &Reply) {}
        fn close(&mut self) {}
    }

    fn core() -> Core<Windowless> {
        Core::new(Windowless, 1)
    }

    const SHORT: Duration = Duration::from_millis(50);

    /// `--idle-timeout` as given, with the default config.
    fn flag(idle: Option<Duration>) -> Live {
        Live::new(Config::default(), idle)
    }

    /// What reached the window seam.
    #[derive(Debug, PartialEq)]
    enum Seen {
        Configured(Config),
        Toast(Toast),
    }

    #[derive(Clone, Default)]
    struct Recorder(Arc<Mutex<Vec<Seen>>>);

    impl Recorder {
        fn seen(&self) -> Vec<Seen> {
            std::mem::take(&mut self.0.lock().unwrap())
        }
    }

    impl Resolver for Recorder {
        fn accepts(&self, _: Mode) -> Result<(), String> {
            Ok(())
        }
        fn show(&mut self, _: SessionId, _: Mode, _: Vec<Item>, _: bool) {}
        fn append(&mut self, _: SessionId, _: Vec<Item>) {}
        fn end(&mut self, _: SessionId) {}
        fn hide(&mut self, _: SessionId) {}
        fn configure(&mut self, config: &Config) {
            self.0
                .lock()
                .unwrap()
                .push(Seen::Configured(config.clone()));
        }
        fn toast(&mut self, toast: Toast) {
            self.0.lock().unwrap().push(Seen::Toast(toast));
        }
    }

    /// Ends a `serve` that would otherwise wait forever, so a broken reload
    /// fails its test instead of hanging it.
    fn watchdog(tx: &std::sync::mpsc::Sender<Event>) {
        let tx = tx.clone();
        thread::spawn(move || {
            thread::sleep(Duration::from_secs(2));
            let reason = "the watchdog barked".to_owned();
            let _ = tx.send(Event::WaylandLost { reason });
        });
    }

    fn idle_after(idle: Duration) -> Config {
        Config {
            idle_timeout: Some(idle),
            ..Config::default()
        }
    }

    fn heresy() -> Error {
        Error {
            path: "/cfg/logis.jsonc".into(),
            problem: Problem::Invalid(Invalid {
                line: 1,
                column: 22,
                message: "invalid type: string \"soon\"".into(),
            }),
        }
    }

    #[test]
    fn a_sound_reload_reaches_the_window_and_sets_the_idle_timeout() {
        let (tx, rx) = channel();
        let recorder = Recorder::default();
        let mut core = Core::new(Box::new(recorder.clone()) as Box<dyn Resolver>, 1);
        tx.send(Event::Reconfigured(Ok(idle_after(SHORT)))).unwrap();
        watchdog(&tx);
        // Without the reload the daemon would stay resident.
        assert_eq!(serve(&mut core, &rx, &mut flag(None)), Exit::Idle);
        assert_eq!(recorder.seen(), [Seen::Configured(idle_after(SHORT))]);
    }

    #[test]
    fn a_refused_reload_raises_a_toast_and_keeps_the_last_sound_config() {
        let (tx, rx) = channel();
        let recorder = Recorder::default();
        let mut core = Core::new(recorder.clone(), 1);
        let mut live = Live::new(idle_after(SHORT), None);
        let peer = Box::new(Mute);
        tx.send(Event::Connected { conn: 1, peer }).unwrap();
        tx.send(Event::Reconfigured(Err(heresy()))).unwrap();
        tx.send(Event::Disconnected { conn: 1 }).unwrap();
        watchdog(&tx);
        // The last sound config's idle timeout still ends the daemon.
        assert_eq!(serve(&mut core, &rx, &mut live), Exit::Idle);
        let [Seen::Toast(Toast::ConfigRejected { error })] = &recorder.seen()[..] else {
            panic!("no single toast");
        };
        assert!(
            error.contains("/cfg/logis.jsonc:1:22: invalid type"),
            "{error}"
        );
        assert_eq!(live.config(), &idle_after(SHORT));
        drop(tx);
    }

    #[test]
    fn an_unchanged_reload_does_not_disturb_the_window() {
        let (tx, rx) = channel();
        let recorder = Recorder::default();
        let mut core = Core::new(recorder.clone(), 1);
        let mut live = Live::new(idle_after(SHORT), None);
        tx.send(Event::Reconfigured(Ok(idle_after(SHORT)))).unwrap();
        assert_eq!(serve(&mut core, &rx, &mut live), Exit::Idle);
        assert_eq!(recorder.seen(), []);
        drop(tx);
    }

    #[test]
    fn an_idle_daemon_exits_after_the_timeout() {
        let (_tx, rx) = channel();
        let start = Instant::now();
        assert_eq!(serve(&mut core(), &rx, &mut flag(Some(SHORT))), Exit::Idle);
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
        assert_eq!(serve(&mut core(), &rx, &mut flag(Some(SHORT))), Exit::Idle);
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
            serve(&mut core(), &rx, &mut flag(None)),
            Exit::WaylandLost("gone".into())
        );
    }

    #[test]
    fn without_any_event_source_the_daemon_is_deaf() {
        let (tx, rx) = channel::<Event>();
        drop(tx);
        assert_eq!(serve(&mut core(), &rx, &mut flag(None)), Exit::Deaf);
    }
}
