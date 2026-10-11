//! The harness's own backdrop: an opaque black layer surface on the
//! background layer, covering the whole output, so whatever a client draws
//! translucently is composited over the same black in every compositor
//! (sway's default output background is dark grey, niri's is configurable).
//! One black wl_shm pixel, stretched by wp_viewporter to the configured
//! size, so it is exact at any scale.

use std::os::fd::AsFd;

use smithay_client_toolkit::reexports::client::protocol::{
    wl_compositor::WlCompositor, wl_shm, wl_surface::WlSurface,
};
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, QueueHandle};
use smithay_client_toolkit::reexports::protocols::wp::viewporter::client::{
    wp_viewport::WpViewport, wp_viewporter::WpViewporter,
};
use smithay_client_toolkit::reexports::protocols_wlr::layer_shell::v1::client::{
    zwlr_layer_shell_v1::{self, ZwlrLayerShellV1},
    zwlr_layer_surface_v1::{self, ZwlrLayerSurfaceV1},
};

use super::wire::{Harness, State};

/// The namespace compositors see for the backdrop.
pub const NAMESPACE: &str = "logis-test-backdrop";

/// The backdrop's objects, kept alive with the harness.
pub struct Backdrop {
    surface: WlSurface,
    layer: ZwlrLayerSurfaceV1,
    viewport: WpViewport,
    buffer: smithay_client_toolkit::reexports::client::protocol::wl_buffer::WlBuffer,
    _pool: smithay_client_toolkit::reexports::client::protocol::wl_shm_pool::WlShmPool,
    /// The size of the last configure that was painted.
    pub(super) painted: Option<(u32, u32)>,
}

impl Harness {
    /// Puts the black backdrop under everything and waits until it is
    /// painted at the size the compositor configured.
    pub(super) fn paint_backdrop(
        &mut self,
        compositor: &WlCompositor,
        layer_shell: &ZwlrLayerShellV1,
        viewporter: &WpViewporter,
    ) {
        let qh = self.qh();
        let surface = compositor.create_surface(&qh, ());
        let layer = layer_shell.get_layer_surface(
            &surface,
            Some(&self.state.output),
            zwlr_layer_shell_v1::Layer::Background,
            NAMESPACE.into(),
            &qh,
            (),
        );
        use zwlr_layer_surface_v1::Anchor;
        layer.set_anchor(Anchor::Top | Anchor::Bottom | Anchor::Left | Anchor::Right);
        layer.set_size(0, 0);
        layer.set_exclusive_zone(-1);
        let viewport = viewporter.get_viewport(&surface, &qh, ());
        // One XRGB pixel of zeros: opaque black.
        let path = self.runtime_dir.join("backdrop.shm");
        std::fs::write(&path, [0u8; 4]).unwrap();
        // Read-write: compositors map shm pools with write access.
        let file = std::fs::OpenOptions::new()
            .read(true)
            .write(true)
            .open(&path)
            .unwrap();
        let pool = self.state.shm.create_pool(file.as_fd(), 4, &qh, ());
        let buffer = pool.create_buffer(0, 1, 1, 4, wl_shm::Format::Xrgb8888, &qh, ());
        surface.commit();
        self.state.backdrop = Some(Backdrop {
            surface,
            layer,
            viewport,
            buffer,
            _pool: pool,
            painted: None,
        });
        self.dispatch_until("the backdrop's configure", |s| {
            s.backdrop.as_ref().is_some_and(|b| b.painted.is_some())
        })
        .unwrap_or_else(|e| panic!("{e}"));
        self.roundtrip();
    }
}

impl Dispatch<ZwlrLayerSurfaceV1, ()> for State {
    fn event(
        state: &mut Self,
        layer: &ZwlrLayerSurfaceV1,
        event: zwlr_layer_surface_v1::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        let Some(backdrop) = state.backdrop.as_mut().filter(|b| &b.layer == layer) else {
            return;
        };
        match event {
            zwlr_layer_surface_v1::Event::Configure {
                serial,
                width,
                height,
            } => {
                layer.ack_configure(serial);
                if width > 0 && height > 0 {
                    backdrop
                        .viewport
                        .set_destination(width as i32, height as i32);
                }
                backdrop.surface.attach(Some(&backdrop.buffer), 0, 0);
                backdrop.surface.damage(0, 0, i32::MAX, i32::MAX);
                backdrop.surface.commit();
                backdrop.painted = Some((width, height));
            }
            zwlr_layer_surface_v1::Event::Closed => {
                panic!("the compositor closed the harness's backdrop")
            }
            _ => {}
        }
    }
}
