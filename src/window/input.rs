//! The input side of [`Windows`]: the [`Action`]s for the visible panel,
//! the focus-loss policy, and waiting for the compositor without busy
//! loops. The event queue's socket is polled with no timeout, unless a held
//! key's next repeat is due (or a closing panel's frame callbacks stopped
//! and its time is up), in which case the wait ends at that deadline; a
//! paste being read adds its pipe to the poll. No timer thread, no async
//! runtime: an idle window sleeps in `poll` until the compositor speaks.

use std::os::fd::BorrowedFd;
use std::time::Instant;

use rustix::event::{PollFd, PollFlags, Timespec, poll};
use rustix::io::Errno;
use smithay_client_toolkit::reexports::client::backend::WaylandError;

use super::{WindowError, WindowQueue, Windows};
use crate::input::{Action, FocusLoss};

impl Windows {
    /// What the seat asked of the visible panel since the last call, oldest
    /// first: keys, repeats, focus loss, the pointer, and pasted text (see
    /// [`crate::input`]). A paste is started here and comes back as an
    /// [`Action::Edit`] once read; it is never returned itself.
    pub fn take_actions(&mut self) -> Vec<Action> {
        let mut actions = std::mem::take(&mut self.actions);
        actions.retain(|action| match action {
            Action::Paste(selection) => {
                self.clipboard.start(*selection);
                false
            }
            _ => true,
        });
        actions
    }

    /// What losing keyboard focus does to the visible panel (the config's
    /// `focus-loss`).
    pub fn set_focus_loss(&mut self, policy: FocusLoss) {
        self.keyboard_mut().set_focus_loss(policy);
    }

    /// Dispatches what the compositor sent, waiting for it if nothing is
    /// queued, and emits the key repeats that are due. Call it in the event
    /// loop in place of `blocking_dispatch`.
    pub fn dispatch(&mut self, queue: &mut WindowQueue) -> Result<(), WindowError> {
        self.dispatch_or_wake(queue, None)
    }

    /// As [`Self::dispatch`], but the wait also ends when `wake` becomes
    /// readable (another thread has work for this one); reading it is the
    /// caller's.
    pub fn dispatch_or_wake(
        &mut self,
        queue: &mut WindowQueue,
        wake: Option<BorrowedFd<'_>>,
    ) -> Result<(), WindowError> {
        let lost = |e: &dyn std::fmt::Display| WindowError::Disconnected(e.to_string());
        if queue.dispatch_pending(self).map_err(|e| lost(&e))? == 0 {
            queue.flush().map_err(|e| lost(&e))?;
            if let Some(guard) = queue.prepare_read() {
                let deadline = [
                    self.keyboard_mut().deadline(),
                    self.motion_deadline(),
                    self.alarm,
                ]
                .into_iter()
                .flatten()
                .min();
                let timeout = deadline.map(until);
                let ready = {
                    let fd = guard.connection_fd();
                    let mut fds = vec![PollFd::new(&fd, PollFlags::IN)];
                    fds.extend(wake.as_ref().map(|w| PollFd::new(w, PollFlags::IN)));
                    let pasting = self.clipboard.fd();
                    fds.extend(pasting.as_ref().map(|p| PollFd::new(p, PollFlags::IN)));
                    poll(&mut fds, timeout.as_ref()).map(|n| (n, !fds[0].revents().is_empty()))
                };
                match ready {
                    // The deadline passed or only `wake` is ready: drop the
                    // guard, nothing to read.
                    Ok((0, _) | (_, false)) | Err(Errno::INTR) => {}
                    Ok((_, true)) => match guard.read() {
                        Ok(_) => {}
                        Err(WaylandError::Io(e)) if e.kind() == std::io::ErrorKind::WouldBlock => {}
                        Err(e) => return Err(lost(&e)),
                    },
                    Err(e) => return Err(lost(&e)),
                }
            }
            queue.dispatch_pending(self).map_err(|e| lost(&e))?;
        }
        let repeats = self.keyboard_mut().repeat(Instant::now());
        self.actions.extend(repeats);
        if let Some(edit) = self.clipboard.pump() {
            self.actions.push(Action::Edit(edit));
        }
        self.expire_closing();
        Ok(())
    }
}

/// The poll timeout from now until `deadline` (zero if it has passed).
fn until(deadline: Instant) -> Timespec {
    let left = deadline.saturating_duration_since(Instant::now());
    Timespec {
        tv_sec: left.as_secs() as i64,
        tv_nsec: i64::from(left.subsec_nanos()),
    }
}
