//! A test-only selection owner: a client of the test compositor that owns
//! the clipboard and the primary selection through wlr-data-control (both
//! test compositors offer it; it needs no keyboard focus or serial) and
//! writes their text to whoever asks, on a thread of its own, until it is
//! dropped. It replaces `wl-copy`: nothing to install on the CI runner,
//! and its connection goes through the isolation guard
//! ([`super::wire::connect_verified`]), so it can never touch the owner's
//! live clipboard.
//!
//! Every source offers the same text under several MIME types, so a test
//! can check which one the client chose ([`SelectionOwner::requests`]).

use std::io::Write;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread::{self, JoinHandle};
use std::time::Duration;

use smithay_client_toolkit::reexports::client::globals::{GlobalListContents, registry_queue_init};
use smithay_client_toolkit::reexports::client::protocol::{
    wl_registry::WlRegistry, wl_seat::WlSeat,
};
use smithay_client_toolkit::reexports::client::{
    Connection, Dispatch, EventQueue, Proxy, QueueHandle, event_created_child,
};
use smithay_client_toolkit::reexports::protocols_wlr::data_control::v1::client::{
    zwlr_data_control_device_v1::{self, ZwlrDataControlDeviceV1},
    zwlr_data_control_manager_v1::ZwlrDataControlManagerV1,
    zwlr_data_control_offer_v1::ZwlrDataControlOfferV1,
    zwlr_data_control_source_v1::{self, ZwlrDataControlSourceV1},
};

use super::TestCompositor;
use super::wire::connect_verified;

/// The MIME types every source offers, the preferred one not first.
pub const OFFERED: [&str; 5] = [
    "text/plain",
    "STRING",
    "text/plain;charset=utf-8",
    "UTF8_STRING",
    "TEXT",
];

/// The owner of the test compositor's clipboard and primary selection.
pub struct SelectionOwner {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<()>>,
    requests: Arc<Mutex<Vec<String>>>,
}

/// Which selection a source holds, and its text.
struct Text {
    selection: &'static str,
    bytes: Vec<u8>,
}

/// Event-queue state of the owner's connection.
struct Owner {
    requests: Arc<Mutex<Vec<String>>>,
}

impl SelectionOwner {
    /// Takes the clipboard and the primary selection of `compositor` with
    /// the given texts (`None` leaves one alone).
    pub fn start(
        compositor: &TestCompositor,
        clipboard: Option<&str>,
        primary: Option<&str>,
    ) -> Self {
        let connection = connect_verified(compositor.socket(), compositor.pid())
            .unwrap_or_else(|e| panic!("{e}"));
        let (globals, mut queue) =
            registry_queue_init::<Owner>(&connection).expect("the compositor lists its globals");
        let qh = queue.handle();
        let seat: WlSeat = globals.bind(&qh, 1..=1, ()).expect("a seat");
        let manager: ZwlrDataControlManagerV1 = globals
            .bind(&qh, 1..=2, ())
            .expect("the test compositor offers zwlr_data_control_manager_v1");
        let device = manager.get_data_device(&seat, &qh, ());
        let source = |selection, text: &str| {
            let bytes = text.as_bytes().to_vec();
            let source = manager.create_data_source(&qh, Text { selection, bytes });
            for mime in OFFERED {
                source.offer(mime.to_owned());
            }
            source
        };
        let mut sources = Vec::new();
        if let Some(text) = clipboard {
            let s = source("clipboard", text);
            device.set_selection(Some(&s));
            sources.push(s);
        }
        if let Some(text) = primary {
            assert!(
                manager.version() >= 2,
                "zwlr_data_control_manager_v1 v2 is needed for the primary selection"
            );
            let s = source("primary", text);
            device.set_primary_selection(Some(&s));
            sources.push(s);
        }
        let requests = Arc::new(Mutex::new(Vec::new()));
        let mut owner = Owner {
            requests: Arc::clone(&requests),
        };
        queue.roundtrip(&mut owner).expect("the compositor answers");
        let stop = Arc::new(AtomicBool::new(false));
        let stopping = Arc::clone(&stop);
        let thread = thread::spawn(move || {
            serve(&mut queue, &mut owner, &stopping);
            drop(connection);
            for s in sources {
                s.destroy();
            }
            device.destroy();
            let _ = queue.roundtrip(&mut owner);
        });
        Self {
            stop,
            thread: Some(thread),
            requests,
        }
    }

    /// What clients asked for so far: `<selection> <MIME type>` each.
    pub fn requests(&self) -> Vec<String> {
        self.requests.lock().unwrap().clone()
    }
}

impl Drop for SelectionOwner {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
    }
}

/// Dispatches the owner's events until `stop`, waking every 20 ms to check.
fn serve(queue: &mut EventQueue<Owner>, owner: &mut Owner, stop: &AtomicBool) {
    use rustix::event::{PollFd, PollFlags, Timespec, poll};
    let tick = Timespec {
        tv_sec: 0,
        tv_nsec: Duration::from_millis(20).as_nanos() as i64,
    };
    while !stop.load(Ordering::Relaxed) {
        if queue.dispatch_pending(owner).is_err() || queue.flush().is_err() {
            return;
        }
        let Some(guard) = queue.prepare_read() else {
            continue;
        };
        let ready = {
            let fd = guard.connection_fd();
            let mut fds = [PollFd::new(&fd, PollFlags::IN)];
            matches!(poll(&mut fds, Some(&tick)), Ok(n) if n > 0)
        };
        if ready {
            let _ = guard.read();
        }
    }
}

impl Dispatch<ZwlrDataControlSourceV1, Text> for Owner {
    fn event(
        owner: &mut Self,
        _: &ZwlrDataControlSourceV1,
        event: zwlr_data_control_source_v1::Event,
        text: &Text,
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        if let zwlr_data_control_source_v1::Event::Send { mime_type, fd } = event {
            let request = format!("{} {mime_type}", text.selection);
            owner.requests.lock().unwrap().push(request);
            let _ = std::fs::File::from(fd).write_all(&text.bytes);
        }
    }
}

impl Dispatch<ZwlrDataControlDeviceV1, ()> for Owner {
    fn event(
        _: &mut Self,
        _: &ZwlrDataControlDeviceV1,
        _: zwlr_data_control_device_v1::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
    }

    event_created_child!(Owner, ZwlrDataControlDeviceV1, [
        zwlr_data_control_device_v1::EVT_DATA_OFFER_OPCODE => (ZwlrDataControlOfferV1, ()),
    ]);
}

impl Dispatch<WlRegistry, GlobalListContents> for Owner {
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

/// Objects whose events the owner does not need.
macro_rules! no_events {
    ($($proxy:ty),*) => {$(
        impl Dispatch<$proxy, ()> for Owner {
            fn event(_: &mut Self, _: &$proxy, _: <$proxy as Proxy>::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
        }
    )*};
}
no_events!(WlSeat, ZwlrDataControlManagerV1, ZwlrDataControlOfferV1);
