// Package scale is the surface scale in the units of
// wp_fractional_scale_v1 (1/120ths) and the logical → buffer size rule
// windows and offscreen renders share (Rust src/render/scale.rs).
package scale

// Size is a width and height, in logical or in buffer pixels.
type Size struct{ W, H uint32 }

// Scale is a scale factor in 1/120ths, exactly as wp_fractional_scale_v1
// reports it (156 is 1.3). The zero Scale counts as 1.0.
type Scale uint32

// One is scale 1.0.
const One Scale = 120

// From120ths is the protocol's numerator over 120; zero is not a scale and
// becomes 1.0.
func From120ths(n uint32) Scale {
	if n == 0 {
		return One
	}
	return Scale(n)
}

// FromInteger is an integer wl_surface.preferred_buffer_scale.
func FromInteger(f uint32) Scale {
	n := uint64(f) * 120
	if n > 1<<32-1 {
		n = 1<<32 - 1
	}
	return From120ths(uint32(n))
}

// As120ths is the protocol's numerator over 120.
func (s Scale) As120ths() uint32 {
	if s == 0 {
		return uint32(One)
	}
	return uint32(s)
}

// Factor is the scale as a float, for shaders.
func (s Scale) Factor() float32 { return float32(s.As120ths()) / 120 }

// Physical is the buffer size for a logical size: each side times the
// scale, rounded half away from zero as wp_fractional_scale_v1 prescribes,
// and never zero.
func (s Scale) Physical(logical Size) Size {
	side := func(l uint32) uint32 {
		scaled := (uint64(l)*uint64(s.As120ths()) + 60) / 120
		return uint32(max(1, min(scaled, 1<<32-1)))
	}
	return Size{side(logical.W), side(logical.H)}
}
