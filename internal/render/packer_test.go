package render

import "testing"

// Ports of Rust src/render/packer.rs's tests.

type at struct {
	x, y uint32
	ok   bool
}

func alloc(p *shelfPacker, w, h uint32) at {
	x, y, ok := p.allocate(w, h)
	return at{x, y, ok}
}

func want(t *testing.T, got at, x, y uint32) {
	t.Helper()
	if got != (at{x, y, true}) {
		t.Errorf("placed at %+v, want (%d, %d)", got, x, y)
	}
}

func refused(t *testing.T, got at) {
	t.Helper()
	if got.ok {
		t.Errorf("placed at (%d, %d), want refused", got.x, got.y)
	}
}

func TestAllocateFillsAShelfLeftToRightWithPadding(t *testing.T) {
	p := newShelfPacker(100, 100)
	want(t, alloc(p, 32, 32), 0, 0)
	want(t, alloc(p, 32, 32), 33, 0)
	want(t, alloc(p, 32, 20), 66, 0)
	// 99 + 33 > 100: a new shelf below.
	want(t, alloc(p, 32, 32), 0, 33)
}

func TestAllocatePrefersTheTightestFittingShelf(t *testing.T) {
	p := newShelfPacker(100, 100)
	want(t, alloc(p, 10, 40), 0, 0)
	want(t, alloc(p, 95, 16), 0, 41)
	want(t, alloc(p, 10, 16), 11, 0)
	want(t, alloc(p, 3, 16), 96, 41)
}

func TestAllocateRefusesWhatDoesNotFit(t *testing.T) {
	p := newShelfPacker(64, 64)
	refused(t, alloc(p, 64, 10)) // padding included
	want(t, alloc(p, 40, 40), 0, 0)
	refused(t, alloc(p, 40, 40))
	refused(t, alloc(p, 1<<32-1, 1))
}

func TestGrowKeepsPlacementsAndMakesRoom(t *testing.T) {
	p := newShelfPacker(64, 64)
	want(t, alloc(p, 40, 40), 0, 0)
	refused(t, alloc(p, 40, 40))
	p.grow()
	if w, h := p.size(); w != 128 || h != 128 {
		t.Fatalf("size %dx%d after grow, want 128x128", w, h)
	}
	// The first shelf now has room on its right.
	want(t, alloc(p, 40, 40), 41, 0)
	want(t, alloc(p, 40, 40), 82, 0)
	want(t, alloc(p, 40, 40), 0, 41)
}
