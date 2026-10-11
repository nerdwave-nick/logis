//! The window-backed resolver: sessions are shown in the cogitator window.
//!
//! The window lives on a thread of its own (`cogitator`), which owns the
//! daemon's single compositor connection (the preflight's) and the GPU,
//! woken there at start, before the daemon reports it listens. The daemon's
//! event loop talks to it through [`WindowResolver`]: each call becomes a
//! [`Command`] on a channel plus a write to an eventfd that the window
//! thread polls beside the compositor's socket, so neither thread busy-waits
//! and an idle window sleeps in `poll`. Answers come back as
//! [`Event::Answered`] (or [`Event::Failed`]) on the daemon's channel; a
//! lost compositor, or a window thread that dies, is [`Event::WaylandLost`].

mod ui;

use std::os::fd::{AsFd, OwnedFd};
use std::sync::Arc;
use std::sync::mpsc::{self, Sender};
use std::thread;

use logis::config::Config;
use logis::input::FocusLoss;
use logis::list::Entry;
use logis::render::BackendChoice;
use logis::window::Windows;
use rustix::event::{EventfdFlags, eventfd};
use wayland_client::Connection;

use super::core::Event;
use super::resolver::Resolver;
use super::sessions::SessionId;
use crate::protocol::{Mode, Options};
use crate::provider::Step;

/// What the daemon asks of the window thread.
pub enum Command {
    /// Show a session's first stage.
    Show {
        session: SessionId,
        mode: Mode,
        options: Options,
        entries: Vec<Entry>,
    },
    /// What the shown session's provider asks next.
    Step { session: SessionId, step: Step },
    /// The session is no longer shown.
    Hide { session: SessionId },
    /// The session is over; keep its toast in view, without the keyboard.
    Linger { session: SessionId },
    /// The shown session's toast has had its time.
    Untoast { session: SessionId },
    /// What losing keyboard focus does (the config's `focus-loss`).
    FocusLoss(FocusLoss),
}

/// The resolver the daemon holds; the window thread does the work.
pub struct WindowResolver {
    commands: Sender<Command>,
    wake: Arc<OwnedFd>,
}

/// Starts the window thread on `connection`: binds the globals, wakes the
/// GPU (`LOGIS_GPU_BACKEND` may force a backend) and resolves the fonts.
/// Returns once that is done, or why it failed.
pub fn start(connection: Connection, events: Sender<Event>) -> Result<WindowResolver, String> {
    let choice = BackendChoice::from_env().map_err(|e| e.to_string())?;
    let wake = eventfd(0, EventfdFlags::CLOEXEC | EventfdFlags::NONBLOCK)
        .map_err(|e| format!("the cogitator cannot be summoned: eventfd: {e}"))?;
    let wake = Arc::new(wake);
    let (commands, inbox) = mpsc::channel();
    let (ready, started) = mpsc::sync_channel(1);
    let theirs = Arc::clone(&wake);
    thread::Builder::new()
        .name("cogitator".into())
        .spawn(move || {
            let mut departure = Departure(Some(events.clone()));
            let (windows, queue) = match Windows::with_connection(connection, choice) {
                Ok(windows) => windows,
                Err(e) => {
                    departure.0 = None;
                    let _ = ready.send(Err(e.to_string()));
                    return;
                }
            };
            let _ = ready.send(Ok(()));
            let lost = ui::run(windows, queue, &inbox, theirs.as_fd(), &events);
            // An orderly end: the compositor is gone, or the daemon dropped
            // its resolver (it is ending); only a panic leaves it armed.
            departure.0 = None;
            if let Some(reason) = lost {
                let _ = events.send(Event::WaylandLost { reason });
            }
        })
        .map_err(|e| format!("the cogitator cannot be summoned: {e}"))?;
    match started.recv() {
        Ok(Ok(())) => Ok(WindowResolver { commands, wake }),
        Ok(Err(e)) => Err(e),
        Err(_) => Err("the cogitator perished while waking".to_owned()),
    }
}

/// Reports a window thread that ends unexpectedly (a panic) as a lost
/// compositor, so the daemon exits non-zero instead of serving blind.
struct Departure(Option<Sender<Event>>);

impl Drop for Departure {
    fn drop(&mut self) {
        if let Some(events) = self.0.take() {
            let reason = "the cogitator thread perished".to_owned();
            let _ = events.send(Event::WaylandLost { reason });
        }
    }
}

impl WindowResolver {
    fn send(&self, command: Command) {
        // A window thread that is gone has reported why already.
        if self.commands.send(command).is_ok() {
            let _ = rustix::io::write(&*self.wake, &1u64.to_ne_bytes());
        }
    }
}

impl Resolver for WindowResolver {
    fn accepts(&self, mode: Mode) -> Result<(), String> {
        match mode {
            Mode::Dmenu | Mode::Provider => Ok(()),
            Mode::Apps | Mode::Servitor => Err(
                "the cogitator knows only dmenu and provider rites yet; the app \
                 launcher and servitor mode are still to be consecrated"
                    .to_owned(),
            ),
        }
    }

    fn show(&mut self, session: SessionId, mode: Mode, options: Options, entries: Vec<Entry>) {
        self.send(Command::Show {
            session,
            mode,
            options,
            entries,
        });
    }

    fn step(&mut self, session: SessionId, step: Step) {
        self.send(Command::Step { session, step });
    }

    /// The list needs no word that the input ended.
    fn end(&mut self, _: SessionId) {}

    fn hide(&mut self, session: SessionId) {
        self.send(Command::Hide { session });
    }

    fn linger(&mut self, session: SessionId) {
        self.send(Command::Linger { session });
    }

    fn untoast(&mut self, session: SessionId) {
        self.send(Command::Untoast { session });
    }

    fn configure(&mut self, config: &Config) {
        self.send(Command::FocusLoss(config.focus_loss));
    }
}
