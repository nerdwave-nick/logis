//! The event-queue state: Wayland globals, the GPU and the visible panel,
//! with the sctk handlers that feed configure, scale and frame events into
//! the panel's state machine.

use smithay_client_toolkit::compositor::{CompositorHandler, CompositorState};
use smithay_client_toolkit::output::{OutputHandler, OutputState};
use smithay_client_toolkit::reexports::client::globals::{GlobalList, registry_queue_init};
use smithay_client_toolkit::reexports::client::protocol::{wl_output, wl_surface};
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, Proxy, QueueHandle};
use smithay_client_toolkit::reexports::protocols::wp::fractional_scale::v1::client::{
    wp_fractional_scale_manager_v1::WpFractionalScaleManagerV1,
    wp_fractional_scale_v1::{self, WpFractionalScaleV1},
};
use smithay_client_toolkit::reexports::protocols::wp::viewporter::client::{
    wp_viewport::WpViewport, wp_viewporter::WpViewporter,
};
use smithay_client_toolkit::registry::{ProvidesRegistryState, RegistryState};
use smithay_client_toolkit::shell::wlr_layer::{
    LayerShell, LayerShellHandler, LayerSurface, LayerSurfaceConfigure,
};
use smithay_client_toolkit::{delegate_dispatch2, delegate_registry, registry_handlers};

use super::handle::WaylandDisplay;
use super::seat::Keyboard;
use super::{Panel, SceneFn, Shell, WindowError, WindowQueue};
use crate::render::{BackendChoice, Gpu, Renderer, Scale};
use crate::text::Fonts;

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
        let connection =
            Connection::connect_to_env().map_err(|e| WindowError::Connect(e.to_string()))?;
        let (globals, queue) = registry_queue_init::<Self>(&connection)
            .map_err(|e| WindowError::Connect(e.to_string()))?;
        let shell = bind(&globals, &queue.handle())?;
        let keyboard = Keyboard::bind(&globals, &queue.handle());
        let gpu =
            Gpu::new(choice, Some(WaylandDisplay(connection.clone()))).map_err(WindowError::Gpu)?;
        let mut renderer = Renderer::new(&gpu);
        // Fonts are resolved at start too (spec §4.1); without them the
        // cogitator still opens, only without text.
        match Fonts::system() {
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
        let panel = Panel::new(&self.shell, &self.gpu, &self.connection, qh, size, scene)?;
        self.renderer.prepare(&self.gpu, panel.format());
        self.panel = Some(panel);
        Ok(())
    }

    /// Hides the visible panel, if any.
    pub fn hide(&mut self) {
        self.panel = None;
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

    /// The evdev codes of keys pressed since the last call, oldest first
    /// (raw codes until the keyboard slice brings keysyms).
    pub fn take_key_presses(&mut self) -> Vec<u32> {
        self.keyboard.take_presses()
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

impl LayerShellHandler for Windows {
    fn closed(&mut self, _: &Connection, _: &QueueHandle<Self>, layer: &LayerSurface) {
        if self.panel.as_ref().is_some_and(|p| p.is(layer)) {
            self.panel = None;
        }
    }

    fn configure(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        layer: &LayerSurface,
        configure: LayerSurfaceConfigure,
        _: u32,
    ) {
        if let Some(panel) = self.panel.as_mut().filter(|p| p.is(layer)) {
            panel.state_mut().configure(configure.new_size);
        }
    }
}

impl CompositorHandler for Windows {
    fn frame(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        surface: &wl_surface::WlSurface,
        _: u32,
    ) {
        if let Some(panel) = self.panel.as_mut().filter(|p| p.has_surface(surface)) {
            panel.state_mut().frame_done();
        }
    }

    // Integer scale and transform are superseded by wp_fractional_scale_v1
    // and the viewport; entering or leaving outputs changes nothing by itself.
    fn scale_factor_changed(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        _: &wl_surface::WlSurface,
        _: i32,
    ) {
    }
    fn transform_changed(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        _: &wl_surface::WlSurface,
        _: wl_output::Transform,
    ) {
    }
    fn surface_enter(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        _: &wl_surface::WlSurface,
        _: &wl_output::WlOutput,
    ) {
    }
    fn surface_leave(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        _: &wl_surface::WlSurface,
        _: &wl_output::WlOutput,
    ) {
    }
}

/// Output changes reach a panel as a new preferred scale, a new configure or
/// `closed`; the output list itself needs no reaction.
impl OutputHandler for Windows {
    fn output_state(&mut self) -> &mut OutputState {
        &mut self.shell.outputs
    }
    fn new_output(&mut self, _: &Connection, _: &QueueHandle<Self>, _: wl_output::WlOutput) {}
    fn update_output(&mut self, _: &Connection, _: &QueueHandle<Self>, _: wl_output::WlOutput) {}
    fn output_destroyed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: wl_output::WlOutput) {}
}

impl Dispatch<WpFractionalScaleV1, ()> for Windows {
    fn event(
        windows: &mut Self,
        fractional: &WpFractionalScaleV1,
        event: wp_fractional_scale_v1::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        if let wp_fractional_scale_v1::Event::PreferredScale { scale } = event
            && let Some(panel) = windows
                .panel
                .as_mut()
                .filter(|p| p.has_fractional(fractional))
        {
            panel.set_scale(Scale::from_120ths(scale));
        }
    }
}

/// Globals and objects without events logis cares about.
macro_rules! no_events {
    ($($proxy:ty),*) => {$(
        impl Dispatch<$proxy, ()> for Windows {
            fn event(_: &mut Self, _: &$proxy, _: <$proxy as Proxy>::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
        }
    )*};
}
no_events!(WpFractionalScaleManagerV1, WpViewporter, WpViewport);

delegate_registry!(Windows);
delegate_dispatch2!(Windows);

impl ProvidesRegistryState for Windows {
    fn registry(&mut self) -> &mut RegistryState {
        &mut self.shell.registry
    }
    registry_handlers![OutputState];
}
