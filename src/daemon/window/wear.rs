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

use logis::icons::IconSource;
use logis::list::scene::{self, Style};
use logis::list::{ListView, Notice, Options};
use logis::motion::{Glide, Motion};
use logis::render::Measure;
use logis::theme::Theme;
use logis::window::trace::emit;
use logis::window::{Backdrop, Drawing, Frame, SceneFn, Windows};
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
        emit(format_args!("theme {}", theme.name));
        // The faces were resolved at start for the first theme; fontconfig
        // is asked again only when they change.
        let same_faces = self.theme.borrow().faces == theme.faces;
        if !same_faces && let Err(e) = windows.set_faces(&theme.faces) {
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

    /// The shown list's size and rows in the theme worn now, from the size
    /// its session `asked` for: the panel is resized; returns the rows.
    pub fn refit(
        &self,
        windows: &mut Windows,
        (lines, width): (Option<u32>, Option<u32>),
        view: &RefCell<ListView>,
    ) -> u32 {
        let asked = Options {
            lines,
            width,
            ..Options::default()
        };
        let (size, rows) = scene::fit(&asked, &self.theme.borrow());
        view.borrow_mut().set_lines(rows);
        windows.resize(size);
        rows
    }

    /// Shows the daemon's `notice` in `toast` (the shown list's) for
    /// [`NOTICE`].
    pub fn notify(
        &mut self,
        windows: &mut Windows,
        toast: &RefCell<Option<Notice>>,
        notice: Notice,
    ) {
        let quoted = serde_json::to_string(&notice.text).unwrap_or_default();
        emit(format_args!("toast {quoted}"));
        toast.replace(Some(notice));
        let until = Instant::now() + NOTICE;
        self.notice_until = Some(until);
        windows.set_alarm(Some(until));
        windows.request_redraw();
    }

    /// Takes the daemon's notice away from `toast` once its time is up.
    pub fn expire(&mut self, windows: &mut Windows, toast: Option<&RefCell<Option<Notice>>>) {
        if self.notice_until.is_none_or(|until| Instant::now() < until) {
            return;
        }
        self.notice_until = None;
        windows.set_alarm(None);
        if let Some(toast) = toast {
            toast.replace(None);
            windows.request_redraw();
            emit(format_args!("untoast"));
        }
    }

    /// Draws the shown list each frame with its toast in the theme worn now:
    /// its columns measured as rows come into view, the selection band
    /// gliding to the selected row, the theme's continuous effects keeping
    /// it drawing, and the body blurred behind.
    pub fn drawing(
        &self,
        view: Rc<RefCell<ListView>>,
        toast: Rc<RefCell<Option<Notice>>>,
        glide: Rc<RefCell<Glide>>,
    ) -> SceneFn {
        let theme = self.cell();
        Box::new(
            move |frame: &Frame, icons: &mut dyn IconSource, measure: &mut dyn Measure| {
                let theme = Arc::clone(&theme.borrow());
                let mut look = view.borrow_mut().look_measured(&theme, measure);
                look.toast = toast.borrow().clone();
                let target = scene::band_top(&look, frame.logical, &theme);
                let (band, gliding) = glide.borrow_mut().toward(target, frame.at, frame.interval);
                let (rect, radius) = scene::body(frame.logical, &theme);
                let style = Style {
                    theme: &theme,
                    at: frame.at,
                };
                Drawing {
                    scene: scene::scene_at(&look, frame.logical, band, style, icons),
                    backdrop: Some(Backdrop { rect, radius }),
                    moving: gliding || scene::moving(&look, &theme),
                }
            },
        )
    }
}
