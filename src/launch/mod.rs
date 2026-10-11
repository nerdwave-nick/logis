//! Launching desktop applications (spec §4.7): in the session environment,
//! into a transient systemd user scope where one is available, with an
//! xdg-activation token, detached so they outlive logis; `DBusActivatable`
//! applications through `org.freedesktop.Application`.
//!
//! - [`wire`]: the D-Bus wire format logis needs;
//! - [`bus`]: a connection to the session bus;
//! - [`session`]: the calls a launch makes on it (systemd, applications);
//! - [`spawn`]: starting a detached process, held until it is in its scope.
//!
//! # Why D-Bus directly, not `systemd-run --user --scope`
//!
//! Both put the process into a transient scope. logis needs a session-bus
//! client anyway, for `DBusActivatable` entries and for the session
//! environment (`Manager.Environment`), and then one more call
//! (`StartTransientUnit`) does what `systemd-run` would: no extra process
//! per launch, no dependency on the `systemd-run` binary, and a missing or
//! refusing user manager is known *before* the application starts, so logis
//! can still start it outside a scope ("where available"). The process is
//! held until systemd has moved it ([`spawn`]), so it and everything it
//! forks lives in its scope, never in the daemon's cgroup (which
//! `systemctl --user stop logis` would end).
//!
//! # The session environment
//!
//! The user manager's environment is the session's: what the compositor and
//! the session scripts imported (`WAYLAND_DISPLAY`, `XDG_CURRENT_DESKTOP` …),
//! kept current by them. Without a user manager, the environment of the
//! client that asked for the launch is used: fresh from the user's session.
//! The daemon's own environment, frozen at its start, is never used.
//! A new activation token replaces any inherited one; without one, none is
//! passed on.

pub mod bus;
pub mod session;
pub mod spawn;
pub mod wire;

#[cfg(test)]
#[path = "../../tests/support/fake_bus.rs"]
pub(crate) mod fake_bus;

use std::ffi::OsString;
use std::fmt;
use std::io::Write;
use std::path::PathBuf;
use std::time::Duration;

use self::bus::Bus;

/// What to launch.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Plan {
    /// The application ID: the desktop-file ID without `.desktop`.
    pub app_id: String,
    /// The translated Name, for the scope's description.
    pub name: String,
    /// The command line (field codes expanded, a terminal's in front);
    /// empty for an application only D-Bus can start.
    pub argv: Vec<String>,
    /// The working directory (`Path`); the home directory when absent.
    pub cwd: Option<PathBuf>,
    /// Started through D-Bus (`DBusActivatable=true`) when the bus can.
    pub dbus: bool,
    /// The desktop action to activate, if not the application itself.
    pub action: Option<String>,
}

/// The session to launch into.
#[derive(Clone, Debug, Default)]
pub struct Session {
    /// The session bus's address, if any.
    pub bus: Option<String>,
    /// The environment when the bus cannot say (the client's: fresh,
    /// never the daemon's own).
    pub env: Vec<(OsString, OsString)>,
}

/// How an application was started.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Via {
    /// `org.freedesktop.Application` on the session bus.
    Bus,
    /// A process in this transient systemd scope.
    Scope(String),
    /// A process outside any scope (no systemd user manager answered).
    Plain,
}

/// A launch that went ahead.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Launched {
    pub via: Via,
    /// The process, unless D-Bus started it.
    pub pid: Option<u32>,
    /// What went wrong on the way without stopping it (for the log).
    pub warnings: Vec<String>,
}

/// Why nothing was launched.
#[derive(Debug)]
pub enum LaunchError {
    /// The program is not found (not on the session's `PATH`).
    NotFound(String),
    /// Only D-Bus could start it, and the bus cannot.
    NotActivatable(String),
    /// The process could not be started.
    Spawn(std::io::Error),
}

impl fmt::Display for LaunchError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NotFound(program) => write!(f, "no program '{program}' answers the summons"),
            Self::NotActivatable(id) => {
                write!(f, "the bus cannot wake '{id}' and it names no Exec")
            }
            Self::Spawn(e) => write!(f, "the process would not start: {e}"),
        }
    }
}

impl std::error::Error for LaunchError {}

/// How long a launch waits for systemd to move a process into its scope.
pub const SCOPE_PATIENCE: Duration = Duration::from_millis(200);

/// The variables that carry an activation token.
const TOKEN_VARS: [&str; 2] = ["XDG_ACTIVATION_TOKEN", "DESKTOP_STARTUP_ID"];

/// Launches `plan` with the activation `token` (if the compositor gave one).
pub fn launch(
    plan: &Plan,
    token: Option<&str>,
    session: &Session,
) -> Result<Launched, LaunchError> {
    let mut warnings = Vec::new();
    let mut bus = session.bus.as_deref().and_then(|address| {
        Bus::connect(address)
            .inspect_err(|e| warnings.push(format!("no session bus: {e}")))
            .ok()
    });
    if plan.dbus {
        let reachable = bus.as_mut().map(|b| session::activatable(b, &plan.app_id));
        if let Some(Ok(true)) = reachable {
            let bus = bus.take().expect("the bus answered");
            activate_later(bus, plan, token);
            return Ok(Launched {
                via: Via::Bus,
                pid: None,
                warnings,
            });
        }
        if plan.argv.is_empty() {
            return Err(LaunchError::NotActivatable(plan.app_id.clone()));
        }
        warnings.push(format!(
            "the bus cannot activate {}; its Exec runs instead",
            plan.app_id
        ));
    }
    let mut env = bus
        .as_mut()
        .and_then(|b| {
            session::environment(b)
                .inspect_err(|e| warnings.push(format!("no user manager's environment: {e}")))
                .ok()
        })
        .unwrap_or_else(|| session.env.clone());
    env.retain(|(name, _)| !TOKEN_VARS.iter().any(|t| name == t));
    if let Some(token) = token {
        env.extend(TOKEN_VARS.map(|name| (OsString::from(name), OsString::from(token))));
    }
    let var = |name: &str| env.iter().find(|(k, _)| k == name).map(|(_, v)| v.clone());
    let program = plan.argv.first().map(String::as_str).unwrap_or_default();
    let search = crate::apps::index::Context::from_vars(|n| {
        var(n).map(|v| v.to_string_lossy().into_owned())
    });
    if !search.finds(program) {
        return Err(LaunchError::NotFound(program.to_owned()));
    }
    let home = var("HOME").map(PathBuf::from);
    let cwd = [plan.cwd.clone(), home]
        .into_iter()
        .flatten()
        .find(|d| d.is_dir())
        .unwrap_or_else(|| PathBuf::from("/"));
    let started =
        spawn::start(&plan.argv, &env, &cwd, bus.is_some()).map_err(LaunchError::Spawn)?;
    let pid = started.pid();
    let via = match bus.as_mut() {
        None => Via::Plain,
        Some(b) => {
            let unit = session::unit_name(&plan.app_id, pid);
            match session::start_scope(b, &unit, pid, &plan.name) {
                Ok(()) => {
                    if !spawn::in_unit(pid, &unit, SCOPE_PATIENCE) {
                        warnings.push(format!("{unit} did not take the process in time"));
                    }
                    Via::Scope(unit)
                }
                Err(e) => {
                    warnings.push(format!("no scope: {e}"));
                    Via::Plain
                }
            }
        }
    };
    started.release();
    Ok(Launched {
        via,
        pid: Some(pid),
        warnings,
    })
}

/// Activates a D-Bus application on a thread of its own: the application
/// answers once it has started, which may take a while.
fn activate_later(mut bus: Bus, plan: &Plan, token: Option<&str>) {
    let (id, action, token) = (
        plan.app_id.clone(),
        plan.action.clone(),
        token.map(str::to_owned),
    );
    let _ = std::thread::Builder::new()
        .name("activate".into())
        .spawn(move || {
            if let Err(e) = session::activate(&mut bus, &id, action.as_deref(), token.as_deref()) {
                // The daemon's stderr is its log; it may be gone, never panic.
                let _ = writeln!(std::io::stderr(), "logis: {id} would not wake: {e}");
            }
        });
}
