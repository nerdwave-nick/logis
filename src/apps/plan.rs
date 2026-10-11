//! What launching an application (or one of its desktop actions) runs: its
//! Exec with the field codes expanded, behind the configured terminal for
//! `Terminal=true`, in its `Path`; or, for `DBusActivatable=true`, an
//! activation the bus may perform (with the Exec, if any, to fall back on).

use std::fmt;

use super::exec::Fields;
use super::index::App;
use crate::launch::Plan;

/// Why an application cannot be launched as asked.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum PlanError {
    /// It has no such desktop action.
    NoAction(String),
    /// `Terminal=true`, and no terminal is configured.
    NoTerminal,
}

impl fmt::Display for PlanError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NoAction(id) => write!(f, "it knows no action '{id}'"),
            Self::NoTerminal => write!(
                f,
                "it must run in a terminal, and none is named (set \"terminal\" in the config, or $TERMINAL)"
            ),
        }
    }
}

impl std::error::Error for PlanError {}

/// The plan for `app`, or its `action`. `terminal` is the program and leading
/// arguments that run a command in a terminal (`["foot", "-e"]`), empty if
/// none is known.
pub fn plan(app: &App, action: Option<&str>, terminal: &[String]) -> Result<Plan, PlanError> {
    let entry = &app.entry;
    let chosen = action
        .map(|id| {
            entry
                .actions
                .iter()
                .find(|a| a.id == id)
                .ok_or_else(|| PlanError::NoAction(id.to_owned()))
        })
        .transpose()?;
    let (exec, icon, name) = match chosen {
        Some(a) => (
            a.exec.as_ref(),
            a.icon.as_deref().or(entry.icon.as_deref()),
            format!("{}: {}", entry.name, a.name),
        ),
        None => (
            entry.exec.as_ref(),
            entry.icon.as_deref(),
            entry.name.clone(),
        ),
    };
    let location = app.location.to_string_lossy();
    let fields = Fields {
        icon,
        name: &entry.name,
        location: &location,
    };
    let mut argv = exec.map(|e| e.expand(&fields)).unwrap_or_default();
    if entry.terminal && !argv.is_empty() {
        if terminal.is_empty() {
            return Err(PlanError::NoTerminal);
        }
        argv.splice(0..0, terminal.iter().cloned());
    }
    Ok(Plan {
        app_id: app.app_id().to_owned(),
        name,
        argv,
        cwd: entry.path.as_ref().map(Into::into),
        dbus: entry.dbus_activatable,
        action: action.map(str::to_owned),
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::apps::index::{Context, Dirs, Index};
    use crate::apps::locale::Locale;
    use std::path::{Path, PathBuf};

    fn index() -> Index {
        let fixtures = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/apps");
        let dirs = Dirs(vec![fixtures.join("system/applications")]);
        let cx = Context {
            locale: Locale::default(),
            desktops: vec!["niri".into()],
            path: vec![fixtures.join("bin")],
        };
        Index::scan(&dirs, &cx)
    }

    fn app<'a>(index: &'a Index, id: &str) -> &'a App {
        index.get(id).unwrap()
    }

    fn strings(words: &[&str]) -> Vec<String> {
        words.iter().map(|w| (*w).to_owned()).collect()
    }

    #[test]
    fn an_app_runs_its_exec_with_the_field_codes_expanded() {
        let index = index();
        let p = plan(app(&index, "forge-anoint.desktop"), None, &[]).unwrap();
        assert_eq!(
            p.argv,
            strings(&["anoint", "--vessel", "--name=Anoint", "--icon", "forge-cog"])
        );
        assert_eq!(
            (p.app_id.as_str(), p.name.as_str()),
            ("forge-anoint", "Anoint")
        );
        assert!(!p.dbus && p.action.is_none() && p.cwd.is_none());
    }

    #[test]
    fn an_action_runs_its_own_exec() {
        let index = index();
        let anoint = app(&index, "forge-anoint.desktop");
        let p = plan(anoint, Some("purge"), &[]).unwrap();
        assert_eq!(p.argv, strings(&["anoint", "--purge"]));
        assert_eq!(p.action.as_deref(), Some("purge"));
        assert_eq!(p.name, "Anoint: Purge");
        assert_eq!(
            plan(anoint, Some("absent"), &[]),
            Err(PlanError::NoAction("absent".into()))
        );
    }

    #[test]
    fn a_terminal_app_runs_behind_the_terminal() {
        let index = index();
        let shell = app(&index, "forge-terminal.desktop");
        let p = plan(shell, None, &strings(&["foot", "-e"])).unwrap();
        assert_eq!(p.argv, strings(&["foot", "-e", "anoint", "--terminal"]));
        assert_eq!(plan(shell, None, &[]), Err(PlanError::NoTerminal));
    }

    #[test]
    fn a_dbus_app_is_activated_by_its_id_and_its_working_dir_is_kept() {
        let index = index();
        let p = plan(app(&index, "org.forge.Bus.desktop"), Some("wake"), &[]).unwrap();
        assert!(p.dbus && p.argv.is_empty());
        assert_eq!(
            (p.app_id.as_str(), p.action.as_deref()),
            ("org.forge.Bus", Some("wake"))
        );
        let mut with_path = app(&index, "forge-anoint.desktop").clone();
        with_path.entry.path = Some("/srv/forge".into());
        let p = plan(&with_path, None, &[]).unwrap();
        assert_eq!(p.cwd, Some(PathBuf::from("/srv/forge")));
    }
}
