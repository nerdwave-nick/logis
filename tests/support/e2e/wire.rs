//! The harness's own Wayland connection to a [`TestCompositor`]: the
//! isolation guard, the globals the injector and screen capture need, and
//! dispatching with a deadline.

use std::os::unix::net::UnixStream;
use std::time::{Duration, Instant};

use smithay_client_toolkit::reexports::client::globals::{GlobalListContents, registry_queue_init};
use smithay_client_toolkit::reexports::client::protocol::{
    wl_buffer::WlBuffer, wl_output::{self, WlOutput}, wl_registry::WlRegistry, wl_seat::WlSeat,
    wl_shm::WlShm, wl_shm_pool::WlShmPool,
};
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, EventQueue, Proxy, QueueHandle};
use smithay_client_toolkit::reexports::protocols_misc::zwp_virtual_keyboard_v1::client::{
    zwp_virtual_keyboard_manager_v1::ZwpVirtualKeyboardManagerV1,
    zwp_virtual_keyboard_v1::ZwpVirtualKeyboardV1,
};
use smithay_client_toolkit::reexports::client::protocol::{
    wl_compositor::WlCompositor, wl_surface::WlSurface,
};
use smithay_client_toolkit::reexports::protocols::wp::viewporter::client::{
    wp_viewport::WpViewport, wp_viewporter::WpViewporter,
};
use smithay_client_toolkit::reexports::protocols_wlr::layer_shell::v1::client::zwlr_layer_shell_v1::ZwlrLayerShellV1;
use smithay_client_toolkit::reexports::protocols_wlr::screencopy::v1::client::zwlr_screencopy_manager_v1::ZwlrScreencopyManagerV1;

use super::{PATIENCE, TestCompositor};

/// Refuses a socket whose server is not process `expected`: the isolation
/// guard. The kernel's peer credentials of a Unix socket name the process
/// that listens on it, so a connection to the live session (or to anything
/// but the compositor the harness spawned) is refused before a single
/// request is sent.
pub fn verify_peer(stream: &UnixStream, expected: u32) -> Result<(), String> {
    let peer = rustix::net::sockopt::socket_peercred(stream)
        .map_err(|e| format!("cannot read the socket's peer credentials: {e}"))?;
    let pid = peer.pid.as_raw_nonzero().get() as u32;
    if pid == expected {
        Ok(())
    } else {
        Err(format!(
            "refusing to inject: the socket is served by process {pid}, not by the test \
             compositor {expected} the harness started"
        ))
    }
}

/// Event-queue state of the harness connection.
pub struct State {
    pub(super) output: WlOutput,
    /// The output's transform: screencopy delivers buffer contents, which
    /// a capture must turn upright (niri's nested output is flipped).
    pub(super) transform: wl_output::Transform,
    /// The current mode's refresh rate in mHz, once announced.
    pub(super) refresh_mhz: i32,
    pub(super) shm: WlShm,
    pub(super) seat: WlSeat,
    pub(super) screencopy: ZwlrScreencopyManagerV1,
    pub(super) keyboards: ZwpVirtualKeyboardManagerV1,
    pub(super) capture: super::screencopy::Capture,
    pub(super) backdrop: Option<super::backdrop::Backdrop>,
}

/// A connection to a test compositor and nothing else.
pub struct Harness {
    pub(super) connection: Connection,
    pub(super) queue: EventQueue<State>,
    pub(super) state: State,
    pub(super) runtime_dir: std::path::PathBuf,
}

impl Harness {
    /// Connects to `compositor`'s socket, after [`verify_peer`] proved its
    /// server is the compositor process (panics otherwise), and paints the
    /// black backdrop (see [`super::backdrop`]) so captures never depend on
    /// the compositor's own background.
    pub fn connect(compositor: &TestCompositor) -> Self {
        let stream = UnixStream::connect(compositor.socket()).unwrap_or_else(|e| {
            panic!(
                "cannot reach the test compositor at {}: {e}",
                compositor.socket().display()
            )
        });
        verify_peer(&stream, compositor.pid()).unwrap_or_else(|e| panic!("{e}"));
        let connection = Connection::from_socket(stream).expect("libwayland-client connects");
        let (globals, queue) =
            registry_queue_init::<State>(&connection).expect("the compositor lists its globals");
        let qh = queue.handle();
        let state = State {
            output: globals
                .bind(&qh, 1..=4, ())
                .unwrap_or_else(|_| missing("wl_output")),
            transform: wl_output::Transform::Normal,
            refresh_mhz: 0,
            shm: globals
                .bind(&qh, 1..=1, ())
                .unwrap_or_else(|_| missing("wl_shm")),
            seat: globals
                .bind(&qh, 1..=7, ())
                .unwrap_or_else(|_| missing("wl_seat")),
            screencopy: globals
                .bind(&qh, 1..=3, ())
                .unwrap_or_else(|_| missing("zwlr_screencopy_manager_v1")),
            keyboards: globals
                .bind(&qh, 1..=1, ())
                .unwrap_or_else(|_| missing("zwp_virtual_keyboard_manager_v1")),
            capture: Default::default(),
            backdrop: None,
        };
        let wl_compositor: WlCompositor = globals
            .bind(&qh, 1..=4, ())
            .unwrap_or_else(|_| missing("wl_compositor"));
        let layer_shell: ZwlrLayerShellV1 = globals
            .bind(&qh, 1..=4, ())
            .unwrap_or_else(|_| missing("zwlr_layer_shell_v1"));
        let viewporter: WpViewporter = globals
            .bind(&qh, 1..=1, ())
            .unwrap_or_else(|_| missing("wp_viewporter"));
        let mut harness = Self {
            connection,
            queue,
            state,
            runtime_dir: compositor.runtime_dir().to_owned(),
        };
        harness.roundtrip();
        harness.paint_backdrop(&wl_compositor, &layer_shell, &viewporter);
        harness
    }

    /// The output's refresh rate (60 Hz if it announces none).
    pub fn refresh_hz(&self) -> f64 {
        match self.state.refresh_mhz {
            mhz if mhz > 0 => f64::from(mhz) / 1000.0,
            _ => 60.0,
        }
    }

    /// The queue handle for new objects.
    pub(super) fn qh(&self) -> QueueHandle<State> {
        self.queue.handle()
    }

    /// Sends everything and waits until the compositor has handled it.
    pub fn roundtrip(&mut self) {
        self.queue
            .roundtrip(&mut self.state)
            .expect("the test compositor answers");
    }

    /// Dispatches events until `done` holds, for at most [`PATIENCE`].
    pub(super) fn dispatch_until(
        &mut self,
        what: &str,
        done: impl Fn(&State) -> bool,
    ) -> Result<(), String> {
        let deadline = Instant::now() + PATIENCE;
        loop {
            self.queue
                .dispatch_pending(&mut self.state)
                .map_err(|e| format!("{what}: {e}"))?;
            if done(&self.state) {
                return Ok(());
            }
            self.queue.flush().map_err(|e| format!("{what}: {e}"))?;
            let left = deadline.saturating_duration_since(Instant::now());
            if left.is_zero() {
                return Err(format!("{what}: nothing after {PATIENCE:?}"));
            }
            let Some(guard) = self.queue.prepare_read() else {
                continue;
            };
            if wait_readable(&guard.connection_fd(), left)? {
                // WouldBlock means another reader took the events; retry.
                let _ = guard.read();
            }
        }
    }
}

fn missing<T>(global: &str) -> T {
    panic!("the test compositor does not offer {global}")
}

fn wait_readable(fd: &impl std::os::fd::AsFd, timeout: Duration) -> Result<bool, String> {
    use rustix::event::{PollFd, PollFlags, Timespec, poll};
    let mut fds = [PollFd::new(fd, PollFlags::IN)];
    let timeout = Timespec {
        tv_sec: timeout.as_secs() as i64,
        tv_nsec: i64::from(timeout.subsec_nanos()),
    };
    match poll(&mut fds, Some(&timeout)) {
        Ok(n) => Ok(n > 0),
        Err(rustix::io::Errno::INTR) => Ok(false),
        Err(e) => Err(format!("poll: {e}")),
    }
}

impl Dispatch<WlRegistry, GlobalListContents> for State {
    fn event(
        _: &mut Self,
        _: &WlRegistry,
        _: <WlRegistry as Proxy>::Event,
        _: &GlobalListContents,
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }
}

/// Objects whose events the harness does not need.
macro_rules! no_events {
    ($($proxy:ty),*) => {$(
        impl Dispatch<$proxy, ()> for State {
            fn event(_: &mut Self, _: &$proxy, _: <$proxy as Proxy>::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
        }
    )*};
}
impl Dispatch<WlOutput, ()> for State {
    fn event(
        state: &mut Self,
        _: &WlOutput,
        event: wl_output::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        use smithay_client_toolkit::reexports::client::WEnum::Value;
        match event {
            wl_output::Event::Geometry {
                transform: Value(transform),
                ..
            } => state.transform = transform,
            wl_output::Event::Mode {
                flags: Value(flags),
                refresh,
                ..
            } if flags.contains(wl_output::Mode::Current) => state.refresh_mhz = refresh,
            _ => {}
        }
    }
}

no_events!(
    WlCompositor,
    WlSurface,
    ZwlrLayerShellV1,
    WpViewporter,
    WpViewport,
    WlShm,
    WlShmPool,
    WlBuffer,
    WlSeat,
    ZwlrScreencopyManagerV1,
    ZwpVirtualKeyboardManagerV1,
    ZwpVirtualKeyboardV1
);
