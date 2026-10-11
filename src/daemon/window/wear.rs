//! The theme the window wears: shared with the drawing of the shown
//! session, so a theme the daemon applies reaches a visible window at once;
//! its faces in the renderer, its motion timings (unless `LOGIS_MOTION`
//! outranks them), and blur behind the body where both the config and the
//! theme want it. Also the timer of a notice the daemon shows over the
//! list (a refused config or theme edit).

use std::cell::RefCell;
use std::rc::Rc;
use std::sync::Arc;
use std::time::{Duration, Instant};

use logis::motion::Motion;
use logis::theme::Theme;
use logis::window::Windows;
use wayland_client::QueueHandle;

/// How long a notice from the daemon shows.
pub const NOTICE: Duration = Duration::from_secs(3);

/// The window's theme, as worn.
pub struct Wear {
    theme: Rc<RefCell<Arc<Theme>>>,
    /// `LOGIS_MOTION`, if set: it outranks the theme's timings.
    env_motion: Option<Motion>,
    /// The config's `blur`.
    blur: bool,
    /// When the daemon's notice goes.
    pub notice_until: Option<Instant>,
}

impl Wear {
    pub fn new(theme: Arc<Theme>, env_motion: Option<Motion>) -> Self {
        Self {
            theme: Rc::new(RefCell::new(theme)),
            env_motion,
            blur: false,
            notice_until: None,
        }
    }

    /// The theme worn now.
    pub fn theme(&self) -> Arc<Theme> {
        Arc::clone(&self.theme.borrow())
    }

    /// The cell a drawing reads the theme from each frame.
    pub fn cell(&self) -> Rc<RefCell<Arc<Theme>>> {
        Rc::clone(&self.theme)
    }

    /// The motion of panels shown in this theme.
    pub fn motion(&self) -> Motion {
        self.env_motion.unwrap_or_else(|| {
            let m = self.theme.borrow().motion;
            Motion::timed(m.open, m.close, m.select)
        })
    }

    /// Wears `theme` from now on, the visible panel included.
    pub fn put_on(&mut self, windows: &mut Windows, qh: &QueueHandle<Windows>, theme: Arc<Theme>) {
        if let Err(e) = windows.set_faces(&theme.faces) {
            crate::daemon::log(&format!("the theme's fonts fall back: {e}"));
        }
        *self.theme.borrow_mut() = theme;
        windows.set_motion(self.motion());
        self.blur(windows, qh, self.blur);
        windows.request_redraw();
    }

    /// The config's `blur`, joined with the theme's.
    pub fn blur(&mut self, windows: &mut Windows, qh: &QueueHandle<Windows>, blur: bool) {
        self.blur = blur;
        let theme = self.theme.borrow().panel.blur;
        windows.set_blur(blur && theme, qh);
    }
}
