//! The caller contract stays pinned: every test the README's "Caller
//! contract" section names as `path.rs::test_name` exists as a `#[test]`
//! in that file, so a renamed or deleted test cannot leave a promise
//! without its proof.

use std::path::Path;

/// The README's section under `## Caller contract`, up to the next `## `.
fn contract_section(readme: &str) -> &str {
    let start = readme
        .find("\n## Caller contract\n")
        .expect("README.md has no '## Caller contract' section");
    let body = &readme[start + 1..];
    let end = body[3..].find("\n## ").map_or(body.len(), |i| i + 3);
    &body[..end]
}

/// Every `path.rs::name` written inside backticks.
fn references(text: &str) -> Vec<(String, String)> {
    text.split('`')
        .skip(1)
        .step_by(2)
        .filter_map(|span| {
            let (path, name) = span.split_once(".rs::")?;
            let word = |s: &str| {
                !s.is_empty()
                    && s.bytes()
                        .all(|b| b.is_ascii_alphanumeric() || b == b'_' || b == b'/')
            };
            (word(path) && word(name)).then(|| (format!("{path}.rs"), name.to_owned()))
        })
        .collect()
}

/// Whether `source` declares `fn name(` right under a `#[test]`.
fn declares_test(source: &str, name: &str) -> bool {
    let signature = format!("fn {name}(");
    let mut previous = "";
    for line in source.lines().map(str::trim) {
        if line.starts_with(&signature) && previous == "#[test]" {
            return true;
        }
        if !line.is_empty() {
            previous = line;
        }
    }
    false
}

#[test]
fn every_test_the_caller_contract_names_exists() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR"));
    let readme = std::fs::read_to_string(root.join("README.md")).unwrap();
    let refs = references(contract_section(&readme));
    assert!(
        refs.len() >= 20,
        "the caller contract names only {} tests",
        refs.len()
    );
    let missing: Vec<String> = refs
        .iter()
        .filter(|(path, name)| {
            std::fs::read_to_string(root.join(path))
                .map_or(true, |source| !declares_test(&source, name))
        })
        .map(|(path, name)| format!("{path}::{name}"))
        .collect();
    assert!(missing.is_empty(), "no such test: {missing:#?}");
}

#[test]
fn references_are_read_from_code_spans_only() {
    let text = "see `tests/a.rs::one`, `src/x/y.rs::two` and tests/b.rs::three; `--dmenu`";
    assert_eq!(
        references(text),
        [
            ("tests/a.rs".to_owned(), "one".to_owned()),
            ("src/x/y.rs".to_owned(), "two".to_owned()),
        ]
    );
}

#[test]
fn a_test_counts_only_under_its_test_attribute() {
    let source = "#[test]\n\nfn pinned() {}\nfn helper() {}\n#[test]\nfn pinned_too_long() {}\n";
    assert!(declares_test(source, "pinned"));
    assert!(!declares_test(source, "helper"));
    assert!(!declares_test(source, "pinned_too"));
}
