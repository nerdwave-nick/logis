//! logis as a library: the parts the daemon owns, kept apart from the
//! command line so tests and the daemon drive the same code.

// The test support shared with the integration tests names the crate.
#[cfg(test)]
extern crate self as logis;

pub mod apps;

pub mod config;

pub mod icons;

pub mod input;

pub mod launch;

pub mod list;

pub mod motion;

pub mod render;

pub mod text;

pub mod theme;

pub mod window;
