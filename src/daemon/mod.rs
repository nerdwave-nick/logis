//! The daemon role: `logis daemon` (spec §4.1). It checks the Wayland
//! session, claims `$XDG_RUNTIME_DIR/logis.sock`, reports readiness with one
//! line on stdout, then serves sessions until it idles out (exit 0) or loses
//! the compositor (exit 2).
//!
//! Wiring for later slices: the window replaces the resolver ([`resolver`])
//! and takes over the Wayland connection, reporting its loss as
//! [`core::Event::WaylandLost`] on the same channel.

pub mod core;
pub mod lifecycle;
pub mod listener;
pub mod resolver;
pub mod sessions;

use std::io::Write;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::mpsc::{Sender, channel};
use std::thread;

use self::core::{Core, Event};
use self::lifecycle::Exit;
use self::listener::Claim;
use self::resolver::{Resolver, Scripted, Windowless};
use crate::cli::DaemonOptions;
use crate::{fail, wayland};

/// Runs the daemon until it must stop.
pub fn run(options: DaemonOptions) -> ExitCode {
    let runtime_dir = std::env::var_os("XDG_RUNTIME_DIR").map(PathBuf::from);
    let Some(runtime_dir) = runtime_dir.filter(|d| d.is_absolute()) else {
        return fail(
            "the machine spirit has no dwelling: XDG_RUNTIME_DIR is not set or not absolute",
        );
    };
    let noosphere = match wayland::preflight() {
        Ok(noosphere) => noosphere,
        Err(e) => return fail(&e.to_string()),
    };
    let listening = match listener::claim(&runtime_dir) {
        Ok(Claim::Listening(listening)) => listening,
        Ok(Claim::AlreadyServed) => {
            announce("another machine spirit already listens; this one returns to rest");
            return ExitCode::SUCCESS;
        }
        Err(e) => return fail(&format!("the machine spirit cannot take its socket: {e}")),
    };

    let (events, inbox) = channel();
    let resolver = match resolver_from_env(&events) {
        Ok(resolver) => resolver,
        Err(message) => return fail(&message),
    };
    match listening.listener.try_clone() {
        Ok(listener) => listener::spawn_acceptor(listener, events.clone()),
        Err(e) => return fail(&format!("the machine spirit cannot listen: {e}")),
    }
    let lost = events.clone();
    thread::spawn(move || {
        let reason = noosphere.wait_for_loss();
        let _ = lost.send(Event::WaylandLost { reason });
    });
    drop(events);
    announce(&format!(
        "the machine spirit wakes and listens at {}",
        listening.path.display()
    ));
    if options.detach {
        detach(&runtime_dir);
    }

    let mut core = Core::new(resolver, std::process::id());
    let exit = lifecycle::serve(&mut core, &inbox, options.idle_timeout);
    drop(listening);
    match exit {
        Exit::Idle => ExitCode::SUCCESS,
        Exit::WaylandLost(reason) => fail(&format!(
            "the noosphere fell silent: the Wayland connection was lost ({reason}). \
             The machine spirit departs; the next summons will rouse it anew."
        )),
        Exit::Deaf => fail("the machine spirit can no longer hear its socket"),
    }
}

/// The readiness line: a client that started the daemon waits for it.
fn announce(message: &str) {
    let mut out = std::io::stdout();
    // The client may already be gone; the daemon carries on regardless.
    let _ = writeln!(out, "logis: {message}");
    let _ = out.flush();
}

/// Leaves the starter's stdio: stdout to /dev/null, stderr to
/// `<runtime_dir>/logis.log`. Only the socket owner gets here, so truncating
/// the log is safe; it holds this daemon's diagnostics only.
fn detach(runtime_dir: &std::path::Path) {
    let log = runtime_dir.join("logis.log");
    let null = std::fs::File::create("/dev/null");
    let log = std::fs::File::create(&log).or_else(|_| std::fs::File::create("/dev/null"));
    if let Ok(null) = null {
        let _ = rustix::stdio::dup2_stdout(null);
    }
    if let Ok(log) = log {
        let _ = rustix::stdio::dup2_stderr(log);
    }
}

/// `LOGIS_RESOLVER=scripted` swaps the window for the [`Scripted`] resolver
/// (tests and measurements); unset, sessions meet the real (windowless) one.
fn resolver_from_env(events: &Sender<Event>) -> Result<Box<dyn Resolver>, String> {
    match std::env::var("LOGIS_RESOLVER") {
        Err(std::env::VarError::NotPresent) => Ok(Box::new(Windowless)),
        Ok(name) if name == "scripted" => Ok(Box::new(Scripted::new(events.clone()))),
        Ok(name) => Err(format!("LOGIS_RESOLVER names no known resolver: '{name}'")),
        Err(e) => Err(format!("LOGIS_RESOLVER: {e}")),
    }
}
