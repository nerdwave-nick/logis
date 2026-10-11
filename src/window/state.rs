//! The pure part of a layer window: what size and scale it must render at
//! and when it may draw. Kept free of Wayland and GPU types so every
//! configure, scale change and frame callback sequence is unit-testable.

use crate::render::Scale;

/// The sizes one frame is rendered at.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Geometry {
    /// Surface size in logical pixels (the viewport destination).
    pub logical: (u32, u32),
    /// Buffer size in pixels.
    pub physical: (u32, u32),
    /// The scale relating them.
    pub scale: Scale,
}

/// Configure and scale state plus frame pacing for one surface.
#[derive(Debug)]
pub struct SurfaceState {
    requested: (u32, u32),
    configured: Option<(u32, u32)>,
    scale: Scale,
    applied: Option<Geometry>,
    dirty: bool,
    frame_pending: bool,
    animating: bool,
}

impl SurfaceState {
    /// A surface that asked the compositor for `requested` logical pixels.
    pub fn new(requested: (u32, u32)) -> Self {
        Self {
            requested,
            configured: None,
            scale: Scale::ONE,
            applied: None,
            dirty: true,
            frame_pending: false,
            animating: false,
        }
    }

    /// The surface asked for another size: a configure that leaves a side
    /// to the client gives this one, and a frame is due.
    pub fn request(&mut self, requested: (u32, u32)) {
        self.requested = requested;
        self.dirty = true;
    }

    /// A configure event. A zero side means "client's choice", so the
    /// requested side is used. Every configure asks for a redraw.
    pub fn configure(&mut self, (width, height): (u32, u32)) {
        let pick = |given: u32, requested: u32| if given == 0 { requested } else { given };
        self.configured = Some((
            pick(width, self.requested.0),
            pick(height, self.requested.1),
        ));
        self.dirty = true;
    }

    /// The compositor's preferred scale (wp_fractional_scale_v1).
    pub fn set_scale(&mut self, scale: Scale) {
        if scale != self.scale {
            self.scale = scale;
            self.dirty = true;
        }
    }

    /// What the next frame must be rendered at, once configured.
    pub fn geometry(&self) -> Option<Geometry> {
        self.configured.map(|logical| Geometry {
            logical,
            physical: self.scale.physical(logical),
            scale: self.scale,
        })
    }

    /// The geometry the swapchain must be reconfigured to before the next
    /// frame, or `None` if it already matches.
    pub fn pending_reconfigure(&self) -> Option<Geometry> {
        self.geometry().filter(|g| self.applied != Some(*g))
    }

    /// Records that the swapchain now matches `geometry`.
    pub fn applied(&mut self, geometry: Geometry) {
        self.applied = Some(geometry);
    }

    /// Forgets the swapchain's geometry (it was lost or outdated).
    pub fn invalidate(&mut self) {
        self.applied = None;
        self.dirty = true;
    }

    /// Asks for one more frame (content changed).
    pub fn request_redraw(&mut self) {
        self.dirty = true;
    }

    /// Keeps drawing on every frame callback while an animation runs.
    pub fn set_animating(&mut self, animating: bool) {
        self.animating = animating;
        self.dirty |= animating;
    }

    /// Whether a frame should be drawn now: configured, something changed,
    /// and the compositor is not still holding the previous frame.
    pub fn should_draw(&self) -> bool {
        self.configured.is_some() && self.dirty && !self.frame_pending
    }

    /// A frame was presented with a frame callback requested.
    pub fn drew(&mut self) {
        self.dirty = false;
        self.frame_pending = true;
    }

    /// The compositor's frame callback arrived.
    pub fn frame_done(&mut self) {
        self.frame_pending = false;
        self.dirty |= self.animating;
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn a_new_request_is_the_size_a_free_configure_gives() {
        let mut s = super::SurfaceState::new((100, 50));
        s.configure((0, 0));
        s.drew();
        s.request((300, 200));
        s.configure((0, 0));
        assert_eq!(s.geometry().unwrap().logical, (300, 200));
    }

    use super::*;

    const S13: u32 = 156;

    #[test]
    fn nothing_is_drawn_before_the_first_configure() {
        let state = SurfaceState::new((640, 360));
        assert!(!state.should_draw());
        assert_eq!(state.geometry(), None);
        assert_eq!(state.pending_reconfigure(), None);
    }

    #[test]
    fn the_first_configure_draws_at_the_configured_size() {
        let mut state = SurfaceState::new((640, 360));
        state.configure((700, 400));
        assert!(state.should_draw());
        let g = state.pending_reconfigure().unwrap();
        assert_eq!((g.logical, g.physical), ((700, 400), (700, 400)));
    }

    #[test]
    fn a_zero_side_falls_back_to_the_requested_side() {
        let mut state = SurfaceState::new((640, 360));
        state.configure((0, 500));
        assert_eq!(state.geometry().unwrap().logical, (640, 500));
    }

    #[test]
    fn a_scale_change_reconfigures_at_the_new_buffer_size() {
        let mut state = SurfaceState::new((640, 360));
        state.configure((640, 360));
        state.applied(state.pending_reconfigure().unwrap());
        state.drew();
        state.frame_done();
        assert!(!state.should_draw());

        state.set_scale(Scale::from_120ths(S13));
        assert!(state.should_draw());
        let g = state.pending_reconfigure().unwrap();
        assert_eq!(g.physical, (832, 468));
        assert_eq!(g.logical, (640, 360));
    }

    #[test]
    fn an_unchanged_scale_neither_redraws_nor_reconfigures() {
        let mut state = SurfaceState::new((640, 360));
        state.configure((640, 360));
        state.applied(state.pending_reconfigure().unwrap());
        state.drew();
        state.frame_done();
        state.set_scale(Scale::ONE);
        assert!(!state.should_draw());
        assert_eq!(state.pending_reconfigure(), None);
    }

    #[test]
    fn a_new_configure_after_an_output_change_leaves_no_stale_size() {
        let mut state = SurfaceState::new((640, 360));
        state.set_scale(Scale::from_integer(2));
        state.configure((640, 360));
        state.applied(state.pending_reconfigure().unwrap());
        // Moved to a 1.3 output that also resized the surface.
        state.set_scale(Scale::from_120ths(S13));
        state.configure((500, 300));
        let g = state.pending_reconfigure().unwrap();
        assert_eq!((g.logical, g.physical), ((500, 300), (650, 390)));
    }

    #[test]
    fn a_pending_frame_holds_back_the_next_draw() {
        let mut state = SurfaceState::new((640, 360));
        state.configure((640, 360));
        state.drew();
        state.request_redraw();
        assert!(!state.should_draw());
        state.frame_done();
        assert!(state.should_draw());
    }

    #[test]
    fn an_idle_surface_draws_nothing_after_its_frame() {
        let mut state = SurfaceState::new((640, 360));
        state.configure((640, 360));
        state.drew();
        state.frame_done();
        assert!(!state.should_draw());
    }

    #[test]
    fn an_animation_draws_on_every_frame_callback_until_it_stops() {
        let mut state = SurfaceState::new((640, 360));
        state.configure((640, 360));
        state.set_animating(true);
        state.drew();
        state.frame_done();
        assert!(state.should_draw());
        state.drew();
        state.set_animating(false);
        state.frame_done();
        assert!(!state.should_draw());
    }

    #[test]
    fn an_invalidated_swapchain_is_reconfigured_and_redrawn() {
        let mut state = SurfaceState::new((640, 360));
        state.configure((640, 360));
        state.applied(state.pending_reconfigure().unwrap());
        state.drew();
        state.frame_done();
        state.invalidate();
        assert!(state.should_draw());
        assert!(state.pending_reconfigure().is_some());
    }
}
