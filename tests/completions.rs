//! The shell completions in `contrib/completions/` (bash, zsh, fish) are
//! maintained by hand, so they are pinned to the parser: each offers
//! exactly the runes that `src/cli/` accepts, plus the `daemon`
//! subcommand. A rune added to or removed from the parser without its
//! completions fails here. The runes are read from the string literals of
//! the parser's source (everything before its `#[cfg(test)]` module), which
//! is where a new rune must appear to be accepted at all.
//!
//! bash and zsh are asked what they offer (zsh through a pseudo-terminal:
//! tests/fixtures/completions/zsh-capture.zsh); the fish script is read and,
//! when fish is installed, asked too. A shell that is not installed prints
//! `SKIPPED` (CI installs all three).

use std::collections::BTreeSet;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

fn root() -> &'static Path {
    Path::new(env!("CARGO_MANIFEST_DIR"))
}

fn completion(name: &str) -> PathBuf {
    root().join("contrib/completions").join(name)
}

fn read(path: &Path) -> String {
    std::fs::read_to_string(path).unwrap_or_else(|e| panic!("cannot read {}: {e}", path.display()))
}

/// Whether `c` may continue a rune's name (`--dmenu` is not `--dmenu0`).
fn name_char(c: char) -> bool {
    c.is_ascii_alphanumeric() || c == '-'
}

/// Every rune written as a string literal that starts with `-`: `"--lines"`,
/// `"-l"`, and the name of `"--config="`.
fn literal_runes(source: &str) -> BTreeSet<String> {
    source
        .split('"')
        .skip(1)
        .step_by(2)
        .filter_map(|lit| {
            let name = lit.strip_suffix('=').unwrap_or(lit);
            let long = name.strip_prefix("--").is_some_and(|n| n.len() > 1);
            let short = name.len() == 2 && name.starts_with('-') && name != "--";
            ((long || short) && name[1..].chars().all(name_char)).then(|| name.to_owned())
        })
        .collect()
}

/// The runes the parser accepts: the literals of `src/cli/*.rs` outside
/// their tests.
fn parser_runes() -> BTreeSet<String> {
    let mut runes = BTreeSet::new();
    for file in ["src/cli/mod.rs", "src/cli/dmenu.rs"] {
        let source = read(&root().join(file));
        let code = source.split("#[cfg(test)]").next().unwrap();
        runes.extend(literal_runes(code));
    }
    runes
}

/// The runes a fish completion offers: the `-l name` and `-s x` options of
/// its `complete` lines, before their description; quoted text is skipped.
fn fish_runes(script: &str) -> BTreeSet<String> {
    let mut runes = BTreeSet::new();
    for line in script
        .lines()
        .filter(|l| l.trim_start().starts_with("complete "))
    {
        let mut unquoted = String::new();
        let mut quote = None;
        for c in line.chars() {
            match (quote, c) {
                (None, '\'' | '"') => quote = Some(c),
                (Some(q), c) if c == q => quote = None,
                (None, c) => unquoted.push(c),
                _ => {}
            }
        }
        let words: Vec<&str> = unquoted
            .split_whitespace()
            .take_while(|w| !matches!(*w, "-d" | "--description"))
            .collect();
        for pair in words.windows(2) {
            match pair[0] {
                "-l" | "--long-option" => runes.insert(format!("--{}", pair[1])),
                "-s" | "--short-option" => runes.insert(format!("-{}", pair[1])),
                _ => false,
            };
        }
    }
    runes
}

fn assert_same_runes(shell: &str, offered: &BTreeSet<String>) {
    let known = parser_runes();
    let missing: Vec<_> = known.difference(offered).collect();
    let unknown: Vec<_> = offered.difference(&known).collect();
    assert!(
        missing.is_empty() && unknown.is_empty(),
        "the {shell} completion drifted from the parser:\n  \
         runes it does not offer: {missing:?}\n  runes the parser does not know: {unknown:?}"
    );
}

/// Runs `program` with `args` in the repository (killed after 20 s), or
/// `None` after printing `SKIPPED` when it is not installed. Fails unless it
/// exits 0.
fn run(program: &str, args: &[&str]) -> Option<String> {
    let mut child = match Command::new(program)
        .args(args)
        .current_dir(root())
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
    {
        Ok(child) => child,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            eprintln!("SKIPPED: {program} is not installed");
            return None;
        }
        Err(e) => panic!("cannot run {program}: {e}"),
    };
    let deadline = Instant::now() + Duration::from_secs(20);
    while child.try_wait().unwrap().is_none() {
        if Instant::now() > deadline {
            let _ = child.kill();
            panic!("{program} {args:?} did not finish within 20 s");
        }
        std::thread::sleep(Duration::from_millis(10));
    }
    let out = child.wait_with_output().unwrap();
    assert!(
        out.status.success(),
        "{program} {args:?} failed: {}",
        String::from_utf8_lossy(&out.stderr)
    );
    Some(String::from_utf8_lossy(&out.stdout).into_owned())
}

/// What the bash completion offers for `words` (the last one being
/// completed), in its order.
fn bash_complete(words: &[&str]) -> Option<Vec<String>> {
    let program = "source contrib/completions/logis.bash; COMP_WORDS=(\"$@\"); \
                   COMP_CWORD=$(($# - 1)); _logis; printf '%s\\n' \"${COMPREPLY[@]}\"";
    let mut args = vec!["--norc", "--noprofile", "-c", program, "bash"];
    args.extend(words);
    let out = run("bash", &args)?;
    Some(
        out.lines()
            .filter(|l| !l.is_empty())
            .map(str::to_owned)
            .collect(),
    )
}

/// What the zsh completion offers for `line`, in its order.
fn zsh_complete(line: &str) -> Option<Vec<String>> {
    let capture = "tests/fixtures/completions/zsh-capture.zsh";
    let out = run("zsh", &["-f", capture, "contrib/completions", line])?;
    Some(out.lines().map(str::to_owned).collect())
}

/// What the fish completion offers for `line` (candidates only).
fn fish_complete(line: &str) -> Option<Vec<String>> {
    let program = format!("source contrib/completions/logis.fish; complete -C '{line}'");
    let out = run("fish", &["--no-config", "-c", &program])?;
    Some(
        out.lines()
            .map(|l| l.split('\t').next().unwrap().to_owned())
            .collect(),
    )
}

/// Everything `complete` offers where a rune may stand: first, and after
/// `daemon`.
fn offered_runes(complete: impl Fn(&[&str]) -> Option<Vec<String>>) -> Option<BTreeSet<String>> {
    let mut runes = BTreeSet::new();
    for words in [&["logis", "-"][..], &["logis", "daemon", "-"]] {
        runes.extend(complete(words)?);
    }
    Some(runes)
}

#[test]
fn the_parser_runes_are_found_in_its_source() {
    let runes = parser_runes();
    for rune in [
        "--dmenu",
        "--dmenu0",
        "-d",
        "--config",
        "--idle-timeout",
        "--detach",
        "--with-nth",
        "-R",
        "-V",
    ] {
        assert!(runes.contains(rune), "{rune} not found in {runes:?}");
    }
    assert!(!runes.contains("--"), "{runes:?}");
}

#[test]
fn rune_scanners_read_words_not_fragments() {
    assert_eq!(
        literal_runes(r#"Some("--ab" | "-b") "--config=" "--" "x-y" "the rune --c""#),
        BTreeSet::from(["--ab", "-b", "--config"].map(str::to_owned))
    );
    assert_eq!(
        fish_runes(
            "# complete -l no\n\
             complete -c logis -n 'x -l quoted' -s d -l dmenu -d 'Read -l x'\n"
        ),
        BTreeSet::from(["--dmenu", "-d"].map(str::to_owned))
    );
}

#[test]
fn the_bash_completion_offers_every_rune() {
    if let Some(runes) = offered_runes(bash_complete) {
        assert_same_runes("bash", &runes);
    }
}

#[test]
fn the_zsh_completion_offers_every_rune() {
    let complete = |words: &[&str]| zsh_complete(&words.join(" "));
    if let Some(runes) = offered_runes(complete) {
        assert_same_runes("zsh", &runes);
    }
}

#[test]
fn the_fish_completion_offers_every_rune() {
    assert_same_runes("fish", &fish_runes(&read(&completion("logis.fish"))));
    let complete = |words: &[&str]| fish_complete(&words.join(" "));
    if let Some(runes) = offered_runes(complete) {
        assert_same_runes("fish (asked)", &runes);
    }
}

#[test]
fn every_shell_completes_a_partial_rune() {
    let dmenu = ["--dmenu", "--dmenu0"].map(str::to_owned);
    if let Some(offered) = bash_complete(&["logis", "--dm"]) {
        assert_eq!(offered, dmenu, "bash");
    }
    if let Some(offered) = zsh_complete("logis --dm") {
        assert_eq!(offered, dmenu, "zsh");
    }
    if let Some(offered) = fish_complete("logis --dm") {
        assert_eq!(offered, dmenu, "fish");
    }
}

#[test]
fn every_shell_completes_the_daemon_subcommand_first() {
    let daemon = ["daemon".to_owned()];
    if let Some(offered) = bash_complete(&["logis", "da"]) {
        assert_eq!(offered, daemon, "bash");
    }
    if let Some(offered) = zsh_complete("logis da") {
        assert_eq!(offered, daemon, "zsh");
    }
    if let Some(offered) = fish_complete("logis da") {
        assert_eq!(offered, daemon, "fish");
    }
}

#[test]
fn every_shell_offers_only_the_daemon_runes_after_daemon() {
    let runes = ["--idle-timeout", "--config", "--detach", "--debug-panel"];
    let runes = BTreeSet::from(runes.map(str::to_owned));
    let set = |v: Vec<String>| v.into_iter().collect::<BTreeSet<_>>();
    if let Some(offered) = bash_complete(&["logis", "daemon", "--"]) {
        assert_eq!(set(offered), runes, "bash");
    }
    if let Some(offered) = zsh_complete("logis daemon --") {
        assert_eq!(set(offered), runes, "zsh");
    }
    if let Some(offered) = fish_complete("logis daemon --") {
        assert_eq!(set(offered), runes, "fish");
    }
}

#[test]
fn modes_exclude_each_other_in_zsh_and_fish() {
    let modes = [
        "--dmenu",
        "--dmenu0",
        "--servitor",
        "--provider",
        "--version",
    ];
    let no_mode = |shell: &str, offered: Vec<String>| {
        let shown: Vec<_> = offered
            .iter()
            .filter(|o| modes.contains(&o.as_str()))
            .collect();
        assert!(shown.is_empty(), "{shell} offers {shown:?} after --apps");
        assert!(
            offered.contains(&"--config".to_owned()),
            "{shell}: {offered:?}"
        );
    };
    if let Some(offered) = zsh_complete("logis --apps -") {
        no_mode("zsh", offered);
    }
    if let Some(offered) = fish_complete("logis --apps -") {
        no_mode("fish", offered);
    }
}
