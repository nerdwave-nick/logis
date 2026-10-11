//! Surface scale in the units of wp_fractional_scale_v1 (1/120ths), and the
//! logical → buffer size rule shared by windows and offscreen renders.

/// A scale factor in 1/120ths, exactly as wp_fractional_scale_v1 reports it
/// (156 is 1.3). Integer units keep sizes exact and comparable.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub struct Scale(u32);

impl Scale {
    /// Scale 1.0.
    pub const ONE: Self = Self(120);

    /// From the protocol's numerator over 120. Zero is not a scale; it
    /// becomes 1.0.
    pub fn from_120ths(numerator: u32) -> Self {
        if numerator == 0 {
            Self::ONE
        } else {
            Self(numerator)
        }
    }

    /// From an integer `wl_surface.preferred_buffer_scale`.
    pub fn from_integer(factor: u32) -> Self {
        Self::from_120ths(factor.saturating_mul(120))
    }

    /// The protocol's numerator over 120.
    pub fn as_120ths(self) -> u32 {
        self.0
    }

    /// The factor as a float, for shaders.
    pub fn factor(self) -> f32 {
        self.0 as f32 / 120.0
    }

    /// The buffer size for a logical size: each side times the scale, rounded
    /// half away from zero, as wp_fractional_scale_v1 prescribes. Never zero.
    pub fn physical(self, (width, height): (u32, u32)) -> (u32, u32) {
        let side = |logical: u32| {
            let scaled = (u64::from(logical) * u64::from(self.0) + 60) / 120;
            u32::try_from(scaled).unwrap_or(u32::MAX).max(1)
        };
        (side(width), side(height))
    }
}

impl Default for Scale {
    fn default() -> Self {
        Self::ONE
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn integer_scales_multiply_exactly() {
        assert_eq!(Scale::ONE.physical((640, 360)), (640, 360));
        assert_eq!(Scale::from_integer(2).physical((640, 360)), (1280, 720));
    }

    #[test]
    fn fractional_scales_round_half_away_from_zero() {
        let s = Scale::from_120ths(156); // 1.3
        assert_eq!(s.physical((640, 360)), (832, 468));
        // 1.25 × 3 = 3.75 → 4; 1.25 × 2 = 2.5 → 3.
        assert_eq!(Scale::from_120ths(150).physical((3, 2)), (4, 3));
    }

    #[test]
    fn a_zero_scale_is_treated_as_one() {
        assert_eq!(Scale::from_120ths(0), Scale::ONE);
        assert_eq!(Scale::from_integer(0), Scale::ONE);
    }

    #[test]
    fn a_physical_side_is_never_zero() {
        assert_eq!(Scale::ONE.physical((0, 0)), (1, 1));
    }

    #[test]
    fn the_factor_matches_the_numerator() {
        assert_eq!(Scale::from_120ths(156).factor(), 1.3);
        assert_eq!(Scale::from_120ths(156).as_120ths(), 156);
    }
}
