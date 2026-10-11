//! Provider sessions in the daemon (spec §4.8): what feeds each open
//! session, starting a provider when its session is shown, turning its
//! lines into window steps, and its timers:
//!
//! - its first stage is due within [`START_PATIENCE`], or the session fails;
//! - a toast shows for [`TOAST_SPAN`], and after the provider's `close` the
//!   window lingers (without the keyboard) until it is over;
//! - a provider retired without ever having been sent an `activate` or
//!   `submit` gets [`GRACE`] to exit after `cancel` and end of input, then
//!   SIGTERM, then after another [`GRACE`] SIGKILL. One that was sent one is
//!   never signalled (an action may be under way; dismissal never kills it).

use std::time::{Duration, Instant};

use rustix::process::Signal;

use super::core::{Conn, Core, Phase};
use super::resolver::Resolver;
use super::sessions::SessionId;
use crate::protocol::{Launch, Mode, Outcome};
use crate::provider::Step;
use crate::provider::dmenu::Dmenu;
use crate::provider::external::{External, Running, Said};
use crate::provider::process::Heard;

/// How long a provider may take to show its first stage.
pub const START_PATIENCE: Duration = Duration::from_secs(5);
/// How long a toast shows.
pub const TOAST_SPAN: Duration = Duration::from_secs(3);
/// How long a provider that may be ended gets after each word.
pub const GRACE: Duration = Duration::from_secs(1);

/// Starts a session's provider process; the session's id names its events.
pub type Spawn = Box<dyn FnMut(SessionId, &Launch) -> Result<Box<dyn Running>, String> + Send>;

/// What feeds an open session.
pub enum Feed {
    /// The built-in dmenu provider: the client's lines, and whether the
    /// client sent them all (app and servitor sessions have none yet).
    Dmenu { lines: Dmenu, ended: bool },
    /// An external provider, started once the session is shown.
    Waiting(Launch),
    /// A started external provider.
    Provider(Box<Live>),
}

/// A started provider in its session.
pub struct Live {
    pub external: External,
    /// The window shows its stage.
    shown: bool,
    /// When its first stage is due.
    due: Option<Instant>,
    /// Until when its toast shows.
    toast: Option<Instant>,
}

/// A retired provider that may still be ended.
struct Retiring {
    session: SessionId,
    running: Box<dyn Running>,
    since: Instant,
    termed: bool,
}

/// The daemon's providers beyond their sessions.
pub struct Providers {
    pub spawn: Spawn,
    retiring: Vec<Retiring>,
    /// A session over, whose toast still shows, and until when.
    lingering: Option<(SessionId, Instant)>,
}

impl Default for Providers {
    fn default() -> Self {
        Self {
            spawn: Box::new(|_, _| Err("this machine spirit starts no providers".to_owned())),
            retiring: Vec::new(),
            lingering: None,
        }
    }
}

impl Providers {
    /// No provider is waiting to be ended.
    pub fn is_idle(&self) -> bool {
        self.retiring.is_empty()
    }

    /// The session of `live` is over: retires its provider. Returns whether
    /// the window should linger on its toast (after the provider's `close`).
    pub fn retire(&mut self, session: SessionId, live: Live, closed: bool) -> bool {
        let now = Instant::now();
        let linger = live.toast.filter(|&until| closed && until > now);
        if let Some(until) = linger {
            self.lingering = Some((session, until));
        }
        if let Some(running) = live.external.retire() {
            self.retiring.push(Retiring {
                session,
                running,
                since: now,
                termed: false,
            });
        }
        linger.is_some()
    }
}

/// Starts the provider of shown session `id`.
pub fn start<R: Resolver>(core: &mut Core<R>, id: SessionId) {
    let Some(Conn {
        phase: Phase::Open { feed, .. },
        ..
    }) = core.conns.get_mut(&id)
    else {
        return;
    };
    let Feed::Waiting(launch) = feed else { return };
    let started = (core.providers.spawn)(id, launch).and_then(External::new);
    match started {
        Ok(external) => {
            *feed = Feed::Provider(Box::new(Live {
                external,
                shown: false,
                due: Some(Instant::now() + START_PATIENCE),
                toast: None,
            }));
        }
        Err(e) => core.fail(id, format!("the provider would not wake: {e}")),
    }
}

fn live<R>(core: &mut Core<R>, session: SessionId) -> Option<&mut Live> {
    match core.conns.get_mut(&session) {
        Some(Conn {
            phase:
                Phase::Open {
                    feed: Feed::Provider(live),
                    ..
                },
            ..
        }) => Some(live),
        _ => None,
    }
}

/// A provider process spoke or exited.
pub fn heard<R: Resolver>(core: &mut Core<R>, session: SessionId, heard: Heard) {
    let Some(live) = live(core, session) else {
        // A retired provider: it is gone now, or its words come too late.
        if matches!(heard, Heard::Exited(_)) {
            core.providers.retiring.retain(|r| r.session != session);
        }
        return;
    };
    let said = match heard {
        Heard::Exited(status) => {
            live.external.exited();
            Err(format!("it departed before its rite was done ({status})"))
        }
        Heard::Line(line) => live.external.heard(line),
    };
    match said {
        Err(e) => core.fail(session, format!("the provider spoke heresy: {e}")),
        Ok(Said::Close) => core.finish(session, Outcome::Closed),
        Ok(Said::Steps(steps)) => {
            for step in steps {
                apply(core, session, step);
            }
        }
    }
}

/// One step of a provider for its window: the first stage shows it.
fn apply<R: Resolver>(core: &mut Core<R>, session: SessionId, step: Step) {
    let Some(live) = live(core, session) else {
        return;
    };
    match step {
        Step::Stage { options, entries } if !live.shown => {
            live.shown = true;
            live.due = None;
            core.resolver
                .show(session, Mode::Provider, options, entries);
        }
        Step::Toast(notice) if !live.shown => {
            super::log(&format!(
                "a provider's toast before its window: {}",
                notice.text
            ));
        }
        _ if !live.shown => {}
        Step::Toast(notice) => {
            live.toast = Some(Instant::now() + TOAST_SPAN);
            core.resolver.step(session, Step::Toast(notice));
        }
        step => core.resolver.step(session, step),
    }
}

/// When the next timer is due.
pub fn deadline<R>(core: &Core<R>) -> Option<Instant> {
    let lives = core.conns.values().filter_map(|c| match &c.phase {
        Phase::Open {
            feed: Feed::Provider(live),
            ..
        } => Some(live),
        _ => None,
    });
    let p = &core.providers;
    lives
        .flat_map(|l| [l.due, l.toast])
        .chain([p.lingering.map(|(_, until)| until)])
        .chain(p.retiring.iter().map(|r| Some(r.next())))
        .flatten()
        .min()
}

/// Runs the timers due at `now`.
pub fn tick<R: Resolver>(core: &mut Core<R>, now: Instant) {
    let mut late = Vec::new();
    let mut spent = Vec::new();
    for (&id, c) in &mut core.conns {
        if let Phase::Open {
            feed: Feed::Provider(live),
            ..
        } = &mut c.phase
        {
            if live.due.is_some_and(|due| due <= now) {
                late.push(id);
            }
            if live.toast.take_if(|until| *until <= now).is_some() {
                spent.push(id);
            }
        }
    }
    for id in spent {
        core.resolver.untoast(id);
    }
    for id in late {
        let secs = START_PATIENCE.as_secs();
        core.fail(id, format!("the provider showed nothing within {secs} s"));
    }
    if let Some((id, _)) = core.providers.lingering.take_if(|(_, until)| *until <= now) {
        core.resolver.hide(id);
    }
    core.providers.retiring.retain_mut(|r| {
        if now >= r.since + 2 * GRACE {
            r.running.signal(Signal::KILL);
            return false;
        }
        if now >= r.since + GRACE && !r.termed {
            r.running.signal(Signal::TERM);
            r.termed = true;
        }
        true
    });
}

impl Retiring {
    fn next(&self) -> Instant {
        self.since + if self.termed { 2 * GRACE } else { GRACE }
    }
}

#[cfg(test)]
mod tests;
