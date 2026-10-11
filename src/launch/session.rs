//! The calls a launch makes on the session bus:
//!
//! - the systemd user manager's environment (`Manager.Environment`): the
//!   session environment the compositor and the session scripts imported;
//! - a transient scope for a launched process (`StartTransientUnit`), named
//!   by systemd's convention for applications
//!   (`app-<launcher>-<escaped app id>-<random>.scope`, in `app.slice`);
//! - `org.freedesktop.Application.Activate`/`ActivateAction` for a
//!   `DBusActivatable` application, with the activation token as platform
//!   data; whether the bus can start it at all is asked first.

use std::ffi::OsString;
use std::os::unix::ffi::OsStringExt;

use super::bus::{Bus, BusError};
use super::wire::{Call, Value};

const SYSTEMD: &str = "org.freedesktop.systemd1";
const SYSTEMD_PATH: &str = "/org/freedesktop/systemd1";
const MANAGER: &str = "org.freedesktop.systemd1.Manager";

fn s(text: &str) -> Value {
    Value::Str(text.into())
}

fn variant(value: Value) -> Value {
    Value::Variant(Box::new(value))
}

/// The user manager's environment, `NAME=VALUE` split.
pub fn environment(bus: &mut Bus) -> Result<Vec<(OsString, OsString)>, BusError> {
    let reply = bus.call(&Call {
        destination: SYSTEMD,
        path: SYSTEMD_PATH,
        interface: "org.freedesktop.DBus.Properties",
        member: "Get",
        body: vec![s(MANAGER), s("Environment")],
        flags: 0,
    })?;
    let unexpected = || BusError::Remote {
        name: "logis.UnexpectedReply".into(),
        message: "Environment is not a list of strings".into(),
    };
    let Some(Value::Variant(inner)) = reply.first() else {
        return Err(unexpected());
    };
    let Value::Array(_, items) = inner.as_ref() else {
        return Err(unexpected());
    };
    Ok(items
        .iter()
        .filter_map(Value::as_str)
        .filter_map(|pair| pair.split_once('='))
        .map(|(k, v)| (OsString::from(k), OsString::from(v)))
        .collect())
}

/// The scope unit for process `pid` of application `app_id`.
pub fn unit_name(app_id: &str, pid: u32) -> String {
    format!("app-logis-{}-{pid}.scope", escape(app_id))
}

/// systemd's unit-name escaping (`systemd-escape`): letters, digits, `:`,
/// `_` and `.` (not leading) stay, `/` becomes `-`, everything else `\xNN`.
pub fn escape(text: &str) -> String {
    let mut out = String::with_capacity(text.len());
    for (i, b) in text.bytes().enumerate() {
        match b {
            b'/' => out.push('-'),
            b'.' if i == 0 => out.push_str("\\x2e"),
            b if b.is_ascii_alphanumeric() || matches!(b, b':' | b'_' | b'.') => {
                out.push(b as char);
            }
            b => out.push_str(&format!("\\x{b:02x}")),
        }
    }
    out
}

/// Starts a transient scope holding process `pid`.
pub fn start_scope(bus: &mut Bus, unit: &str, pid: u32, description: &str) -> Result<(), BusError> {
    let property = |name: &str, value: Value| Value::Struct(vec![s(name), variant(value)]);
    bus.call(&Call {
        destination: SYSTEMD,
        path: SYSTEMD_PATH,
        interface: MANAGER,
        member: "StartTransientUnit",
        body: vec![
            s(unit),
            s("fail"),
            Value::Array(
                "(sv)".into(),
                vec![
                    property("PIDs", Value::Array("u".into(), vec![Value::U32(pid)])),
                    property("Slice", s("app.slice")),
                    property("Description", s(description)),
                    property("CollectMode", s("inactive-or-failed")),
                ],
            ),
            Value::Array("(sa(sv))".into(), Vec::new()),
        ],
        flags: 0,
    })
    .map(drop)
}

fn dbus_call<'a>(member: &'a str, body: Vec<Value>) -> Call<'a> {
    Call {
        destination: "org.freedesktop.DBus",
        path: "/org/freedesktop/DBus",
        interface: "org.freedesktop.DBus",
        member,
        body,
        flags: 0,
    }
}

/// Whether the bus can reach `name`: it runs, or the bus can start it.
pub fn activatable(bus: &mut Bus, name: &str) -> Result<bool, BusError> {
    let owned = bus.call(&dbus_call("NameHasOwner", vec![s(name)]))?;
    if owned.first() == Some(&Value::Bool(true)) {
        return Ok(true);
    }
    let names = bus.call(&dbus_call("ListActivatableNames", Vec::new()))?;
    Ok(matches!(names.first(), Some(Value::Array(_, all)) if all.contains(&s(name))))
}

/// The object path of application `app_id` (`org.gnome.Nautilus` →
/// `/org/gnome/Nautilus`; `-` becomes `_`).
pub fn object_path(app_id: &str) -> String {
    format!("/{}", app_id.replace('.', "/").replace('-', "_"))
}

/// Activates application `app_id` (or its `action`), waiting for its reply.
pub fn activate(
    bus: &mut Bus,
    app_id: &str,
    action: Option<&str>,
    token: Option<&str>,
) -> Result<(), BusError> {
    let data = token
        .map(|t| {
            ["activation-token", "desktop-startup-id"]
                .map(|key| Value::Entry(Box::new(s(key)), Box::new(variant(s(t)))))
                .to_vec()
        })
        .unwrap_or_default();
    let data = Value::Array("{sv}".into(), data);
    let (member, body) = match action {
        None => ("Activate", vec![data]),
        Some(action) => (
            "ActivateAction",
            vec![s(action), Value::Array("v".into(), Vec::new()), data],
        ),
    };
    let path = object_path(app_id);
    bus.call(&Call {
        destination: app_id,
        path: &path,
        interface: "org.freedesktop.Application",
        member,
        body,
        flags: 0,
    })
    .map(drop)
}

/// `NAME=VALUE` byte pairs as an environment.
pub fn environment_from_bytes(
    pairs: impl IntoIterator<Item = Vec<u8>>,
) -> Vec<(OsString, OsString)> {
    pairs
        .into_iter()
        .filter_map(|mut pair| {
            let eq = pair.iter().position(|&b| b == b'=')?;
            let value = pair.split_off(eq + 1);
            pair.pop();
            Some((OsString::from_vec(pair), OsString::from_vec(value)))
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unit_names_escape_the_application_id() {
        assert_eq!(
            unit_name("forge-anoint", 42),
            "app-logis-forge\\x2danoint-42.scope"
        );
        assert_eq!(
            unit_name("org.gnome.Nautilus", 7),
            "app-logis-org.gnome.Nautilus-7.scope"
        );
        assert_eq!(escape(".hidden/x y"), "\\x2ehidden-x\\x20y");
    }

    #[test]
    fn object_paths_follow_the_application_id() {
        assert_eq!(object_path("org.gnome.Nautilus"), "/org/gnome/Nautilus");
        assert_eq!(object_path("org.forge.holy-oil"), "/org/forge/holy_oil");
    }

    #[test]
    fn environment_bytes_split_at_the_first_equals_sign() {
        let env = environment_from_bytes([b"A=1=2".to_vec(), b"NOEQUALS".to_vec(), b"B=".to_vec()]);
        assert_eq!(env, [("A".into(), "1=2".into()), ("B".into(), "".into())]);
    }
}
