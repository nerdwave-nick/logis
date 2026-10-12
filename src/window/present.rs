//! Swapchain negotiation from what the surface offers (spec §4.3).

use wgpu::{CompositeAlphaMode, PresentMode, TextureFormat};

/// What every swapchain of the daemon is made with. A surface offers the
/// same for every window of one connection and adapter, so it is asked of
/// the first window only: each query costs several round trips to the
/// compositor on the open path (Mesa sets its Wayland state up anew for
/// each), and a warm open must not wait on the compositor.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Negotiated {
    pub format: TextureFormat,
    pub present_mode: PresentMode,
    pub alpha_mode: CompositeAlphaMode,
}

/// The choices from what a surface offers; `None` if it offers no format.
pub fn negotiate(offered: &wgpu::SurfaceCapabilities) -> Option<Negotiated> {
    Some(Negotiated {
        format: choose_format(&offered.formats)?,
        present_mode: choose_present_mode(&offered.present_modes),
        alpha_mode: choose_alpha_mode(&offered.alpha_modes),
    })
}

/// An 8-bit format that does not encode sRGB itself: colours are sRGB values
/// blended as given, which is also what glyphon's `ColorMode::Web` expects.
/// Falls back to the first offered format (the shader then stores linear).
pub fn choose_format(offered: &[TextureFormat]) -> Option<TextureFormat> {
    const PREFERRED: [TextureFormat; 2] = [TextureFormat::Bgra8Unorm, TextureFormat::Rgba8Unorm];
    PREFERRED
        .into_iter()
        .find(|f| offered.contains(f))
        .or_else(|| offered.first().copied())
}

/// Mailbox never blocks presentation or acquisition on the compositor; the
/// frame callbacks pace drawing. FIFO is the guaranteed baseline.
pub fn choose_present_mode(offered: &[PresentMode]) -> PresentMode {
    if offered.contains(&PresentMode::Mailbox) {
        PresentMode::Mailbox
    } else {
        PresentMode::Fifo
    }
}

/// The renderer outputs premultiplied alpha; anything else is a fallback
/// in which translucent areas may not blend with what lies behind.
pub fn choose_alpha_mode(offered: &[CompositeAlphaMode]) -> CompositeAlphaMode {
    [
        CompositeAlphaMode::PreMultiplied,
        CompositeAlphaMode::Inherit,
    ]
    .into_iter()
    .find(|m| offered.contains(m))
    .or_else(|| offered.first().copied())
    .unwrap_or(CompositeAlphaMode::Auto)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_plain_8_bit_format_beats_srgb_and_wide_formats() {
        let offered = [
            TextureFormat::Bgra8UnormSrgb,
            TextureFormat::Rgb10a2Unorm,
            TextureFormat::Bgra8Unorm,
        ];
        assert_eq!(choose_format(&offered), Some(TextureFormat::Bgra8Unorm));
        assert_eq!(
            choose_format(&[TextureFormat::Rgba8UnormSrgb, TextureFormat::Rgba8Unorm]),
            Some(TextureFormat::Rgba8Unorm)
        );
    }

    #[test]
    fn without_a_preferred_format_the_first_offered_is_used() {
        assert_eq!(
            choose_format(&[TextureFormat::Bgra8UnormSrgb]),
            Some(TextureFormat::Bgra8UnormSrgb)
        );
        assert_eq!(choose_format(&[]), None);
    }

    #[test]
    fn mailbox_is_preferred_and_fifo_is_the_baseline() {
        assert_eq!(
            choose_present_mode(&[PresentMode::Fifo, PresentMode::Mailbox]),
            PresentMode::Mailbox
        );
        assert_eq!(
            choose_present_mode(&[PresentMode::Fifo, PresentMode::Immediate]),
            PresentMode::Fifo
        );
        assert_eq!(choose_present_mode(&[]), PresentMode::Fifo);
    }

    #[test]
    fn a_surface_is_negotiated_from_its_offer_and_needs_a_format() {
        let offered = wgpu::SurfaceCapabilities {
            formats: vec![TextureFormat::Bgra8UnormSrgb, TextureFormat::Bgra8Unorm],
            present_modes: vec![PresentMode::Fifo, PresentMode::Mailbox],
            alpha_modes: vec![
                CompositeAlphaMode::Opaque,
                CompositeAlphaMode::PreMultiplied,
            ],
            ..Default::default()
        };
        assert_eq!(
            negotiate(&offered),
            Some(Negotiated {
                format: TextureFormat::Bgra8Unorm,
                present_mode: PresentMode::Mailbox,
                alpha_mode: CompositeAlphaMode::PreMultiplied,
            })
        );
        let formatless = wgpu::SurfaceCapabilities {
            formats: Vec::new(),
            ..offered
        };
        assert_eq!(negotiate(&formatless), None);
    }

    #[test]
    fn premultiplied_alpha_is_preferred() {
        let offered = [
            CompositeAlphaMode::Opaque,
            CompositeAlphaMode::PreMultiplied,
        ];
        assert_eq!(
            choose_alpha_mode(&offered),
            CompositeAlphaMode::PreMultiplied
        );
        assert_eq!(
            choose_alpha_mode(&[CompositeAlphaMode::Opaque]),
            CompositeAlphaMode::Opaque
        );
        assert_eq!(choose_alpha_mode(&[]), CompositeAlphaMode::Auto);
    }
}
