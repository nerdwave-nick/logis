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
mod motion;

use super::blur::Effects;
use super::clock::{FrameClock, Presentation};
use super::drawing::SceneFn;
use super::handle::WaylandDisplay;
use super::panel::Painter;
use super::paste::Clipboard;
use super::pointer::Pointer;
use super::seat::Keyboard;
use super::{Panel, Shell, WindowError, WindowQueue};
use crate::input::Action;
use crate::motion::{Motion, Transition};
use crate::render::{BackendChoice, Gpu, Renderer};
use crate::text::{Face, FontError, PendingFonts};

/// How soon a frame that got no swapchain image is tried again.
const RETRY: std::time::Duration = std::time::Duration::from_millis(4);

/// What the daemon owns for its windows: the connection, the bound globals,
/// the GPU (woken at connect, never lazily), the renderer, at most one
/// visible panel and at most one panel still closing.
pub struct Windows {
    // Drop order: the panels (and their wgpu surfaces) before the GPU they
    // were created on, then the globals, then the connection.
    pub(super) panel: Option<Panel>,
    closing: Option<Panel>,
    retired: Option<Panel>,
    /// What the first panel's surface offered and was chosen; later panels
    /// ask the compositor nothing before they are shown.
    negotiated: Option<super::present::Negotiated>,
    verbose: bool,
    motion: Motion,
    clock: FrameClock,
    blur: bool,
    /// When the owner wants the loop to wake ([`Self::set_alarm`]).
    pub(super) alarm: Option<std::time::Instant>,
    /// When to try a frame again that got no swapchain image.
    pub(super) retry_at: Option<std::time::Instant>,
    renderer: Renderer,
    gpu: Gpu,
    /// What the seat asked of the visible panel, oldest first.
    pub(super) actions: Vec<Action>,
    pub(super) clipboard: Clipboard,
    pub(super) pointer: Pointer,
    keyboard: Keyboard,
    pub(super) activation: super::activation::Activation,
    /// The icon theme's name and its cache, once a theme is set.
    icons: Option<(String, crate::icons::IconCache)>,
    effects: Option<Effects>,
    presentation: Option<Presentation>,
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
        faces: &[Face],
    ) -> Result<(Self, WindowQueue), WindowError> {
        Self::build(
            connection,
            choice,
            PendingFonts::system_with(faces.to_vec()),
        )
    }

    /// The faces texts name by index from now on (a theme's); the visible
    /// panel is drawn again with them.
    pub fn set_faces(&mut self, faces: &[Face]) -> Result<(), FontError> {
        let result = self.renderer.set_faces(faces);
        self.request_redraw();
        result
    }

    /// Asks the compositor for another size for the visible panel (its
    /// theme changed); it is drawn again once configured.
    pub fn resize(&mut self, size: (u32, u32)) {
        if let Some(panel) = &mut self.panel {
            panel.resize(size);
        }
    }

    /// Wakes the window's loop at `when` (or never), for its owner's own
    /// timers: [`Self::dispatch_or_wake`] returns by then.
    pub fn set_alarm(&mut self, when: Option<std::time::Instant>) {
        self.alarm = when;
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
        let pointer = Pointer::bind(&globals, &queue.handle());
        let clipboard = Clipboard::bind(&globals, &queue.handle(), keyboard.seat());
        let activation = super::activation::Activation::bind(&globals, &queue.handle());
        // Used when offered (spec §4.2): blur and presentation timing.
        let effects = globals
            .bind(&queue.handle(), 1..=1, ())
            .ok()
            .map(|manager| Effects { manager });
        let presentation =
            globals
                .bind(&queue.handle(), 1..=2, ())
                .ok()
                .map(|global| Presentation {
                    global,
                    monotonic: false,
                });
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
            closing: None,
            retired: None,
            negotiated: None,
            verbose: false,
            motion: Motion::OFF,
            clock: FrameClock::default(),
            blur: false,
            alarm: None,
            retry_at: None,
            renderer,
            gpu,
            actions: Vec::new(),
            clipboard,
            pointer,
            keyboard,
            activation,
            icons: None,
            effects,
            presentation,
            shell,
            connection,
        };
        Ok((windows, queue))
    }

    /// Looks icons up in theme `name` (then its parents, hicolor and the
    /// pixmaps). The theme's index files are read now, once per change.
    pub fn set_icon_theme(&mut self, name: &str) {
        if self.icons.as_ref().is_some_and(|(n, _)| n == name) {
            return;
        }
        let themes = crate::icons::IconThemes::load(&crate::icons::SearchPaths::from_env(), name);
        self.icons = Some((name.to_owned(), crate::icons::IconCache::new(themes)));
        self.request_redraw();
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
    /// visible one and cutting short any still closing. It draws once the
    /// compositor configures it, opening with the [`Motion`] in force.
    ///
    /// A panel still closing stops where it is and goes only after the new
    /// panel's first frame (trace `cut`): destroying it blocks on its
    /// swapchain and makes the compositor draw a frame, which would delay
    /// that first frame (the warm-open budget).
    pub fn show(
        &mut self,
        qh: &QueueHandle<Self>,
        size: (u32, u32),
        scene: SceneFn,
    ) -> Result<(), WindowError> {
        self.panel = None;
        if let Some(closing) = self.closing.take() {
            self.retired = Some(closing);
        }
        self.reset_input();
        let transition = if self.motion.open.duration.is_zero() {
            Transition::resting()
        } else {
            Transition::opening(self.motion.open_from, self.motion.open)
        };
        let effects = self.effects.as_ref().filter(|_| self.blur);
        let panel = Panel::new(
            &self.shell,
            &self.gpu,
            &self.connection,
            qh,
            size,
            scene,
            transition,
            effects,
            self.negotiated,
        );
        let panel = match panel {
            Ok(panel) => panel,
            Err(e) => {
                self.cut_retired();
                return Err(e);
            }
        };
        self.renderer.prepare(&self.gpu, panel.format());
        self.negotiated = Some(panel.negotiated());
        self.panel = Some(panel);
        Ok(())
    }

    /// Hides the visible panel at once, if any (no close transition).
    pub fn hide(&mut self) {
        self.panel = None;
        self.cut_retired();
        self.reset_input();
    }

    /// Nothing typed, pointed at or pasted carries over to another panel.
    fn reset_input(&mut self) {
        self.keyboard.reset();
        self.pointer.reset();
        self.clipboard.cancel();
        self.actions.clear();
    }

    /// The visible panel's size in logical pixels, once configured: the
    /// space pointer positions are in.
    pub fn size(&self) -> Option<(u32, u32)> {
        let panel = self.panel.as_ref()?;
        panel.geometry().map(|g| g.logical)
    }

    /// Lets the visible panel go of the keyboard: it stays in view, but
    /// keys go to whatever has focus beneath it (a toast left to read).
    pub fn release_keyboard(&mut self) {
        if let Some(panel) = &self.panel {
            panel.release_keyboard();
        }
        self.reset_input();
    }

    /// Whether a panel is shown.
    pub fn is_visible(&self) -> bool {
        self.panel.is_some()
    }

    /// Whether `surface` is the visible panel's.
    pub(super) fn shows(&self, surface: &wl_surface::WlSurface) -> bool {
        self.panel.as_ref().is_some_and(|p| p.has_surface(surface))
    }

    pub(super) fn keyboard(&self) -> &Keyboard {
        &self.keyboard
    }

    /// The visible panel and the closing one, whichever exist.
    pub(super) fn panels_mut(&mut self) -> impl Iterator<Item = &mut Panel> {
        self.panel.iter_mut().chain(self.closing.iter_mut())
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

    /// Draws the visible panel, and the one closing, if a frame is due.
    /// Call it after each dispatched batch of events: event handlers only
    /// update state, so a preferred scale and the configure that follows it
    /// in the same batch produce one frame, not two.
    pub fn draw_pending(&mut self, qh: &QueueHandle<Self>) {
        let mut painter = Painter {
            gpu: &self.gpu,
            renderer: &mut self.renderer,
            qh,
            compositor: &self.shell.compositor,
            presentation: self.presentation.as_ref().map(|p| &p.global),
            clock: &mut self.clock,
            icons: self.icons.as_mut().map(|(_, cache)| cache),
        };
        if let Some(closing) = &mut self.closing
            && closing.draw_if_needed(&mut painter).finished
        {
            self.closing = None;
            super::trace::emit(format_args!("faded"));
        }
        let Some(panel) = &mut self.panel else {
            return;
        };
        let drawn = panel.draw_if_needed(&mut painter);
        let reconfigured = drawn.reconfigured;
        // A frame that got no swapchain image is still due: wake again
        // shortly instead of whenever the next event comes.
        self.retry_at = drawn.retry.then(|| std::time::Instant::now() + RETRY);
        let shown = panel.was_shown();
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
        if shown {
            self.cut_retired();
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
