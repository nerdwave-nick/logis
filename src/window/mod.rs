//! Layer-shell windows (spec §4.2): the Wayland globals logis needs, the
//! event-queue state the daemon owns, and panels drawn by [`crate::render`].
//!
//! The daemon connects once ([`Windows::connect`]), which also wakes the GPU
//! at start, then shows and hides panels per session while dispatching its
//! [`EventQueue`].

mod debug;
mod handle;
mod panel;
mod present;
mod seat;
mod state;
pub mod trace;
mod windows;

use std::fmt;

use smithay_client_toolkit::compositor::CompositorState;
use smithay_client_toolkit::output::OutputState;
use smithay_client_toolkit::reexports::client::EventQueue;
use smithay_client_toolkit::reexports::protocols::wp::fractional_scale::v1::client::wp_fractional_scale_manager_v1::WpFractionalScaleManagerV1;
use smithay_client_toolkit::reexports::protocols::wp::viewporter::client::wp_viewporter::WpViewporter;
use smithay_client_toolkit::registry::RegistryState;
use smithay_client_toolkit::shell::wlr_layer::LayerShell;

pub use debug::{DEBUG_SIZE, Reaction, debug_scene, debug_scene_at, react, run_debug_panel};
pub use handle::{SurfaceHandle, WaylandDisplay};
pub use panel::{NAMESPACE, Panel, SceneFn};
pub use state::{Geometry, SurfaceState};
pub use windows::Windows;

/// The bound globals every panel is built from.
pub struct Shell {
    registry: RegistryState,
    outputs: OutputState,
    compositor: CompositorState,
    layer_shell: LayerShell,
    fractional: WpFractionalScaleManagerV1,
    viewporter: WpViewporter,
}

/// Why the cogitator could not be shown.
#[derive(Debug)]
pub enum WindowError {
    /// No compositor could be reached or greeted.
    Connect(String),
    /// A required global is missing; the name is the protocol interface.
    MissingGlobal(&'static str),
    /// The GPU could not be woken.
    Gpu(crate::render::GpuError),
    /// The wgpu surface could not be created or used.
    Surface(String),
    /// The compositor went away or broke the connection.
    Disconnected(String),
}

impl fmt::Display for WindowError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Connect(reason) => write!(
                f,
                "the noosphere is silent: no Wayland session answers ({reason})"
            ),
            Self::MissingGlobal(interface) => write!(
                f,
                "this compositor withholds a sacred rite: it does not offer {interface}"
            ),
            Self::Gpu(e) => e.fmt(f),
            Self::Surface(reason) => {
                write!(
                    f,
                    "the cogitator's glass would not take the image: {reason}"
                )
            }
            Self::Disconnected(reason) => {
                write!(f, "the noosphere fell silent mid-rite: {reason}")
            }
        }
    }
}

impl std::error::Error for WindowError {}

/// The event queue the daemon dispatches for its [`Windows`].
pub type WindowQueue = EventQueue<Windows>;
