//! The release stays consistent: the PKGBUILD (packaging/arch/), its
//! .SRCINFO, the man page and the changelog name the crate's version, the
//! package installs everything the repository ships for it, and it never
//! enables the systemd units (spec §4.1: the user enables them).

use std::collections::BTreeSet;
use std::path::Path;

const VERSION: &str = env!("CARGO_PKG_VERSION");

fn read(path: &str) -> String {
    let full = Path::new(env!("CARGO_MANIFEST_DIR")).join(path);
    std::fs::read_to_string(&full).unwrap_or_else(|e| panic!("cannot read {path}: {e}"))
}

/// The PKGBUILD without its comments.
fn pkgbuild_code() -> String {
    read("packaging/arch/PKGBUILD")
        .lines()
        .filter(|l| !l.trim_start().starts_with('#'))
        .collect::<Vec<_>>()
        .join("\n")
}

/// The values of `key = value` lines in the .SRCINFO.
fn srcinfo(key: &str) -> Vec<String> {
    read("packaging/arch/.SRCINFO")
        .lines()
        .filter_map(|l| l.trim().split_once(" = "))
        .filter(|(k, _)| *k == key)
        .map(|(_, v)| v.to_owned())
        .collect()
}

/// The quoted words of the PKGBUILD array `name=( ... )`.
fn pkgbuild_array(name: &str) -> Vec<String> {
    let code = pkgbuild_code();
    let start = code
        .find(&format!("\n{name}=("))
        .unwrap_or_else(|| panic!("the PKGBUILD has no {name}"));
    let body = &code[start + name.len() + 3..];
    // One line (`a=('x' 'y')`), or one word per line up to a lone `)`.
    let end = if body.starts_with('\n') {
        body.find("\n)")
    } else {
        body.find(')')
    };
    let body = &body[..end.unwrap()];
    body.split('\'')
        .skip(1)
        .step_by(2)
        .map(str::to_owned)
        .collect()
}

#[test]
fn the_package_and_its_srcinfo_carry_the_crate_version() {
    assert!(
        pkgbuild_code().contains(&format!("\npkgver={VERSION}\n")),
        "the PKGBUILD's pkgver is not {VERSION}"
    );
    assert_eq!(srcinfo("pkgver"), [VERSION]);
}

#[test]
fn the_srcinfo_lists_the_pkgbuild_dependencies() {
    for key in ["depends", "makedepends", "optdepends"] {
        let pkgbuild: BTreeSet<_> = pkgbuild_array(key).into_iter().collect();
        let srcinfo: BTreeSet<_> = srcinfo(key).into_iter().collect();
        assert_eq!(
            srcinfo, pkgbuild,
            "{key}: regenerate .SRCINFO (makepkg --printsrcinfo > .SRCINFO)"
        );
    }
}

#[test]
fn the_man_page_and_the_changelog_name_the_crate_version() {
    let man = read("docs/man/logis.1");
    let title = man.lines().find(|l| l.starts_with(".TH ")).unwrap();
    assert!(title.contains(&format!("\"logis {VERSION}\"")), "{title}");
    assert!(
        read("CHANGELOG.md").contains(&format!("\n## [{VERSION}]")),
        "CHANGELOG.md has no section for {VERSION}"
    );
}

#[test]
fn the_package_installs_what_the_repository_ships() {
    let code = pkgbuild_code();
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let mut shipped = vec!["docs/man/logis.1".to_owned(), "LICENSE".to_owned()];
    for dir in ["contrib/completions", "contrib/systemd", "schema"] {
        for entry in std::fs::read_dir(root.join(dir)).unwrap() {
            let name = entry.unwrap().file_name().into_string().unwrap();
            shipped.push(format!("{dir}/{name}"));
        }
    }
    let missing: Vec<_> = shipped.iter().filter(|f| !code.contains(*f)).collect();
    assert!(
        missing.is_empty(),
        "the PKGBUILD does not install {missing:?}"
    );
    for unit in ["logis.service", "logis.socket"] {
        assert!(code.contains(&format!("contrib/systemd/{unit}")), "{unit}");
    }
    assert!(code.contains("$pkgdir/usr/lib/systemd/user"), "{code}");
}

#[test]
fn the_package_never_enables_the_units() {
    let code = pkgbuild_code();
    for word in ["systemctl", ".wants", "install="] {
        assert!(!code.contains(word), "the PKGBUILD uses {word}");
    }
}

#[test]
fn the_service_spares_what_the_daemon_started() {
    let service = read("contrib/systemd/logis.service");
    assert!(
        service.lines().any(|l| l == "KillMode=process"),
        "{service}"
    );
}
