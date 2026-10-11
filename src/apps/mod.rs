//! Desktop applications (spec §3 stories 5–7, §4.7): logis's own reading of
//! desktop entries (the Desktop Entry Specification 1.5), not GIO's.
//!
//! - [`keyfile`]: the key-file format, its escapes and lists;
//! - [`locale`]: which localised value suits the user's locale.

pub mod entry;
pub mod exec;
pub mod frecency;
pub mod index;
pub mod keyfile;
pub mod locale;
pub mod watch;

pub use locale::Locale;
