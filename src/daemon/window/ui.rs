//! The window thread's loop: commands from the daemon, keys from the
//! compositor, and a frame only when something changed (the panel's frame
//! callback paces drawing; an idle window draws nothing and sleeps in
//! `poll`).
//!
//! An answer goes to the daemon. A dmenu answer ends its session, so the
//! window closes at once (its close transition is only for the eye: the
//! answer is sent first); after a provider's answer the window stays and
//! Return waits for the provider's next stage or `ready` (Escape still
//! dismisses). A provider's next stage is not a close and an open: the
//! window stays where it is and only its list changes, the selection band
//! taking the new list's row at once rather than gliding there.
//!
//! With the test trace on (`LOGIS_TEST_TRACE=1`, see
//! [`logis::window::trace`]) it reports `show <session>`, `stage <session>`
//! (a provider's next stage), `items <total>`, `update <item>`, `remove
//! <item>`, `toast <JSON string>`, `untoast`, `ready`, `query <JSON
//! string>`, `select <item>` (or `select none`), `accept <item>`, `typed
//! <JSON string>`, `dismiss`, `linger <session>` and `hide <session>` (the
//! daemon took it away: answered, cancelled or replaced), beside the
//! window's own focus, key and commit events. A theme it puts on is
//! `theme <name>`.

use std::cell::RefCell;
use std::os::fd::BorrowedFd;
use std::rc::Rc;
use std::sync::mpsc::{Receiver, Sender, TryRecvError};

use logis::input::Action;
use logis::list::scene;
use logis::list::{Choice, Entry, ListView, Notice, Reaction};
use logis::motion::Glide;
use logis::theme::Theme;
use logis::window::trace::emit;
use logis::window::{WindowQueue, Windows};
use wayland_client::QueueHandle;

use super::Command;
use super::wear::Wear;
use crate::daemon::core::Event;
use crate::daemon::sessions::SessionId;
use crate::protocol::Mode;
use crate::provider::{Answer, Step};

/// The session on screen: its list and its toast, as drawn.
struct Shown {
    session: SessionId,
    /// The first stage's lines and width as asked (`None`: the theme's):
    /// the window's size, which later stages keep.
    asked: (Option<u32>, Option<u32>),
    /// The most rows the list shows (what the window was sized for).
    rows: u32,
    view: Rc<RefCell<ListView>>,
    toast: Rc<RefCell<Option<Notice>>>,
    /// The selection band's movement, reset by a new stage.
    glide: Rc<RefCell<Glide>>,
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
    mut wear: Wear,
) -> Option<String> {
    let qh = queue.handle();
    let mut shown: Option<Shown> = None;
    loop {
        loop {
            match inbox.try_recv() {
                Ok(command) => apply(command, &mut windows, &qh, &mut shown, events, &mut wear),
                Err(TryRecvError::Empty) => break,
                Err(TryRecvError::Disconnected) => return None,
            }
        }
        wear.expire(&mut windows, shown.as_ref().map(|s| &*s.toast));
        react(&mut windows, &mut shown, events, &wear.theme());
        for (session, token) in windows.take_tokens() {
            let _ = events.send(Event::Activation { session, token });
        }
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
    wear: &mut Wear,
) {
    let mine = |session: SessionId, shown: &mut Option<Shown>| {
        shown.as_ref().is_some_and(|s| s.session == session)
    };
    match command {
        Command::Show {
            session,
            mode,
            mut options,
            entries,
        } => {
            let theme = wear.theme();
            let asked = (options.lines, options.width);
            let (size, rows) = scene::fit(&options, &theme);
            options.lines = Some(rows);
            let view = Rc::new(RefCell::new(ListView::new(options)));
            let count = entries.len();
            view.borrow_mut().push(entries);
            let toast = Rc::new(RefCell::new(None));
            let glide = Rc::new(RefCell::new(Glide::new(windows.motion().selection)));
            let draw = wear.drawing(Rc::clone(&view), Rc::clone(&toast), Rc::clone(&glide));
            *shown = None;
            match windows.show(qh, size, draw) {
                Ok(()) => {
                    emit(format_args!("show {session}"));
                    emit(format_args!("items {count}"));
                    *shown = Some(Shown {
                        session,
                        asked,
                        rows,
                        view,
                        toast,
                        glide,
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
            if s.step(step, &wear.theme()) {
                windows.request_redraw();
            }
        }
        Command::Hide { session } => {
            if mine(session, shown) {
                *shown = None;
                windows.close();
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
        Command::IconTheme(name) => windows.set_icon_theme(&name),
        Command::Activation { session, app_id } => {
            if mine(session, shown) {
                windows.request_token(qh, session, &app_id);
            } else {
                let _ = events.send(Event::Activation {
                    session,
                    token: None,
                });
            }
        }
        Command::Blur(blur) => wear.blur(windows, qh, blur),
        Command::Theme(theme) => {
            wear.put_on(windows, qh, theme);
            if let Some(s) = shown.as_mut() {
                s.rows = wear.refit(windows, s.asked, &s.view);
            }
        }
        Command::Notice(notice) => {
            if let Some(s) = shown.as_ref().filter(|s| !s.lingering) {
                wear.notify(windows, &s.toast, notice);
            }
        }
    }
}

impl Shown {
    /// Applies a provider's step (`theme` sizes a new stage's list);
    /// whether the list must be drawn again.
    fn step(&mut self, step: Step, theme: &Theme) -> bool {
        let mut view = self.view.borrow_mut();
        match step {
            Step::Stage {
                mut options,
                entries,
            } => {
                // The window keeps its size: the list scrolls within it.
                (options.lines, options.width) = self.asked;
                let (_, rows) = scene::fit(&options, theme);
                options.lines = Some(rows);
                *view = ListView::new(options);
                // A new list: the band takes its row at once.
                self.glide.borrow_mut().reset();
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
            Step::Toast { notice, .. } => {
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
fn react(windows: &mut Windows, shown: &mut Option<Shown>, events: &Sender<Event>, theme: &Theme) {
    for action in windows.take_actions() {
        let Some(s) = shown.as_mut().filter(|s| !s.lingering) else {
            continue;
        };
        let reaction = match (&action, windows.size()) {
            (Action::Point(point), Some(size)) => {
                let grid = scene::grid(size, s.rows, theme);
                s.view.borrow_mut().point(point, &grid)
            }
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
            Reaction::Accept(Choice::Descend(index)) => {
                emit(format_args!("descend {index}"));
                Answer::Descend(index)
            }
            Reaction::Accept(Choice::Typed(text)) => {
                let quoted = serde_json::to_string(&text).unwrap_or_default();
                emit(format_args!("typed {quoted}"));
                Answer::Typed(text)
            }
        };
        let session = s.session;
        if s.ends_on_answer || given == Answer::Dismissed {
            // The session ends: the daemon answers the client at once (and
            // shows the next session, if one waits), then the window closes;
            // its close transition delays nothing.
            *shown = None;
            answer(events, session, given);
            windows.close();
        } else {
            s.waiting = true;
            answer(events, session, given);
        }
    }
}

fn answer(events: &Sender<Event>, session: SessionId, answer: Answer) {
    let _ = events.send(Event::Answered { session, answer });
}

#[cfg(test)]
mod tests {
    use super::*;
    use logis::list::Options;

    fn shown(options: Options) -> Shown {
        Shown {
            session: 1,
            asked: (options.lines, options.width),
            rows: options.rows(),
            view: Rc::new(RefCell::new(ListView::new(options))),
            toast: Rc::new(RefCell::new(None)),
            glide: Rc::new(RefCell::new(Glide::new(logis::motion::Curve::INSTANT))),
            count: 0,
            ends_on_answer: false,
            waiting: true,
            lingering: false,
        }
    }

    #[test]
    fn a_later_stage_keeps_the_rows_the_window_has_room_for() {
        let first = Options {
            lines: Some(6),
            width: Some(44),
            ..Options::default()
        };
        let mut s = shown(first);
        let next = Options {
            lines: Some(15),
            prompt: "aspect: ".into(),
            ..Options::default()
        };
        let entries = vec![Entry::from("on".to_owned())];
        let theme = Theme::bundled_default();
        let stage = Step::Stage {
            options: next,
            entries,
        };
        assert!(s.step(stage, &theme));
        let look = s.view.borrow_mut().look();
        assert_eq!((look.lines, look.prompt.as_str()), (6, "aspect: "));
        assert!(!s.waiting, "a new stage takes Return again");
    }
}
