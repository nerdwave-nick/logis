//! The window thread's loop: commands from the daemon, keys from the
//! compositor, and a frame only when something changed (the panel's frame
//! callback paces drawing; an idle window draws nothing and sleeps in
//! `poll`).
//!
//! An answer goes to the daemon. A dmenu answer ends its session, so the
//! window goes at once; after a provider's answer the window stays and Return
//! waits for the provider's next stage or `ready` (Escape still dismisses).
//!
//! With the test trace on (`LOGIS_TEST_TRACE=1`, see
//! [`logis::window::trace`]) it reports `show <session>`, `stage <session>`
//! (a provider's next stage), `items <total>`, `update <item>`, `remove
//! <item>`, `toast <JSON string>`, `untoast`, `ready`, `query <JSON
//! string>`, `select <item>` (or `select none`), `accept <item>`, `typed
//! <JSON string>`, `dismiss`, `linger <session>` and `hide <session>` (the
//! daemon took it away: answered, cancelled or replaced), beside the
//! window's own focus, key and commit events.

use std::cell::RefCell;
use std::os::fd::BorrowedFd;
use std::rc::Rc;
use std::sync::mpsc::{Receiver, Sender, TryRecvError};

use logis::input::Action;
use logis::list::{Choice, Entry, ListView, Notice, Reaction, scene};
use logis::window::trace::emit;
use logis::window::{WindowQueue, Windows};
use wayland_client::QueueHandle;

use super::Command;
use crate::daemon::core::Event;
use crate::daemon::sessions::SessionId;
use crate::protocol::Mode;
use crate::provider::{Answer, Step};

/// The session on screen: its list and its toast, as drawn.
struct Shown {
    session: SessionId,
    view: Rc<RefCell<ListView>>,
    toast: Rc<RefCell<Option<Notice>>>,
    /// How many items the stage has had.
    count: usize,
    /// A dmenu answer ends the session: the window goes at once.
    ends_on_answer: bool,
    /// An answer went out; Return waits for the provider.
    waiting: bool,
    /// The session is over; its toast is left to read.
    lingering: bool,
}

/// Runs until the daemon drops its resolver (`None`) or the compositor
/// connection is lost (`Some(reason)`).
pub fn run(
    mut windows: Windows,
    mut queue: WindowQueue,
    inbox: &Receiver<Command>,
    wake: BorrowedFd<'_>,
    events: &Sender<Event>,
) -> Option<String> {
    let qh = queue.handle();
    let mut shown: Option<Shown> = None;
    loop {
        loop {
            match inbox.try_recv() {
                Ok(command) => apply(command, &mut windows, &qh, &mut shown, events),
                Err(TryRecvError::Empty) => break,
                Err(TryRecvError::Disconnected) => return None,
            }
        }
        react(&mut windows, &mut shown, events);
        if shown.is_some() && !windows.is_visible() {
            // The compositor closed the surface (its output went away).
            let gone = shown.take().filter(|s| !s.lingering);
            if let Some(s) = gone {
                answer(events, s.session, Answer::Dismissed);
            }
        }
        windows.draw_pending(&qh);
        if let Err(e) = windows.dispatch_or_wake(&mut queue, Some(wake)) {
            return Some(e.to_string());
        }
        let mut count = [0; 8];
        let _ = rustix::io::read(wake, &mut count);
    }
}

fn apply(
    command: Command,
    windows: &mut Windows,
    qh: &QueueHandle<Windows>,
    shown: &mut Option<Shown>,
    events: &Sender<Event>,
) {
    let mine = |session: SessionId, shown: &mut Option<Shown>| {
        shown.as_ref().is_some_and(|s| s.session == session)
    };
    match command {
        Command::Show {
            session,
            mode,
            options,
            entries,
        } => {
            let size = scene::size(&options);
            let view = Rc::new(RefCell::new(ListView::new(options)));
            let count = entries.len();
            view.borrow_mut().push(entries);
            let toast = Rc::new(RefCell::new(None));
            let (drawn, said) = (Rc::clone(&view), Rc::clone(&toast));
            let draw = move |logical| {
                let mut look = drawn.borrow_mut().look();
                look.toast = said.borrow().clone();
                scene::scene(&look, logical)
            };
            *shown = None;
            match windows.show(qh, size, Box::new(draw)) {
                Ok(()) => {
                    emit(format_args!("show {session}"));
                    emit(format_args!("items {count}"));
                    *shown = Some(Shown {
                        session,
                        view,
                        toast,
                        count,
                        ends_on_answer: mode == Mode::Dmenu,
                        waiting: false,
                        lingering: false,
                    });
                }
                Err(e) => {
                    let message = e.to_string();
                    let _ = events.send(Event::Failed { session, message });
                }
            }
        }
        Command::Step { session, step } => {
            let Some(s) = shown
                .as_mut()
                .filter(|s| s.session == session && !s.lingering)
            else {
                return;
            };
            if s.step(step) {
                windows.request_redraw();
            }
        }
        Command::Hide { session } => {
            if mine(session, shown) {
                *shown = None;
                windows.hide();
                emit(format_args!("hide {session}"));
            }
        }
        Command::Linger { session } => {
            if let Some(s) = shown.as_mut().filter(|s| s.session == session) {
                s.lingering = true;
                windows.release_keyboard();
                emit(format_args!("linger {session}"));
            }
        }
        Command::Untoast { session } => {
            if let Some(s) = shown.as_mut().filter(|s| s.session == session) {
                s.toast.replace(None);
                windows.request_redraw();
                emit(format_args!("untoast"));
            }
        }
        Command::FocusLoss(policy) => windows.set_focus_loss(policy),
    }
}

impl Shown {
    /// Applies a provider's step; whether the list must be drawn again.
    fn step(&mut self, step: Step) -> bool {
        let mut view = self.view.borrow_mut();
        match step {
            Step::Stage { options, entries } => {
                *view = ListView::new(options);
                self.count = entries.len();
                view.push(entries);
                self.waiting = false;
                emit(format_args!("stage {}", self.session));
                emit(format_args!("items {}", self.count));
                true
            }
            Step::Append(entries) => {
                self.count += entries.len();
                let changed = view.push(entries);
                emit(format_args!("items {}", self.count));
                changed
            }
            Step::Update(changed) => {
                changed
                    .into_iter()
                    .fold(false, |any, (i, entry): (u32, Entry)| {
                        emit(format_args!("update {i}"));
                        view.update(i, entry) || any
                    })
            }
            Step::Remove(gone) => gone.into_iter().fold(false, |any, i| {
                emit(format_args!("remove {i}"));
                view.remove(i) || any
            }),
            Step::Preselect(i) => view.preselect(i),
            Step::Toast(notice) => {
                let quoted = serde_json::to_string(&notice.text).unwrap_or_default();
                emit(format_args!("toast {quoted}"));
                self.toast.replace(Some(notice));
                true
            }
            Step::Ready => {
                self.waiting = false;
                emit(format_args!("ready"));
                false
            }
        }
    }
}

/// Applies the keys, pointer and pastes since the last call to the shown
/// list. A point means the row drawn under it, on the panel's current size.
/// A lingering window (its session over) takes none of them.
fn react(windows: &mut Windows, shown: &mut Option<Shown>, events: &Sender<Event>) {
    for action in windows.take_actions() {
        let Some(s) = shown.as_mut().filter(|s| !s.lingering) else {
            continue;
        };
        let reaction = match (&action, windows.size()) {
            (Action::Point(point), Some(size)) => s.view.borrow_mut().point(point, size),
            _ => s.view.borrow_mut().apply(&action),
        };
        let given = match reaction {
            Reaction::Unchanged => continue,
            Reaction::Changed => {
                let view = s.view.borrow();
                let quoted = serde_json::to_string(view.query()).unwrap_or_default();
                emit(format_args!("query {quoted}"));
                match view.selected() {
                    Some(item) => emit(format_args!("select {item}")),
                    None => emit(format_args!("select none")),
                }
                windows.request_redraw();
                continue;
            }
            Reaction::Accept(_) if s.waiting => continue,
            Reaction::Dismiss => {
                emit(format_args!("dismiss"));
                Answer::Dismissed
            }
            Reaction::Accept(Choice::Item(index)) => {
                emit(format_args!("accept {index}"));
                Answer::Item(index)
            }
            Reaction::Accept(Choice::Typed(text)) => {
                let quoted = serde_json::to_string(&text).unwrap_or_default();
                emit(format_args!("typed {quoted}"));
                Answer::Typed(text)
            }
        };
        let session = s.session;
        if s.ends_on_answer || given == Answer::Dismissed {
            // The session ends: the window goes at once, the daemon answers
            // the client and shows the next session, if one waits.
            *shown = None;
            windows.hide();
        } else {
            s.waiting = true;
        }
        answer(events, session, given);
    }
}

fn answer(events: &Sender<Event>, session: SessionId, answer: Answer) {
    let _ = events.send(Event::Answered { session, answer });
}
