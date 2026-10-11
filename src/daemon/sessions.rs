//! The concurrency policy for sessions (spec §4.1): at most one session is
//! visible. While one is, a new dmenu session queues (first in, first shown)
//! and a new interactive session replaces it. Pure bookkeeping; the daemon
//! carries out the effects.

use std::collections::VecDeque;

/// A session's identity within one daemon run (its connection's number).
pub type SessionId = u64;

/// What the daemon must do after a policy decision, in order.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Effect {
    /// Show this session; it is now the visible one.
    Show(SessionId),
    /// This session waits behind the visible one.
    Queue(SessionId),
    /// Stop showing this session.
    Hide(SessionId),
    /// This session lost its place to an interactive one and is over.
    Replaced(SessionId),
}

/// The visible session and the queue behind it.
#[derive(Debug, Default)]
pub struct Sessions {
    visible: Option<SessionId>,
    queue: VecDeque<SessionId>,
}

impl Sessions {
    /// A new session arrives.
    pub fn open(&mut self, id: SessionId, interactive: bool) -> Vec<Effect> {
        match self.visible {
            None => {
                self.visible = Some(id);
                vec![Effect::Show(id)]
            }
            Some(_) if !interactive => {
                self.queue.push_back(id);
                vec![Effect::Queue(id)]
            }
            Some(old) => {
                self.visible = Some(id);
                vec![Effect::Hide(old), Effect::Replaced(old), Effect::Show(id)]
            }
        }
    }

    /// A session ends (result, cancellation or disconnect) and leaves the policy.
    pub fn close(&mut self, id: SessionId) -> Vec<Effect> {
        if self.visible != Some(id) {
            self.queue.retain(|&queued| queued != id);
            return Vec::new();
        }
        let mut effects = vec![Effect::Hide(id)];
        self.visible = self.queue.pop_front();
        effects.extend(self.visible.map(Effect::Show));
        effects
    }

    /// The session currently shown, if any.
    pub fn visible(&self) -> Option<SessionId> {
        self.visible
    }
}

#[cfg(test)]
mod tests {
    use super::Effect::*;
    use super::*;

    const DMENU: bool = false;
    const INTERACTIVE: bool = true;

    #[test]
    fn the_first_session_is_shown_at_once() {
        let mut s = Sessions::default();
        assert_eq!(s.open(1, DMENU), [Show(1)]);
        assert_eq!(s.visible(), Some(1));
    }

    #[test]
    fn dmenu_sessions_queue_and_are_shown_first_in_first_out() {
        let mut s = Sessions::default();
        s.open(1, DMENU);
        assert_eq!(s.open(2, DMENU), [Queue(2)]);
        assert_eq!(s.open(3, DMENU), [Queue(3)]);
        assert_eq!(s.close(1), [Hide(1), Show(2)]);
        assert_eq!(s.close(2), [Hide(2), Show(3)]);
        assert_eq!(s.close(3), [Hide(3)]);
        assert_eq!(s.visible(), None);
    }

    #[test]
    fn a_dmenu_session_queues_behind_an_interactive_one() {
        let mut s = Sessions::default();
        s.open(1, INTERACTIVE);
        assert_eq!(s.open(2, DMENU), [Queue(2)]);
        assert_eq!(s.close(1), [Hide(1), Show(2)]);
    }

    #[test]
    fn an_interactive_session_replaces_the_visible_one_and_keeps_the_queue() {
        let mut s = Sessions::default();
        s.open(1, DMENU);
        s.open(2, DMENU);
        assert_eq!(s.open(3, INTERACTIVE), [Hide(1), Replaced(1), Show(3)]);
        assert_eq!(s.open(4, INTERACTIVE), [Hide(3), Replaced(3), Show(4)]);
        assert_eq!(s.close(4), [Hide(4), Show(2)]);
    }

    #[test]
    fn closing_a_queued_session_drops_it_silently() {
        let mut s = Sessions::default();
        s.open(1, DMENU);
        s.open(2, DMENU);
        s.open(3, DMENU);
        assert_eq!(s.close(2), []);
        assert_eq!(s.close(1), [Hide(1), Show(3)]);
    }

    #[test]
    fn closing_an_unknown_or_replaced_session_does_nothing() {
        let mut s = Sessions::default();
        assert_eq!(s.close(9), []);
        s.open(1, INTERACTIVE);
        s.open(2, INTERACTIVE);
        assert_eq!(s.close(1), []);
        assert_eq!(s.visible(), Some(2));
    }
}
