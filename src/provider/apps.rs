//! The app launcher as a built-in provider (spec §3 stories 5–7, §4.7): the
//! applications of the daemon's index as the first stage, most frecent
//! first, matched by name, generic name, keywords and program; an app's
//! desktop actions as its second stage.
//!
//! - Return on an app launches it; Shift+Return on an app with actions opens
//!   them (the app itself first, then each action), and Return there
//!   launches the one chosen. Shift+Return on an app without actions
//!   launches it.
//! - A launch the daemon could not carry out is answered with an error
//!   toast, and the stage takes Return again.

use std::path::Path;

use logis::apps::frecency::Frecency;
use logis::apps::index::{App, Index};
use logis::apps::plan::plan;
use logis::launch::Plan;
use logis::list::{Decor, Entry, Level, Notice, Options};

use super::{Answer, Step};

/// What the user's answer asks of the daemon.
#[derive(Debug, PartialEq)]
pub enum Said {
    /// Steps for the window (the actions stage, or nothing).
    Steps(Vec<Step>),
    /// Launch this, then note the launch of app `id` for frecency.
    Launch { plan: Plan, id: String },
}

/// One apps session's stages.
#[derive(Debug, Default)]
pub struct Apps {
    stage: Stage,
}

#[derive(Debug, Default)]
enum Stage {
    /// The applications: desktop-file IDs by row.
    #[default]
    Empty,
    List(Vec<String>),
    /// One app's actions: `None` (the app itself) or an action ID, by row.
    Actions {
        id: String,
        rows: Vec<Option<String>>,
    },
}

/// The first stage's options.
pub fn options() -> Options {
    Options {
        placeholder: "summon an application".into(),
        width: 40,
        only_match: true,
        ..Options::default()
    }
}

impl Apps {
    /// The first stage: every app of `index`, the most frecent first (at
    /// `now`, seconds since the epoch), then by name.
    pub fn open(index: &Index, frecency: &Frecency, now: u64) -> (Self, Step) {
        let mut ranked: Vec<(u32, &App)> = index
            .apps()
            .iter()
            .map(|app| (frecency.boost(&app.id, now), app))
            .collect();
        // Stable: equal boosts keep the index's order by name.
        ranked.sort_by_key(|(boost, _)| std::cmp::Reverse(*boost));
        let ids = ranked.iter().map(|(_, app)| app.id.clone()).collect();
        let entries = ranked
            .iter()
            .map(|&(boost, app)| entry(app, boost))
            .collect();
        let apps = Self {
            stage: Stage::List(ids),
        };
        (apps, stage(options(), entries))
    }

    /// The user's answer; `terminal` runs `Terminal=true` apps.
    pub fn answer(&mut self, answer: Answer, index: &Index, terminal: &[String]) -> Said {
        let (row, descend) = match answer {
            Answer::Item(row) => (row, false),
            Answer::Descend(row) => (row, true),
            Answer::Typed(_) | Answer::Dismissed => return Said::Steps(Vec::new()),
        };
        let (id, action) = match &self.stage {
            Stage::List(ids) => match ids.get(row as usize) {
                Some(id) => (id.clone(), None),
                None => return Said::Steps(Vec::new()),
            },
            Stage::Actions { id, rows } => match rows.get(row as usize) {
                Some(action) => (id.clone(), action.clone()),
                None => return Said::Steps(Vec::new()),
            },
            Stage::Empty => return Said::Steps(Vec::new()),
        };
        let Some(app) = index.get(&id) else {
            return Said::Steps(failed(format!("{id} is gone from the census")));
        };
        let listing = matches!(self.stage, Stage::List(_));
        if descend && listing && !app.entry.actions.is_empty() {
            let (rows, step) = actions(app);
            self.stage = Stage::Actions { id, rows };
            return Said::Steps(vec![step]);
        }
        match plan(app, action.as_deref(), terminal) {
            Ok(plan) => Said::Launch { plan, id },
            Err(e) => Said::Steps(failed(format!(
                "{} cannot be summoned: {e}",
                app.entry.name
            ))),
        }
    }
}

/// A launch that failed, as the window shows it: an error toast, and
/// Return taken again.
pub fn failed(message: String) -> Vec<Step> {
    let notice = Notice {
        text: message,
        level: Level::Error,
    };
    let span = std::time::Duration::from_secs(5);
    vec![Step::Toast { notice, span }, Step::Ready]
}

fn stage(options: Options, entries: Vec<Entry>) -> Step {
    Step::Stage { options, entries }
}

/// An app's row: its name, matched also by generic name, keywords and
/// program, with its icon and frecency boost.
fn entry(app: &App, boost: u32) -> Entry {
    let e = &app.entry;
    let program = e
        .exec
        .as_ref()
        .and_then(|x| Path::new(x.program()).file_name())
        .map(|p| p.to_string_lossy().into_owned());
    let keywords = (!e.keywords.is_empty()).then(|| e.keywords.join(" "));
    let more: Vec<String> = [e.generic_name.clone(), keywords, program]
        .into_iter()
        .flatten()
        .filter(|m| !m.is_empty())
        .collect();
    let match_text = (!more.is_empty()).then(|| format!("{}\t{}", e.name, more.join("\t")));
    decorated(e.name.clone(), match_text, e.icon.clone(), boost)
}

fn decorated(text: String, match_text: Option<String>, icon: Option<String>, boost: u32) -> Entry {
    Entry {
        text,
        match_text,
        decor: Some(Box::new(Decor {
            icon,
            boost,
            ..Decor::default()
        })),
    }
}

/// An app's actions stage: the app itself, then each action.
fn actions(app: &App) -> (Vec<Option<String>>, Step) {
    let e = &app.entry;
    let mut rows = vec![None];
    let mut entries = vec![decorated(e.name.clone(), None, e.icon.clone(), 0)];
    for action in &e.actions {
        rows.push(Some(action.id.clone()));
        let icon = action.icon.clone().or_else(|| e.icon.clone());
        entries.push(decorated(action.name.clone(), None, icon, 0));
    }
    let options = Options {
        prompt: format!("{}: ", e.name),
        placeholder: String::new(),
        ..options()
    };
    (rows, stage(options, entries))
}

#[cfg(test)]
mod tests {
    use super::*;
    use logis::apps::index::{Context, Dirs};
    use logis::apps::locale::Locale;
    use std::path::Path;

    const NOW: u64 = 1_760_000_000;

    fn index() -> Index {
        let fixtures = Path::new(env!("CARGO_MANIFEST_DIR")).join("tests/fixtures/apps");
        let dirs = Dirs(vec![fixtures.join("system/applications")]);
        let cx = Context {
            locale: Locale::default(),
            desktops: vec!["niri".into()],
            path: vec![fixtures.join("bin")],
        };
        Index::scan(&dirs, &cx)
    }

    fn opened(frecency: &Frecency) -> (Apps, Options, Vec<Entry>, Index) {
        let index = index();
        let (apps, step) = Apps::open(&index, frecency, NOW);
        let Step::Stage { options, entries } = step else {
            panic!("{step:?}");
        };
        (apps, options, entries, index)
    }

    fn texts(entries: &[Entry]) -> Vec<&str> {
        entries.iter().map(|e| e.text.as_str()).collect()
    }

    #[test]
    fn the_first_stage_lists_apps_by_name_with_icons_and_match_texts() {
        let (_, options, entries, _) = opened(&Frecency::default());
        assert!(options.only_match, "an app list takes no typed text");
        assert_eq!(
            texts(&entries),
            [
                "Anoint",
                "Broken in home",
                "Bus Spirit",
                "Cogitator shell",
                "Deleted in home",
                "Oil (system)",
                "Only in niri",
                "Present TryExec"
            ]
        );
        let anoint = &entries[0];
        assert_eq!(
            anoint.match_text.as_deref(),
            Some("Anoint\tHoly Oil\toil unguent blessing\tanoint")
        );
        let decor = anoint.decor.as_ref().unwrap();
        assert_eq!(decor.icon.as_deref(), Some("forge-cog"));
        assert_eq!(decor.boost, 0);
    }

    #[test]
    fn frecent_apps_come_first_and_are_boosted() {
        let mut f = Frecency::default();
        f.record("forge-oil.desktop", NOW);
        f.record("forge-oil.desktop", NOW);
        f.record("forge-terminal.desktop", NOW - 100 * 24 * 3600);
        let (_, _, entries, _) = opened(&f);
        assert_eq!(
            &texts(&entries)[..3],
            ["Oil (system)", "Cogitator shell", "Anoint"]
        );
        assert!(
            entries[0].decor.as_ref().unwrap().boost > entries[1].decor.as_ref().unwrap().boost
        );
    }

    fn row(entries: &[Entry], text: &str) -> u32 {
        entries.iter().position(|e| e.text == text).unwrap() as u32
    }

    #[test]
    fn return_launches_the_app() {
        let (mut apps, _, entries, index) = opened(&Frecency::default());
        let said = apps.answer(Answer::Item(row(&entries, "Anoint")), &index, &[]);
        let Said::Launch { plan, id } = said else {
            panic!("{said:?}");
        };
        assert_eq!(id, "forge-anoint.desktop");
        assert_eq!(plan.argv[0], "anoint");
        assert_eq!(plan.action, None);
    }

    #[test]
    fn shift_return_opens_the_actions_and_return_there_launches_one() {
        let (mut apps, _, entries, index) = opened(&Frecency::default());
        let said = apps.answer(Answer::Descend(row(&entries, "Anoint")), &index, &[]);
        let Said::Steps(steps) = said else {
            panic!("{said:?}")
        };
        let [
            Step::Stage {
                options,
                entries: actions,
            },
        ] = &steps[..]
        else {
            panic!("{steps:?}");
        };
        assert_eq!(options.prompt, "Anoint: ");
        assert_eq!(texts(actions), ["Anoint", "Purge", "Bless"]);
        let icons: Vec<_> = actions
            .iter()
            .map(|a| a.decor.as_ref().and_then(|d| d.icon.as_deref()))
            .collect();
        assert_eq!(
            icons,
            [Some("forge-cog"), Some("forge-purge"), Some("forge-cog")]
        );
        let Said::Launch { plan, id } = apps.answer(Answer::Item(1), &index, &[]) else {
            panic!("no launch");
        };
        assert_eq!(
            (id.as_str(), plan.action.as_deref()),
            ("forge-anoint.desktop", Some("purge"))
        );
        let Said::Launch { plan, .. } = apps.answer(Answer::Descend(0), &index, &[]) else {
            panic!("no launch");
        };
        assert_eq!(plan.action, None, "the app itself");
    }

    #[test]
    fn shift_return_on_an_app_without_actions_launches_it() {
        let (mut apps, _, entries, index) = opened(&Frecency::default());
        let said = apps.answer(Answer::Descend(row(&entries, "Oil (system)")), &index, &[]);
        assert!(matches!(said, Said::Launch { .. }), "{said:?}");
    }

    #[test]
    fn an_app_that_cannot_be_planned_says_why_and_takes_return_again() {
        let (mut apps, _, entries, index) = opened(&Frecency::default());
        let said = apps.answer(Answer::Item(row(&entries, "Cogitator shell")), &index, &[]);
        let Said::Steps(steps) = said else {
            panic!("{said:?}")
        };
        let [Step::Toast { notice, .. }, Step::Ready] = &steps[..] else {
            panic!("{steps:?}");
        };
        assert_eq!(notice.level, Level::Error);
        assert!(notice.text.contains("terminal"), "{}", notice.text);
        assert_eq!(failed("x".into()).len(), 2);
    }

    #[test]
    fn an_app_gone_from_the_index_meanwhile_is_an_error_not_a_panic() {
        let (mut apps, _, entries, _) = opened(&Frecency::default());
        let said = apps.answer(
            Answer::Item(row(&entries, "Anoint")),
            &Index::default(),
            &[],
        );
        assert!(
            matches!(said, Said::Steps(s) if matches!(s[..], [Step::Toast { .. }, Step::Ready]))
        );
    }
}
