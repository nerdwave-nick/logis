//! App sessions in the daemon (spec §3 stories 5–7, §4.7): the index the
//! daemon keeps resident (rescanned whenever the applications directories
//! change), frecency, and the launch of the chosen application.
//!
//! A launch first asks the window for an xdg-activation token (the
//! compositor issues it for the focused window and the key that chose), and
//! launches once it comes, or after [`TOKEN_PATIENCE`] without one. The
//! session then closes (the client exits 0) and the launch is noted for
//! frecency; a launch that fails is shown as an error toast instead and the
//! stage takes Return again.

use std::ffi::OsString;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::{Duration, Instant};

use logis::apps::frecency::{self, Frecency};
use logis::apps::index::Index;
use logis::launch::{self, Plan, Session};

use super::core::{Conn, Core, Phase};
use super::providing::Feed;
use super::resolver::Resolver;
use super::sessions::SessionId;
use crate::protocol::{Bytes, Mode, Outcome};
use crate::provider::apps::{self, Apps, Said};
use crate::provider::{Answer, Step};

/// How long a launch waits for the compositor's activation token.
pub const TOKEN_PATIENCE: Duration = Duration::from_millis(500);

/// The daemon's apps: the index, frecency and how to launch.
pub struct AppsState {
    pub index: Arc<Index>,
    /// Whether the index has been read at all yet.
    pub indexed: bool,
    pub frecency: Frecency,
    /// Where frecency is kept; `None`: not kept.
    pub frecency_path: Option<PathBuf>,
    /// The config's `terminal`; `None`: `$TERMINAL -e`.
    pub terminal: Option<Vec<String>>,
    /// The session bus's address.
    pub bus: Option<String>,
}

impl Default for AppsState {
    fn default() -> Self {
        Self {
            index: Arc::new(Index::default()),
            indexed: false,
            frecency: Frecency::default(),
            frecency_path: None,
            terminal: None,
            bus: None,
        }
    }
}

/// One app session.
pub struct AppsFeed {
    /// The client's environment.
    env: Vec<(OsString, OsString)>,
    /// Its stages, once shown with an index.
    apps: Option<Apps>,
    /// A launch waiting for its activation token.
    awaiting: Option<Awaiting>,
}

struct Awaiting {
    plan: Plan,
    id: String,
    due: Instant,
}

impl AppsFeed {
    pub fn new(env: Vec<Bytes>) -> Self {
        let env = launch::session::environment_from_bytes(env.into_iter().map(|b| b.0));
        Self {
            env,
            apps: None,
            awaiting: None,
        }
    }
}

fn feed<R>(core: &mut Core<R>, session: SessionId) -> Option<&mut AppsFeed> {
    match core.conns.get_mut(&session) {
        Some(Conn {
            phase:
                Phase::Open {
                    feed: Feed::Apps(feed),
                    ..
                },
            ..
        }) => Some(feed),
        _ => None,
    }
}

/// The first stage, or a placeholder while the index is first read.
fn first_stage(state: &AppsState) -> (Option<Apps>, Step) {
    if !state.indexed {
        let options = logis::list::Options {
            placeholder: "the census of applications is taken…".into(),
            ..apps::options()
        };
        let step = Step::Stage {
            options,
            entries: Vec::new(),
        };
        return (None, step);
    }
    let (apps, step) = Apps::open(&state.index, &state.frecency, frecency::now());
    (Some(apps), step)
}

/// Session `id` became visible: its first stage.
pub fn show<R: Resolver>(core: &mut Core<R>, id: SessionId) {
    let (apps, step) = first_stage(&core.apps);
    let Some(f) = feed(core, id) else { return };
    f.apps = apps;
    if let Step::Stage { options, entries } = step {
        core.resolver.show(id, Mode::Apps, options, entries);
    }
}

/// A new index: kept, and shown by a visible session still waiting for it.
pub fn indexed<R: Resolver>(core: &mut Core<R>, index: Arc<Index>) {
    core.apps.index = index;
    core.apps.indexed = true;
    let waiting: Vec<SessionId> = core
        .conns
        .iter()
        .filter_map(|(&id, c)| match &c.phase {
            Phase::Open {
                feed: Feed::Apps(f),
                ..
            } if f.apps.is_none() => Some(id),
            _ => None,
        })
        .collect();
    let visible = core.sessions.visible();
    for id in waiting.into_iter().filter(|&id| visible == Some(id)) {
        let (apps, step) = first_stage(&core.apps);
        if let Some(f) = feed(core, id) {
            f.apps = apps;
            core.resolver.step(id, step);
        }
    }
}

/// The user answered app session `session` (not a dismissal).
pub fn answered<R: Resolver>(core: &mut Core<R>, session: SessionId, answer: Answer) {
    let index = Arc::clone(&core.apps.index);
    let configured = core.apps.terminal.clone();
    let Some(f) = feed(core, session) else { return };
    let terminal = terminal(configured, &f.env);
    if f.awaiting.is_some() {
        return;
    }
    let Some(apps) = f.apps.as_mut() else { return };
    match apps.answer(answer, &index, &terminal) {
        Said::Steps(steps) => {
            for step in steps {
                core.resolver.step(session, step);
            }
        }
        Said::Launch { plan, id } => {
            let app_id = plan.app_id.clone();
            f.awaiting = Some(Awaiting {
                plan,
                id,
                due: Instant::now() + TOKEN_PATIENCE,
            });
            core.resolver.activation(session, &app_id);
        }
    }
}

/// The compositor's activation token for `session`'s launch (or none).
pub fn activated<R: Resolver>(core: &mut Core<R>, session: SessionId, token: Option<String>) {
    let Some(f) = feed(core, session) else { return };
    let Some(awaiting) = f.awaiting.take() else {
        return;
    };
    let env = f.env.clone();
    let bus = core.apps.bus.clone();
    let launched = launch::launch(&awaiting.plan, token.as_deref(), &Session { bus, env });
    match launched {
        Ok(launched) => {
            for warning in &launched.warnings {
                super::log(&format!("{}: {warning}", awaiting.plan.app_id));
            }
            note(&mut core.apps, &awaiting.id);
            core.finish(session, Outcome::Closed);
        }
        Err(e) => {
            let message = format!("{} cannot be summoned: {e}", awaiting.plan.name);
            super::log(&message);
            for step in apps::failed(message) {
                core.resolver.step(session, step);
            }
        }
    }
}

/// Notes a launch for frecency and keeps it (on a thread of its own: the
/// file is synced to disk).
fn note(state: &mut AppsState, id: &str) {
    state.frecency.record(id, frecency::now());
    let Some(path) = state.frecency_path.clone() else {
        return;
    };
    let snapshot = state.frecency.clone();
    let _ = std::thread::Builder::new()
        .name("frecency".into())
        .spawn(move || {
            if let Err(e) = snapshot.save(&path) {
                super::log(&format!("frecency is not kept in {}: {e}", path.display()));
            }
        });
}

/// The terminal prefix: the config's, or `$TERMINAL -e` from the caller.
fn terminal(configured: Option<Vec<String>>, env: &[(OsString, OsString)]) -> Vec<String> {
    if let Some(terminal) = configured {
        return terminal;
    }
    env.iter()
        .find(|(k, _)| k == "TERMINAL")
        .map(|(_, v)| v.to_string_lossy().into_owned())
        .filter(|t| !t.is_empty())
        .map(|t| vec![t, "-e".to_owned()])
        .unwrap_or_default()
}

/// When the next launch stops waiting for its token.
pub fn deadline<R>(core: &Core<R>) -> Option<Instant> {
    core.conns
        .values()
        .filter_map(|c| match &c.phase {
            Phase::Open {
                feed: Feed::Apps(f),
                ..
            } => f.awaiting.as_ref().map(|a| a.due),
            _ => None,
        })
        .min()
}

/// Launches without a token what waited too long for one.
pub fn tick<R: Resolver>(core: &mut Core<R>, now: Instant) {
    let late: Vec<SessionId> = core
        .conns
        .iter()
        .filter_map(|(&id, c)| match &c.phase {
            Phase::Open {
                feed: Feed::Apps(f),
                ..
            } if f.awaiting.as_ref().is_some_and(|a| a.due <= now) => Some(id),
            _ => None,
        })
        .collect();
    for id in late {
        super::log("no activation token came; the app is launched without one");
        activated(core, id, None);
    }
}

#[cfg(test)]
mod tests;
