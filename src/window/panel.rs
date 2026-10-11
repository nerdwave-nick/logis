//! One layer surface and its swapchain: the visible panel, or one closing.

mod paint;

use smithay_client_toolkit::compositor::{CompositorState, Region};
use smithay_client_toolkit::reexports::client::protocol::wl_surface::WlSurface;
use smithay_client_toolkit::reexports::client::{Connection, QueueHandle};
use smithay_client_toolkit::reexports::protocols::wp::fractional_scale::v1::client::wp_fractional_scale_v1::WpFractionalScaleV1;
use smithay_client_toolkit::reexports::protocols::wp::viewporter::client::wp_viewport::WpViewport;
use smithay_client_toolkit::shell::WaylandSurface;
use smithay_client_toolkit::shell::wlr_layer::{KeyboardInteractivity, Layer, LayerSurface};

pub(super) use paint::Painter;

use super::blur::{Blur, Effects};
use super::drawing::SceneFn;
use super::handle::SurfaceHandle;
use super::present::{choose_alpha_mode, choose_format, choose_present_mode};
use super::state::{Geometry, SurfaceState};
use super::{Shell, WindowError, Windows};
use crate::motion::{Motion, Nanos, Transition};
use crate::render::{Gpu, Scale};

/// The layer-shell namespace compositors see (spec §4.2).
pub const NAMESPACE: &str = "logis";

/// The scaling objects of a surface, destroyed with it.
struct Scaling {
    fractional: WpFractionalScaleV1,
    viewport: WpViewport,
}

impl Drop for Scaling {
    fn drop(&mut self) {
        self.viewport.destroy();
        self.fractional.destroy();
    }
}

/// A shown cogitator window: an overlay layer surface with exclusive
/// keyboard focus, drawn by the GPU at the compositor's fractional scale.
///
/// Dropping it tears down the swapchain, then the blur and scaling
/// objects, then the layer surface; the wgpu surface owns its own
/// reference to the `wl_surface` (see [`super::handle`]), so the order
/// cannot be broken.
pub struct Panel {
    surface: wgpu::Surface<'static>,
    blur: Option<Blur>,
    scaling: Scaling,
    layer: LayerSurface,
    state: SurfaceState,
    config: wgpu::SurfaceConfiguration,
    scene: SceneFn,
    transition: Transition,
    drawn_once: bool,
    was_moving: bool,
}

impl Panel {
    /// Creates the layer surface (requesting `size` logical pixels) and its
    /// wgpu surface, and commits it so the compositor sends a configure.
    /// It opens with `transition`; `effects` blur behind it.
    #[allow(clippy::too_many_arguments)] // the parts of one window, once
    pub(super) fn new(
        shell: &Shell,
        gpu: &Gpu,
        connection: &Connection,
        qh: &QueueHandle<Windows>,
        size: (u32, u32),
        scene: SceneFn,
        transition: Transition,
        effects: Option<&Effects>,
    ) -> Result<Self, WindowError> {
        let wl_surface = shell.compositor.create_surface(qh);
        let scaling = Scaling {
            fractional: shell.fractional.get_fractional_scale(&wl_surface, qh, ()),
            viewport: shell.viewporter.get_viewport(&wl_surface, qh, ()),
        };
        let blur = effects.map(|e| Blur::new(e, &wl_surface, qh));
        let layer = shell.layer_shell.create_layer_surface(
            qh,
            wl_surface,
            Layer::Overlay,
            Some(NAMESPACE),
            None,
        );
        layer.set_size(size.0, size.1);
        layer.set_keyboard_interactivity(KeyboardInteractivity::Exclusive);
        layer.commit();

        let handle = SurfaceHandle::new(connection.clone(), layer.clone());
        let surface = gpu
            .instance
            .create_surface(wgpu::SurfaceTarget::DisplayAndWindow(Box::new(handle)))
            .map_err(|e| WindowError::Surface(e.to_string()))?;
        if !gpu.adapter.is_surface_supported(&surface) {
            return Err(WindowError::Surface(format!(
                "the adapter '{}' cannot present to this compositor",
                gpu.info().name
            )));
        }
        let caps = surface.get_capabilities(&gpu.adapter);
        let format = choose_format(&caps.formats)
            .ok_or_else(|| WindowError::Surface("the surface offers no format".into()))?;
        let config = wgpu::SurfaceConfiguration {
            usage: wgpu::TextureUsages::RENDER_ATTACHMENT,
            format,
            width: size.0,
            height: size.1,
            present_mode: choose_present_mode(&caps.present_modes),
            desired_maximum_frame_latency: 2,
            alpha_mode: choose_alpha_mode(&caps.alpha_modes),
            view_formats: Vec::new(),
            color_space: Default::default(),
        };
        Ok(Self {
            surface,
            blur,
            scaling,
            layer,
            state: SurfaceState::new(size),
            config,
            scene,
            transition,
            drawn_once: false,
            // So the first frame that leaves nothing moving reports it.
            was_moving: true,
        })
    }

    /// Whether `layer` is this panel's layer surface.
    pub(super) fn is(&self, layer: &LayerSurface) -> bool {
        self.layer.wl_surface() == layer.wl_surface()
    }

    /// Whether `fractional` is this panel's fractional-scale object.
    pub(super) fn has_fractional(&self, fractional: &WpFractionalScaleV1) -> bool {
        &self.scaling.fractional == fractional
    }

    /// Whether `surface` is this panel's `wl_surface`.
    pub(super) fn has_surface(&self, surface: &WlSurface) -> bool {
        self.layer.wl_surface() == surface
    }

    /// Gives up keyboard focus while staying on screen.
    pub(super) fn release_keyboard(&self) {
        self.layer
            .set_keyboard_interactivity(KeyboardInteractivity::None);
        self.layer.commit();
    }

    /// The swapchain format, for pipeline warm-up.
    pub fn format(&self) -> wgpu::TextureFormat {
        self.config.format
    }

    /// The configure/scale/pacing state machine.
    pub(super) fn state_mut(&mut self) -> &mut SurfaceState {
        &mut self.state
    }

    /// The compositor's preferred scale changed.
    pub(super) fn set_scale(&mut self, scale: Scale) {
        self.state.set_scale(scale);
    }

    /// What the next frame is rendered at, once configured.
    pub(super) fn geometry(&self) -> Option<Geometry> {
        self.state.geometry()
    }

    /// The negotiated swapchain configuration.
    pub fn config(&self) -> &wgpu::SurfaceConfiguration {
        &self.config
    }

    /// Turns blur behind the panel on (with `effects`) or off; the change
    /// reaches the compositor with the next frame.
    pub(super) fn set_blur(&mut self, effects: Option<&Effects>, qh: &QueueHandle<Windows>) {
        match (effects, &self.blur) {
            (Some(e), None) => self.blur = Some(Blur::new(e, self.layer.wl_surface(), qh)),
            (None, Some(_)) => self.blur = None,
            _ => return,
        }
        self.state.request_redraw();
    }

    /// Whether a frame of this panel was ever shown.
    pub(super) fn was_shown(&self) -> bool {
        self.drawn_once
    }

    /// Starts the close transition at `now`. The panel gives up keyboard
    /// focus and pointer input at once, so whatever comes next (the
    /// launched app, the next session) gets them; only its look lingers.
    pub(super) fn close(&mut self, motion: &Motion, now: Nanos, compositor: &CompositorState) {
        self.transition.close(motion.close_to, motion.close, now);
        self.layer
            .set_keyboard_interactivity(KeyboardInteractivity::None);
        if let Ok(nothing) = Region::new(compositor) {
            self.layer
                .wl_surface()
                .set_input_region(Some(nothing.wl_region()));
        }
        self.layer.commit();
        self.state.set_animating(true);
    }

    /// When a closing panel is done, if it is closing.
    pub(super) fn finish_time(&self) -> Option<Nanos> {
        self.transition.finish_time()
    }

    /// Whether a closing panel is done at `time`.
    pub(super) fn finished(&self, time: Nanos) -> bool {
        self.transition.finished(time)
    }
}
