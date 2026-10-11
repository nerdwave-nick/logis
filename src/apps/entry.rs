//! One desktop entry of `Type=Application`, read from its key file: what the
//! launcher shows, matches, hides and runs (Desktop Entry Specification,
//! "Recognized desktop entry keys" and "Additional applications actions").
//!
//! - `Name` (required), `GenericName` and `Keywords` are localised;
//! - `Exec` is required unless `DBusActivatable=true`; an invalid one makes
//!   the entry invalid;
//! - `Hidden=true` means the entry was deleted: nothing else is required;
//! - actions named in `Actions` need their `[Desktop Action id]` group with
//!   a `Name`; an action without one, or with an invalid `Exec`, is skipped.

use std::fmt;

use super::exec::{Exec, ExecError};
use super::keyfile::{self, Group, Invalid};
use super::locale::Locale;

/// A desktop entry of an application.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct DesktopEntry {
    /// The translated `Name`.
    pub name: String,
    /// The translated `GenericName` ("Web Browser").
    pub generic_name: Option<String>,
    /// The translated `Keywords`.
    pub keywords: Vec<String>,
    /// `Icon`: a themed icon name or an absolute path.
    pub icon: Option<String>,
    /// `Exec`; `None` only for a D-Bus activatable or hidden entry.
    pub exec: Option<Exec>,
    /// `TryExec`: a program that must exist for the entry to show.
    pub try_exec: Option<String>,
    /// `Path`: the working directory.
    pub path: Option<String>,
    /// `Terminal`: run in a terminal.
    pub terminal: bool,
    /// `NoDisplay`: never shown in menus.
    pub no_display: bool,
    /// `Hidden`: deleted.
    pub hidden: bool,
    /// `OnlyShowIn`: the desktops it shows in (all when empty).
    pub only_show_in: Vec<String>,
    /// `NotShowIn`: the desktops it never shows in.
    pub not_show_in: Vec<String>,
    /// `DBusActivatable`: started through D-Bus (org.freedesktop.Application).
    pub dbus_activatable: bool,
    /// The desktop actions, in `Actions` order.
    pub actions: Vec<Action>,
}

/// An additional entry point of an application.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Action {
    /// Its identifier (in `Actions` and `[Desktop Action id]`).
    pub id: String,
    /// Its translated `Name`.
    pub name: String,
    /// Its `Icon`, if it has its own.
    pub icon: Option<String>,
    /// Its `Exec`; `None` only for a D-Bus activatable application.
    pub exec: Option<Exec>,
}

/// Why a desktop file is not an application entry logis can show.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum EntryError {
    /// The key file itself is malformed.
    KeyFile(Invalid),
    /// No `[Desktop Entry]` group.
    NoDesktopEntry,
    /// `Type` is not `Application` (a link or directory, or missing).
    NotAnApplication(String),
    /// No `Name`.
    NoName,
    /// No `Exec`, and not D-Bus activatable.
    NoExec,
    /// An `Exec` that cannot be run.
    Exec(ExecError),
}

impl fmt::Display for EntryError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::KeyFile(e) => write!(f, "malformed: {e}"),
            Self::NoDesktopEntry => write!(f, "no [Desktop Entry] group"),
            Self::NotAnApplication(kind) => write!(f, "Type={kind}, not Application"),
            Self::NoName => write!(f, "no Name"),
            Self::NoExec => write!(f, "no Exec and not DBusActivatable"),
            Self::Exec(e) => e.fmt(f),
        }
    }
}

impl std::error::Error for EntryError {}

/// Reads an application's desktop entry, localised for `locale`.
pub fn parse(text: &str, locale: &Locale) -> Result<DesktopEntry, EntryError> {
    let file = keyfile::parse(text).map_err(EntryError::KeyFile)?;
    let group = file
        .group("Desktop Entry")
        .ok_or(EntryError::NoDesktopEntry)?;
    if group.boolean("Hidden") {
        return Ok(DesktopEntry {
            hidden: true,
            ..DesktopEntry::default()
        });
    }
    let kind = group.string("Type").unwrap_or_default();
    if kind != "Application" {
        return Err(EntryError::NotAnApplication(kind));
    }
    let name = group
        .localized("Name", locale)
        .filter(|n| !n.is_empty())
        .ok_or(EntryError::NoName)?;
    let dbus_activatable = group.boolean("DBusActivatable");
    let exec = exec(group).transpose().map_err(EntryError::Exec)?;
    if exec.is_none() && !dbus_activatable {
        return Err(EntryError::NoExec);
    }
    let actions = group
        .list("Actions")
        .iter()
        .filter_map(|id| action(&file, id, locale, dbus_activatable))
        .collect();
    let nonempty = |v: Option<String>| v.filter(|s| !s.is_empty());
    Ok(DesktopEntry {
        name,
        generic_name: nonempty(group.localized("GenericName", locale)),
        keywords: group.localized_list("Keywords", locale),
        icon: nonempty(group.localized("Icon", locale)),
        exec,
        try_exec: nonempty(group.string("TryExec")),
        path: nonempty(group.string("Path")),
        terminal: group.boolean("Terminal"),
        no_display: group.boolean("NoDisplay"),
        hidden: false,
        only_show_in: group.list("OnlyShowIn"),
        not_show_in: group.list("NotShowIn"),
        dbus_activatable,
        actions,
    })
}

/// Action `id` of `file`, if it is complete and sound.
fn action(file: &keyfile::KeyFile, id: &str, locale: &Locale, dbus: bool) -> Option<Action> {
    let group = file.group(&format!("Desktop Action {id}"))?;
    let name = group.localized("Name", locale).filter(|n| !n.is_empty())?;
    let exec = exec(group).transpose().ok()?;
    if exec.is_none() && !dbus {
        return None;
    }
    Some(Action {
        id: id.to_owned(),
        name,
        icon: group.localized("Icon", locale).filter(|i| !i.is_empty()),
        exec,
    })
}

/// A group's `Exec`, if it has one.
fn exec(group: &Group) -> Option<Result<Exec, ExecError>> {
    group
        .string("Exec")
        .filter(|e| !e.trim().is_empty())
        .map(|e| Exec::parse(&e))
}

#[cfg(test)]
mod tests {
    use super::*;

    const FORGE: &str =
        include_str!("../../tests/fixtures/apps/system/applications/forge-anoint.desktop");

    fn de() -> Locale {
        Locale::parse("de_DE.UTF-8")
    }

    #[test]
    fn an_application_entry_is_read_with_its_localised_names() {
        let e = parse(FORGE, &de()).unwrap();
        assert_eq!(e.name, "Salbung");
        assert_eq!(e.generic_name.as_deref(), Some("Weihöl"));
        assert_eq!(e.keywords, ["öl", "salbe"]);
        assert_eq!(e.icon.as_deref(), Some("forge-cog"));
        assert_eq!(e.exec.as_ref().unwrap().program(), "anoint");
        assert!(!e.terminal && !e.no_display && !e.hidden && !e.dbus_activatable);
        let c = parse(FORGE, &Locale::default()).unwrap();
        assert_eq!((c.name.as_str(), c.keywords.len()), ("Anoint", 3));
    }

    #[test]
    fn actions_come_in_their_declared_order_and_bad_ones_are_skipped() {
        let e = parse(FORGE, &de()).unwrap();
        let actions: Vec<(&str, &str)> = e
            .actions
            .iter()
            .map(|a| (a.id.as_str(), a.name.as_str()))
            .collect();
        // `nameless` has no Name, `broken` an invalid Exec, `absent` no group.
        assert_eq!(actions, [("purge", "Läutern"), ("bless", "Segnen")]);
        assert_eq!(e.actions[0].icon.as_deref(), Some("forge-purge"));
        assert_eq!(e.actions[1].exec.as_ref().unwrap().program(), "anoint");
    }

    fn entry(body: &str) -> Result<DesktopEntry, EntryError> {
        parse(&format!("[Desktop Entry]\n{body}"), &Locale::default())
    }

    #[test]
    fn what_makes_an_entry_unusable() {
        assert_eq!(
            entry("Type=Link\nName=x\nURL=y"),
            Err(EntryError::NotAnApplication("Link".into()))
        );
        assert_eq!(
            entry("Name=x\nExec=y"),
            Err(EntryError::NotAnApplication(String::new()))
        );
        assert_eq!(entry("Type=Application\nExec=y"), Err(EntryError::NoName));
        assert_eq!(entry("Type=Application\nName=x"), Err(EntryError::NoExec));
        assert_eq!(
            entry("Type=Application\nName=x\nExec=y %q"),
            Err(EntryError::Exec(ExecError::UnknownCode('q')))
        );
        assert_eq!(
            parse("[Other]\nName=x", &Locale::default()),
            Err(EntryError::NoDesktopEntry)
        );
        assert!(matches!(
            parse("garbage", &Locale::default()),
            Err(EntryError::KeyFile(_))
        ));
    }

    #[test]
    fn a_hidden_entry_needs_nothing_else_and_dbus_needs_no_exec() {
        let hidden = entry("Hidden=true").unwrap();
        assert!(hidden.hidden);
        let dbus = entry("Type=Application\nName=Bus\nDBusActivatable=true\nActions=a\n\n[Desktop Action a]\nName=A").unwrap();
        assert!(dbus.dbus_activatable && dbus.exec.is_none());
        assert_eq!(dbus.actions.len(), 1, "a D-Bus action needs no Exec");
    }

    #[test]
    fn visibility_and_launch_keys_are_read() {
        let e = entry(
            "Type=Application\nName=x\nExec=y\nTryExec=/bin/forge\nPath=/srv\nTerminal=true\n\
             NoDisplay=true\nOnlyShowIn=niri;sway;\nNotShowIn=GNOME;",
        )
        .unwrap();
        assert_eq!(e.try_exec.as_deref(), Some("/bin/forge"));
        assert_eq!(e.path.as_deref(), Some("/srv"));
        assert!(e.terminal && e.no_display);
        assert_eq!(e.only_show_in, ["niri", "sway"]);
        assert_eq!(e.not_show_in, ["GNOME"]);
    }
}
