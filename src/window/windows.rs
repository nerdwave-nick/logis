//! The event-queue state: Wayland globals, the GPU and the visible panel.
//! Its sctk handlers ([`handlers`]) feed configure, scale and frame events
//! into the panel's state machine.

use smithay_client_toolkit::compositor::CompositorState;
use smithay_client_toolkit::output::OutputState;
use smithay_client_toolkit::reexports::client::globals::{GlobalList, registry_queue_init};
use smithay_client_toolkit::reexports::client::protocol::wl_surface;
use smithay_client_toolkit::reexports::client::{Connection, QueueHandle};
use smithay_client_toolkit::registry::RegistryState;
use smithay_client_toolkit::shell::wlr_layer::LayerShell;

mod handlers;

use super::handle::WaylandDisplay;
use super::seat::Keyboard;
use super::{Panel, SceneFn, Shell, WindowError, WindowQueue};
use crate::render::{BackendChoice, Gpu, Renderer};
use crate::text::PendingFonts;

/// What the daemon owns for its windows: the connection, the bound globals,
/// the GPU (woken at connect, never lazily), the renderer and at most one
/// visible panel.
pub struct Windows {
    // Drop order: the panel (and its wgpu surface) before the GPU it was
    // created on, then the globals, then the connection.
    panel: Option<Panel>,
    verbose: bool,
    renderer: Renderer,
    gpu: Gpu,
    keyboard: Keyboard,
    shell: Shell,
    connection: Connection,
}

impl Windows {
    /// Connects to the session's compositor, binds the required globals and
    /// wakes the GPU with `choice`.
    pub fn connect(choice: BackendChoice) -> Result<(Self, WindowQueue), WindowError> {
        // Fonts are resolved at start too (spec §4.1), on their own thread:
        // fontconfig's start-up then overlaps the compositor's and the GPU's.
        let fonts = PendingFonts::system();
        let connection =
            Connection::connect_to_env().map_err(|e| WindowError::Connect(e.to_string()))?;
        Self::build(connection, choice, fonts)
    }

    /// As [`Self::connect`], on a connection that is already open (the
    /// daemon's single compositor connection, from its preflight).
    pub fn with_connection(
        connection: Connection,
        choice: BackendChoice,
    ) -> Result<(Self, WindowQueue), WindowError> {
        Self::build(connection, choice, PendingFonts::system())
    }

    fn build(
        connection: Connection,
        choice: BackendChoice,
        fonts: PendingFonts,
    ) -> Result<(Self, WindowQueue), WindowError> {
        let (globals, queue) = registry_queue_init::<Self>(&connection)
            .map_err(|e| WindowError::Connect(e.to_string()))?;
        let shell = bind(&globals, &queue.handle())?;
        let keyboard = Keyboard::bind(&globals, &queue.handle());
        let gpu =
            Gpu::new(choice, Some(WaylandDisplay(connection.clone()))).map_err(WindowError::Gpu)?;
        let mut renderer = Renderer::new(&gpu);
        // Without fonts the cogitator still opens, only without text.
        match fonts.wait() {
            Ok(fonts) => renderer.set_fonts(&gpu, fonts),
            Err(e) => eprintln!("logis: {e}; the cogitator stays mute"),
        }
        // The usual Wayland swapchain format, so the first open compiles nothing.
        renderer.prepare(&gpu, wgpu::TextureFormat::Bgra8Unorm);
        let windows = Self {
            panel: None,
            verbose: false,
            renderer,
            gpu,
            keyboard,
            shell,
            connection,
        };
        Ok((windows, queue))
    }

    /// Reports every swapchain (re)configuration on stderr.
    pub fn set_verbose(&mut self, verbose: bool) {
        self.verbose = verbose;
    }

    /// The woken GPU.
    pub fn gpu(&self) -> &Gpu {
        &self.gpu
    }

    /// Shows a panel of `size` logical pixels drawing `scene`, replacing any
    /// visible one. It draws once the compositor configures it.
    pub fn show(
        &mut self,
        qh: &QueueHandle<Self>,
        size: (u32, u32),
        scene: SceneFn,
    ) -> Result<(), WindowError> {
        self.panel = None;
        self.keyboard.reset();
        let panel = Panel::new(&self.shell, &self.gpu, &self.connection, qh, size, scene)?;
        self.renderer.prepare(&self.gpu, panel.format());
        self.panel = Some(panel);
        Ok(())
    }

    /// Hides the visible panel, if any.
    pub fn hide(&mut self) {
        self.panel = None;
        self.keyboard.reset();
    }

    /// Lets the visible panel go of the keyboard: it stays in view, but
    /// keys go to whatever has focus beneath it (a toast left to read).
    pub fn release_keyboard(&mut self) {
        if let Some(panel) = &self.panel {
            panel.release_keyboard();
        }
        self.keyboard.reset();
    }

    /// Whether a panel is shown.
    pub fn is_visible(&self) -> bool {
        self.panel.is_some()
    }

    /// Whether `surface` is the visible panel's.
    pub(super) fn shows(&self, surface: &wl_surface::WlSurface) -> bool {
        self.panel.as_ref().is_some_and(|p| p.has_surface(surface))
    }

    pub(super) fn keyboard_mut(&mut self) -> &mut Keyboard {
        &mut self.keyboard
    }

    /// Asks the visible panel to redraw (its scene changed); it is drawn by
    /// the next [`Self::draw_pending`].
    pub fn request_redraw(&mut self) {
        if let Some(panel) = &mut self.panel {
            panel.state_mut().request_redraw();
        }
    }

    /// Draws the visible panel if a frame is due. Call it after each
    /// dispatched batch of events: event handlers only update state, so a
    /// preferred scale and the configure that follows it in the same batch
    /// produce one frame, not two.
    pub fn draw_pending(&mut self, qh: &QueueHandle<Self>) {
        let Some(panel) = &mut self.panel else { return };
        let reconfigured = panel.draw_if_needed(&self.gpu, &mut self.renderer, qh);
        if let Some(g) = reconfigured.filter(|_| self.verbose) {
            let config = panel.config();
            eprintln!(
                "logis: cogitator glass {}x{} px for {}x{} logical at scale {} ({:?}, {:?}, {:?})",
                g.physical.0,
                g.physical.1,
                g.logical.0,
                g.logical.1,
                g.scale.factor(),
                config.format,
                config.present_mode,
                config.alpha_mode,
            );
        }
    }
}

fn bind(globals: &GlobalList, qh: &QueueHandle<Windows>) -> Result<Shell, WindowError> {
    use WindowError::MissingGlobal;
    Ok(Shell {
        registry: RegistryState::new(globals),
        outputs: OutputState::new(globals, qh),
        compositor: CompositorState::bind(globals, qh)
            .map_err(|_| MissingGlobal("wl_compositor"))?,
        layer_shell: LayerShell::bind(globals, qh)
            .map_err(|_| MissingGlobal("zwlr_layer_shell_v1"))?,
        fractional: globals
            .bind(qh, 1..=1, ())
            .map_err(|_| MissingGlobal("wp_fractional_scale_manager_v1"))?,
        viewporter: globals
            .bind(qh, 1..=1, ())
            .map_err(|_| MissingGlobal("wp_viewporter"))?,
    })
}
