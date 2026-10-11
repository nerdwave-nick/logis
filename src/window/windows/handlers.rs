//! The sctk handlers of [`Windows`]: configure, scale and frame events
//! update the visible panel's state machine; drawing happens in
//! [`Windows::draw_pending`], once per dispatched batch.

use smithay_client_toolkit::compositor::CompositorHandler;
use smithay_client_toolkit::output::{OutputHandler, OutputState};
use smithay_client_toolkit::reexports::client::protocol::{wl_output, wl_surface};
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, Proxy, QueueHandle};
use smithay_client_toolkit::reexports::protocols::ext::background_effect::v1::client::ext_background_effect_surface_v1::ExtBackgroundEffectSurfaceV1;
use smithay_client_toolkit::reexports::protocols::wp::fractional_scale::v1::client::{
    wp_fractional_scale_manager_v1::WpFractionalScaleManagerV1,
    wp_fractional_scale_v1::{self, WpFractionalScaleV1},
};
use smithay_client_toolkit::reexports::protocols::wp::viewporter::client::{
    wp_viewport::WpViewport, wp_viewporter::WpViewporter,
};
use smithay_client_toolkit::registry::{ProvidesRegistryState, RegistryState};
use smithay_client_toolkit::shell::wlr_layer::{
    LayerShellHandler, LayerSurface, LayerSurfaceConfigure,
};
use smithay_client_toolkit::{delegate_dispatch2, delegate_registry, registry_handlers};

use super::Windows;
use crate::render::Scale;

impl LayerShellHandler for Windows {
    fn closed(&mut self, _: &Connection, _: &QueueHandle<Self>, layer: &LayerSurface) {
        if self.panel.as_ref().is_some_and(|p| p.is(layer)) {
            self.panel = None;
        }
        if self.closing.as_ref().is_some_and(|p| p.is(layer)) {
            self.closing = None;
        }
        if self.retired.as_ref().is_some_and(|p| p.is(layer)) {
            self.retired = None;
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
        for panel in self.panels_mut().filter(|p| p.has_surface(surface)) {
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
no_events!(
    WpFractionalScaleManagerV1,
    WpViewporter,
    WpViewport,
    ExtBackgroundEffectSurfaceV1
);

delegate_registry!(Windows);
delegate_dispatch2!(Windows);

impl ProvidesRegistryState for Windows {
    fn registry(&mut self) -> &mut RegistryState {
        &mut self.shell.registry
    }
    registry_handlers![OutputState];
}
