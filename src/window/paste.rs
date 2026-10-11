//! Paste from the clipboard (`wl_data_device`) and the primary selection
//! (`zwp_primary_selection_device_v1`), both through sctk, which tracks the
//! current selection offers and their MIME types as the compositor
//! announces them to the focused window.
//!
//! A paste picks the best text type on offer ([`paste::choose`]), asks the
//! owner to write it into a pipe, and reads that pipe without blocking:
//! the read end is non-blocking and polled beside the compositor's socket
//! ([`Clipboard::fd`]), so the frame loop keeps running while a slow owner
//! writes. Once the owner closes the pipe (or [`paste::LIMIT`] is
//! reached), the text becomes an [`Edit::Insert`] at the query's cursor. A
//! newer paste replaces one still being read; hiding the panel drops it.
//!
//! Both protocols are used when offered; without one, its paste does
//! nothing (trace `paste <selection> unavailable`).
//!
//! Trace events (see [`super::trace`]): `paste <selection> <MIME type>`
//! when a read starts, `paste <selection> none` when nothing text-like is
//! on offer, and `pasted <selection> <bytes>` when it ends.

use std::io::{ErrorKind, Read};
use std::os::fd::{AsFd, BorrowedFd};

use smithay_client_toolkit::data_device_manager::data_device::{DataDevice, DataDeviceHandler};
use smithay_client_toolkit::data_device_manager::data_offer::{DataOfferHandler, DragOffer};
use smithay_client_toolkit::data_device_manager::{DataDeviceManagerState, ReadPipe};
use smithay_client_toolkit::primary_selection::PrimarySelectionManagerState;
use smithay_client_toolkit::primary_selection::device::{
    PrimarySelectionDevice, PrimarySelectionDeviceHandler,
};
use smithay_client_toolkit::reexports::client::globals::GlobalList;
use smithay_client_toolkit::reexports::client::protocol::wl_data_device::WlDataDevice;
use smithay_client_toolkit::reexports::client::protocol::wl_data_device_manager::DndAction;
use smithay_client_toolkit::reexports::client::protocol::wl_seat::WlSeat;
use smithay_client_toolkit::reexports::client::protocol::wl_surface::WlSurface;
use smithay_client_toolkit::reexports::client::{Connection, QueueHandle};
use smithay_client_toolkit::reexports::protocols::wp::primary_selection::zv1::client::zwp_primary_selection_device_v1::ZwpPrimarySelectionDeviceV1;

use super::Windows;
use super::trace::emit;
use crate::input::{Edit, Selection, paste};

/// The selection devices of the seat and the paste being read.
pub(super) struct Clipboard {
    // Devices before their managers.
    data: Option<DataDevice>,
    primary: Option<PrimarySelectionDevice>,
    _data_manager: Option<DataDeviceManagerState>,
    _primary_manager: Option<PrimarySelectionManagerState>,
    reading: Option<Reading>,
}

/// A paste in progress: the pipe's read end and what came so far.
struct Reading {
    selection: Selection,
    pipe: ReadPipe,
    bytes: paste::Bytes,
}

impl Clipboard {
    /// Binds the managers the compositor offers and gets their devices for
    /// `seat` (none without a seat).
    pub(super) fn bind(
        globals: &GlobalList,
        qh: &QueueHandle<Windows>,
        seat: Option<&WlSeat>,
    ) -> Self {
        let data_manager = DataDeviceManagerState::bind(globals, qh).ok();
        let primary_manager = PrimarySelectionManagerState::bind(globals, qh).ok();
        Self {
            data: seat
                .zip(data_manager.as_ref())
                .map(|(s, m)| m.get_data_device(qh, s)),
            primary: seat
                .zip(primary_manager.as_ref())
                .map(|(s, m)| m.get_selection_device(qh, s)),
            _data_manager: data_manager,
            _primary_manager: primary_manager,
            reading: None,
        }
    }

    /// Starts reading `selection`'s text, replacing a paste in progress.
    pub(super) fn start(&mut self, selection: Selection) {
        self.reading = None;
        let name = selection.name();
        let pipe = match selection {
            Selection::Clipboard => self.data.as_ref().map(|device| {
                device.data().selection_offer().and_then(|offer| {
                    let mime = offer.with_mime_types(|m| paste::choose(m).map(str::to_owned))?;
                    emit(format_args!("paste {name} {mime}"));
                    offer.receive(mime).ok()
                })
            }),
            Selection::Primary => self.primary.as_ref().map(|device| {
                device.data().selection_offer().and_then(|offer| {
                    let mime = offer.with_mime_types(|m| paste::choose(m).map(str::to_owned))?;
                    emit(format_args!("paste {name} {mime}"));
                    offer.receive(mime).ok()
                })
            }),
        };
        match pipe {
            None => emit(format_args!("paste {name} unavailable")),
            Some(None) => emit(format_args!("paste {name} none")),
            Some(Some(pipe)) => {
                // A blocking read would stall the frame loop on a slow owner.
                if rustix::io::ioctl_fionbio(&pipe, true).is_ok() {
                    self.reading = Some(Reading {
                        selection,
                        pipe,
                        bytes: paste::Bytes::default(),
                    });
                }
            }
        }
    }

    /// Drops a paste in progress (the panel was hidden or replaced).
    pub(super) fn cancel(&mut self) {
        self.reading = None;
    }

    /// The pipe of the paste in progress, to poll beside the compositor.
    pub(super) fn fd(&self) -> Option<BorrowedFd<'_>> {
        self.reading.as_ref().map(|r| r.pipe.as_fd())
    }

    /// Reads what the owner has written so far; once it closed the pipe
    /// (or the limit is reached), the edit that inserts the text.
    pub(super) fn pump(&mut self) -> Option<Edit> {
        let reading = self.reading.as_mut()?;
        let mut chunk = [0; 8192];
        let done = loop {
            match reading.pipe.read(&mut chunk) {
                Ok(0) => break true,
                Ok(n) if reading.bytes.push(&chunk[..n]) => break true,
                Ok(_) => {}
                Err(e) if e.kind() == ErrorKind::Interrupted => {}
                Err(e) if e.kind() == ErrorKind::WouldBlock => break false,
                // A broken pipe ends the paste with what came.
                Err(_) => break true,
            }
        };
        if !done {
            return None;
        }
        let reading = self.reading.take()?;
        let text = reading.bytes.text();
        emit(format_args!(
            "pasted {} {}",
            reading.selection.name(),
            text.len()
        ));
        (!text.is_empty()).then_some(Edit::Insert(text))
    }
}

/// Selection offers are tracked by sctk; logis reads them only on paste and
/// accepts no drag and drop.
impl DataDeviceHandler for Windows {
    fn enter(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        _: &WlDataDevice,
        _: f64,
        _: f64,
        _: &WlSurface,
    ) {
    }
    fn leave(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &WlDataDevice) {}
    fn motion(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &WlDataDevice, _: f64, _: f64) {}
    fn selection(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &WlDataDevice) {}
    fn drop_performed(&mut self, _: &Connection, _: &QueueHandle<Self>, _: &WlDataDevice) {}
}

impl DataOfferHandler for Windows {
    fn source_actions(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        _: &mut DragOffer,
        _: DndAction,
    ) {
    }
    fn selected_action(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        _: &mut DragOffer,
        _: DndAction,
    ) {
    }
}

impl PrimarySelectionDeviceHandler for Windows {
    fn selection(
        &mut self,
        _: &Connection,
        _: &QueueHandle<Self>,
        _: &ZwpPrimarySelectionDeviceV1,
    ) {
    }
}
