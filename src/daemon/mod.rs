//! The daemon role: `logis daemon` (spec §4.1). It checks the Wayland
//! session, claims `$XDG_RUNTIME_DIR/logis.sock`, wakes the window (its
//! compositor connection, the GPU and the fonts) and reports readiness with
//! one line on stdout, then serves sessions until it idles out (exit 0) or
//! loses the compositor (exit 2).
//!
//! The preflight's connection is the daemon's only one: the window thread
//! ([`window`]) takes it over and reports its loss as
//! [`core::Event::WaylandLost`] on the daemon's channel.

pub mod apps;
pub mod core;
pub mod events;
pub mod lifecycle;
pub mod listener;
pub mod providing;
pub mod resolver;
pub mod sessions;
pub mod window;

use std::io::Write;
use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::Arc;
use std::sync::mpsc::{Sender, channel};
use std::thread;

use logis::apps::frecency::Frecency;
use logis::apps::index::{Context, Dirs, Index};
use logis::apps::watch::Watcher as AppsWatcher;
use logis::config::{self, Source, live::Live, watch::Watcher};

use self::core::{Core, Event};
use self::lifecycle::Exit;
use self::listener::Claim;
use self::resolver::{Resolver, Scripted};
use crate::cli::DaemonOptions;
use crate::provider::external::Running;
use crate::provider::process::{self, Process};
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
    let source = Source::new(options.config.as_deref());
    // Watch before reading, so that no edit falls between the two.
    let watcher = source.path().map(Watcher::new);
    let config = match source.load() {
        Ok(config) => config,
        Err(e) => return fail(&config::heresy(&e)),
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
    let resolver = match resolver_from_env(noosphere, &events) {
        Ok(resolver) => resolver,
        Err(message) => return fail(&message),
    };
    match listening.listener.try_clone() {
        Ok(listener) => listener::spawn_acceptor(listener, events.clone()),
        Err(e) => return fail(&format!("the machine spirit cannot listen: {e}")),
    }
    announce(&format!(
        "the machine spirit wakes and listens at {}",
        listening.path.display()
    ));
    if options.detach {
        detach(&runtime_dir);
    }
    // After detaching, so that what a reload logs reaches `logis.log`; edits
    // made meanwhile wait in the watch.
    match watcher {
        Some(Ok(watcher)) => {
            let reload = events.clone();
            watcher.spawn(move || reload.send(Event::Reconfigured(source.load())).is_ok());
        }
        Some(Err(e)) => log(&format!(
            "the config is not watched, edits need a restart: {e}"
        )),
        None => {}
    }
    let spawn = spawner(events.clone());
    index_apps(events.clone());
    drop(events);

    let mut core = Core::new(resolver, std::process::id())
        .with_spawn(spawn)
        .with_apps(apps_state());
    core.configure(&config);
    let mut live = Live::new(config, options.idle_timeout);
    let exit = lifecycle::serve(&mut core, &inbox, &mut live);
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

/// One line in the daemon's log (stderr: the starter's, or `logis.log` once
/// detached). Never panics, even when nobody reads stderr any more.
pub fn log(message: &str) {
    let _ = writeln!(std::io::stderr(), "logis: {message}");
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

/// The apps' state at start: frecency from `$XDG_STATE_HOME/logis/`, the
/// session bus's address. The index comes from [`index_apps`].
fn apps_state() -> apps::AppsState {
    let var = |name: &str| std::env::var(name).ok();
    let frecency_path = Frecency::path_from_vars(var);
    let frecency = frecency_path
        .as_deref()
        .map(|path| {
            Frecency::load(path).unwrap_or_else(|e| {
                log(&format!("{e}; frecency starts anew"));
                Frecency::default()
            })
        })
        .unwrap_or_default();
    apps::AppsState {
        frecency,
        frecency_path,
        bus: logis::launch::bus::session_address(var),
        ..apps::AppsState::default()
    }
}

/// Reads the app index on a thread of its own (so it never delays the
/// first frame), sends it as [`Event::Indexed`], and reads it again
/// whenever the applications directories change. The directories, the
/// current desktops (`OnlyShowIn`), `PATH` (`TryExec`) and the locale are
/// the daemon's own environment's.
fn index_apps(events: Sender<Event>) {
    let indexer = move || {
        let (dirs, cx) = (Dirs::from_env(), Context::from_env());
        let mut watcher = AppsWatcher::new()
            .inspect_err(|e| {
                log(&format!(
                    "applications are not watched, changes need a restart: {e}"
                ))
            })
            .ok();
        let mut read = Vec::new();
        loop {
            // Watched before reading, so no change falls between the two.
            if let Some(w) = watcher.as_mut() {
                w.arm(&read, &dirs.0);
            }
            let index = Index::scan(&dirs, &cx);
            read.clone_from(&index.dirs);
            if let Some(w) = watcher.as_mut() {
                w.arm(&read, &dirs.0);
            }
            if events.send(Event::Indexed(Arc::new(index))).is_err() {
                return;
            }
            match watcher.as_mut().map(|w| w.wait(None)) {
                Some(Ok(_)) => {}
                _ => return,
            }
        }
    };
    if let Err(e) = thread::Builder::new().name("apps".into()).spawn(indexer) {
        log(&format!("the census of applications cannot be taken: {e}"));
    }
}

/// Starts provider processes; what each says comes back as
/// [`Event::Heard`] for its session.
fn spawner(events: Sender<Event>) -> providing::Spawn {
    Box::new(move |session, launch| {
        let events = events.clone();
        let heard = move |heard| events.send(Event::Heard { session, heard }).is_ok();
        let process = Process::spawn(&process::Launch::from(launch), heard)?;
        Ok(Box::new(process) as Box<dyn Running>)
    })
}

/// The window ([`window::start`]) on the preflight's connection; with
/// `LOGIS_RESOLVER=scripted`, the [`Scripted`] resolver instead (tests and
/// measurements without a window), while a thread watches the connection.
fn resolver_from_env(
    noosphere: wayland::Noosphere,
    events: &Sender<Event>,
) -> Result<Box<dyn Resolver>, String> {
    match std::env::var("LOGIS_RESOLVER") {
        Err(std::env::VarError::NotPresent) => {
            window::start(noosphere.into_connection(), events.clone())
                .map(|r| Box::new(r) as Box<dyn Resolver>)
        }
        Ok(name) if name == "scripted" => {
            let lost = events.clone();
            thread::spawn(move || {
                let reason = noosphere.wait_for_loss();
                let _ = lost.send(Event::WaylandLost { reason });
            });
            Ok(Box::new(Scripted::new(events.clone())))
        }
        Ok(name) => Err(format!("LOGIS_RESOLVER names no known resolver: '{name}'")),
        Err(e) => Err(format!("LOGIS_RESOLVER: {e}")),
    }
}
