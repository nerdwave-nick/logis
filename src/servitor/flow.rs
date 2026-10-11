//! servitor mode as a provider flow (spec §3 stories 8–10, §4.8): the
//! census's rites, then the chosen rite's aspects, then one prompt per
//! inscription of its canon, then `servitor invoke` and `close`. A pure state
//! machine: what the census tells and what logis answers go in, the lines to
//! say and the invocation to launch come out (src/servitor/mod.rs does the
//! I/O). logis shows no outcome: servitor's vox announces the verdict.
//!
//! Nothing unsettled drives anything (story 10): a rite whose auspex is still
//! awaited is pending (never chosen), and an aspect is marked current and
//! preselected only from a settled entry ([`Entry::current`]).

use std::collections::HashMap;

use crate::provider::wire::{FromProvider, Level, PROTOCOL, ToProvider};

use super::census::{Declaration, Entry, Herald, Told};
use super::shown;

/// What the flow asks of its provider process.
#[derive(Debug, PartialEq, Eq)]
pub enum Act {
    /// A line to logis.
    Say(FromProvider),
    /// Launch `servitor` with these arguments, detached; then tell the
    /// flow how that went ([`Flow::launched`]).
    Invoke(Vec<String>),
    /// The provider is done, with this exit status.
    Exit(u8),
}

/// Where the flow stands.
#[derive(Default)]
enum At {
    #[default]
    Rites,
    Aspect {
        rite: String,
    },
    /// Asking inscription `next` of the rite's canon; `words` holds the
    /// answers so far, in canon order.
    Inscription {
        rite: String,
        aspect: Option<String>,
        words: Vec<String>,
    },
}

/// One servitor-mode session.
#[derive(Default)]
pub struct Flow {
    /// Every rite told, by name.
    entries: HashMap<String, Entry>,
    concluded: bool,
    at: At,
}

impl Flow {
    /// The greeting and the rite stage, at once (the census fills it).
    pub fn begin(&mut self) -> Vec<Act> {
        let hello = FromProvider::Hello { protocol: PROTOCOL };
        vec![Act::Say(hello), say_stage(shown::rites())]
    }

    /// The census spoke, or ended.
    pub fn told(&mut self, told: Told) -> Vec<Act> {
        match told {
            Told::Herald(Herald::Rite(entry)) => self.rite_told(entry),
            Told::Herald(Herald::Lament { lament, rite }) => {
                let rite = rite.map_or_else(String::new, |r| format!(" ({r})"));
                eprintln!("logis servitor: the census laments{rite}: {lament}");
                Vec::new()
            }
            Told::Herald(Herald::Concluded) => {
                self.concluded = true;
                Vec::new()
            }
            Told::Herald(Herald::Unknown) => Vec::new(),
            Told::Ended(_) if self.concluded && !self.entries.is_empty() => Vec::new(),
            Told::Ended(_) if self.concluded => vec![toast(Level::Info, shown::NO_RITES)],
            Told::Ended(status) => vec![toast(Level::Error, &shown::broken_off(&status))],
        }
    }

    /// The census could not be started.
    pub fn unheard(&mut self, error: String) -> Vec<Act> {
        vec![toast(Level::Error, &shown::no_servitor(&error))]
    }

    /// logis spoke.
    pub fn heard(&mut self, message: ToProvider) -> Vec<Act> {
        match message {
            ToProvider::Hello { protocol } if protocol == PROTOCOL => Vec::new(),
            ToProvider::Hello { protocol } => {
                eprintln!("logis servitor: logis speaks protocol {protocol}, not {PROTOCOL}");
                vec![Act::Exit(2)]
            }
            ToProvider::Cancel => vec![Act::Exit(0)],
            ToProvider::Activate { stage, id } => self.activated(&stage, id),
            ToProvider::Submit { stage, text } => self.submitted(&stage, text),
        }
    }

    /// The invocation was launched, or could not be.
    pub fn launched(&mut self, result: Result<(), String>) -> Vec<Act> {
        match result {
            Ok(()) => vec![Act::Say(FromProvider::Close), Act::Exit(0)],
            Err(e) => vec![toast(Level::Error, &shown::not_roused(&e)), ready()],
        }
    }

    fn rite_told(&mut self, entry: Entry) -> Vec<Act> {
        let name = entry.rite.clone();
        let acts = match &self.at {
            At::Rites => vec![Act::Say(FromProvider::Items {
                items: vec![shown::rite(&entry)],
                select: None,
            })],
            At::Aspect { rite } if *rite == name => {
                let (items, select) = shown::aspects(&entry);
                vec![Act::Say(FromProvider::Items { items, select })]
            }
            _ => Vec::new(),
        };
        self.entries.insert(name, entry);
        acts
    }

    fn activated(&mut self, stage: &str, id: String) -> Vec<Act> {
        match (&self.at, stage) {
            (At::Rites, shown::RITE) => self.rite_chosen(id),
            (At::Aspect { rite }, shown::ASPECT) => {
                let rite = rite.clone();
                match self.entries.get(&rite) {
                    Some(entry) if entry.aspects.contains(&id) => {
                        self.inscribe(rite, Some(id), Vec::new())
                    }
                    _ => vec![ready()],
                }
            }
            _ => vec![ready()],
        }
    }

    fn rite_chosen(&mut self, name: String) -> Vec<Act> {
        let Some(entry) = self.entries.get(&name).filter(|e| !e.awaited()) else {
            return vec![ready()];
        };
        if entry.heretical() || (!entry.edict && entry.aspects.is_empty()) {
            return vec![toast(Level::Error, &shown::heretical(&name)), ready()];
        }
        if entry.edict {
            return self.inscribe(name, None, Vec::new());
        }
        let (items, select) = shown::aspects(entry);
        let stage = shown::aspect_stage(&name, items, select);
        self.at = At::Aspect { rite: name };
        vec![say_stage(stage)]
    }

    fn submitted(&mut self, stage: &str, text: String) -> Vec<Act> {
        let At::Inscription {
            rite,
            aspect,
            words,
        } = &self.at
        else {
            return vec![ready()];
        };
        let Some(asked) = self.canon(rite).get(words.len()) else {
            return vec![ready()];
        };
        if stage != shown::inscription_id(asked) {
            return vec![ready()];
        }
        if asked.mandatory && text.is_empty() {
            return vec![toast(Level::Error, &shown::mandatory(&asked.key)), ready()];
        }
        let (rite, aspect) = (rite.clone(), aspect.clone());
        let mut words = words.clone();
        words.push(text);
        self.inscribe(rite, aspect, words)
    }

    /// Asks the next inscription, or invokes once every one is answered.
    /// The flow stays at the last question until the launch succeeds, so a
    /// failed launch can be answered again.
    fn inscribe(&mut self, rite: String, aspect: Option<String>, words: Vec<String>) -> Vec<Act> {
        let canon = self.canon(&rite);
        if let Some(next) = canon.get(words.len()) {
            let stage = shown::inscription_stage(next, aspect.as_deref());
            self.at = At::Inscription {
                rite,
                aspect,
                words,
            };
            return vec![say_stage(stage)];
        }
        vec![Act::Invoke(invocation(
            &rite,
            aspect.as_deref(),
            canon,
            &words,
        ))]
    }

    fn canon(&self, rite: &str) -> &[Declaration] {
        self.entries.get(rite).map_or(&[], |e| &e.canon)
    }
}

/// `invoke <rite> [<aspect>] [--<key> <word>]...`: every word given, and an
/// empty one where it clears a decree; an empty word without a decree is
/// left out (it would inscribe nothing all the same).
fn invocation(
    rite: &str,
    aspect: Option<&str>,
    canon: &[Declaration],
    words: &[String],
) -> Vec<String> {
    let mut argv = vec!["invoke".to_owned(), rite.to_owned()];
    argv.extend(aspect.map(str::to_owned));
    for (declaration, word) in canon.iter().zip(words) {
        if !word.is_empty() || declaration.decreed(aspect).is_some() {
            argv.push(format!("--{}", declaration.key));
            argv.push(word.clone());
        }
    }
    argv
}

fn say_stage(stage: crate::provider::wire::Stage) -> Act {
    Act::Say(FromProvider::Stage(stage))
}

fn ready() -> Act {
    Act::Say(FromProvider::Ready)
}

fn toast(level: Level, text: &str) -> Act {
    Act::Say(FromProvider::Toast {
        text: text.to_owned(),
        level,
        seconds: Some(shown::TOAST_SECONDS),
    })
}

#[cfg(test)]
mod tests;
