//! One visible layer surface and its swapchain.

use smithay_client_toolkit::compositor::FrameCallbackData;
use smithay_client_toolkit::reexports::client::{Connection, QueueHandle};
use smithay_client_toolkit::reexports::protocols::wp::fractional_scale::v1::client::wp_fractional_scale_v1::WpFractionalScaleV1;
use smithay_client_toolkit::reexports::protocols::wp::viewporter::client::wp_viewport::WpViewport;
use smithay_client_toolkit::shell::WaylandSurface;
use smithay_client_toolkit::shell::wlr_layer::{KeyboardInteractivity, Layer, LayerSurface};

use super::handle::SurfaceHandle;
use super::present::{choose_alpha_mode, choose_format, choose_present_mode};
use super::state::{Geometry, SurfaceState};
use super::{Shell, WindowError, Windows};
use crate::render::{Gpu, Renderer, Scale, Scene, Target};

/// The layer-shell namespace compositors see (spec §4.2).
pub const NAMESPACE: &str = "logis";

/// Builds the scene for a logical surface size.
pub type SceneFn = Box<dyn Fn((u32, u32)) -> Scene>;

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
/// Dropping it tears down the swapchain, then the scaling objects, then the
/// layer surface; the wgpu surface owns its own reference to the
/// `wl_surface` (see [`super::handle`]), so the order cannot be broken.
pub struct Panel {
    surface: wgpu::Surface<'static>,
    scaling: Scaling,
    layer: LayerSurface,
    state: SurfaceState,
    config: wgpu::SurfaceConfiguration,
    scene: SceneFn,
}

impl Panel {
    /// Creates the layer surface (requesting `size` logical pixels) and its
    /// wgpu surface, and commits it so the compositor sends a configure.
    pub(super) fn new(
        shell: &Shell,
        gpu: &Gpu,
        connection: &Connection,
        qh: &QueueHandle<Windows>,
        size: (u32, u32),
        scene: SceneFn,
    ) -> Result<Self, WindowError> {
        let wl_surface = shell.compositor.create_surface(qh);
        let scaling = Scaling {
            fractional: shell.fractional.get_fractional_scale(&wl_surface, qh, ()),
            viewport: shell.viewporter.get_viewport(&wl_surface, qh, ()),
        };
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
            scaling,
            layer,
            state: SurfaceState::new(size),
            config,
            scene,
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
    pub(super) fn has_surface(
        &self,
        surface: &smithay_client_toolkit::reexports::client::protocol::wl_surface::WlSurface,
    ) -> bool {
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

    /// Draws a frame if one is due, and returns the geometry the swapchain
    /// was reconfigured to, if it was. Never waits on the GPU: the frame
    /// callback paces drawing, and Mailbox (or FIFO) presentation queues.
    pub(super) fn draw_if_needed(
        &mut self,
        gpu: &Gpu,
        renderer: &mut Renderer,
        qh: &QueueHandle<Windows>,
    ) -> Option<Geometry> {
        if !self.state.should_draw() {
            return None;
        }
        let reconfigured = self.state.pending_reconfigure();
        if let Some(geometry) = reconfigured {
            self.config.width = geometry.physical.0;
            self.config.height = geometry.physical.1;
            self.surface.configure(&gpu.device, &self.config);
            let (width, height) = geometry.logical;
            self.scaling
                .viewport
                .set_destination(width as i32, height as i32);
            self.state.applied(geometry);
        }
        let geometry = self.state.geometry()?;
        let frame = match self.surface.get_current_texture() {
            wgpu::CurrentSurfaceTexture::Success(frame) => frame,
            wgpu::CurrentSurfaceTexture::Suboptimal(frame) => {
                self.state.invalidate();
                frame
            }
            wgpu::CurrentSurfaceTexture::Outdated | wgpu::CurrentSurfaceTexture::Lost => {
                self.state.invalidate();
                return reconfigured;
            }
            // Timeout, occluded or a validation error: try again on the next event.
            _ => return reconfigured,
        };
        let view = frame
            .texture
            .create_view(&wgpu::TextureViewDescriptor::default());
        let target = Target {
            view: &view,
            format: self.config.format,
            size: geometry.physical,
            scale: geometry.scale,
        };
        let scene = (self.scene)(geometry.logical);
        let mut encoder = gpu
            .device
            .create_command_encoder(&wgpu::CommandEncoderDescriptor::default());
        renderer.encode(gpu, &mut encoder, &target, &scene);
        gpu.queue.submit([encoder.finish()]);
        let wl_surface = self.layer.wl_surface();
        wl_surface.frame(qh, FrameCallbackData(wl_surface.clone()));
        gpu.queue.present(frame);
        super::trace::emit(format_args!("commit"));
        self.state.drew();
        reconfigured
    }
}
