//! Matching the query against every item with nucleo's fzf-like matcher
//! (spec §4.5): case-insensitive, accent-insensitive (a plain `e` matches
//! `é`), with fzf's syntax (`^prefix`, `suffix$`, `'exact`, `!not`, words
//! matched separately). Best scores first; equal scores keep input order.
//!
//! Incremental, so 100 000 streamed lines stay responsive:
//! - items arriving later are matched alone and merged in;
//! - a query that only grows (typing on) re-matches only the last matches;
//! - large sets are matched on scoped threads, one matcher each.

use nucleo_matcher::pattern::{CaseMatching, Normalization, Pattern};
use nucleo_matcher::{Config, Matcher, Utf32String};

/// Below this many candidates, matching stays on the calling thread.
const PARALLEL_MIN: usize = 8192;
/// At most this many matching threads.
const MAX_WORKERS: usize = 8;

/// One matching item: its index (input order) and its score.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Match {
    /// The item's index, in the order the items arrived.
    pub index: u32,
    /// How well it matches; higher is better (0 for an empty query).
    pub score: u32,
}

/// The items, the query and what matches it.
pub struct Filter {
    haystacks: Vec<Utf32String>,
    /// Per item, whether it was removed; shorter than `haystacks` when the
    /// later items never were (a dmenu list never removes any).
    removed: Vec<bool>,
    /// Per item, how much its matches score higher; shorter than
    /// `haystacks` when the later items have none.
    boosts: Vec<u32>,
    query: String,
    pattern: Pattern,
    matches: Vec<Match>,
    matchers: Vec<Matcher>,
}

impl Default for Filter {
    fn default() -> Self {
        Self::new()
    }
}

impl Filter {
    /// No items, an empty query.
    pub fn new() -> Self {
        Self {
            haystacks: Vec::new(),
            removed: Vec::new(),
            boosts: Vec::new(),
            query: String::new(),
            pattern: parse(""),
            matches: Vec::new(),
            matchers: Vec::new(),
        }
    }

    /// How many items there are.
    pub fn len(&self) -> usize {
        self.haystacks.len()
    }

    /// Whether there are no items.
    pub fn is_empty(&self) -> bool {
        self.haystacks.is_empty()
    }

    /// The matching items, best first.
    pub fn matches(&self) -> &[Match] {
        &self.matches
    }

    /// The query in force.
    pub fn query(&self) -> &str {
        &self.query
    }

    /// Adds items whose matches score `boost` higher (frecency), and
    /// matches them.
    pub fn push_boosted<'a>(&mut self, items: impl IntoIterator<Item = (&'a str, u32)>) {
        let first = self.haystacks.len();
        let items: Vec<(&str, u32)> = items.into_iter().collect();
        if items.iter().any(|&(_, boost)| boost > 0) {
            self.boosts.resize(first, 0);
            self.boosts.extend(items.iter().map(|&(_, boost)| boost));
        }
        self.push(items.into_iter().map(|(text, _)| text));
    }

    /// Adds items (the texts they are matched by) and matches them.
    pub fn push<'a>(&mut self, texts: impl IntoIterator<Item = &'a str>) {
        let first = self.haystacks.len();
        self.haystacks
            .extend(texts.into_iter().map(Utf32String::from));
        let fresh: Vec<u32> = (first..self.haystacks.len()).map(index).collect();
        if self.pattern.atoms.is_empty() {
            self.matches
                .extend(fresh.iter().map(|&index| Match { index, score: 0 }));
            return;
        }
        let found = self.score(&fresh);
        self.matches = merge(std::mem::take(&mut self.matches), found);
    }

    /// Removes item `index` for good: it never matches again, and the other
    /// items keep their indices.
    pub fn remove(&mut self, index: u32) {
        if !self.alive(index) {
            return;
        }
        self.removed.resize(self.haystacks.len(), false);
        self.removed[index as usize] = true;
        self.haystacks[index as usize] = Utf32String::default();
        self.matches.retain(|m| m.index != index);
    }

    /// Gives item `index` a new text to be matched by, in its place.
    pub fn update(&mut self, index: u32, text: &str) {
        if !self.alive(index) {
            return;
        }
        self.haystacks[index as usize] = Utf32String::from(text);
        self.matches.retain(|m| m.index != index);
        let found = if self.pattern.atoms.is_empty() {
            vec![Match { index, score: 0 }]
        } else {
            self.score(&[index])
        };
        self.matches = merge(std::mem::take(&mut self.matches), found);
    }

    /// Whether item `index` exists and was not removed.
    fn alive(&self, index: u32) -> bool {
        let i = index as usize;
        i < self.haystacks.len() && !self.removed.get(i).copied().unwrap_or(false)
    }

    /// Every item that was not removed, ascending.
    fn every(&self) -> Vec<u32> {
        (0..self.haystacks.len())
            .map(index)
            .filter(|&i| self.alive(i))
            .collect()
    }

    /// Matches every item against `query`; nothing happens if it is the
    /// query in force.
    pub fn set_query(&mut self, query: &str) {
        if query == self.query {
            return;
        }
        // Typing on only ever removes matches, unless what is typed can
        // widen them (a negation, or an escape that changes a word's kind).
        let narrows = query.starts_with(self.query.as_str())
            && !self.pattern.atoms.is_empty()
            && !query.contains(['!', '\\']);
        self.query = query.to_owned();
        self.pattern = parse(query);
        if self.pattern.atoms.is_empty() {
            self.matches = self
                .every()
                .into_iter()
                .map(|index| Match { index, score: 0 })
                .collect();
            return;
        }
        let candidates: Vec<u32> = if narrows {
            let mut previous: Vec<u32> = self.matches.iter().map(|m| m.index).collect();
            previous.sort_unstable();
            previous
        } else {
            self.every()
        };
        self.matches = self.score(&candidates);
    }

    /// The positions in item `index` that the query matched, as graphemes
    /// counted from 0, ascending (empty for an empty query or no match).
    pub fn positions(&mut self, index: u32) -> Vec<u32> {
        let mut positions = Vec::new();
        if self.pattern.atoms.is_empty() || index as usize >= self.haystacks.len() {
            return positions;
        }
        self.matchers(1);
        let haystack = self.haystacks[index as usize].slice(..);
        let matcher = &mut self.matchers[0];
        if self
            .pattern
            .indices(haystack, matcher, &mut positions)
            .is_none()
        {
            positions.clear();
        }
        positions.sort_unstable();
        positions.dedup();
        positions
    }

    /// Scores `candidates` (ascending indices), best first.
    fn score(&mut self, candidates: &[u32]) -> Vec<Match> {
        let workers = if candidates.len() < PARALLEL_MIN {
            1
        } else {
            std::thread::available_parallelism()
                .map_or(1, usize::from)
                .clamp(1, MAX_WORKERS)
        };
        self.matchers(workers);
        let (pattern, haystacks, boosts) = (&self.pattern, &self.haystacks, &self.boosts[..]);
        let chunk = candidates.len().div_ceil(workers).max(1);
        let mut found: Vec<Match> = if workers == 1 {
            score_chunk(
                pattern,
                haystacks,
                boosts,
                candidates,
                &mut self.matchers[0],
            )
        } else {
            std::thread::scope(|scope| {
                let jobs: Vec<_> = candidates
                    .chunks(chunk)
                    .zip(self.matchers.iter_mut())
                    .map(|(part, matcher)| {
                        scope.spawn(move || score_chunk(pattern, haystacks, boosts, part, matcher))
                    })
                    .collect();
                jobs.into_iter()
                    .flat_map(|job| job.join().expect("a matching thread"))
                    .collect()
            })
        };
        found.sort_unstable_by_key(key);
        found
    }

    /// At least `n` matchers (each holds about 135 KB of scratch memory).
    fn matchers(&mut self, n: usize) -> &mut [Matcher] {
        while self.matchers.len() < n {
            self.matchers.push(Matcher::new(Config::DEFAULT));
        }
        &mut self.matchers
    }
}

fn parse(query: &str) -> Pattern {
    Pattern::parse(query, CaseMatching::Ignore, Normalization::Smart)
}

fn index(i: usize) -> u32 {
    u32::try_from(i).expect("fewer than 2^32 items")
}

/// The sort key: best score first, then input order.
fn key(m: &Match) -> (std::cmp::Reverse<u32>, u32) {
    (std::cmp::Reverse(m.score), m.index)
}

fn score_chunk(
    pattern: &Pattern,
    haystacks: &[Utf32String],
    boosts: &[u32],
    candidates: &[u32],
    matcher: &mut Matcher,
) -> Vec<Match> {
    candidates
        .iter()
        .filter_map(|&index| {
            let haystack = haystacks[index as usize].slice(..);
            pattern.score(haystack, matcher).map(|score| Match {
                index,
                score: score + boosts.get(index as usize).copied().unwrap_or(0),
            })
        })
        .collect()
}

/// Two lists sorted by [`key`], merged into one.
fn merge(a: Vec<Match>, b: Vec<Match>) -> Vec<Match> {
    let mut out = Vec::with_capacity(a.len() + b.len());
    let (mut a, mut b) = (a.into_iter().peekable(), b.into_iter().peekable());
    loop {
        let next = match (a.peek(), b.peek()) {
            (Some(x), Some(y)) if key(x) <= key(y) => a.next(),
            (Some(_), Some(_)) => b.next(),
            (Some(_), None) => a.next(),
            (None, _) => b.next(),
        };
        match next {
            Some(m) => out.push(m),
            None => return out,
        }
    }
}

#[cfg(test)]
#[allow(clippy::single_range_in_vec_init)] // one highlighted range is a case
mod tests {
    use super::*;

    fn filter(items: &[&str]) -> Filter {
        let mut f = Filter::new();
        f.push(items.iter().copied());
        f
    }

    fn indices(f: &Filter) -> Vec<u32> {
        f.matches().iter().map(|m| m.index).collect()
    }

    #[test]
    fn an_empty_query_matches_everything_in_input_order() {
        let f = filter(&["b", "a", "c"]);
        assert_eq!(indices(&f), [0, 1, 2]);
        assert_eq!(f.len(), 3);
    }

    #[test]
    fn matching_is_fuzzy_and_best_first() {
        let mut f = filter(&["xaxbxc", "abc", "nothing", "a-b-c"]);
        f.set_query("abc");
        assert_eq!(indices(&f)[0], 1, "the contiguous match is best");
        assert!(!indices(&f).contains(&2));
        assert_eq!(f.matches().len(), 3);
    }

    #[test]
    fn matching_ignores_case_and_accents() {
        let mut f = filter(&["Café Noir", "CAFE", "tea"]);
        f.set_query("cafe");
        let mut found = indices(&f);
        found.sort_unstable();
        assert_eq!(found, [0, 1]);
        f.set_query("CAFÉ");
        assert_eq!(indices(&f), [0], "a typed accent is meant");
    }

    #[test]
    fn a_boost_raises_a_match_but_never_makes_one() {
        let mut f = Filter::new();
        f.push_boosted([("rite of oil", 0), ("rite of oil", 30), ("litany", 48)]);
        f.set_query("rite");
        assert_eq!(
            indices(&f),
            [1, 0],
            "the boosted twin first; no match from a boost"
        );
        f.set_query("");
        assert_eq!(indices(&f), [0, 1, 2], "an empty query keeps input order");
        let mut g = Filter::new();
        g.push_boosted([("firefox", 0), ("foreign rite fox", 8)]);
        g.set_query("firefox");
        assert_eq!(
            indices(&g),
            [0, 1],
            "a much better match beats a small boost"
        );
    }

    #[test]
    fn equal_scores_keep_input_order() {
        let mut f = filter(&["ab 2", "ab 1", "ab 3"]);
        f.set_query("ab");
        assert_eq!(indices(&f), [0, 1, 2]);
    }

    #[test]
    fn fzf_syntax_applies() {
        let mut f = filter(&["rite of oil", "oil rite", "litany"]);
        f.set_query("^rite");
        assert_eq!(indices(&f), [0]);
        f.set_query("!oil");
        assert_eq!(indices(&f), [2]);
        f.set_query("rite oil");
        assert_eq!(indices(&f).len(), 2);
    }

    #[test]
    fn fzf_suffix_and_exact_terms_apply() {
        let mut f = filter(&["rite of oil", "oil rite", "orbital"]);
        f.set_query("oil$");
        assert_eq!(indices(&f), [0]);
        f.set_query("'rbit");
        assert_eq!(indices(&f), [2]);
        f.set_query("rbt");
        assert_eq!(indices(&f), [2], "fuzzy without the quote");
        f.set_query("'rbt");
        assert_eq!(indices(&f), [] as [u32; 0]);
    }

    #[test]
    fn items_pushed_later_are_matched_and_merged_in_order() {
        let mut f = filter(&["xxa", "b"]);
        f.set_query("a");
        f.push(["a", "zzzzzzzza", "c"]);
        assert_eq!(f.len(), 5);
        let mut whole = filter(&["xxa", "b", "a", "zzzzzzzza", "c"]);
        whole.set_query("a");
        assert_eq!(f.matches(), whole.matches());
        assert_eq!(indices(&f)[0], 2, "the exact match ranks first");
    }

    #[test]
    fn a_growing_query_narrows_and_a_shrinking_one_widens() {
        let mut f = filter(&["abc", "abd", "xyz"]);
        f.set_query("ab");
        assert_eq!(f.matches().len(), 2);
        f.set_query("abc");
        assert_eq!(indices(&f), [0]);
        f.set_query("a");
        assert_eq!(f.matches().len(), 2);
        f.set_query("");
        assert_eq!(indices(&f), [0, 1, 2]);
        // A negation that grows excludes less: it must not narrow from the
        // last matches.
        let mut f = filter(&["abc", "acb", "xyz"]);
        f.set_query("!a");
        assert_eq!(indices(&f), [2]);
        f.set_query("!ab");
        assert_eq!(indices(&f), [1, 2]);
    }

    #[test]
    fn large_sets_match_in_parallel_exactly_as_one_thread_would() {
        let items: Vec<String> = (0..3 * PARALLEL_MIN)
            .map(|i| format!("{}-{i}", ["cogitator", "rite", "aspect"][i % 3]))
            .collect();
        let mut f = Filter::new();
        f.push(items.iter().map(String::as_str));
        f.set_query("cog1");
        let mut serial = Pattern::parse("cog1", CaseMatching::Ignore, Normalization::Smart)
            .match_list(
                items.iter().map(String::as_str),
                &mut Matcher::new(Config::DEFAULT),
            );
        serial.sort_by_key(|&(text, score)| {
            let index = items.iter().position(|i| i == text).unwrap();
            (std::cmp::Reverse(score), index)
        });
        let expected: Vec<&str> = serial.iter().map(|(t, _)| *t).collect();
        let got: Vec<&str> = f
            .matches()
            .iter()
            .map(|m| items[m.index as usize].as_str())
            .collect();
        assert_eq!(got, expected);
    }

    #[test]
    fn a_removed_item_never_matches_again() {
        let mut f = filter(&["abc", "abd", "xyz"]);
        f.remove(1);
        assert_eq!(indices(&f), [0, 2], "gone at once from an empty query");
        f.set_query("ab");
        assert_eq!(indices(&f), [0]);
        f.set_query("");
        assert_eq!(indices(&f), [0, 2]);
        f.set_query("x");
        f.set_query("");
        assert_eq!(indices(&f), [0, 2], "and after a full rematch");
        assert_eq!(f.len(), 3, "indices stay stable");
        f.push(["abe"]);
        f.set_query("ab");
        assert_eq!(indices(&f), [0, 3]);
    }

    #[test]
    fn an_updated_item_is_matched_by_its_new_text_in_its_place() {
        let mut f = filter(&["x-x-ab", "xyz", "x-x-abd"]);
        f.set_query("ab");
        f.update(1, "ab");
        assert_eq!(indices(&f)[0], 1, "the exact match ranks first");
        assert_eq!(f.matches().len(), 3);
        f.update(1, "nothing");
        assert_eq!(indices(&f), [0, 2]);
        f.set_query("");
        f.update(2, "q");
        assert_eq!(indices(&f), [0, 1, 2], "input order kept");
        f.remove(0);
        f.update(0, "ab");
        assert_eq!(indices(&f), [1, 2], "a removed item stays removed");
    }

    #[test]
    fn positions_are_graphemes_the_query_matched() {
        let mut f = filter(&["xaxbc", "e\u{301}tude"]);
        f.set_query("abc");
        assert_eq!(f.positions(0), [1, 3, 4]);
        assert_eq!(f.positions(1), Vec::<u32>::new());
        f.set_query("tud");
        assert_eq!(f.positions(1), [1, 2, 3], "é is one grapheme");
    }
}
