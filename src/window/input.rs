//! The keyboard side of [`Windows`]: the [`Action`]s for the visible panel,
//! the focus-loss policy, and waiting for the compositor without busy
//! loops. The event queue's socket is polled with no timeout, unless a held
//! key's next repeat is due, in which case the wait ends at that deadline.
//! No timer thread, no async runtime: an idle window sleeps in `poll` until
//! the compositor speaks.

use std::time::Instant;

use rustix::event::{PollFd, PollFlags, Timespec, poll};
use rustix::io::Errno;
use smithay_client_toolkit::reexports::client::backend::WaylandError;

use super::{WindowError, WindowQueue, Windows};
use crate::input::{Action, FocusLoss};

impl Windows {
    /// What the keyboard asked of the visible panel since the last call,
    /// oldest first: keys, repeats and focus loss (see [`crate::input`]).
    pub fn take_actions(&mut self) -> Vec<Action> {
        self.keyboard_mut().take_actions()
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
        let lost = |e: &dyn std::fmt::Display| WindowError::Disconnected(e.to_string());
        if queue.dispatch_pending(self).map_err(|e| lost(&e))? == 0 {
            queue.flush().map_err(|e| lost(&e))?;
            if let Some(guard) = queue.prepare_read() {
                let timeout = self.keyboard_mut().deadline().map(until);
                let ready = {
                    let fd = guard.connection_fd();
                    poll(&mut [PollFd::new(&fd, PollFlags::IN)], timeout.as_ref())
                };
                match ready {
                    // The deadline passed: drop the guard, nothing to read.
                    Ok(0) | Err(Errno::INTR) => {}
                    Ok(_) => match guard.read() {
                        Ok(_) => {}
                        Err(WaylandError::Io(e)) if e.kind() == std::io::ErrorKind::WouldBlock => {}
                        Err(e) => return Err(lost(&e)),
                    },
                    Err(e) => return Err(lost(&e)),
                }
            }
            queue.dispatch_pending(self).map_err(|e| lost(&e))?;
        }
        self.keyboard_mut().repeat(Instant::now());
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
