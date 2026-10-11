//! xdg-activation (spec §4.2): tokens for the applications the launcher
//! starts, so the compositor focuses them. A token is asked for the visible
//! panel's surface with the seat and the serial of the last key press or
//! button press (the one that chose the app): niri, like most compositors,
//! issues a valid token only for a focused surface and a recent serial.
//! Without the global, every request is answered at once with no token.

use smithay_client_toolkit::activation::{ActivationHandler, ActivationState, RequestData};
use smithay_client_toolkit::reexports::client::QueueHandle;
use smithay_client_toolkit::reexports::client::globals::GlobalList;

use super::Windows;
use super::trace::emit;

/// The activation global, the last input serial and the tokens issued.
pub(super) struct Activation {
    state: Option<ActivationState>,
    /// The serial of the last key or button press on the panel.
    pub(super) serial: Option<u32>,
    /// Tokens for requests, oldest first: (request, token or none).
    tokens: Vec<(u64, Option<String>)>,
}

impl Activation {
    pub(super) fn bind(globals: &GlobalList, qh: &QueueHandle<Windows>) -> Self {
        Self {
            state: ActivationState::bind(globals, qh).ok(),
            serial: None,
            tokens: Vec::new(),
        }
    }
}

impl ActivationHandler for Windows {
    type RequestUdata = u64;

    fn new_token(&mut self, token: String, data: &RequestData<u64>) {
        emit(format_args!("token {}", data.udata));
        self.activation.tokens.push((data.udata, Some(token)));
    }
}

impl Windows {
    /// Asks for a token to activate `app_id`, named `request` in
    /// [`Self::take_tokens`].
    pub fn request_token(&mut self, qh: &QueueHandle<Self>, request: u64, app_id: &str) {
        let Some(state) = &self.activation.state else {
            self.activation.tokens.push((request, None));
            return;
        };
        let seat = self.keyboard().seat().cloned();
        state.request_token(
            qh,
            RequestData {
                app_id: Some(app_id.to_owned()),
                seat_and_serial: seat.zip(self.activation.serial),
                surface: self.panel.as_ref().map(|p| p.surface().clone()),
                udata: request,
            },
        );
    }

    /// The tokens issued (or refused) since the last call.
    pub fn take_tokens(&mut self) -> Vec<(u64, Option<String>)> {
        std::mem::take(&mut self.activation.tokens)
    }
}
