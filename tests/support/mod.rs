//! Test scaffolding: scratch directories and a fake Wayland compositor.

pub mod fake_compositor;
pub mod rig;

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicUsize, Ordering};

/// A scratch directory under the system temp dir, removed on drop.
pub struct ScratchDir(PathBuf);

impl ScratchDir {
    pub fn new() -> Self {
        static NEXT: AtomicUsize = AtomicUsize::new(0);
        let n = NEXT.fetch_add(1, Ordering::Relaxed);
        let path = std::env::temp_dir().join(format!("logis-test-{}-{n}", std::process::id()));
        std::fs::create_dir_all(&path).unwrap();
        Self(path)
    }

    pub fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for ScratchDir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
