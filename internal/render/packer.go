package render

// shelfPacker places rectangles in an area for the image atlas (Rust
// src/render/packer.rs): shelves (rows) filled left to right, a new shelf
// opened below the last when none fits. Icons come in a few sizes, so
// shelves stay tight. Growing doubles both sides and keeps every placed
// rectangle where it is.
type shelfPacker struct {
	width, height uint32
	shelves       []shelf
}

type shelf struct {
	y, height uint32
	x         uint32 // the next free column
}

func newShelfPacker(width, height uint32) *shelfPacker {
	return &shelfPacker{width: width, height: height}
}

func (p *shelfPacker) size() (uint32, uint32) { return p.width, p.height }

// padding is free pixels kept right of and below every rectangle, so
// sampling never bleeds into a neighbour.
const padding = 1

// allocate is the top-left corner for a width×height rectangle, or !ok
// when the area is full. It picks the shelf that fits with the least
// height to spare, and opens a shelf otherwise.
func (p *shelfPacker) allocate(width, height uint32) (x, y uint32, ok bool) {
	w, h := uint64(width)+padding, uint64(height)+padding
	best := -1
	for i, s := range p.shelves {
		if uint64(s.height) >= h && uint64(s.x)+w <= uint64(p.width) &&
			(best < 0 || s.height < p.shelves[best].height) {
			best = i
		}
	}
	if best >= 0 {
		s := &p.shelves[best]
		x, y = s.x, s.y
		s.x += uint32(w)
		return x, y, true
	}
	if n := len(p.shelves); n > 0 {
		y = p.shelves[n-1].y + p.shelves[n-1].height
	}
	if w > uint64(p.width) || uint64(y)+h > uint64(p.height) {
		return 0, 0, false
	}
	p.shelves = append(p.shelves, shelf{y: y, height: uint32(h), x: uint32(w)})
	return 0, y, true
}

// grow doubles both sides; placed rectangles keep their corners.
func (p *shelfPacker) grow() {
	double := func(v uint32) uint32 { return uint32(min(uint64(v)*2, 1<<32-1)) }
	p.width, p.height = double(p.width), double(p.height)
}
