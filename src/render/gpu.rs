//! The GPU device. The daemon creates it once at start, never lazily (spec
//! §4.3, ADR 0002): Vulkan first, then GL, then a software rasteriser (Mesa
//! lavapipe on Vulkan, llvmpipe on GL). There is no CPU renderer of our own.

use std::fmt;
use std::future::Future;
use std::pin::pin;
use std::str::FromStr;
use std::task::{Context, Poll, Waker};

use wgpu::rwh::HasDisplayHandle;
use wgpu::{Backend, Backends, DeviceType};

/// What wgpu accepts as an instance's display: a shareable display handle.
pub trait DisplaySource: HasDisplayHandle + fmt::Debug + Send + Sync + Clone + 'static {}

impl<T: HasDisplayHandle + fmt::Debug + Send + Sync + Clone + 'static> DisplaySource for T {}

/// Which backends the cogitator may wake. `Auto` walks the whole chain; the
/// others force one backend (or software rasterisation) for checks and tests.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum BackendChoice {
    /// Vulkan hardware, then GL hardware, then lavapipe, then llvmpipe.
    #[default]
    Auto,
    /// Vulkan only (hardware first, lavapipe if nothing else).
    Vulkan,
    /// GL only (hardware first, llvmpipe if nothing else).
    Gl,
    /// Software rasterisers only: lavapipe, then llvmpipe.
    Software,
}

impl BackendChoice {
    /// The name accepted by [`FromStr`] and shown in messages.
    pub fn name(self) -> &'static str {
        match self {
            Self::Auto => "auto",
            Self::Vulkan => "vulkan",
            Self::Gl => "gl",
            Self::Software => "software",
        }
    }
}

impl FromStr for BackendChoice {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, String> {
        [Self::Auto, Self::Vulkan, Self::Gl, Self::Software]
            .into_iter()
            .find(|c| c.name() == s)
            .ok_or_else(|| format!("unknown GPU backend '{s}' (auto, vulkan, gl or software)"))
    }
}

impl fmt::Display for BackendChoice {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.name())
    }
}

/// Hardware adapters versus CPU rasterisers within one backend.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Class {
    Hardware,
    Software,
}

/// The order in which (backend, class) pairs are tried for a choice.
fn tiers(choice: BackendChoice) -> &'static [(Backend, Class)] {
    use Backend::{Gl, Vulkan};
    use Class::{Hardware, Software};
    match choice {
        BackendChoice::Auto => &[
            (Vulkan, Hardware),
            (Gl, Hardware),
            (Vulkan, Software),
            (Gl, Software),
        ],
        BackendChoice::Vulkan => &[(Vulkan, Hardware), (Vulkan, Software)],
        BackendChoice::Gl => &[(Gl, Hardware), (Gl, Software)],
        BackendChoice::Software => &[(Vulkan, Software), (Gl, Software)],
    }
}

/// Preference among adapters of one class: lower is better. A launcher wants
/// the low-power GPU, so integrated beats discrete.
fn rank(device: DeviceType, class: Class) -> Option<u8> {
    match (class, device) {
        (Class::Software, DeviceType::Cpu) => Some(0),
        (Class::Software, _) | (Class::Hardware, DeviceType::Cpu) => None,
        (Class::Hardware, DeviceType::IntegratedGpu) => Some(0),
        (Class::Hardware, DeviceType::DiscreteGpu) => Some(1),
        (Class::Hardware, DeviceType::VirtualGpu) => Some(2),
        (Class::Hardware, DeviceType::Other) => Some(3),
    }
}

/// Walks the tiers of `choice` and returns the chosen backend and the index of
/// the adapter within that backend's list. `enumerate` is asked at most once
/// per backend and only when that backend's tier is reached, so the happy
/// path never pays for initialising GL.
fn select(
    choice: BackendChoice,
    mut enumerate: impl FnMut(Backend) -> Vec<DeviceType>,
) -> Option<(Backend, usize)> {
    let mut seen: Vec<(Backend, Vec<DeviceType>)> = Vec::new();
    for &(backend, class) in tiers(choice) {
        if !seen.iter().any(|(b, _)| *b == backend) {
            seen.push((backend, enumerate(backend)));
        }
        let (_, devices) = seen.iter().find(|(b, _)| *b == backend)?;
        let best = devices
            .iter()
            .enumerate()
            .filter_map(|(i, d)| rank(*d, class).map(|r| (r, i)))
            .min();
        if let Some((_, index)) = best {
            return Some((backend, index));
        }
    }
    None
}

/// Why no GPU device could be woken.
#[derive(Debug)]
pub enum GpuError {
    /// No adapter matches the choice.
    NoAdapter(BackendChoice),
    /// The chosen adapter refused to give a device.
    Device(wgpu::RequestDeviceError),
}

impl fmt::Display for GpuError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NoAdapter(choice) => write!(
                f,
                "no machine spirit answers: no GPU adapter for backend '{choice}' \
                 (Vulkan, GL, or Mesa lavapipe/llvmpipe for software rendering)"
            ),
            Self::Device(e) => write!(f, "the GPU adapter refused the rite of binding: {e}"),
        }
    }
}

impl std::error::Error for GpuError {}

/// The woken GPU: one adapter, its device and queue. Created once; every
/// window and offscreen render shares it.
pub struct Gpu {
    /// The instance the adapter came from; window surfaces are created on it.
    pub instance: wgpu::Instance,
    /// The chosen adapter.
    pub adapter: wgpu::Adapter,
    /// The logical device.
    pub device: wgpu::Device,
    /// The device's queue.
    pub queue: wgpu::Queue,
}

impl Gpu {
    /// A device for offscreen rendering only (tests, golden images).
    pub fn headless(choice: BackendChoice) -> Result<Self, GpuError> {
        Self::new::<NoDisplay>(choice, None)
    }

    /// Wakes the device. `display` is the Wayland display windows will be
    /// presented on; the GL backend needs it at instance creation.
    pub fn new<D>(choice: BackendChoice, display: Option<D>) -> Result<Self, GpuError>
    where
        D: DisplaySource,
    {
        let mut found: Vec<(Backend, wgpu::Instance, Vec<wgpu::Adapter>)> = Vec::new();
        let picked = select(choice, |backend| {
            let instance = instance(backend, display.clone());
            let adapters = block_on(instance.enumerate_adapters(Backends::from(backend)));
            let devices = adapters.iter().map(|a| a.get_info().device_type).collect();
            found.push((backend, instance, adapters));
            devices
        });
        let (backend, index) = picked.ok_or(GpuError::NoAdapter(choice))?;
        let (_, instance, mut adapters) = found
            .into_iter()
            .find(|(b, _, _)| *b == backend)
            .ok_or(GpuError::NoAdapter(choice))?;
        let adapter = adapters.swap_remove(index);
        let limits = wgpu::Limits::downlevel_webgl2_defaults().using_resolution(adapter.limits());
        let (device, queue) = block_on(adapter.request_device(&wgpu::DeviceDescriptor {
            label: Some("logis"),
            required_limits: limits,
            ..Default::default()
        }))
        .map_err(GpuError::Device)?;
        Ok(Self {
            instance,
            adapter,
            device,
            queue,
        })
    }

    /// What was woken, for logs and tests.
    pub fn info(&self) -> wgpu::AdapterInfo {
        self.adapter.get_info()
    }
}

fn instance<D: DisplaySource>(backend: Backend, display: Option<D>) -> wgpu::Instance {
    let mut desc = wgpu::InstanceDescriptor::new_without_display_handle();
    desc.backends = Backends::from(backend);
    if let Some(display) = display {
        desc = desc.with_display_handle(Box::new(display));
    }
    wgpu::Instance::new(desc)
}

/// The display type of [`Gpu::headless`], which never has one.
#[derive(Clone, Debug)]
enum NoDisplay {}

impl HasDisplayHandle for NoDisplay {
    fn display_handle(&self) -> Result<wgpu::rwh::DisplayHandle<'_>, wgpu::rwh::HandleError> {
        match *self {}
    }
}

/// Drives one of wgpu's native futures, which complete without waking on
/// native backends. Used only at device creation and in offscreen readback,
/// never in the frame loop.
pub(crate) fn block_on<F: Future>(future: F) -> F::Output {
    let mut future = pin!(future);
    let mut cx = Context::from_waker(Waker::noop());
    loop {
        if let Poll::Ready(output) = future.as_mut().poll(&mut cx) {
            return output;
        }
        std::thread::yield_now();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use DeviceType::{Cpu, DiscreteGpu, IntegratedGpu};

    /// A fake machine: the device types each backend would enumerate, and a
    /// log of which backends were asked.
    fn machine(
        vulkan: &[DeviceType],
        gl: &[DeviceType],
    ) -> (
        impl FnMut(Backend) -> Vec<DeviceType>,
        std::rc::Rc<std::cell::RefCell<Vec<Backend>>>,
    ) {
        let log = std::rc::Rc::new(std::cell::RefCell::new(Vec::new()));
        let (vulkan, gl, asked) = (vulkan.to_vec(), gl.to_vec(), log.clone());
        let enumerate = move |b: Backend| {
            asked.borrow_mut().push(b);
            match b {
                Backend::Vulkan => vulkan.clone(),
                Backend::Gl => gl.clone(),
                _ => Vec::new(),
            }
        };
        (enumerate, log)
    }

    #[test]
    fn auto_prefers_vulkan_hardware_and_never_wakes_gl() {
        let (enumerate, asked) = machine(&[Cpu, IntegratedGpu], &[IntegratedGpu]);
        assert_eq!(
            select(BackendChoice::Auto, enumerate),
            Some((Backend::Vulkan, 1))
        );
        assert_eq!(*asked.borrow(), vec![Backend::Vulkan]);
    }

    #[test]
    fn auto_falls_back_to_gl_hardware_before_lavapipe() {
        let (enumerate, _) = machine(&[Cpu], &[IntegratedGpu]);
        assert_eq!(
            select(BackendChoice::Auto, enumerate),
            Some((Backend::Gl, 0))
        );
    }

    #[test]
    fn auto_falls_back_to_lavapipe_then_llvmpipe() {
        let (enumerate, _) = machine(&[Cpu], &[Cpu]);
        assert_eq!(
            select(BackendChoice::Auto, enumerate),
            Some((Backend::Vulkan, 0))
        );
        let (enumerate, _) = machine(&[], &[Cpu]);
        assert_eq!(
            select(BackendChoice::Auto, enumerate),
            Some((Backend::Gl, 0))
        );
    }

    #[test]
    fn integrated_gpu_beats_discrete() {
        let (enumerate, _) = machine(&[DiscreteGpu, IntegratedGpu], &[]);
        assert_eq!(
            select(BackendChoice::Auto, enumerate),
            Some((Backend::Vulkan, 1))
        );
    }

    #[test]
    fn forced_backends_stay_within_their_backend() {
        let (enumerate, asked) = machine(&[IntegratedGpu], &[IntegratedGpu]);
        assert_eq!(select(BackendChoice::Gl, enumerate), Some((Backend::Gl, 0)));
        assert_eq!(*asked.borrow(), vec![Backend::Gl]);

        let (enumerate, _) = machine(&[], &[IntegratedGpu]);
        assert_eq!(select(BackendChoice::Vulkan, enumerate), None);
    }

    #[test]
    fn forced_software_ignores_hardware() {
        let (enumerate, _) = machine(&[IntegratedGpu, Cpu], &[IntegratedGpu]);
        assert_eq!(
            select(BackendChoice::Software, enumerate),
            Some((Backend::Vulkan, 1))
        );
        let (enumerate, _) = machine(&[IntegratedGpu], &[IntegratedGpu]);
        assert_eq!(select(BackendChoice::Software, enumerate), None);
    }

    #[test]
    fn no_adapter_at_all_selects_nothing() {
        let (enumerate, _) = machine(&[], &[]);
        assert_eq!(select(BackendChoice::Auto, enumerate), None);
    }

    #[test]
    fn choices_parse_from_their_names() {
        for c in [
            BackendChoice::Auto,
            BackendChoice::Vulkan,
            BackendChoice::Gl,
            BackendChoice::Software,
        ] {
            assert_eq!(c.name().parse(), Ok(c));
        }
        assert!("metal".parse::<BackendChoice>().is_err());
    }
}
