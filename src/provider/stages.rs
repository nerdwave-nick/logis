//! A provider's stages as the window takes them: the provider names items by
//! id, the list by index (the order the ids first arrived in its stage). This
//! maps one onto the other, turns wire items into list entries, and turns
//! the user's answers back into lines for the provider.

use std::collections::HashMap;
use std::time::Duration;

use logis::list::{Decor, Entry, Notice, Options, Preselect};

use super::wire::{FromProvider, Input, Item, Stage, ToProvider};
use super::{Answer, Step};

/// How long a toast shows unless the provider says otherwise.
pub const TOAST_SECONDS: u32 = 3;
/// The longest a toast may show.
pub const MAX_TOAST_SECONDS: u32 = 60;

/// The current stage of one provider session.
#[derive(Default)]
pub struct Stages {
    current: Option<Current>,
}

struct Current {
    id: String,
    /// Item id → index.
    index: HashMap<String, u32>,
    /// Index → item id.
    ids: Vec<String>,
    /// A `select` whose item has not arrived yet.
    select: Option<String>,
}

impl Stages {
    /// What a provider's message asks of the window. `hello` and `close`
    /// are the session's business, not the stages'.
    pub fn apply(&mut self, message: FromProvider) -> Result<Vec<Step>, String> {
        match message {
            FromProvider::Stage(stage) => Ok(self.stage(stage)),
            FromProvider::Items { items, select } => {
                let current = self.current_mut("items")?;
                if select.is_some() {
                    current.select = select;
                }
                Ok(current.upsert(items))
            }
            FromProvider::Remove { ids } => {
                let current = self.current_mut("remove")?;
                let gone: Vec<u32> = ids
                    .iter()
                    .filter_map(|id| current.index.get(id))
                    .copied()
                    .collect();
                Ok(if gone.is_empty() {
                    Vec::new()
                } else {
                    vec![Step::Remove(gone)]
                })
            }
            FromProvider::Toast {
                text,
                level,
                seconds,
            } => {
                let seconds = seconds.unwrap_or(TOAST_SECONDS).clamp(1, MAX_TOAST_SECONDS);
                let notice = Notice { text, level };
                let span = Duration::from_secs(u64::from(seconds));
                Ok(vec![Step::Toast { notice, span }])
            }
            FromProvider::Ready => Ok(vec![Step::Ready]),
            FromProvider::Hello { .. } | FromProvider::Close => Ok(Vec::new()),
        }
    }

    /// The line an answer sends the provider (`None` for a dismissal, or
    /// an index no item has).
    pub fn answer(&self, answer: Answer) -> Option<ToProvider> {
        let current = self.current.as_ref()?;
        let stage = current.id.clone();
        match answer {
            Answer::Item(index) | Answer::Descend(index) => {
                current
                    .ids
                    .get(index as usize)
                    .map(|id| ToProvider::Activate {
                        stage,
                        id: id.clone(),
                    })
            }
            Answer::Typed(text) => Some(ToProvider::Submit { stage, text }),
            Answer::Dismissed => None,
        }
    }

    fn current_mut(&mut self, what: &str) -> Result<&mut Current, String> {
        self.current
            .as_mut()
            .ok_or_else(|| format!("'{what}' came before any 'stage'"))
    }

    fn stage(&mut self, stage: Stage) -> Vec<Step> {
        let defaults = Options::default();
        let mut options = Options {
            prompt: stage.prompt.unwrap_or(defaults.prompt),
            placeholder: stage.placeholder,
            search: stage.query,
            only_match: stage.input == Input::None,
            required: stage.input == Input::Required,
            lines: stage.lines.unwrap_or(defaults.lines),
            width: stage.width.unwrap_or(defaults.width),
            ..defaults
        };
        let mut current = Current {
            id: stage.id,
            index: HashMap::new(),
            ids: Vec::new(),
            select: None,
        };
        let mut entries = Vec::new();
        for item in stage.items {
            match current.index.get(&item.id) {
                Some(&i) => entries[i as usize] = entry(item),
                None => {
                    current
                        .index
                        .insert(item.id.clone(), current.ids.len() as u32);
                    current.ids.push(item.id.clone());
                    entries.push(entry(item));
                }
            }
        }
        if let Some(select) = stage.select {
            match current.index.get(&select) {
                Some(&i) => options.select = Some(Preselect::Index(u64::from(i))),
                None => current.select = Some(select),
            }
        }
        self.current = Some(current);
        vec![Step::Stage { options, entries }]
    }
}

impl Current {
    /// New ids are appended, known ones replaced in their place.
    fn upsert(&mut self, items: Vec<Item>) -> Vec<Step> {
        let mut fresh = Vec::new();
        let mut changed = Vec::new();
        for item in items {
            match self.index.get(&item.id) {
                Some(&i) => changed.push((i, entry(item))),
                None => {
                    let i = self.ids.len() as u32;
                    self.index.insert(item.id.clone(), i);
                    self.ids.push(item.id.clone());
                    fresh.push(entry(item));
                }
            }
        }
        let mut steps = Vec::new();
        if !fresh.is_empty() {
            steps.push(Step::Append(fresh));
        }
        if !changed.is_empty() {
            steps.push(Step::Update(changed));
        }
        if let Some(&i) = self.select.as_ref().and_then(|s| self.index.get(s)) {
            self.select = None;
            steps.push(Step::Preselect(i));
        }
        steps
    }
}

/// A wire item as a list entry: its columns joined by tabs (a tab or line
/// break inside a column becomes a space, so columns stay lined up).
fn entry(item: Item) -> Entry {
    let text = item
        .columns
        .iter()
        .map(|c| c.replace(['\t', '\n', '\r'], " "))
        .collect::<Vec<_>>()
        .join("\t");
    let plain =
        item.glyph.is_none() && item.icon.is_none() && item.badges.is_empty() && !item.pending;
    Entry {
        text,
        match_text: item.match_text,
        decor: (!plain).then(|| {
            Box::new(Decor {
                glyph: item.glyph,
                icon: item.icon,
                badges: item.badges,
                pending: item.pending,
                boost: 0,
            })
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn item(id: &str, text: &str) -> Item {
        Item {
            id: id.into(),
            columns: vec![text.into()],
            ..Item::default()
        }
    }

    fn texts(entries: &[Entry]) -> Vec<&str> {
        entries.iter().map(|e| e.text.as_str()).collect()
    }

    fn staged(stages: &mut Stages, stage: Stage) -> (Options, Vec<Entry>) {
        match stages.apply(FromProvider::Stage(stage)).unwrap().as_slice() {
            [Step::Stage { options, entries }] => (options.clone(), entries.clone()),
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn a_stage_becomes_a_list_with_its_options() {
        let mut s = Stages::default();
        let (options, entries) = staged(
            &mut s,
            Stage {
                id: "rite".into(),
                prompt: Some("rite: ".into()),
                query: "oi".into(),
                items: vec![
                    Item {
                        columns: vec!["rite\tof oil".into(), "unguent".into()],
                        glyph: Some("⚙".into()),
                        ..item("oil", "")
                    },
                    item("fire", "rite of fire"),
                ],
                ..Stage::default()
            },
        );
        assert_eq!(
            (options.prompt.as_str(), options.search.as_str()),
            ("rite: ", "oi")
        );
        assert!(options.only_match, "no typed text by default");
        assert_eq!(texts(&entries), ["rite of oil\tunguent", "rite of fire"]);
        assert_eq!(
            entries[0].decor.as_ref().unwrap().glyph.as_deref(),
            Some("⚙")
        );
        assert!(entries[1].decor.is_none(), "a plain item stays plain");
    }

    #[test]
    fn items_upsert_by_id_and_removals_name_their_indices() {
        let mut s = Stages::default();
        let stage = Stage {
            id: "s".into(),
            items: vec![item("a", "A"), item("b", "B")],
            ..Stage::default()
        };
        staged(&mut s, stage);
        let steps = s
            .apply(FromProvider::Items {
                items: vec![item("c", "C"), item("a", "A2")],
                select: None,
            })
            .unwrap();
        assert_eq!(
            steps,
            [
                Step::Append(vec![Entry::from("C".to_owned())]),
                Step::Update(vec![(0, Entry::from("A2".to_owned()))]),
            ]
        );
        let gone = s
            .apply(FromProvider::Remove {
                ids: vec!["c".into(), "nobody".into()],
            })
            .unwrap();
        assert_eq!(gone, [Step::Remove(vec![2])]);
    }

    #[test]
    fn answers_name_the_stage_and_the_item_id() {
        let mut s = Stages::default();
        assert_eq!(s.answer(Answer::Item(0)), None, "no stage yet");
        staged(
            &mut s,
            Stage {
                id: "aspect".into(),
                items: vec![item("on", "On"), item("off", "Off")],
                ..Stage::default()
            },
        );
        assert_eq!(
            s.answer(Answer::Item(1)),
            Some(ToProvider::Activate {
                stage: "aspect".into(),
                id: "off".into()
            })
        );
        assert_eq!(
            s.answer(Answer::Typed("x".into())),
            Some(ToProvider::Submit {
                stage: "aspect".into(),
                text: "x".into()
            })
        );
        assert_eq!(s.answer(Answer::Item(7)), None);
        assert_eq!(
            s.answer(Answer::Descend(1)),
            s.answer(Answer::Item(1)),
            "as Return"
        );
        assert_eq!(s.answer(Answer::Dismissed), None);
    }

    #[test]
    fn a_new_stage_starts_its_own_ids() {
        let mut s = Stages::default();
        staged(
            &mut s,
            Stage {
                id: "one".into(),
                items: vec![item("x", "X")],
                ..Stage::default()
            },
        );
        staged(
            &mut s,
            Stage {
                id: "two".into(),
                items: vec![item("y", "Y")],
                ..Stage::default()
            },
        );
        assert_eq!(
            s.answer(Answer::Item(0)),
            Some(ToProvider::Activate {
                stage: "two".into(),
                id: "y".into()
            })
        );
    }

    #[test]
    fn select_names_an_item_now_or_once_it_arrives() {
        let mut s = Stages::default();
        let (options, _) = staged(
            &mut s,
            Stage {
                id: "s".into(),
                select: Some("b".into()),
                items: vec![item("a", "A"), item("b", "B")],
                ..Stage::default()
            },
        );
        assert_eq!(options.select, Some(Preselect::Index(1)));
        let (options, _) = staged(
            &mut s,
            Stage {
                id: "t".into(),
                select: Some("late".into()),
                ..Stage::default()
            },
        );
        assert_eq!(options.select, None);
        let steps = s
            .apply(FromProvider::Items {
                items: vec![item("late", "L")],
                select: None,
            })
            .unwrap();
        assert_eq!(steps.last(), Some(&Step::Preselect(0)));
    }

    #[test]
    fn items_may_name_the_item_to_select_once_known() {
        let mut s = Stages::default();
        staged(
            &mut s,
            Stage {
                id: "aspect".into(),
                items: vec![item("on", "On"), item("off", "Off")],
                ..Stage::default()
            },
        );
        let steps = s
            .apply(FromProvider::Items {
                items: vec![item("off", "Off")],
                select: Some("off".into()),
            })
            .unwrap();
        assert_eq!(steps.last(), Some(&Step::Preselect(1)));
        let steps = s
            .apply(FromProvider::Items {
                items: vec![],
                select: Some("on".into()),
            })
            .unwrap();
        assert_eq!(steps, [Step::Preselect(0)]);
    }

    #[test]
    fn a_toast_shows_for_its_seconds_within_bounds() {
        let mut s = Stages::default();
        let span = |s: &mut Stages, seconds| {
            let toast = FromProvider::Toast {
                text: "t".into(),
                level: Default::default(),
                seconds,
            };
            match s.apply(toast).unwrap().as_slice() {
                [Step::Toast { span, .. }] => span.as_secs(),
                other => panic!("{other:?}"),
            }
        };
        assert_eq!(span(&mut s, None), 3);
        assert_eq!(span(&mut s, Some(8)), 8);
        assert_eq!(span(&mut s, Some(0)), 1);
        assert_eq!(span(&mut s, Some(3600)), 60);
    }

    #[test]
    fn input_decides_what_typed_text_may_do() {
        let mut s = Stages::default();
        let (optional, _) = staged(
            &mut s,
            Stage {
                input: Input::Optional,
                ..Stage::default()
            },
        );
        assert!(!optional.only_match && !optional.required);
        let (required, _) = staged(
            &mut s,
            Stage {
                input: Input::Required,
                ..Stage::default()
            },
        );
        assert!(!required.only_match && required.required);
    }

    #[test]
    fn items_before_any_stage_are_a_protocol_error() {
        let mut s = Stages::default();
        let err = s
            .apply(FromProvider::Items {
                items: vec![],
                select: None,
            })
            .unwrap_err();
        assert!(err.contains("before any 'stage'"), "{err}");
        assert!(s.apply(FromProvider::Remove { ids: vec![] }).is_err());
    }
}
