//! servitor's streaming census, `servitor census --vigil` (servitor caller
//! interface S1, S4, S5; codex passages `vigil`, `herald`, `canon`): one
//! JSON object per line, tagged by `herald`. A `rite` line is a whole census
//! entry that replaces any earlier one of the same rite; `lament` lines may
//! come anywhere; `concluded` comes once, last. Heralds and keys only ever
//! grow, so unknown ones are passed over.

use std::collections::BTreeMap;
use std::ffi::OsStr;
use std::io::{BufRead, BufReader};
use std::process::{Command, Stdio};
use std::thread;

use serde::Deserialize;

/// One line of the vigil.
#[derive(Clone, Debug, PartialEq, Eq, Deserialize)]
#[serde(tag = "herald", rename_all = "snake_case")]
pub enum Herald {
    /// A rite, whole.
    Rite(Entry),
    /// A lament, such as a data-slate that cannot be read.
    Lament {
        lament: String,
        #[serde(default)]
        rite: Option<String>,
    },
    /// Every rite is settled; the last line.
    Concluded,
    /// A herald of a later servitor.
    #[serde(other)]
    Unknown,
}

/// A census entry, as far as logis reads it (`last_rite` and `recorded_in`
/// are not needed and are passed over like any key yet unknown).
#[derive(Clone, Debug, Default, PartialEq, Eq, Deserialize)]
#[serde(default)]
pub struct Entry {
    pub rite: String,
    pub edict: bool,
    /// The aspect it stands in; `""` when none is known (and for an edict).
    pub aspect: String,
    /// performed, dormant, corrupted, tainted, desecrated, heretical, proclaimed.
    pub standing: String,
    /// What its auspex gave: `""`, `forgone`, `awaited`, `heeded` or `mute`.
    pub auspex: String,
    /// Its aspects in written order; `[]` for an edict.
    pub aspects: Vec<String>,
    pub purpose: String,
    pub desecrated: bool,
    /// Its inscriptions as declared, in written order.
    pub canon: Vec<Declaration>,
}

/// One inscription of a rite's canon (S5): every field is always present.
#[derive(Clone, Debug, Default, PartialEq, Eq, Deserialize)]
#[serde(default)]
pub struct Declaration {
    pub key: String,
    pub purpose: String,
    pub mandatory: bool,
    /// The decreed word per aspect, `"*"` already resolved; aspects without
    /// a decree are absent. `{}` for an edict.
    pub decrees: BTreeMap<String, String>,
    /// An edict's one decree; `None` for a rite.
    pub decree: Option<String>,
}

impl Entry {
    /// Its auspex has not settled yet: the entry will be told again.
    pub fn awaited(&self) -> bool {
        self.auspex == "awaited"
    }

    /// The aspect it stands in, when it may be trusted: the entry is
    /// settled by an auspex that answered, failed or does not exist (never
    /// `awaited`, nor `forgone` or a marker of a later servitor), and the
    /// aspect is one of its own.
    pub fn current(&self) -> Option<&str> {
        let settled = matches!(self.auspex.as_str(), "" | "heeded" | "mute");
        (settled && !self.edict && self.aspects.contains(&self.aspect))
            .then_some(self.aspect.as_str())
    }

    pub fn heretical(&self) -> bool {
        self.standing == "heretical"
    }
}

impl Declaration {
    /// The word servitor inscribes for `aspect` (or for an edict, `None`)
    /// when the invocation gives none.
    pub fn decreed(&self, aspect: Option<&str>) -> Option<&str> {
        match aspect {
            Some(aspect) => self.decrees.get(aspect).map(String::as_str),
            None => self.decree.as_deref(),
        }
    }
}

/// Reads one line of the vigil.
pub fn parse(line: &[u8]) -> Result<Herald, String> {
    serde_json::from_slice(line).map_err(|e| format!("a census line that cannot be read: {e}"))
}

/// What the vigil tells.
#[derive(Debug, PartialEq, Eq)]
pub enum Told {
    Herald(Herald),
    /// The census ended (after its stdout closed): how.
    Ended(String),
}

/// Starts `PROGRAM census --vigil` with stdin on /dev/null and stderr
/// inherited (the daemon's log); `told` hears each line while it returns
/// `true`, then how the census ended. Lines that cannot be read are logged
/// and passed over. Dropping the reader (the provider's exit) closes the
/// census's stdout, which ends the census and its auspexes (S4).
pub fn keep_vigil(
    program: &OsStr,
    mut told: impl FnMut(Told) -> bool + Send + 'static,
) -> Result<(), String> {
    let mut child = Command::new(program)
        .args(["census", "--vigil"])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .map_err(|e| e.to_string())?;
    let stdout = child.stdout.take().expect("piped");
    thread::Builder::new()
        .name("census".into())
        .spawn(move || {
            let mut lines = BufReader::new(stdout);
            let mut line = Vec::new();
            loop {
                line.clear();
                match lines.read_until(b'\n', &mut line) {
                    Ok(0) | Err(_) => break,
                    Ok(_) if line.iter().all(u8::is_ascii_whitespace) => {}
                    Ok(_) => match parse(&line) {
                        Ok(herald) => {
                            if !told(Told::Herald(herald)) {
                                return;
                            }
                        }
                        Err(e) => eprintln!("logis servitor: {e}"),
                    },
                }
            }
            let status = child
                .wait()
                .map_or_else(|e| format!("unknown ({e})"), |s| s.to_string());
            told(Told::Ended(status));
        })
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::servitor::testing::{Scratch, fixture};
    use std::sync::mpsc::channel;
    use std::time::Duration;

    /// The heralds of a recorded vigil, in order.
    fn recorded(name: &str) -> Vec<Herald> {
        std::fs::read_to_string(fixture(name))
            .unwrap()
            .lines()
            .filter(|l| l.starts_with('{'))
            .map(|l| parse(l.as_bytes()).unwrap())
            .collect()
    }

    fn rite(herald: &Herald) -> &Entry {
        match herald {
            Herald::Rite(entry) => entry,
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn a_recorded_vigil_reads_rite_by_rite_then_its_conclusion() {
        let heralds = recorded("vigil.jsonl");
        let names: Vec<&str> = heralds[..7].iter().map(|h| rite(h).rite.as_str()).collect();
        assert_eq!(
            names,
            [
                "broken",
                "dnd",
                "hush",
                "screenshot",
                "theme",
                "hush",
                "dnd"
            ]
        );
        assert_eq!(heralds[7], Herald::Concluded);
        let dnd = rite(&heralds[1]);
        assert!(dnd.awaited());
        assert_eq!(dnd.current(), None, "not while awaited");
        assert_eq!(dnd.aspects, ["on", "off"]);
        let settled = rite(&heralds[6]);
        assert_eq!(
            (settled.auspex.as_str(), settled.current()),
            ("heeded", Some("on"))
        );
        let reason = &settled.canon[0];
        assert_eq!((reason.key.as_str(), reason.mandatory), ("reason", true));
        assert_eq!(reason.purpose, "why the silence was imposed");
        let origin = &settled.canon[1];
        assert_eq!(origin.decreed(Some("off")), Some("the cogitator"));
        assert_eq!(origin.decreed(Some("on")), Some("the hotkey"));
        assert!(!rite(&heralds[5]).awaited(), "hush settles mute");
        assert_eq!(rite(&heralds[5]).current(), None, "and stands in no aspect");
    }

    #[test]
    fn edicts_and_heretical_rites_read_as_servitor_tells_them() {
        let heralds = recorded("vigil.jsonl");
        let broken = rite(&heralds[0]);
        assert!(broken.heretical() && broken.canon.is_empty() && broken.aspects.is_empty());
        let edict = rite(&heralds[3]);
        assert!(edict.edict && edict.aspects.is_empty());
        assert_eq!(edict.current(), None);
        assert_eq!(edict.canon[0].decreed(None), Some("chosen"));
        assert!(edict.canon[0].decrees.is_empty());
    }

    #[test]
    fn laments_and_unknown_heralds_and_keys_are_read_or_passed_over() {
        let lament = br#"{"herald":"lament","lament":"the data-slate is garbled","rite":"theme"}"#;
        assert_eq!(
            parse(lament),
            Ok(Herald::Lament {
                lament: "the data-slate is garbled".into(),
                rite: Some("theme".into())
            })
        );
        assert_eq!(parse(br#"{"herald":"omen","x":1}"#), Ok(Herald::Unknown));
        let later =
            br#"{"herald":"rite","rite":"t","auspex":"","aspects":["a"],"aspect":"a","sigil":7}"#;
        assert_eq!(rite(&parse(later).unwrap()).current(), Some("a"));
        assert!(parse(b"not json").is_err());
        assert!(parse(br#"{"rite":"t"}"#).is_err(), "no herald");
    }

    #[test]
    fn only_a_settled_aspect_of_the_rite_is_current() {
        let entry = |auspex: &str, aspect: &str| Entry {
            auspex: auspex.into(),
            aspect: aspect.into(),
            aspects: vec!["on".into(), "off".into()],
            ..Entry::default()
        };
        for (auspex, aspect, current) in [
            ("", "on", Some("on")),
            ("heeded", "off", Some("off")),
            ("mute", "on", Some("on")),
            ("awaited", "on", None),
            ("forgone", "on", None),
            ("a marker of a later servitor", "on", None),
            ("heeded", "", None),
            ("heeded", "sideways", None),
        ] {
            assert_eq!(
                entry(auspex, aspect).current(),
                current,
                "{auspex} {aspect}"
            );
        }
    }

    #[test]
    fn the_vigil_is_heard_line_by_line_then_its_end() {
        let dir = Scratch::new("vigil");
        let (tx, rx) = channel();
        let program = dir.fake(&fixture("vigil.jsonl"));
        keep_vigil(program.as_os_str(), move |t| tx.send(t).is_ok()).unwrap();
        let mut heralds = Vec::new();
        loop {
            match rx.recv_timeout(Duration::from_secs(5)).unwrap() {
                Told::Herald(h) => heralds.push(h),
                Told::Ended(status) => {
                    assert_eq!(status, "exit status: 0");
                    break;
                }
            }
        }
        assert_eq!(heralds, recorded("vigil.jsonl"));
    }

    #[test]
    fn a_census_that_cannot_start_is_an_error() {
        let err = keep_vigil(OsStr::new("/nonexistent/servitor"), |_| true).unwrap_err();
        assert!(!err.is_empty());
    }
}
