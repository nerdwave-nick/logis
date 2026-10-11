//! A fake provider process for unit tests: it records what it was sent and
//! how it was treated, one string each.

use std::sync::{Arc, Mutex};

use rustix::process::Signal;

use super::external::Running;
use super::wire::{self, ToProvider};

#[derive(Clone, Default)]
pub struct Fake(Arc<Mutex<Vec<String>>>);

impl Fake {
    /// What happened since the last call: sent lines (without newline),
    /// `eof` for a closed input, `signal N`.
    pub fn take(&self) -> Vec<String> {
        std::mem::take(&mut self.0.lock().unwrap())
    }
}

impl Running for Fake {
    fn send(&mut self, message: &ToProvider) -> Result<(), String> {
        let line = String::from_utf8(wire::line(message)).unwrap();
        self.0.lock().unwrap().push(line.trim_end().to_owned());
        Ok(())
    }
    fn close_input(&mut self) {
        self.0.lock().unwrap().push("eof".into());
    }
    fn signal(&self, signal: Signal) {
        self.0
            .lock()
            .unwrap()
            .push(format!("signal {}", signal.as_raw()));
    }
}
