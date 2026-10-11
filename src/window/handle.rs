//! Raw-handle glue between wayland-client and wgpu, with the destruction
//! order encoded in ownership rather than in field order (stack.md §7).
//!
//! A wgpu surface is created from a [`SurfaceHandle`] that it takes by
//! value. wgpu keeps that value until after its own native surface is gone
//! (`Surface::_handle_source` drops last), and the handle owns a clone of the
//! [`LayerSurface`] — whose last clone destroys the `wl_surface` — and of the
//! [`Connection`] that keeps the `wl_display` alive. So no field order, drop
//! call or refactoring can destroy the `wl_surface` or the display while a
//! wgpu surface still references them.

use std::ptr::NonNull;

use smithay_client_toolkit::reexports::client::{Connection, Proxy};
use smithay_client_toolkit::shell::WaylandSurface;
use smithay_client_toolkit::shell::wlr_layer::LayerSurface;
use wgpu::rwh::{
    DisplayHandle, HandleError, HasDisplayHandle, HasWindowHandle, RawDisplayHandle,
    RawWindowHandle, WaylandDisplayHandle, WaylandWindowHandle, WindowHandle,
};

/// The Wayland display as wgpu's instance display (the GL backend needs it).
#[derive(Clone, Debug)]
pub struct WaylandDisplay(pub Connection);

impl HasDisplayHandle for WaylandDisplay {
    fn display_handle(&self) -> Result<DisplayHandle<'_>, HandleError> {
        display_handle(&self.0)
    }
}

/// What a wgpu surface for one layer surface owns: see the module docs.
#[derive(Debug)]
pub struct SurfaceHandle {
    connection: Connection,
    layer: LayerSurface,
}

impl SurfaceHandle {
    /// A handle keeping `layer`'s `wl_surface` and the display alive.
    pub fn new(connection: Connection, layer: LayerSurface) -> Self {
        Self { connection, layer }
    }
}

impl HasDisplayHandle for SurfaceHandle {
    fn display_handle(&self) -> Result<DisplayHandle<'_>, HandleError> {
        display_handle(&self.connection)
    }
}

impl HasWindowHandle for SurfaceHandle {
    fn window_handle(&self) -> Result<WindowHandle<'_>, HandleError> {
        let surface = self.layer.wl_surface().id().as_ptr();
        let surface = NonNull::new(surface.cast()).ok_or(HandleError::Unavailable)?;
        let raw = RawWindowHandle::Wayland(WaylandWindowHandle::new(surface));
        // SAFETY: the pointer is the live wl_surface proxy of `self.layer`,
        // which this handle owns a clone of; it stays valid for as long as
        // the returned borrow of `self`.
        Ok(unsafe { WindowHandle::borrow_raw(raw) })
    }
}

fn display_handle(connection: &Connection) -> Result<DisplayHandle<'_>, HandleError> {
    let display = connection.backend().display_ptr();
    let display = NonNull::new(display.cast()).ok_or(HandleError::Unavailable)?;
    let raw = RawDisplayHandle::Wayland(WaylandDisplayHandle::new(display));
    // SAFETY: with the system backend this is libwayland's wl_display, kept
    // alive by `connection`, which outlives the returned borrow.
    Ok(unsafe { DisplayHandle::borrow_raw(raw) })
}
