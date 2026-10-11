//! Screen capture of the test compositor's output (wlr-screencopy over
//! wl_shm, which niri and sway both offer) and cropping of the capture to a
//! layer surface for golden comparison.

use std::os::fd::AsFd;

use logis::render::Image;
use smithay_client_toolkit::reexports::client::protocol::wl_output::Transform;
use smithay_client_toolkit::reexports::client::protocol::wl_shm::Format;
use smithay_client_toolkit::reexports::client::{Connection, Dispatch, Proxy, QueueHandle, WEnum};
use smithay_client_toolkit::reexports::protocols_wlr::screencopy::v1::client::zwlr_screencopy_frame_v1::{
    self, ZwlrScreencopyFrameV1,
};

use super::wire::{Harness, State};
use crate::support::golden::{self, Tolerance};

/// The progress of one capture.
#[derive(Default)]
pub struct Capture {
    buffer: Option<(WEnum<Format>, u32, u32, u32)>,
    buffer_done: bool,
    y_invert: bool,
    ready: bool,
    failed: bool,
}

impl Dispatch<ZwlrScreencopyFrameV1, ()> for State {
    fn event(
        state: &mut Self,
        _: &ZwlrScreencopyFrameV1,
        event: zwlr_screencopy_frame_v1::Event,
        _: &(),
        _: &Connection,
        _: &QueueHandle<Self>,
    ) {
        use zwlr_screencopy_frame_v1::Event;
        let capture = &mut state.capture;
        match event {
            Event::Buffer {
                format,
                width,
                height,
                stride,
            } => capture.buffer = Some((format, width, height, stride)),
            Event::BufferDone => capture.buffer_done = true,
            Event::Flags { flags } => {
                capture.y_invert = flags
                    .into_result()
                    .is_ok_and(|f| f.contains(zwlr_screencopy_frame_v1::Flags::YInvert));
            }
            Event::Ready { .. } => capture.ready = true,
            Event::Failed => capture.failed = true,
            _ => {}
        }
    }
}

impl Harness {
    /// Captures the whole output (without the cursor) as opaque RGBA.
    pub fn capture(&mut self) -> Image {
        self.state.capture = Capture::default();
        let qh = self.qh();
        let frame = self
            .state
            .screencopy
            .capture_output(0, &self.state.output, &qh, ());
        let v3 = frame.version() >= 3;
        self.dispatch_until("screencopy buffer", |s| {
            s.capture.failed || (s.capture.buffer.is_some() && (!v3 || s.capture.buffer_done))
        })
        .unwrap_or_else(|e| panic!("{e}"));
        assert!(
            !self.state.capture.failed,
            "the compositor refused the capture"
        );
        let (format, width, height, stride) = self.state.capture.buffer.unwrap();
        let WEnum::Value(format) = format else {
            panic!("the compositor offers an unknown shm format {format:?}");
        };
        let size = stride * height;
        let path = self.runtime_dir.join("capture.shm");
        let file = std::fs::OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .truncate(true)
            .open(&path)
            .unwrap();
        file.set_len(u64::from(size)).unwrap();
        let pool = self
            .state
            .shm
            .create_pool(file.as_fd(), size as i32, &qh, ());
        let buffer = pool.create_buffer(
            0,
            width as i32,
            height as i32,
            stride as i32,
            format,
            &qh,
            (),
        );
        frame.copy(&buffer);
        self.dispatch_until("screencopy", |s| s.capture.ready || s.capture.failed)
            .unwrap_or_else(|e| panic!("{e}"));
        assert!(!self.state.capture.failed, "the capture failed");
        let bytes = std::fs::read(&path).unwrap();
        buffer.destroy();
        pool.destroy();
        frame.destroy();
        let _ = std::fs::remove_file(&path);
        let image = to_rgba(
            &bytes,
            format,
            width,
            height,
            stride,
            self.state.capture.y_invert,
        );
        upright(image, self.state.transform)
    }
}

/// Converts a little-endian 32-bit shm image to opaque RGBA rows top down.
pub fn to_rgba(
    bytes: &[u8],
    format: Format,
    width: u32,
    height: u32,
    stride: u32,
    y_invert: bool,
) -> Image {
    // Byte order in memory of each format's red, green and blue.
    let [r, g, b] = match format {
        Format::Argb8888 | Format::Xrgb8888 => [2, 1, 0],
        Format::Abgr8888 | Format::Xbgr8888 => [0, 1, 2],
        other => panic!("the harness cannot read shm format {other:?}"),
    };
    let mut pixels = Vec::with_capacity((width * height * 4) as usize);
    for y in 0..height {
        let row = if y_invert { height - 1 - y } else { y };
        let line = &bytes[(row * stride) as usize..][..(width * 4) as usize];
        for px in line.chunks(4) {
            pixels.extend_from_slice(&[px[r], px[g], px[b], 255]);
        }
    }
    Image {
        width,
        height,
        pixels,
    }
}

/// Applies an output's transform to a buffer-space capture, as the output
/// shows it. Rotated outputs are not used by the test compositors.
pub fn upright(image: Image, transform: Transform) -> Image {
    let (mirror_x, mirror_y) = match transform {
        Transform::Normal => (false, false),
        Transform::_180 => (true, true),
        Transform::Flipped => (true, false),
        Transform::Flipped180 => (false, true),
        other => panic!("the harness does not turn {other:?} outputs upright"),
    };
    let (w, h) = (image.width as usize, image.height as usize);
    let mut pixels = Vec::with_capacity(image.pixels.len());
    for y in 0..h {
        let sy = if mirror_y { h - 1 - y } else { y };
        for x in 0..w {
            let sx = if mirror_x { w - 1 - x } else { x };
            let i = (sy * w + sx) * 4;
            pixels.extend_from_slice(&image.pixels[i..i + 4]);
        }
    }
    Image { pixels, ..image }
}

/// The `size` rectangle of `image` at `(x, y)`.
pub fn crop(image: &Image, (x, y): (u32, u32), (width, height): (u32, u32)) -> Image {
    assert!(
        x + width <= image.width && y + height <= image.height,
        "a {width}x{height} crop at ({x}, {y}) leaves the {}x{} capture",
        image.width,
        image.height
    );
    let mut pixels = Vec::with_capacity((width * height * 4) as usize);
    for row in y..y + height {
        let start = ((row * image.width + x) * 4) as usize;
        pixels.extend_from_slice(&image.pixels[start..start + (width * 4) as usize]);
    }
    Image {
        width,
        height,
        pixels,
    }
}

/// Crops a centred `size` surface (a layer surface without anchors) out of
/// an output capture. Compositors round an odd remainder either way, so
/// with a `reference` the offsets within one pixel of the centre are tried
/// and the closest crop is returned.
pub fn crop_centred(capture: &Image, size: (u32, u32), reference: Option<&Image>) -> Image {
    assert!(
        capture.width >= size.0 && capture.height >= size.1,
        "the {}x{} output is smaller than the {}x{} surface",
        capture.width,
        capture.height,
        size.0,
        size.1
    );
    let x = (capture.width - size.0) / 2;
    let y = (capture.height - size.1) / 2;
    let Some(reference) = reference else {
        return crop(capture, (x, y), size);
    };
    let mut best: Option<(usize, Image)> = None;
    for dy in [0, 1] {
        for dx in [0, 1] {
            let (cx, cy) = (x + dx, y + dy);
            if cx + size.0 > capture.width || cy + size.1 > capture.height {
                continue;
            }
            let candidate = crop(capture, (cx, cy), size);
            let tolerance = Tolerance::OFFSCREEN.channel;
            let Ok(c) = golden::compare_with(&candidate, reference, tolerance) else {
                continue;
            };
            if best
                .as_ref()
                .is_none_or(|(outliers, _)| c.outliers < *outliers)
            {
                best = Some((c.outliers, candidate));
            }
        }
    }
    best.map(|(_, image)| image)
        .unwrap_or_else(|| crop(capture, (x, y), size))
}
