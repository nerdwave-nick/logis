//! The window thread's loop: commands from the daemon, keys from the
//! compositor, and a frame only when something changed (the panel's frame
//! callback paces drawing; an idle window draws nothing and sleeps in
//! `poll`).
//!
//! With the test trace on (`LOGIS_TEST_TRACE=1`, see
//! [`logis::window::trace`]) it reports `show <session>`, `items <total>`,
//! `query <JSON string>`, `select <item>` (or `select none`), `accept
//! <item>`, `typed <JSON string>`, `dismiss` and `hide <session>` (the
//! daemon took it away: cancelled or replaced), beside the window's own
//! focus, key and commit events.

use std::cell::RefCell;
use std::os::fd::BorrowedFd;
use std::rc::Rc;
use std::sync::mpsc::{Receiver, Sender, TryRecvError};

use logis::list::{Choice, ListView, Reaction, scene};
use logis::motion::Glide;
use logis::window::trace::emit;
use logis::window::{Backdrop, Drawing, Frame, SceneFn, WindowQueue, Windows};

use super::Command;
use crate::daemon::core::Event;
use crate::daemon::sessions::SessionId;
use crate::protocol::{Item, Outcome};

/// The session on screen: its list and the items as the client sent them.
struct Shown {
    session: SessionId,
    view: Rc<RefCell<ListView>>,
    items: Vec<Item>,
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
            resolve(&mut windows, &mut shown, events, Outcome::Dismissed);
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
    qh: &wayland_client::QueueHandle<Windows>,
    shown: &mut Option<Shown>,
    events: &Sender<Event>,
) {
    match command {
        Command::Show {
            session,
            options,
            items,
        } => {
            let size = scene::size(&options);
            let view = Rc::new(RefCell::new(ListView::new(options)));
            view.borrow_mut().push(items.iter().map(entry));
            let draw = drawing(Rc::clone(&view), Glide::new(windows.motion().selection));
            *shown = None;
            match windows.show(qh, size, draw) {
                Ok(()) => {
                    emit(format_args!("show {session}"));
                    emit(format_args!("items {}", items.len()));
                    *shown = Some(Shown {
                        session,
                        view,
                        items,
                    });
                }
                Err(e) => {
                    let message = e.to_string();
                    let _ = events.send(Event::Failed { session, message });
                }
            }
        }
        Command::Append { session, items } => {
            let Some(s) = shown.as_mut().filter(|s| s.session == session) else {
                return;
            };
            if s.view.borrow_mut().push(items.iter().map(entry)) {
                windows.request_redraw();
            }
            s.items.extend(items);
            emit(format_args!("items {}", s.items.len()));
        }
        Command::Hide { session } => {
            if shown.as_ref().is_some_and(|s| s.session == session) {
                *shown = None;
                windows.close();
                emit(format_args!("hide {session}"));
            }
        }
        Command::FocusLoss(policy) => windows.set_focus_loss(policy),
        Command::Blur(blur) => windows.set_blur(blur, qh),
    }
}

/// Draws `view` each frame: the selection band glides to the selected row,
/// and the body is blurred behind.
fn drawing(view: Rc<RefCell<ListView>>, mut glide: Glide) -> SceneFn {
    Box::new(move |frame: &Frame| {
        let look = view.borrow_mut().look();
        let target = scene::band_top(&look, frame.logical);
        let (band, moving) = glide.toward(target, frame.at, frame.interval);
        let (rect, radius) = scene::body(frame.logical);
        Drawing {
            scene: scene::scene_at(&look, frame.logical, band),
            backdrop: Some(Backdrop { rect, radius }),
            moving,
        }
    })
}

/// An item as the list takes it: its shown text, and its match text when
/// that differs.
fn entry(item: &Item) -> (String, Option<String>) {
    (item.text.clone(), item.match_text.clone())
}

/// Applies the keys since the last call to the shown list.
fn react(windows: &mut Windows, shown: &mut Option<Shown>, events: &Sender<Event>) {
    for action in windows.take_actions() {
        let Some(s) = shown.as_ref() else { return };
        let reaction = s.view.borrow_mut().apply(&action);
        let outcome = match reaction {
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
            Reaction::Dismiss => {
                emit(format_args!("dismiss"));
                Outcome::Dismissed
            }
            Reaction::Accept(Choice::Item(index)) => {
                emit(format_args!("accept {index}"));
                let item = s.items[index as usize].clone();
                Outcome::Selected {
                    index: u64::from(index),
                    item,
                }
            }
            Reaction::Accept(Choice::Typed(text)) => {
                let quoted = serde_json::to_string(&text).unwrap_or_default();
                emit(format_args!("typed {quoted}"));
                Outcome::Typed { text }
            }
        };
        resolve(windows, shown, events, outcome);
        return;
    }
}

/// Ends the shown session with `outcome`: the daemon answers the client at
/// once (and shows the next session, if one waits), then the window closes;
/// its close transition is only for the eye and delays nothing.
fn resolve(
    windows: &mut Windows,
    shown: &mut Option<Shown>,
    events: &Sender<Event>,
    outcome: Outcome,
) {
    if let Some(s) = shown.take() {
        let _ = events.send(Event::Resolved {
            session: s.session,
            outcome,
        });
        windows.close();
    }
}
