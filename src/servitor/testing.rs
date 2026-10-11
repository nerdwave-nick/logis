//! Test helpers: the fake servitor (tests/fixtures/servitor/servitor) and
//! scratch directories for its records.

use std::path::{Path, PathBuf};

/// A file of tests/fixtures/servitor.
pub fn fixture(name: &str) -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/servitor")
        .join(name)
}

/// A scratch directory, removed when dropped.
pub struct Scratch(PathBuf);

impl Scratch {
    pub fn new(name: &str) -> Self {
        let dir =
            std::env::temp_dir().join(format!("logis-servitor-{}-{name}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        Self(dir)
    }

    pub fn path(&self) -> &Path {
        &self.0
    }

    /// A `servitor` here that runs the fake on `census`, keeping its
    /// records in this directory.
    pub fn fake(&self, census: &Path) -> PathBuf {
        let wrapper = self.0.join("servitor");
        let script = format!(
            "#!/bin/sh\nFAKE_SERVITOR_DIR='{}' FAKE_SERVITOR_CENSUS='{}' exec '{}' \"$@\"\n",
            self.0.display(),
            census.display(),
            fixture("servitor").display()
        );
        std::fs::write(&wrapper, script).unwrap();
        let mut perms = std::fs::metadata(&wrapper).unwrap().permissions();
        std::os::unix::fs::PermissionsExt::set_mode(&mut perms, 0o755);
        std::fs::set_permissions(&wrapper, perms).unwrap();
        wrapper
    }
}

impl Drop for Scratch {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}
