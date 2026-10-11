//! Rectangle packing for the image atlas: shelves (rows) filled left to
//! right, a new shelf opened below the last when none fits. Icons come in a
//! few sizes, so shelves stay tight. Growing doubles both sides and keeps
//! every placed rectangle where it is.

/// Free pixels kept right of and below every rectangle, so sampling never
/// bleeds into a neighbour.
const PADDING: u32 = 1;

#[derive(Clone, Copy, Debug)]
struct Shelf {
    y: u32,
    height: u32,
    /// The next free column.
    x: u32,
}

/// Places rectangles in a `width` × `height` area.
#[derive(Clone, Debug)]
pub(crate) struct ShelfPacker {
    width: u32,
    height: u32,
    shelves: Vec<Shelf>,
}

impl ShelfPacker {
    /// An empty area.
    pub fn new(width: u32, height: u32) -> Self {
        Self {
            width,
            height,
            shelves: Vec::new(),
        }
    }

    /// The area's size.
    pub fn size(&self) -> (u32, u32) {
        (self.width, self.height)
    }

    /// The top-left corner for a `width` × `height` rectangle, or `None` when
    /// the area is full. Picks the lowest shelf that fits with the least
    /// height to spare; opens a shelf otherwise.
    pub fn allocate(&mut self, width: u32, height: u32) -> Option<(u32, u32)> {
        let (w, h) = (width.checked_add(PADDING)?, height.checked_add(PADDING)?);
        let best = self
            .shelves
            .iter_mut()
            .filter(|s| s.height >= h && s.x.checked_add(w).is_some_and(|end| end <= self.width))
            .min_by_key(|s| s.height - h);
        if let Some(shelf) = best {
            let at = (shelf.x, shelf.y);
            shelf.x += w;
            return Some(at);
        }
        let y = self.shelves.last().map_or(0, |s| s.y + s.height);
        if w > self.width || y.checked_add(h)? > self.height {
            return None;
        }
        self.shelves.push(Shelf { y, height: h, x: w });
        Some((0, y))
    }

    /// Doubles both sides; placed rectangles keep their corners.
    pub fn grow(&mut self) {
        self.width = self.width.saturating_mul(2);
        self.height = self.height.saturating_mul(2);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn allocate_fills_a_shelf_left_to_right_with_padding() {
        let mut packer = ShelfPacker::new(100, 100);
        assert_eq!(packer.allocate(32, 32), Some((0, 0)));
        assert_eq!(packer.allocate(32, 32), Some((33, 0)));
        assert_eq!(packer.allocate(32, 20), Some((66, 0)));
        // 99 + 33 > 100: a new shelf below.
        assert_eq!(packer.allocate(32, 32), Some((0, 33)));
    }

    #[test]
    fn allocate_prefers_the_tightest_fitting_shelf() {
        let mut packer = ShelfPacker::new(100, 100);
        assert_eq!(packer.allocate(10, 40), Some((0, 0)));
        assert_eq!(packer.allocate(95, 16), Some((0, 41)));
        assert_eq!(packer.allocate(10, 16), Some((11, 0)));
        assert_eq!(packer.allocate(3, 16), Some((96, 41)));
    }

    #[test]
    fn allocate_refuses_what_does_not_fit() {
        let mut packer = ShelfPacker::new(64, 64);
        assert_eq!(packer.allocate(64, 10), None); // padding included
        assert_eq!(packer.allocate(40, 40), Some((0, 0)));
        assert_eq!(packer.allocate(40, 40), None);
        assert_eq!(packer.allocate(u32::MAX, 1), None);
    }

    #[test]
    fn grow_keeps_placements_and_makes_room() {
        let mut packer = ShelfPacker::new(64, 64);
        assert_eq!(packer.allocate(40, 40), Some((0, 0)));
        assert_eq!(packer.allocate(40, 40), None);
        packer.grow();
        assert_eq!(packer.size(), (128, 128));
        // The first shelf now has room on its right.
        assert_eq!(packer.allocate(40, 40), Some((41, 0)));
        assert_eq!(packer.allocate(40, 40), Some((82, 0)));
        assert_eq!(packer.allocate(40, 40), Some((0, 41)));
    }
}
