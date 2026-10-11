//! One session's external provider: the protocol's rules on top of a
//! running process. It greets the provider, checks its greeting, keeps its
//! stages and maps the user's answers to its lines.
//!
//! When the session ends without the provider's `close`, logis says
//! `cancel` and closes the provider's input. Whether logis may then also
//! signal it decides how it is retired: a provider that was never sent an
//! `activate` or `submit` cannot have started an action, so it is ended if
//! it lingers; one that was may be carrying out an action, which dismissal
//! must never kill, so it is never signalled (spec §4.8, story 10).

use rustix::process::Signal;

use super::stages::Stages;
use super::wire::{FromProvider, Line, PROTOCOL, ToProvider};
use super::{Answer, Step};

/// The process side of a provider, as the session uses it.
pub trait Running {
    /// Sends one line; an error ends the session.
    fn send(&mut self, message: &ToProvider) -> Result<(), String>;
    /// Closes its input: it reads end of file.
    fn close_input(&mut self);
    /// Signals the provider alone (never its group or children).
    fn signal(&self, signal: Signal);
}

impl Running for super::process::Process {
    fn send(&mut self, message: &ToProvider) -> Result<(), String> {
        super::process::Process::send(self, message)
    }
    fn close_input(&mut self) {
        super::process::Process::close_input(self);
    }
    fn signal(&self, signal: Signal) {
        super::process::Process::signal(self, signal);
    }
}

/// What the provider's line means for the session.
#[derive(Debug, PartialEq, Eq)]
pub enum Said {
    /// Steps for the window (perhaps none).
    Steps(Vec<Step>),
    /// The provider ended its flow.
    Close,
}

/// An external provider in one session.
pub struct External {
    running: Box<dyn Running>,
    stages: Stages,
    greeted: bool,
    /// An `activate` or `submit` went out: an action may be under way.
    acted: bool,
    /// The provider said `close`.
    closed: bool,
    /// The process is gone.
    exited: bool,
}

impl External {
    /// A started provider, greeted.
    pub fn new(mut running: Box<dyn Running>) -> Result<Self, String> {
        running.send(&ToProvider::Hello { protocol: PROTOCOL })?;
        Ok(Self {
            running,
            stages: Stages::default(),
            greeted: false,
            acted: false,
            closed: false,
            exited: false,
        })
    }

    /// One line from the provider.
    pub fn heard(&mut self, line: Result<Line, String>) -> Result<Said, String> {
        let message = match line? {
            Line::Message(message) => message,
            Line::Unknown(kind) => {
                return Ok(Said::Steps(Vec::new())).inspect(|_| {
                    crate::daemon::log(&format!("a provider's '{kind}' line is unknown; skipped"));
                });
            }
        };
        match (self.greeted, message) {
            (false, FromProvider::Hello { protocol }) if protocol == PROTOCOL => {
                self.greeted = true;
                Ok(Said::Steps(Vec::new()))
            }
            (false, FromProvider::Hello { protocol }) => Err(format!(
                "the provider speaks protocol {protocol}; this cogitator speaks {PROTOCOL}"
            )),
            (false, _) => Err("the provider did not greet first ('hello')".to_owned()),
            (true, FromProvider::Hello { .. }) => Err("the provider greeted twice".to_owned()),
            (true, FromProvider::Close) => {
                self.closed = true;
                Ok(Said::Close)
            }
            (true, message) => self.stages.apply(message).map(Said::Steps),
        }
    }

    /// The user answered the current stage (not a dismissal).
    pub fn answer(&mut self, answer: Answer) -> Result<(), String> {
        let Some(line) = self.stages.answer(answer) else {
            return Ok(());
        };
        self.acted = true;
        self.running.send(&line)
    }

    /// The process is gone.
    pub fn exited(&mut self) {
        self.exited = true;
    }

    /// The session is over. Says `cancel` unless the provider closed it,
    /// closes its input, and returns the process if logis may still end it
    /// (see the module docs); otherwise lets it be.
    pub fn retire(mut self) -> Option<Box<dyn Running>> {
        if !self.closed && !self.exited {
            let _ = self.running.send(&ToProvider::Cancel);
        }
        self.running.close_input();
        (!self.acted && !self.exited).then_some(self.running)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::provider::fake::Fake;
    use crate::provider::wire::{Item, Stage};

    fn msg(message: FromProvider) -> Result<Line, String> {
        Ok(Line::Message(message))
    }

    fn greeted() -> (External, Fake) {
        let fake = Fake::default();
        let mut e = External::new(Box::new(fake.clone())).unwrap();
        assert_eq!(fake.take(), [r#"{"type":"hello","protocol":1}"#]);
        e.heard(msg(FromProvider::Hello { protocol: 1 })).unwrap();
        (e, fake)
    }

    fn stage(e: &mut External) {
        let stage = Stage {
            id: "rite".into(),
            items: vec![Item {
                id: "oil".into(),
                columns: vec!["oil".into()],
                ..Item::default()
            }],
            ..Stage::default()
        };
        let said = e.heard(msg(FromProvider::Stage(stage))).unwrap();
        assert!(matches!(said, Said::Steps(s) if matches!(s[..], [Step::Stage { .. }])));
    }

    #[test]
    fn the_provider_must_greet_first_in_this_protocol() {
        let fake = Fake::default();
        let mut e = External::new(Box::new(fake.clone())).unwrap();
        let err = e.heard(msg(FromProvider::Ready)).unwrap_err();
        assert!(err.contains("hello"), "{err}");
        let mut e = External::new(Box::new(fake)).unwrap();
        let err = e
            .heard(msg(FromProvider::Hello { protocol: 2 }))
            .unwrap_err();
        assert!(err.contains("protocol 2"), "{err}");
    }

    #[test]
    fn unknown_lines_are_skipped_and_errors_end_the_session() {
        let (mut e, _) = greeted();
        assert_eq!(
            e.heard(Ok(Line::Unknown("omen".into()))),
            Ok(Said::Steps(vec![]))
        );
        assert!(e.heard(Err("not a JSON line".into())).is_err());
    }

    #[test]
    fn an_answer_is_sent_as_activate() {
        let (mut e, fake) = greeted();
        stage(&mut e);
        e.answer(Answer::Item(0)).unwrap();
        assert_eq!(
            fake.take(),
            [r#"{"type":"activate","stage":"rite","id":"oil"}"#]
        );
    }

    #[test]
    fn a_provider_that_never_acted_is_cancelled_and_may_be_ended() {
        let (mut e, fake) = greeted();
        stage(&mut e);
        let rest = e.retire();
        assert_eq!(fake.take(), [r#"{"type":"cancel"}"#, "eof"]);
        assert!(rest.is_some(), "it may be signalled if it lingers");
    }

    #[test]
    fn a_provider_that_acted_is_cancelled_but_never_signalled() {
        let (mut e, fake) = greeted();
        stage(&mut e);
        e.answer(Answer::Item(0)).unwrap();
        fake.take();
        assert!(e.retire().is_none(), "an action may be under way");
        assert_eq!(fake.take(), [r#"{"type":"cancel"}"#, "eof"]);
    }

    #[test]
    fn a_closed_flow_hears_no_cancel_and_a_gone_process_no_signal() {
        let (mut e, fake) = greeted();
        stage(&mut e);
        assert_eq!(e.heard(msg(FromProvider::Close)), Ok(Said::Close));
        let rest = e.retire();
        assert_eq!(fake.take(), ["eof"]);
        assert!(rest.is_some(), "it never acted");
        let (mut e, _) = greeted();
        e.exited();
        assert!(e.retire().is_none());
    }
}
