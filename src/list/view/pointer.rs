//! The pointer over a session's list (spec §3 story 16): the row under the
//! pointer is selected (hover highlights it), the primary button pressed
//! and released on the same item accepts it, and scrolling moves the rows
//! in view, the selection staying on the row under the pointer.
//!
//! Scrolling: a wheel notch moves [`ROWS_PER_NOTCH`] rows, a high-resolution
//! wheel's fractions add up to that; a finger or a continuous device moves
//! one row per row pitch ([`Grid::pitch`]), so the rows follow the finger;
//! remainders carry over to the next frame, and a lifted finger drops its
//! remainder.

use super::{Choice, ListView, Reaction};
use crate::input::{Point, Scroll};
use crate::list::scene::Grid;

/// How many rows one wheel notch scrolls (the common desktop default of
/// three lines).
pub const ROWS_PER_NOTCH: i32 = 3;

/// What the pointer left in the list: the item a press began on, and the
/// scroll not yet worth a whole row.
#[derive(Clone, Debug, Default)]
pub(super) struct Pointing {
    pressed: Option<u32>,
    wheel: i32,
    pixels: f64,
}

impl ListView {
    /// What `point` does to the list whose rows lie on `grid`.
    pub fn point(&mut self, point: &Point, grid: &Grid) -> Reaction {
        let item = |v: &Self, x, y| {
            grid.row_at((x, y))
                .and_then(|row| v.filter.matches().get(v.top + row))
                .map(|m| m.index)
        };
        match *point {
            Point::Moved(x, y) => self.hover(item(self, x, y)),
            Point::Left => {
                self.pointing.pressed = None;
                Reaction::Unchanged
            }
            Point::Pressed(x, y) => {
                let under = item(self, x, y);
                self.pointing.pressed = under;
                self.hover(under)
            }
            Point::Released(x, y) => match (self.pointing.pressed.take(), item(self, x, y)) {
                // A pending item is never accepted, by key or by click.
                (Some(down), Some(up)) if down == up && !self.pending(up) => {
                    Reaction::Accept(Choice::Item(up))
                }
                _ => Reaction::Unchanged,
            },
            Point::Scrolled(scroll, x, y) => {
                let rows = self.pointing.rows(scroll, grid.pitch);
                let scrolled = self.scroll(rows);
                let hovered = self.hover(item(self, x, y));
                if scrolled == Reaction::Changed {
                    scrolled
                } else {
                    hovered
                }
            }
        }
    }

    /// Selects `item` (under the pointer) if it is not already.
    fn hover(&mut self, item: Option<u32>) -> Reaction {
        match item {
            Some(item) if self.selected() != Some(item) => {
                self.preselect = None;
                self.select_item(item);
                self.follow = Some(item);
                Reaction::Changed
            }
            _ => Reaction::Unchanged,
        }
    }

    /// Moves the rows in view by `rows` (positive: towards later items),
    /// keeping the selection among them.
    fn scroll(&mut self, rows: i32) -> Reaction {
        let lines = (self.options.rows() as usize).max(1);
        let last_top = self.matched().saturating_sub(lines);
        let top = (self.top as i64 + i64::from(rows)).clamp(0, last_top as i64) as usize;
        if top == self.top {
            return Reaction::Unchanged;
        }
        self.top = top;
        let bottom = (top + lines).min(self.matched()) - 1;
        self.selected = self.selected.clamp(top, bottom);
        self.preselect = None;
        self.follow = self.selected();
        Reaction::Changed
    }
}

impl Pointing {
    /// The whole rows `scroll` adds up to, keeping the remainder.
    fn rows(&mut self, scroll: Scroll, pitch: f32) -> i32 {
        match scroll {
            Scroll::Wheel(value120) => {
                self.wheel = self
                    .wheel
                    .saturating_add(value120.saturating_mul(ROWS_PER_NOTCH));
                let rows = self.wheel / 120;
                self.wheel -= rows * 120;
                rows
            }
            Scroll::Pixels(pixels) => {
                let pitch = f64::from(pitch.max(1.0));
                self.pixels += pixels;
                let rows = (self.pixels / pitch).trunc();
                self.pixels -= rows * pitch;
                rows as i32
            }
            Scroll::Stop => {
                self.pixels = 0.0;
                0
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::list::Options;

    const LINES: u32 = 3;
    const PITCH: f32 = 26.0;

    fn options() -> Options {
        Options {
            lines: Some(LINES),
            ..Options::default()
        }
    }

    /// Three rows below a prompt line, as a scene lays them out.
    fn logical() -> Grid {
        Grid {
            left: 24.0,
            right: 400.0,
            first: 72.0,
            pitch: PITCH,
            rows: LINES as usize,
        }
    }

    fn list(n: usize) -> ListView {
        let mut v = ListView::new(options());
        v.push((0..n).map(|i| (format!("item {i}"), None)));
        v
    }

    /// The middle of row `row` in view.
    fn over(row: usize) -> (f64, f64) {
        (100.0, f64::from(logical().top(row) + PITCH / 2.0))
    }

    fn moved(v: &mut ListView, row: usize) -> Reaction {
        let (x, y) = over(row);
        v.point(&Point::Moved(x, y), &logical())
    }

    fn click(v: &mut ListView, down: usize, up: usize) -> Reaction {
        let (x, y) = over(down);
        v.point(&Point::Pressed(x, y), &logical());
        let (x, y) = over(up);
        v.point(&Point::Released(x, y), &logical())
    }

    fn scroll(v: &mut ListView, scroll: Scroll, row: usize) -> Reaction {
        let (x, y) = over(row);
        v.point(&Point::Scrolled(scroll, x, y), &logical())
    }

    fn rows(v: &mut ListView) -> Vec<String> {
        v.look().rows.into_iter().map(|r| r.text).collect()
    }

    #[test]
    fn hovering_a_row_selects_it() {
        let mut v = list(5);
        assert_eq!(moved(&mut v, 2), Reaction::Changed);
        assert_eq!(v.selected(), Some(2));
        assert_eq!(moved(&mut v, 2), Reaction::Unchanged, "already selected");
        assert_eq!(
            v.point(&Point::Moved(100.0, 5.0), &logical()),
            Reaction::Unchanged,
            "the prompt line"
        );
        assert_eq!(v.selected(), Some(2));
        assert_eq!(v.point(&Point::Left, &logical()), Reaction::Unchanged);
    }

    #[test]
    fn hovering_an_empty_row_selects_nothing() {
        let mut v = list(2);
        assert_eq!(moved(&mut v, 2), Reaction::Unchanged);
        assert_eq!(v.selected(), Some(0));
    }

    #[test]
    fn a_click_on_a_row_accepts_its_item() {
        let mut v = list(5);
        assert_eq!(click(&mut v, 1, 1), Reaction::Accept(Choice::Item(1)));
    }

    #[test]
    fn a_press_dragged_to_another_row_accepts_nothing() {
        let mut v = list(5);
        assert_eq!(click(&mut v, 0, 2), Reaction::Unchanged);
        assert_eq!(v.selected(), Some(0), "the press selected row 0");
        let (x, y) = over(1);
        assert_eq!(
            v.point(&Point::Released(x, y), &logical()),
            Reaction::Unchanged,
            "a release without a press"
        );
    }

    #[test]
    fn a_click_after_typing_accepts_the_filtered_row() {
        let mut v = list(12);
        v.apply(&crate::input::Action::Edit(crate::input::Edit::Insert(
            "1".into(),
        )));
        assert_eq!(rows(&mut v)[1], "item 10");
        assert_eq!(click(&mut v, 1, 1), Reaction::Accept(Choice::Item(10)));
    }

    #[test]
    fn a_wheel_notch_scrolls_three_rows_and_the_row_under_the_pointer_is_selected() {
        let mut v = list(10);
        assert_eq!(scroll(&mut v, Scroll::Wheel(120), 0), Reaction::Changed);
        assert_eq!(rows(&mut v), ["item 3", "item 4", "item 5"]);
        assert_eq!(v.selected(), Some(3));
        scroll(&mut v, Scroll::Wheel(-120), 2);
        assert_eq!(rows(&mut v), ["item 0", "item 1", "item 2"]);
        assert_eq!(v.selected(), Some(2));
    }

    #[test]
    fn high_resolution_wheel_fractions_add_up_to_rows() {
        let mut v = list(10);
        assert_eq!(scroll(&mut v, Scroll::Wheel(30), 0), Reaction::Unchanged);
        assert_eq!(scroll(&mut v, Scroll::Wheel(30), 0), Reaction::Changed);
        assert_eq!(rows(&mut v)[0], "item 1", "60/120 of a notch: one row");
        scroll(&mut v, Scroll::Wheel(-20), 0);
        assert_eq!(rows(&mut v)[0], "item 1", "the remainder carried");
    }

    #[test]
    fn scrolling_stops_at_both_ends() {
        let mut v = list(5);
        assert_eq!(scroll(&mut v, Scroll::Wheel(-120), 0), Reaction::Unchanged);
        scroll(&mut v, Scroll::Wheel(1200), 0);
        assert_eq!(rows(&mut v), ["item 2", "item 3", "item 4"]);
        assert_eq!(scroll(&mut v, Scroll::Wheel(120), 0), Reaction::Unchanged);
        assert_eq!(
            list(2).point(&scroll_down(), &logical()),
            Reaction::Unchanged
        );
    }

    fn scroll_down() -> Point {
        let (x, y) = over(0);
        Point::Scrolled(Scroll::Wheel(120), x, y)
    }

    #[test]
    fn a_finger_scrolls_a_row_per_row_pitch_and_its_stop_drops_the_rest() {
        let mut v = list(10);
        let pitch = f64::from(PITCH);
        scroll(&mut v, Scroll::Pixels(pitch * 0.75), 0);
        assert_eq!(rows(&mut v)[0], "item 0");
        scroll(&mut v, Scroll::Pixels(pitch * 0.5), 0);
        assert_eq!(rows(&mut v)[0], "item 1");
        scroll(&mut v, Scroll::Stop, 0);
        scroll(&mut v, Scroll::Pixels(pitch * 0.75), 0);
        assert_eq!(rows(&mut v)[0], "item 1", "the quarter row was dropped");
    }

    #[test]
    fn scrolling_away_from_the_pointer_keeps_the_selection_in_view() {
        let mut v = list(10);
        // The pointer is over the prompt line: no row under it.
        let point = Point::Scrolled(Scroll::Wheel(240), 100.0, 5.0);
        assert_eq!(v.point(&point, &logical()), Reaction::Changed);
        assert_eq!(rows(&mut v), ["item 6", "item 7", "item 8"]);
        assert_eq!(v.selected(), Some(6));
    }

    #[test]
    fn a_click_never_accepts_a_pending_item() {
        let mut v = ListView::new(options());
        let pending = crate::list::Entry {
            decor: Some(Box::new(crate::list::Decor {
                pending: true,
                ..crate::list::Decor::default()
            })),
            ..crate::list::Entry::from("awaited".to_owned())
        };
        v.push([pending]);
        assert_eq!(click(&mut v, 0, 0), Reaction::Unchanged);
    }

    #[test]
    fn hovering_ends_a_late_preselection() {
        let mut v = list(3);
        moved(&mut v, 1);
        assert!(!v.preselect(2), "the user moved with the pointer");
        assert_eq!(v.selected(), Some(1));
    }

    #[test]
    fn the_hovered_item_stays_selected_while_items_stream_in() {
        let mut v = list(3);
        moved(&mut v, 2);
        v.push([("a newcomer".to_owned(), None)]);
        assert_eq!(v.selected(), Some(2));
    }
}
