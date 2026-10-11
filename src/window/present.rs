//! Swapchain negotiation from what the surface offers (spec §4.3).

use wgpu::{CompositeAlphaMode, PresentMode, TextureFormat};

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
