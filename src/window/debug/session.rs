//! The debug panel as a session: the keyboard's [`Action`]s move its
//! selection, edit its query, accept or dismiss it. With the test trace on
//! it reports `select <row>`, `query <text>` (a JSON string), `accept <row>`
//! and `dismiss`, so harnesses can check what each key did.

use std::cell::RefCell;
use std::io::Write;
use std::rc::Rc;

use super::{DEBUG_SIZE, ROWS, SELECTABLE, debug_scene_at};
use crate::input::{Action, FocusLoss, Query, Step};
use crate::render::BackendChoice;
use crate::window::trace::emit;
use crate::window::{Drawing, Frame, WindowError, Windows};

/// The debug panel's state: the selected row and the query.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct DebugSession {
    row: usize,
    query: Query,
}

/// What an action did to a [`DebugSession`].
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Outcome {
    /// Nothing changed.
    Unchanged,
    /// The selection moved.
    Selected,
    /// The query (text or cursor) changed.
    Edited,
    /// The session ended without a choice.
    Dismissed,
    /// The selected row was chosen.
    Accepted,
}

impl DebugSession {
    /// The selected row.
    pub fn row(&self) -> usize {
        self.row
    }

    /// The query typed so far.
    pub fn query(&self) -> &Query {
        &self.query
    }

    /// Applies `action`.
    pub fn apply(&mut self, action: &Action) -> Outcome {
        match action {
            Action::Dismiss => Outcome::Dismissed,
            Action::Accept | Action::Descend => Outcome::Accepted,
            Action::Select(Step::Previous) if self.row > 0 => {
                self.row -= 1;
                Outcome::Selected
            }
            Action::Select(Step::Next) if self.row + 1 < SELECTABLE => {
                self.row += 1;
                Outcome::Selected
            }
            Action::Select(_) => Outcome::Unchanged,
            Action::Edit(edit) if self.query.apply(edit) => Outcome::Edited,
            Action::Edit(_) => Outcome::Unchanged,
            // The window turns pastes into edits; the debug panel has no
            // pointer rows.
            Action::Paste(_) | Action::Point(_) => Outcome::Unchanged,
        }
    }
}

/// `logis daemon --debug-panel`: wakes the GPU, shows the panel and keeps
/// it, re-rendering only on configure, scale, selection or query changes,
/// until it is dismissed (Escape, Ctrl+G, Ctrl+[, or focus loss unless
/// `focus_loss` keeps it), accepted (Return: the selected row is printed),
/// closed by the compositor, or the process ends.
pub fn run_debug_panel(choice: BackendChoice, focus_loss: FocusLoss) -> Result<(), WindowError> {
    let (mut windows, mut queue) = Windows::connect(choice)?;
    let info = windows.gpu().info();
    eprintln!(
        "logis: the machine spirit wakes on {} ({:?}, {})",
        info.name, info.backend, info.driver
    );
    windows.set_verbose(true);
    windows.set_focus_loss(focus_loss);
    let session = Rc::new(RefCell::new(DebugSession::default()));
    let shown = Rc::clone(&session);
    let scene = move |frame: &Frame, _: &mut dyn crate::icons::IconSource| {
        let s = shown.borrow();
        Drawing::still(debug_scene_at(frame.logical, s.row(), s.query()))
    };
    windows.show(&queue.handle(), DEBUG_SIZE, Box::new(scene))?;
    let qh = queue.handle();
    while windows.is_visible() {
        windows.dispatch(&mut queue)?;
        for action in windows.take_actions() {
            let mut s = session.borrow_mut();
            match s.apply(&action) {
                Outcome::Unchanged => {}
                Outcome::Selected => {
                    emit(format_args!("select {}", s.row()));
                    windows.request_redraw();
                }
                Outcome::Edited => {
                    let quoted = serde_json::to_string(s.query().text()).unwrap_or_default();
                    emit(format_args!("query {quoted}"));
                    windows.request_redraw();
                }
                Outcome::Dismissed => {
                    emit(format_args!("dismiss"));
                    windows.hide();
                    break;
                }
                Outcome::Accepted => {
                    emit(format_args!("accept {}", s.row()));
                    // A closed stdout must not turn the choice into a panic.
                    let _ = writeln!(std::io::stdout(), "{}", ROWS[s.row()]);
                    windows.hide();
                    break;
                }
            }
        }
        windows.draw_pending(&qh);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::input::Edit;

    #[test]
    fn next_and_previous_move_the_selection_within_the_rows() {
        let mut s = DebugSession::default();
        let next = Action::Select(Step::Next);
        let previous = Action::Select(Step::Previous);
        assert_eq!(s.apply(&previous), Outcome::Unchanged);
        assert_eq!(s.apply(&next), Outcome::Selected);
        assert_eq!(s.row(), 1);
        for _ in 0..SELECTABLE {
            s.apply(&next);
        }
        assert_eq!(s.row(), SELECTABLE - 1);
        assert_eq!(s.apply(&next), Outcome::Unchanged);
        assert_eq!(s.apply(&previous), Outcome::Selected);
    }

    #[test]
    fn edits_change_the_query_and_report_only_real_changes() {
        let mut s = DebugSession::default();
        let insert = Action::Edit(Edit::Insert("ö".into()));
        assert_eq!(s.apply(&insert), Outcome::Edited);
        assert_eq!(s.query().text(), "ö");
        assert_eq!(s.apply(&Action::Edit(Edit::Clear)), Outcome::Edited);
        assert_eq!(s.apply(&Action::Edit(Edit::Clear)), Outcome::Unchanged);
    }

    #[test]
    fn dismiss_and_accept_end_the_session() {
        let mut s = DebugSession::default();
        assert_eq!(s.apply(&Action::Dismiss), Outcome::Dismissed);
        assert_eq!(s.apply(&Action::Accept), Outcome::Accepted);
    }
}
