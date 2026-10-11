use super::*;
use crate::provider::wire::{FromProvider, Input, Item, Level, Stage, ToProvider};
use crate::servitor::census::{Herald, Told, parse};
use crate::servitor::testing::fixture;

/// The recorded vigil (tests/fixtures/servitor/vigil.jsonl), line by line:
/// broken, dnd (awaited), hush (awaited), screenshot, theme, hush (mute),
/// dnd (heeded "on"), concluded.
fn vigil() -> Vec<Herald> {
    std::fs::read_to_string(fixture("vigil.jsonl"))
        .unwrap()
        .lines()
        .filter(|l| l.starts_with('{'))
        .map(|l| parse(l.as_bytes()).unwrap())
        .collect()
}

/// A flow that has begun and heard the first `n` lines of the vigil.
fn after(n: usize) -> Flow {
    let mut flow = Flow::default();
    flow.begin();
    for herald in vigil().into_iter().take(n) {
        flow.told(Told::Herald(herald));
    }
    flow
}

fn said(acts: Vec<Act>) -> Vec<FromProvider> {
    acts.into_iter()
        .map(|a| match a {
            Act::Say(m) => m,
            other => panic!("not a line: {other:?}"),
        })
        .collect()
}

fn one_stage(acts: Vec<Act>) -> Stage {
    match &said(acts)[..] {
        [FromProvider::Stage(stage)] => stage.clone(),
        other => panic!("not one stage: {other:?}"),
    }
}

fn items(acts: Vec<Act>) -> Vec<Item> {
    match said(acts).as_slice() {
        [
            FromProvider::Items {
                items,
                select: None,
            },
        ] => items.clone(),
        other => panic!("not items: {other:?}"),
    }
}

fn activate(flow: &mut Flow, stage: &str, id: &str) -> Vec<Act> {
    flow.heard(ToProvider::Activate {
        stage: stage.into(),
        id: id.into(),
    })
}

fn submit(flow: &mut Flow, stage: &str, text: &str) -> Vec<Act> {
    flow.heard(ToProvider::Submit {
        stage: stage.into(),
        text: text.into(),
    })
}

fn invoked(acts: Vec<Act>) -> Vec<String> {
    match &acts[..] {
        [Act::Invoke(argv)] => argv.clone(),
        other => panic!("no invocation: {other:?}"),
    }
}

fn is_ready(acts: &[Act]) -> bool {
    matches!(acts, [Act::Say(FromProvider::Ready)])
}

fn error_toast_then_ready(acts: Vec<Act>) -> String {
    match &said(acts)[..] {
        [
            FromProvider::Toast {
                text,
                level: Level::Error,
                ..
            },
            FromProvider::Ready,
        ] => text.clone(),
        other => panic!("not an error toast and ready: {other:?}"),
    }
}

#[test]
fn it_greets_and_shows_the_rite_stage_at_once_before_the_census_speaks() {
    let mut flow = Flow::default();
    let first = said(flow.begin());
    assert_eq!(first[0], FromProvider::Hello { protocol: 1 });
    let FromProvider::Stage(stage) = &first[1] else {
        panic!("{first:?}");
    };
    assert_eq!(first.len(), 2);
    assert_eq!((stage.id.as_str(), stage.input), ("rite", Input::None));
    assert!(stage.items.is_empty() && stage.select.is_none());
    assert!(!stage.placeholder.is_empty());
}

#[test]
fn every_rite_appears_as_it_is_told_and_an_awaited_one_is_pending() {
    let mut flow = after(0);
    let lines = vigil();
    let told: Vec<Item> = lines[..5]
        .iter()
        .flat_map(|h| items(flow.told(Told::Herald(h.clone()))))
        .collect();
    let ids: Vec<&str> = told.iter().map(|i| i.id.as_str()).collect();
    assert_eq!(ids, ["broken", "dnd", "hush", "screenshot", "theme"]);
    let pending: Vec<bool> = told.iter().map(|i| i.pending).collect();
    assert_eq!(pending, [false, true, true, false, false]);
    assert_eq!(
        told[1].columns,
        ["dnd", "·", "Silence the vox of the desktop"]
    );
    assert_eq!(told[0].badges, ["heresy"]);
    assert_eq!(told[3].badges, ["edict"]);
    assert!(told[4].badges.is_empty());
}

#[test]
fn a_settling_line_replaces_its_rite_settled_with_its_aspect() {
    let mut flow = after(6);
    let settled = items(flow.told(Told::Herald(vigil()[6].clone())));
    assert_eq!(settled.len(), 1);
    assert_eq!(settled[0].id, "dnd");
    assert!(!settled[0].pending);
    assert_eq!(settled[0].columns[1], "on");
}

#[test]
fn a_pending_rite_is_never_taken() {
    let mut flow = after(5);
    let acts = activate(&mut flow, "rite", "dnd");
    assert!(is_ready(&acts), "{acts:?}");
    let acts = activate(&mut flow, "rite", "hush");
    assert!(is_ready(&acts), "{acts:?}");
}

#[test]
fn a_settled_rite_offers_its_aspects_with_the_current_marked_and_selected() {
    let mut flow = after(7);
    let stage = one_stage(activate(&mut flow, "rite", "dnd"));
    assert_eq!((stage.id.as_str(), stage.input), ("aspect", Input::None));
    let ids: Vec<&str> = stage.items.iter().map(|i| i.id.as_str()).collect();
    assert_eq!(ids, ["on", "off"]);
    assert_eq!(stage.items[0].badges, ["current"]);
    assert!(stage.items[1].badges.is_empty());
    assert_eq!(stage.select.as_deref(), Some("on"));
}

#[test]
fn a_stale_or_unknown_aspect_is_never_marked_or_selected() {
    let mut flow = after(0);
    // A census whose auspex was forgone: its aspect is the data-slate's word.
    let stale = br#"{"herald":"rite","rite":"dnd","edict":false,"aspect":"off","standing":"performed","auspex":"forgone","aspects":["on","off"],"canon":[]}"#;
    flow.told(Told::Herald(parse(stale).unwrap()));
    let stage = one_stage(activate(&mut flow, "rite", "dnd"));
    assert!(stage.select.is_none());
    assert!(stage.items.iter().all(|i| i.badges.is_empty()));
    // hush settled mute and stands in no aspect: nothing is marked either.
    let mut flow = after(7);
    let stage = one_stage(activate(&mut flow, "rite", "hush"));
    assert!(stage.select.is_none());
    assert!(stage.items.iter().all(|i| i.badges.is_empty()));
}

#[test]
fn inscriptions_are_asked_in_written_order_from_the_canon() {
    let mut flow = after(7);
    one_stage(activate(&mut flow, "rite", "dnd"));
    let reason = one_stage(activate(&mut flow, "aspect", "off"));
    assert_eq!(reason.id, "inscription:reason");
    assert_eq!(reason.placeholder, "why the silence was imposed");
    assert_eq!(reason.query, "", "no decree to prefill");
    assert_eq!(reason.input, Input::Required, "mandatory without a decree");
    assert!(reason.prompt.as_deref().unwrap().contains("reason"));
    let origin = one_stage(submit(
        &mut flow,
        "inscription:reason",
        "the forge demands focus",
    ));
    assert_eq!(origin.id, "inscription:origin");
    assert_eq!(
        origin.query, "the cogitator",
        "the decree of the chosen aspect"
    );
    assert_eq!(origin.input, Input::Optional);
    assert!(
        !origin.placeholder.is_empty(),
        "a purpose of its own when unwritten"
    );
    let argv = invoked(submit(&mut flow, "inscription:origin", "the cogitator"));
    assert_eq!(
        argv,
        [
            "invoke",
            "dnd",
            "off",
            "--reason",
            "the forge demands focus",
            "--origin",
            "the cogitator"
        ]
    );
}

#[test]
fn the_decree_prefilled_follows_the_aspect_chosen() {
    let mut flow = after(7);
    activate(&mut flow, "rite", "dnd");
    activate(&mut flow, "aspect", "on");
    let origin = one_stage(submit(&mut flow, "inscription:reason", "r"));
    assert_eq!(origin.query, "the hotkey");
}

#[test]
fn a_mandatory_inscription_must_be_filled_before_invoking() {
    let mut flow = after(7);
    activate(&mut flow, "rite", "dnd");
    activate(&mut flow, "aspect", "on");
    let text = error_toast_then_ready(submit(&mut flow, "inscription:reason", ""));
    assert!(text.contains("reason"), "{text}");
    // Still asked: a word now leads on.
    let next = one_stage(submit(&mut flow, "inscription:reason", "now"));
    assert_eq!(next.id, "inscription:origin");
}

#[test]
fn a_cleared_decree_is_passed_and_an_empty_word_without_one_is_left_out() {
    let mut flow = after(7);
    activate(&mut flow, "rite", "dnd");
    activate(&mut flow, "aspect", "on");
    submit(&mut flow, "inscription:reason", "r");
    let argv = invoked(submit(&mut flow, "inscription:origin", ""));
    assert_eq!(
        argv,
        ["invoke", "dnd", "on", "--reason", "r", "--origin", ""]
    );

    let mut flow = after(7);
    activate(&mut flow, "rite", "theme");
    let reason = one_stage(activate(&mut flow, "aspect", "porpl"));
    assert_eq!(reason.input, Input::Optional);
    let argv = invoked(submit(&mut flow, "inscription:reason", ""));
    assert_eq!(argv, ["invoke", "theme", "porpl"]);
}

#[test]
fn an_edict_needs_no_aspect_and_its_decree_is_prefilled() {
    let mut flow = after(7);
    let region = one_stage(activate(&mut flow, "rite", "screenshot"));
    assert_eq!(region.id, "inscription:region");
    assert_eq!(region.query, "chosen");
    let argv = invoked(submit(&mut flow, "inscription:region", "whole"));
    assert_eq!(argv, ["invoke", "screenshot", "--region", "whole"]);
}

#[test]
fn a_rite_without_inscriptions_is_invoked_once_its_aspect_is_chosen() {
    let mut flow = after(7);
    activate(&mut flow, "rite", "hush");
    let argv = invoked(activate(&mut flow, "aspect", "quiet"));
    assert_eq!(argv, ["invoke", "hush", "quiet"]);
}

#[test]
fn once_invoke_is_launched_the_flow_closes_without_a_toast() {
    let mut flow = after(7);
    activate(&mut flow, "rite", "hush");
    invoked(activate(&mut flow, "aspect", "quiet"));
    let acts = flow.launched(Ok(()));
    assert!(
        matches!(&acts[..], [Act::Say(FromProvider::Close), Act::Exit(0)]),
        "{acts:?}"
    );
}

#[test]
fn a_launch_that_fails_is_told_and_the_last_stage_answers_again() {
    let mut flow = after(7);
    activate(&mut flow, "rite", "screenshot");
    let argv = invoked(submit(&mut flow, "inscription:region", "whole"));
    let text = error_toast_then_ready(flow.launched(Err("No such file or directory".into())));
    assert!(text.contains("No such file"), "{text}");
    assert_eq!(
        invoked(submit(&mut flow, "inscription:region", "whole")),
        argv
    );
}

#[test]
fn a_heretical_rite_is_refused_in_place() {
    let mut flow = after(7);
    let text = error_toast_then_ready(activate(&mut flow, "rite", "broken"));
    assert!(text.contains("broken"), "{text}");
}

#[test]
fn answers_out_of_turn_only_free_the_stage() {
    let mut flow = after(7);
    assert!(is_ready(&activate(&mut flow, "rite", "nosuch")));
    assert!(is_ready(&activate(&mut flow, "aspect", "on")));
    assert!(is_ready(&submit(&mut flow, "rite", "dnd")));
    activate(&mut flow, "rite", "hush");
    assert!(is_ready(&activate(&mut flow, "aspect", "sideways")));
    assert!(is_ready(&activate(&mut flow, "rite", "hush")));
}

#[test]
fn later_census_lines_leave_a_later_stage_alone_unless_they_tell_its_rite() {
    let mut flow = after(1);
    let lines = vigil();
    flow.told(Told::Herald(lines[2].clone())); // hush, awaited
    flow.told(Told::Herald(lines[3].clone())); // screenshot
    // dnd as the forgone census would tell it: chosen while not yet heard.
    let forgone = br#"{"herald":"rite","rite":"dnd","edict":false,"aspect":"","standing":"dormant","auspex":"","aspects":["on","off"],"canon":[]}"#;
    flow.told(Told::Herald(parse(forgone).unwrap()));
    one_stage(activate(&mut flow, "rite", "dnd"));
    assert!(
        flow.told(Told::Herald(lines[4].clone())).is_empty(),
        "theme"
    );
    assert!(flow.told(Told::Herald(lines[5].clone())).is_empty(), "hush");
    // dnd itself is told again, settled in "on": it is marked and selected.
    let acts = said(flow.told(Told::Herald(lines[6].clone())));
    let [FromProvider::Items { items, select }] = &acts[..] else {
        panic!("{acts:?}");
    };
    assert_eq!(select.as_deref(), Some("on"));
    assert_eq!(items[0].badges, ["current"]);
}

#[test]
fn the_census_s_own_troubles_are_told_in_the_window() {
    let mut flow = after(0);
    let acts = said(flow.unheard("No such file or directory".into()));
    assert!(
        matches!(&acts[..], [FromProvider::Toast { level: Level::Error, text, .. }] if text.contains("No such file")),
        "{acts:?}"
    );
    let mut flow = after(3);
    let acts = said(flow.told(Told::Ended("exit status: 2".into())));
    assert!(
        matches!(&acts[..], [FromProvider::Toast { level: Level::Error, text, .. }] if text.contains("exit status: 2")),
        "{acts:?}"
    );
    let mut flow = after(0);
    flow.told(Told::Herald(Herald::Concluded));
    let acts = said(flow.told(Told::Ended("exit status: 0".into())));
    assert!(
        matches!(&acts[..], [FromProvider::Toast { .. }]),
        "an empty Librarium is told: {acts:?}"
    );
    let mut flow = after(8);
    assert!(flow.told(Told::Ended("exit status: 0".into())).is_empty());
}

#[test]
fn laments_and_unknown_heralds_change_nothing_shown() {
    let mut flow = after(2);
    let lament = Herald::Lament {
        lament: "the data-slate is garbled".into(),
        rite: Some("dnd".into()),
    };
    assert!(flow.told(Told::Herald(lament)).is_empty());
    assert!(flow.told(Told::Herald(Herald::Unknown)).is_empty());
}

#[test]
fn cancel_ends_the_provider_and_a_foreign_protocol_is_an_error() {
    let mut flow = after(7);
    assert!(matches!(
        &flow.heard(ToProvider::Cancel)[..],
        [Act::Exit(0)]
    ));
    let mut flow = after(0);
    assert!(flow.heard(ToProvider::Hello { protocol: 1 }).is_empty());
    assert!(matches!(
        &flow.heard(ToProvider::Hello { protocol: 2 })[..],
        [Act::Exit(2)]
    ));
}

#[test]
fn nothing_the_flow_says_is_an_outcome_toast() {
    // Through a whole flow to its close, no success toast is ever said:
    // servitor announces the verdict itself.
    let mut flow = Flow::default();
    let mut all = flow.begin();
    for herald in vigil() {
        all.extend(flow.told(Told::Herald(herald)));
    }
    all.extend(activate(&mut flow, "rite", "dnd"));
    all.extend(activate(&mut flow, "aspect", "on"));
    all.extend(submit(&mut flow, "inscription:reason", "r"));
    all.extend(submit(&mut flow, "inscription:origin", "the hotkey"));
    all.extend(flow.launched(Ok(())));
    assert!(
        !all.iter()
            .any(|a| matches!(a, Act::Say(FromProvider::Toast { .. }))),
        "{all:?}"
    );
}
