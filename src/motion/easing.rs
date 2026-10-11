//! Easing curves: how a transition's progress (0 → 1 in time) maps to how
//! far it has moved (0 → 1 in value). CSS's `cubic-bezier(x1, y1, x2, y2)`,
//! so a theme can name the curves designers already know.

/// A progress curve from (0, 0) to (1, 1).
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Easing {
    /// Value follows time.
    Linear,
    /// CSS `cubic-bezier(x1, y1, x2, y2)`: the control points of a cubic
    /// Bézier from (0, 0) to (1, 1). `x1` and `x2` are clamped to 0..=1 so
    /// the curve stays a function of time.
    CubicBezier([f32; 4]),
}

impl Easing {
    /// Fast start, gentle stop: things arriving (`easeOutCubic`).
    pub const EASE_OUT: Self = Self::CubicBezier([0.33, 1.0, 0.68, 1.0]);
    /// Gentle start, fast end: things leaving (`easeInCubic`).
    pub const EASE_IN: Self = Self::CubicBezier([0.32, 0.0, 0.67, 0.0]);
    /// Gentle at both ends (CSS `ease-in-out`).
    pub const EASE_IN_OUT: Self = Self::CubicBezier([0.42, 0.0, 0.58, 1.0]);

    /// The value at time `t`; `t` is clamped to 0..=1, and the ends are
    /// exact.
    pub fn apply(self, t: f32) -> f32 {
        let t = if t.is_nan() { 0.0 } else { t.clamp(0.0, 1.0) };
        if t == 0.0 || t == 1.0 {
            return t;
        }
        match self {
            Self::Linear => t,
            Self::CubicBezier([x1, y1, x2, y2]) => {
                let s = solve_x(x1.clamp(0.0, 1.0), x2.clamp(0.0, 1.0), t);
                bezier(y1, y2, s)
            }
        }
    }
}

/// One coordinate of the Bézier with control values `a`, `b` at `s`.
fn bezier(a: f32, b: f32, s: f32) -> f32 {
    let r = 1.0 - s;
    3.0 * r * r * s * a + 3.0 * r * s * s * b + s * s * s
}

/// Its derivative in `s`.
fn slope(a: f32, b: f32, s: f32) -> f32 {
    let r = 1.0 - s;
    3.0 * r * r * a + 6.0 * r * s * (b - a) + 3.0 * s * s * (1.0 - b)
}

/// The curve parameter whose x is `x`: Newton's method from `x`, and
/// bisection where the slope is too flat (x is monotonic in s for control
/// x values in 0..=1, so the bisection always converges).
fn solve_x(x1: f32, x2: f32, x: f32) -> f32 {
    let mut s = x;
    for _ in 0..8 {
        let error = bezier(x1, x2, s) - x;
        if error.abs() < 1e-6 {
            return s;
        }
        let d = slope(x1, x2, s);
        if d.abs() < 1e-6 {
            break;
        }
        s = (s - error / d).clamp(0.0, 1.0);
    }
    let (mut low, mut high) = (0.0_f32, 1.0_f32);
    s = x;
    for _ in 0..40 {
        let at = bezier(x1, x2, s);
        if (at - x).abs() < 1e-6 {
            break;
        }
        if at < x {
            low = s;
        } else {
            high = s;
        }
        s = (low + high) / 2.0;
    }
    s
}

#[cfg(test)]
mod tests {
    use super::*;

    const CSS_EASE: Easing = Easing::CubicBezier([0.25, 0.1, 0.25, 1.0]);
    const ALL: [Easing; 5] = [
        Easing::Linear,
        Easing::EASE_OUT,
        Easing::EASE_IN,
        Easing::EASE_IN_OUT,
        CSS_EASE,
    ];

    #[test]
    fn every_curve_starts_at_0_and_ends_at_1() {
        for easing in ALL {
            assert_eq!(easing.apply(0.0), 0.0, "{easing:?}");
            assert_eq!(easing.apply(1.0), 1.0, "{easing:?}");
        }
    }

    #[test]
    fn time_outside_the_transition_is_clamped() {
        for easing in ALL {
            assert_eq!(easing.apply(-0.5), 0.0, "{easing:?}");
            assert_eq!(easing.apply(7.0), 1.0, "{easing:?}");
            assert_eq!(easing.apply(f32::NAN), 0.0, "{easing:?}");
        }
    }

    #[test]
    fn linear_is_the_identity() {
        for t in [0.1, 0.25, 0.5, 0.9] {
            assert_eq!(Easing::Linear.apply(t), t);
        }
    }

    #[test]
    fn cubic_bezier_matches_css_ease() {
        // Browsers' cubic-bezier(0.25, 0.1, 0.25, 1) at 0.5: 0.8024033877.
        assert!((CSS_EASE.apply(0.5) - 0.802_403_4).abs() < 1e-4);
        // easeOutCubic is 1 - (1 - t)^3; its Bézier fit is close to it.
        for t in [0.1_f32, 0.3, 0.5, 0.8] {
            let exact = 1.0 - (1.0 - t).powi(3);
            assert!((Easing::EASE_OUT.apply(t) - exact).abs() < 0.02, "{t}");
        }
    }

    #[test]
    fn ease_out_runs_ahead_and_ease_in_lags_behind() {
        for t in [0.1, 0.5, 0.9] {
            assert!(Easing::EASE_OUT.apply(t) > t);
            assert!(Easing::EASE_IN.apply(t) < t);
        }
    }

    #[test]
    fn ease_in_out_is_symmetric() {
        for t in [0.1_f32, 0.3, 0.45] {
            let sum = Easing::EASE_IN_OUT.apply(t) + Easing::EASE_IN_OUT.apply(1.0 - t);
            assert!((sum - 1.0).abs() < 1e-4, "{t}: {sum}");
        }
    }

    #[test]
    fn standard_curves_never_go_backwards() {
        for easing in ALL {
            let mut last = 0.0;
            for i in 1..=200 {
                let v = easing.apply(i as f32 / 200.0);
                assert!(v >= last - 1e-5, "{easing:?} at {i}: {v} < {last}");
                last = v;
            }
        }
    }

    #[test]
    fn out_of_range_control_x_values_are_clamped() {
        let wild = Easing::CubicBezier([-1.0, 0.0, 2.0, 1.0]);
        let tame = Easing::CubicBezier([0.0, 0.0, 1.0, 1.0]);
        assert_eq!(wild.apply(0.3), tame.apply(0.3));
    }
}
