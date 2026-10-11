//! The Wayland preflight: is there a session, and does its compositor offer
//! wlr-layer-shell? (spec §4.2: a missing layer shell is a clear error.)

use std::fmt;
use std::os::unix::net::UnixStream;
use std::path::{Path, PathBuf};

use wayland_client::globals::{GlobalListContents, registry_queue_init};
use wayland_client::protocol::wl_registry::{self, WlRegistry};
use wayland_client::{Connection, Dispatch, EventQueue, QueueHandle};

/// The global a compositor must offer for logis to raise its window.
pub const LAYER_SHELL: &str = "zwlr_layer_shell_v1";

/// Resolves the compositor socket the way libwayland does, without its
/// `wayland-0` default: an unset `WAYLAND_DISPLAY` means there is no session.
pub fn socket_path(
    wayland_display: Option<&Path>,
    runtime_dir: Option<&Path>,
) -> Result<PathBuf, String> {
    let display = wayland_display
        .filter(|d| !d.as_os_str().is_empty())
        .ok_or("WAYLAND_DISPLAY is not set")?;
    if display.is_absolute() {
        return Ok(display.to_owned());
    }
    let runtime_dir = runtime_dir
        .filter(|d| d.is_absolute())
        .ok_or("XDG_RUNTIME_DIR is not set or not absolute")?;
    Ok(runtime_dir.join(display))
}

/// Whether a compositor advertising these global interfaces offers layer shell.
pub fn offers_layer_shell<'a>(interfaces: impl IntoIterator<Item = &'a str>) -> bool {
    interfaces.into_iter().any(|i| i == LAYER_SHELL)
}

/// Why the Wayland session cannot host the cogitator.
#[derive(Debug)]
pub enum PreflightError {
    /// No compositor could be reached or greeted; the reason says why.
    NoSession(String),
    /// The compositor answered but does not offer wlr-layer-shell.
    NoLayerShell,
}

impl fmt::Display for PreflightError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NoSession(reason) => write!(
                f,
                "the noosphere is silent: no Wayland session answers ({reason}). \
                 The cogitator manifests only within a Wayland session."
            ),
            Self::NoLayerShell => write!(
                f,
                "this compositor offers no altar: it does not support wlr-layer-shell \
                 ({LAYER_SHELL}). The cogitator rises only on compositors that do, \
                 such as niri or sway; GNOME/Mutter is not one of them."
            ),
        }
    }
}

/// Connects to the session's compositor and checks that it offers layer shell.
/// The connection stays open for as long as the returned [`Noosphere`] lives.
pub fn preflight() -> Result<Noosphere, PreflightError> {
    let conn = connect()?;
    let (globals, queue) = registry_queue_init::<Registry>(&conn).map_err(|e| {
        PreflightError::NoSession(format!("the compositor broke off the greeting: {e}"))
    })?;
    let offered = globals
        .contents()
        .with_list(|list| offers_layer_shell(list.iter().map(|g| g.interface.as_str())));
    if offered {
        Ok(Noosphere { _conn: conn, queue })
    } else {
        Err(PreflightError::NoLayerShell)
    }
}

/// A compositor connection that passed the preflight.
pub struct Noosphere {
    _conn: Connection,
    queue: EventQueue<Registry>,
}

impl Noosphere {
    /// Blocks until the compositor connection breaks; returns why.
    pub fn wait_for_loss(mut self) -> String {
        loop {
            if let Err(e) = self.queue.blocking_dispatch(&mut Registry) {
                return e.to_string();
            }
        }
    }
}

fn connect() -> Result<Connection, PreflightError> {
    use std::env::var_os;
    // A compositor that spawned us may hand over a connected socket instead.
    if var_os("WAYLAND_SOCKET").is_some() {
        return Connection::connect_to_env()
            .map_err(|e| PreflightError::NoSession(format!("WAYLAND_SOCKET: {e}")));
    }
    let display = var_os("WAYLAND_DISPLAY").map(PathBuf::from);
    let runtime_dir = var_os("XDG_RUNTIME_DIR").map(PathBuf::from);
    let path = socket_path(display.as_deref(), runtime_dir.as_deref())
        .map_err(PreflightError::NoSession)?;
    let stream = UnixStream::connect(&path)
        .map_err(|e| PreflightError::NoSession(format!("cannot reach {}: {e}", path.display())))?;
    Connection::from_socket(stream).map_err(|e| PreflightError::NoSession(e.to_string()))
}

/// Event-queue state for reading the registry; later events are not needed.
struct Registry;

impl Dispatch<WlRegistry, GlobalListContents> for Registry {
    fn event(
        _: &mut Self,
        _: &WlRegistry,
        _: wl_registry::Event,
        _: &GlobalListContents,
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn layer_shell_is_found_among_the_globals() {
        let globals = ["wl_compositor", "zwlr_layer_shell_v1", "wl_seat"];
        assert!(offers_layer_shell(globals));
    }

    #[test]
    fn globals_without_layer_shell_are_refused() {
        assert!(!offers_layer_shell([
            "wl_compositor",
            "xdg_wm_base",
            "wl_seat"
        ]));
        assert!(!offers_layer_shell([]));
    }

    #[test]
    fn a_similar_interface_name_is_not_layer_shell() {
        assert!(!offers_layer_shell([
            "zwlr_layer_shell_v1_extra",
            "wlr_layer_shell"
        ]));
    }

    #[test]
    fn a_relative_display_is_joined_to_the_runtime_dir() {
        let path = socket_path(
            Some(Path::new("wayland-1")),
            Some(Path::new("/run/user/1000")),
        );
        assert_eq!(path, Ok(PathBuf::from("/run/user/1000/wayland-1")));
    }

    #[test]
    fn an_absolute_display_is_used_as_is() {
        let path = socket_path(Some(Path::new("/tmp/wayland-x")), None);
        assert_eq!(path, Ok(PathBuf::from("/tmp/wayland-x")));
    }

    #[test]
    fn an_unset_or_empty_display_means_no_session() {
        let runtime = Some(Path::new("/run/user/1000"));
        assert_eq!(
            socket_path(None, runtime),
            Err("WAYLAND_DISPLAY is not set".into())
        );
        assert_eq!(
            socket_path(Some(Path::new("")), runtime),
            Err("WAYLAND_DISPLAY is not set".into())
        );
    }

    #[test]
    fn a_relative_display_needs_an_absolute_runtime_dir() {
        let display = Some(Path::new("wayland-1"));
        let unset = Err("XDG_RUNTIME_DIR is not set or not absolute".into());
        assert_eq!(socket_path(display, None), unset);
        assert_eq!(socket_path(display, Some(Path::new("run/user"))), unset);
    }
}
