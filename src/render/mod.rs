//! logis's own small GPU renderer (spec §4.3, ADR 0001): a wgpu device woken
//! once, SDF shapes in WGSL with premultiplied alpha, and an offscreen entry
//! point for golden tests. Windows present through [`crate::window`].

mod gpu;
mod offscreen;
mod pipeline;
mod scale;
mod scene;
mod text;
mod text_pass;

pub use gpu::{BackendChoice, DisplaySource, Gpu, GpuError};
pub use offscreen::{Image, OFFSCREEN_FORMAT, ReadbackError, render_offscreen};
pub use pipeline::{Renderer, Target};
pub use scale::Scale;
pub use scene::{Border, Glow, Paint, Rgba, RoundedRect, Scene};
pub use text::{Overflow, Text};
