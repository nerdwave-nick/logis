//! Differential oracle for logis's Go matcher (internal/fuzzy).
//!
//! Runs nucleo-matcher 0.3.1 exactly as Rust logis v1's `Filter` did
//! (`Pattern::parse(query, CaseMatching::Ignore, Normalization::Smart)`,
//! `Config::DEFAULT`, haystacks as `Utf32String`, positions sorted and
//! deduplicated) over a deterministic corpus, and prints JSON:
//!
//! - `items`, `queries`: the corpus;
//! - `cases`: `[item, query, score or null, positions]` for every pair tried;
//! - `fold`: `[c, folded]` for every char `to_lower_case` changes;
//! - `normalize`: `[c, normalized]` for every char `normalize` changes;
//! - `classes`: `[lo, hi, class]` ranges of nucleo's class of non-ASCII chars
//!   (w whitespace, n non-word, l lower, u upper, a letter, d number).
//!
//! Standalone, offline: `cargo run --release --offline > ../nucleo.json`.

use nucleo_matcher::chars;
use nucleo_matcher::pattern::{CaseMatching, Normalization, Pattern};
use nucleo_matcher::{Config, Matcher, Utf32String};

fn json_str(out: &mut String, s: &str) {
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
}

/// xorshift64*, so the corpus is the same on every run.
struct Rng(u64);
impl Rng {
    fn next(&mut self) -> u64 {
        self.0 ^= self.0 >> 12;
        self.0 ^= self.0 << 25;
        self.0 ^= self.0 >> 27;
        self.0.wrapping_mul(0x2545_F491_4F6C_DD1D)
    }
    fn below(&mut self, n: usize) -> usize {
        (self.next() % n as u64) as usize
    }
    fn pick<'a>(&mut self, xs: &[&'a str]) -> &'a str {
        xs[self.below(xs.len())]
    }
}

const FIXED_ITEMS: &[&str] = &[
    "rite of oil", "oil rite", "litany", "orbital", "rite of ignition", "Café Noir", "CAFE", "tea",
    "xaxbxc", "abc", "abd", "acb", "xyz", "a-b-c", "nothing", "firefox", "foreign rite fox",
    "e\u{301}tude", "xaxbc", "fooBarbaz1", "fooBarbaz", "/usr/share/doc/at/ChangeLog", "foo bar baz",
    "Foo Bar Baz", "/AutomatorDocument.icns", "/man1/zshcompctl.1", "/.oh-my-zsh/cache",
    "ab0123 456", "abc123 456", "foo-bar", "Só Danço Samba", "Só Danç", "你好世界", "你好界",
    "Ἐν ἀρχῇ ἦν ὁ λόγος", "機械の精霊", "naïve café", "ÆSIR øre Ørsted", "straße STRASSE ẞ",
    "İstanbul ıi", "ΣΊΣΥΦΟΣ ς σ", "Ⲽxx", "  padded  ", "\tTAB\tsep", " ",
    "mouse-autohide-toggle\toff\tVeil the cursor when idle", "vox\tauto\tWho hears the machine",
    "--FOO --foo", "éab", "café x canon nero", "a\\b c", "a b", "foo$", "^foo", "foo", "!bang",
    "'quote", "👍🏽 thumbs up", "🇩🇪🇫🇷 flags", "한국어 문장", "Cafe\u{301} decomposed", "ǅemal ǆ",
    "\u{fb01}le ligature", "\u{212a}elvin K", "ℌilbert", "ＡＢＣ fullwidth", "x1y2z3", "CamelCaseWord",
    "snake_case_word", "kebab-case-word", "path/to:file;x|y,z", "ۂ(GCGɴCG", "Ꭰꭰ cherokee",
    "ab 1", "ab 2", "ab 3", "ab", "x-x-ab", "x-x-abd", "zzzzzzzza", "xxa", "a", "b", "c",
    "cogitator-1", "rite-2", "aspect-3", "Ññ ÇÇ Łódź Đorđe Œuvre æther þorn", "ŁÓDŹ łódź",
    "machine spirit", "MACHINE SPIRIT", "mAcHiNe", "the rite of the long litany that never ends",
    "do-not-disturb\ton\tSilence the vox of the desktop", "power-profile\t·\tThe hunger",
    "ﬀ ﬃ", "Æ", "ø", "\u{0130}", "ı", "ß", "ẞ", "ς", "Σ", "ɴ",
];

const FIXED_QUERIES: &[&str] = &[
    "", " ", "a", "b", "abc", "ab", "rite", "^rite", "oil$", "'rbit", "rbt", "'rbt", "!oil", "!a",
    "!ab", "rite oil", "cafe", "CAFÉ", "café", "Café", "CAFE", "e", "é", "tud", "etude", "obr",
    "changelog", "br", "fbb", "rdoc", "zshc", "12356", "o-ba", "sox", "so", "sod", "soc", "你",
    "世界", "ss", "strasse", "ß", "ẞ", "i", "ı", "İ", "istanbul", "σ", "ς", "Σ", "ⲽ", "Ⲽ", "^foo$",
    "foo", "\\^foo", "foo\\$", "\\!bang", "!^foo", "!foo$", "--foo", "'--foo", "'ab", "'éa", "x",
    "n", "a\\ b", "café\\ noir", "a\\b", "😀", "👍", "de", "한", "fi", "k", "kelvin", "ab$", "^ab",
    "^ab$", "o", "e\u{301}", "\\ ", "word", "case", "wcw", "x1", "123", "pad", "padded$",
    "^padded", "^  padded", "padded  $", "tab", "veil", "mat", "aaa", "ae", "oe", "o", "l", "lodz",
    "łódź", "dorde", "oeuvre", "aether", "thorn", "ñ", "n", "ç", "ms", "machine spirit", "MACHINE",
    "mAcHiNe", "^MACH", "spirit$", "!machine", "!^mach", "!rit$", "'chin", "!'chin", "cog1", "a$",
    "^a", "b$", "''", "!", "^", "$", "^$", "'", "\\", "the rite", "long", "ⓐ", "abcdefghijklmnopq",
    "ꭰ", "Ꭰ", "ab0", "456", "z", "zz", "ǆ", "ǅ", "le", "ﬁ", "ab ab", "a a a", "rite rite",
    "o-o", "/", "/usr", "doc/at", ":", ";x|", "x|y", "silence", "hunger", "·",
];

fn random_item(rng: &mut Rng) -> String {
    const PARTS: &[&str] = &[
        "a", "b", "c", "e", "o", "r", "s", "t", "x", "A", "B", "R", "T", "é", "É", "ø", "ß", "ü",
        "ñ", "ł", "σ", "Σ", "ς", "λ", "ж", "Ж", "你", "界", "😀", "👍🏽", "e\u{301}", " ", "  ", "-",
        "_", "/", ":", ";", ",", "|", ".", "1", "2", "9", "\t", "'", "^", "$", "!", "\\", "rite",
        "oil", "Forge", "machine", "spirit", "Cogitator", "cafe", "café", "x86_64", "fooBar",
    ];
    let len = rng.below(14);
    let mut s = String::new();
    for _ in 0..len {
        s.push_str(rng.pick(PARTS));
    }
    s
}

/// A query likely to match `item`: some of its graphemes in order, with
/// case flips, and now and then fzf syntax.
fn random_query(rng: &mut Rng, item: &str) -> String {
    use unicode_segmentation::UnicodeSegmentation;
    let graphemes: Vec<&str> = item.graphemes(true).collect();
    let mut q = String::new();
    if !graphemes.is_empty() {
        let mut i = rng.below(graphemes.len());
        let want = 1 + rng.below(5);
        let mut taken = 0;
        while i < graphemes.len() && taken < want {
            let g = graphemes[i];
            match rng.below(4) {
                0 => q.push_str(&g.to_uppercase()),
                _ => q.push_str(g),
            }
            taken += 1;
            i += 1 + rng.below(3);
        }
    }
    match rng.below(10) {
        0 => format!("^{q}"),
        1 => format!("{q}$"),
        2 => format!("'{q}"),
        3 => format!("!{q}"),
        4 => format!("{q} {}", rng.pick(&["a", "e", "rite", "x"])),
        _ => q,
    }
}

fn class_char(c: char) -> char {
    // nucleo's char_class_non_ascii, through its public pieces.
    if c.is_lowercase() {
        'l'
    } else if chars::is_upper_case(c) {
        'u'
    } else if c.is_numeric() {
        'd'
    } else if c.is_alphabetic() {
        'a'
    } else if c.is_whitespace() {
        'w'
    } else {
        'n'
    }
}

fn main() {
    let mut rng = Rng(0x9E37_79B9_7F4A_7C15);
    let mut items: Vec<String> = FIXED_ITEMS.iter().map(|s| s.to_string()).collect();
    let mut queries: Vec<String> = FIXED_QUERIES.iter().map(|s| s.to_string()).collect();
    let mut pairs: Vec<(usize, usize)> = Vec::new();
    for i in 0..FIXED_ITEMS.len() {
        for q in 0..FIXED_QUERIES.len() {
            pairs.push((i, q));
        }
    }
    // Random items, each with random queries drawn from it and from others.
    for _ in 0..400 {
        let item = random_item(&mut rng);
        let i = items.len();
        items.push(item.clone());
        for _ in 0..6 {
            let source = if rng.below(3) == 0 {
                items[rng.below(items.len())].clone()
            } else {
                item.clone()
            };
            let q = queries.len();
            queries.push(random_query(&mut rng, &source));
            pairs.push((i, q));
        }
    }
    // Long items: past the matrix limit nucleo falls back to greedy matching.
    let long_queries = ["abc", "rite", "zq", "a b", "xyz", "'rite", "^ab", "oil$", "éz", "eoz", "!q"];
    let long_items = [
        format!("{}rite of oil", "ab-c xy ".repeat(2000)),
        format!("x{}z q", "a_b-c ".repeat(5000)),
        format!("{}éz end", "é a b c ".repeat(3000)),
        format!("{}xyz", "abc ".repeat(17000)),
        format!("q{}", "e o z r i t e ".repeat(900)),
    ];
    for item in long_items {
        let i = items.len();
        items.push(item);
        for lq in long_queries {
            let q = queries.len();
            queries.push(lq.to_string());
            pairs.push((i, q));
        }
    }

    let mut matcher = Matcher::new(Config::DEFAULT);
    let haystacks: Vec<Utf32String> = items.iter().map(|s| Utf32String::from(s.as_str())).collect();
    let patterns: Vec<Pattern> = queries
        .iter()
        .map(|q| Pattern::parse(q, CaseMatching::Ignore, Normalization::Smart))
        .collect();

    let mut out = String::from("{\n\"items\": [");
    for (n, item) in items.iter().enumerate() {
        if n > 0 {
            out.push(',');
        }
        out.push('\n');
        json_str(&mut out, item);
    }
    out.push_str("\n],\n\"queries\": [");
    for (n, q) in queries.iter().enumerate() {
        if n > 0 {
            out.push(',');
        }
        out.push('\n');
        json_str(&mut out, q);
    }
    out.push_str("\n],\n\"cases\": [");
    for (n, &(i, q)) in pairs.iter().enumerate() {
        let haystack = haystacks[i].slice(..);
        let pattern = &patterns[q];
        let score = pattern.score(haystack, &mut matcher);
        let mut positions = Vec::new();
        if !pattern.atoms.is_empty()
            && pattern.indices(haystack, &mut matcher, &mut positions).is_none()
        {
            positions.clear();
        }
        positions.sort_unstable();
        positions.dedup();
        if n > 0 {
            out.push(',');
        }
        let score = score.map_or("null".to_string(), |s| s.to_string());
        let positions: Vec<String> = positions.iter().map(u32::to_string).collect();
        out.push_str(&format!("\n[{i},{q},{score},[{}]]", positions.join(",")));
    }
    out.push_str("\n],\n\"fold\": [");
    let all = (0..=0x10FFFFu32).filter_map(char::from_u32);
    let mut first = true;
    for c in all.clone() {
        let f = chars::to_lower_case(c);
        if f != c {
            out.push_str(if first { "\n" } else { ",\n" });
            first = false;
            out.push_str(&format!("[{},{}]", c as u32, f as u32));
        }
    }
    out.push_str("\n],\n\"normalize\": [");
    first = true;
    for c in all.clone() {
        let f = chars::normalize(c);
        if f != c {
            out.push_str(if first { "\n" } else { ",\n" });
            first = false;
            out.push_str(&format!("[{},{}]", c as u32, f as u32));
        }
    }
    out.push_str("\n],\n\"classes\": [");
    first = true;
    let mut run: Option<(u32, u32, char)> = None;
    let flush = |out: &mut String, r: (u32, u32, char), first: &mut bool| {
        out.push_str(if *first { "\n" } else { ",\n" });
        *first = false;
        out.push_str(&format!("[{},{},\"{}\"]", r.0, r.1, r.2));
    };
    for c in all.filter(|c| !c.is_ascii()) {
        let k = class_char(c);
        let u = c as u32;
        run = match run {
            Some((lo, hi, kk)) if kk == k && hi + 1 == u => Some((lo, u, k)),
            Some(r) => {
                flush(&mut out, r, &mut first);
                Some((u, u, k))
            }
            None => Some((u, u, k)),
        };
    }
    if let Some(r) = run {
        flush(&mut out, r, &mut first);
    }
    out.push_str("\n]\n}\n");
    print!("{out}");
}
