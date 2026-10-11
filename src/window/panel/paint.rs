//! Drawing one frame of a [`Panel`]: the scene at the frame's moment, in the
//! pose of its open or close transition, its blur region following the
//! body, a frame callback (and presentation feedback) requested, then
//! presented. Never waits on the GPU: frame callbacks pace drawing, and
//! Mailbox (or FIFO) presentation queues.

use smithay_client_toolkit::compositor::{CompositorState, FrameCallbackData};
use smithay_client_toolkit::reexports::client::QueueHandle;
use smithay_client_toolkit::reexports::protocols::wp::presentation_time::client::wp_presentation::WpPresentation;
use smithay_client_toolkit::shell::WaylandSurface;

use super::Panel;
use crate::render::{Gpu, Renderer, Target};
use crate::window::Windows;
use crate::window::blur::Backdrop;
use crate::window::clock::FrameTime;
use crate::window::drawing::{Drawing, Frame};
use crate::window::state::Geometry;
use crate::window::trace::emit;

/// What drawing a frame needs besides the panel.
pub(in crate::window) struct Painter<'a> {
    pub gpu: &'a Gpu,
    pub renderer: &'a mut Renderer,
    pub qh: &'a QueueHandle<Windows>,
    pub compositor: &'a CompositorState,
    pub presentation: Option<&'a WpPresentation>,
    pub time: FrameTime,
}

/// What [`Panel::draw_if_needed`] did.
#[derive(Clone, Copy, Debug, Default, PartialEq)]
pub(in crate::window) struct Drawn {
    /// The geometry the swapchain was reconfigured to, if it was.
    pub reconfigured: Option<Geometry>,
    /// A closing panel has finished: drop it, nothing was drawn.
    pub finished: bool,
}

impl Panel {
    /// Draws a frame if one is due. The test trace reports its commit as
    /// `commit` for the visible panel and `commit closing` for one closing,
    /// and `settled` after the first frame that leaves nothing moving.
    pub(in crate::window) fn draw_if_needed(&mut self, p: &mut Painter<'_>) -> Drawn {
        let closing = self.transition.is_closing();
        if self.transition.finished(p.time.at) {
            return Drawn {
                finished: true,
                ..Drawn::default()
            };
        }
        let mut drawn = Drawn::default();
        if !self.state.should_draw() {
            return drawn;
        }
        drawn.reconfigured = self.state.pending_reconfigure();
        if let Some(geometry) = drawn.reconfigured {
            self.config.width = geometry.physical.0;
            self.config.height = geometry.physical.1;
            self.surface.configure(&p.gpu.device, &self.config);
            let (width, height) = geometry.logical;
            self.scaling
                .viewport
                .set_destination(width as i32, height as i32);
            self.state.applied(geometry);
        }
        let Some(geometry) = self.state.geometry() else {
            return drawn;
        };
        let texture = match self.surface.get_current_texture() {
            wgpu::CurrentSurfaceTexture::Success(texture) => texture,
            wgpu::CurrentSurfaceTexture::Suboptimal(texture) => {
                self.state.invalidate();
                texture
            }
            wgpu::CurrentSurfaceTexture::Outdated | wgpu::CurrentSurfaceTexture::Lost => {
                self.state.invalidate();
                return drawn;
            }
            // Timeout, occluded or a validation error: try again on the next event.
            _ => return drawn,
        };
        let frame = Frame {
            logical: geometry.logical,
            at: p.time.at,
            interval: p.time.interval,
        };
        let Drawing {
            mut scene,
            backdrop,
            moving,
        } = (self.scene)(&frame);
        let pose = self.transition.pose(frame.at, frame.interval);
        pose.apply(&mut scene);
        if let (Some(blur), Some(b)) = (&mut self.blur, backdrop) {
            let rect = pose.place(b.rect);
            blur.follow(p.compositor, Backdrop { rect, ..b });
        }

        let view = texture
            .texture
            .create_view(&wgpu::TextureViewDescriptor::default());
        let target = Target {
            view: &view,
            format: self.config.format,
            size: geometry.physical,
            scale: geometry.scale,
        };
        let mut encoder = p
            .gpu
            .device
            .create_command_encoder(&wgpu::CommandEncoderDescriptor::default());
        p.renderer.encode(p.gpu, &mut encoder, &target, &scene);
        p.gpu.queue.submit([encoder.finish()]);
        let wl_surface = self.layer.wl_surface();
        wl_surface.frame(p.qh, FrameCallbackData(wl_surface.clone()));
        if let Some(presentation) = p.presentation {
            presentation.feedback(wl_surface, p.qh, ());
        }
        p.gpu.queue.present(texture);
        if closing {
            emit(format_args!("commit closing"));
        } else {
            emit(format_args!("commit"));
        }
        self.drawn_once = true;
        self.state.drew();
        let still_moving = moving || self.transition.moving(frame.at);
        self.state.set_animating(still_moving);
        if self.was_moving && !still_moving && !closing {
            emit(format_args!("settled"));
        }
        self.was_moving = still_moving;
        drawn
    }
}
