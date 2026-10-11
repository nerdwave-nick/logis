//! Launching desktop applications (spec §4.7): in the session environment,
//! into a transient systemd user scope where one is available, with an
//! xdg-activation token, detached so they outlive logis; `DBusActivatable`
//! applications through `org.freedesktop.Application`.
//!
//! - [`wire`]: the D-Bus wire format logis needs.

pub mod wire;
