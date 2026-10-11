//! The built-in dmenu provider: its items are the lines the client streams
//! from stdin, its one stage the list the client asked for, and an answer
//! ends the session with the chosen line's original bytes (or the typed
//! text). It speaks the same [`Step`]s and [`Answer`]s as an external
//! provider, without a process or ids: an item's index is its line number.

use logis::list::{Entry, Options};

use super::{Answer, Step};
use crate::protocol::{Item, Outcome};

/// One dmenu session's provider.
pub struct Dmenu {
    /// The list the client asked for, until it is shown.
    options: Option<Options>,
    /// Every line so far, as the client sent it.
    items: Vec<Item>,
}

impl Dmenu {
    pub fn new(options: Options) -> Self {
        Self {
            options: Some(options),
            items: Vec::new(),
        }
    }

    /// The session is shown: its stage with the lines so far.
    pub fn show(&mut self) -> Step {
        Step::Stage {
            options: self.options.take().unwrap_or_default(),
            entries: self.items.iter().map(entry).collect(),
        }
    }

    /// More lines; what the window gets of them once it shows the list.
    pub fn push(&mut self, items: Vec<Item>) -> Option<Step> {
        let first = self.items.len();
        self.items.extend(items);
        if self.options.is_some() {
            return None;
        }
        Some(Step::Append(
            self.items[first..].iter().map(entry).collect(),
        ))
    }

    /// How the session ends for this answer.
    pub fn outcome(&self, answer: Answer) -> Outcome {
        match answer {
            Answer::Item(index) | Answer::Descend(index) => match self.items.get(index as usize) {
                Some(item) => Outcome::Selected {
                    index: u64::from(index),
                    item: item.clone(),
                },
                None => Outcome::Dismissed,
            },
            Answer::Typed(text) => Outcome::Typed { text },
            Answer::Dismissed => Outcome::Dismissed,
        }
    }
}

/// A line as the list takes it: its shown text and its match text.
fn entry(item: &Item) -> Entry {
    Entry {
        text: item.text.clone(),
        match_text: item.match_text.clone(),
        decor: None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn lines(texts: &[&str]) -> Vec<Item> {
        texts
            .iter()
            .map(|t| Item::from_raw(t.as_bytes().to_vec()))
            .collect()
    }

    fn texts(step: &Step) -> Vec<String> {
        match step {
            Step::Stage { entries, .. } | Step::Append(entries) => {
                entries.iter().map(|e| e.text.clone()).collect()
            }
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn lines_before_the_show_come_with_the_stage_and_later_ones_append() {
        let options = Options {
            prompt: "rite? ".into(),
            ..Options::default()
        };
        let mut d = Dmenu::new(options.clone());
        assert_eq!(d.push(lines(&["a", "b"])), None, "not shown yet");
        let stage = d.show();
        assert_eq!(texts(&stage), ["a", "b"]);
        let Step::Stage { options: shown, .. } = stage else {
            unreachable!()
        };
        assert_eq!(shown, options);
        assert_eq!(texts(&d.push(lines(&["c"])).unwrap()), ["c"]);
    }

    #[test]
    fn an_answer_is_the_chosen_lines_bytes_or_the_typed_text() {
        let mut d = Dmenu::new(Options::default());
        d.push(vec![
            Item::from_raw(b"x".to_vec()),
            Item::from_raw(b"\xff y".to_vec()),
        ]);
        assert_eq!(
            d.outcome(Answer::Item(1)),
            Outcome::Selected {
                index: 1,
                item: Item::from_raw(b"\xff y".to_vec())
            }
        );
        assert_eq!(d.outcome(Answer::Descend(1)), d.outcome(Answer::Item(1)));
        assert_eq!(
            d.outcome(Answer::Typed("free".into())),
            Outcome::Typed {
                text: "free".into()
            }
        );
        assert_eq!(d.outcome(Answer::Dismissed), Outcome::Dismissed);
    }
}
