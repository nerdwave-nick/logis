use super::*;
use crate::motion::{Easing, Motion};

const MS: Nanos = 1_000_000;
const T0: Nanos = 1_000 * MS;
const LINEAR_100: Curve = Curve::millis(100, Easing::Linear);

#[test]
fn a_tween_moves_from_its_start_to_its_end() {
    let tween = Tween::new(10.0_f32, 20.0, LINEAR_100, T0);
    assert_eq!(tween.at(T0 - 5 * MS), 10.0, "before the start");
    assert_eq!(tween.at(T0), 10.0);
    assert!((tween.at(T0 + 50 * MS) - 15.0).abs() < 1e-4);
    assert_eq!(tween.at(T0 + 100 * MS), 20.0);
    assert_eq!(tween.at(T0 + 900 * MS), 20.0);
    assert!(!tween.done(T0 + 99 * MS));
    assert!(tween.done(T0 + 100 * MS));
    assert_eq!(tween.end_time(), T0 + 100 * MS);
}

#[test]
fn a_finished_tween_rests_exactly_on_its_end() {
    let curve = Curve::millis(150, Easing::EASE_OUT);
    let from = Pose {
        opacity: 0.4,
        offset: [0.0, 10.0],
    };
    let tween = Tween::new(from, Pose::REST, curve, T0);
    assert!(tween.at(T0 + 150 * MS).is_rest());
}

#[test]
fn opening_starts_with_the_first_frame_one_interval_in() {
    let motion = Motion::default();
    let mut t = Transition::opening(motion.open_from, LINEAR_100);
    assert!(t.moving(0), "an unstarted open will move");
    // The first frame is shown at T0; the open began one 10 ms frame earlier.
    let first = t.pose(T0, 10 * MS);
    t.presented(T0, 10 * MS);
    assert!(
        (first.opacity - (0.4 + 0.6 * 0.1)).abs() < 1e-4,
        "{first:?}"
    );
    assert!((first.offset[1] - 9.0).abs() < 1e-4, "{first:?}");
    assert!(t.moving(T0));
    let later = t.pose(T0 + 50 * MS, 10 * MS);
    assert!(later.opacity > first.opacity && later.offset[1] < first.offset[1]);
    assert_eq!(t.pose(T0 + 90 * MS, 10 * MS), Pose::REST);
    assert!(!t.moving(T0 + 90 * MS), "at rest, nothing more to draw");
    assert!(!t.finished(T0 + 90 * MS), "an open panel stays");
}

#[test]
fn a_slow_first_frame_does_not_eat_into_the_open() {
    let from = Pose {
        opacity: 0.0,
        offset: [0.0, 0.0],
    };
    let mut t = Transition::opening(from, LINEAR_100);
    let first = t.pose(T0, 10 * MS);
    assert!((first.opacity - 0.1).abs() < 1e-4, "{first:?}");
    // The first frame took 80 ms to reach the screen: the open starts there.
    t.presented(T0 + 80 * MS, 10 * MS);
    let second = t.pose(T0 + 90 * MS, 10 * MS);
    assert!((second.opacity - 0.2).abs() < 1e-4, "{second:?}");
    assert!(t.moving(T0 + 150 * MS));
    assert_eq!(t.pose(T0 + 170 * MS, 10 * MS), Pose::REST);
}

#[test]
fn until_it_is_presented_every_frame_shows_the_open_one_interval_in() {
    let mut t = Transition::opening(
        Pose {
            opacity: 0.0,
            offset: [0.0, 0.0],
        },
        LINEAR_100,
    );
    assert_eq!(t.pose(T0, 10 * MS), t.pose(T0 + 50 * MS, 10 * MS));
    assert!(t.moving(T0 + 500 * MS));
}

#[test]
fn a_resting_panel_never_moves() {
    let mut t = Transition::resting();
    assert!(!t.moving(T0));
    assert_eq!(t.pose(T0, 10 * MS), Pose::REST);
}

#[test]
fn closing_runs_from_the_last_pose_to_the_closed_pose_and_finishes() {
    let mut t = Transition::resting();
    t.pose(T0, 0);
    let to = Pose {
        opacity: 0.0,
        offset: [0.0, 6.0],
    };
    t.close(to, LINEAR_100, T0 + 10 * MS);
    assert!(t.is_closing());
    assert!(t.moving(T0 + 10 * MS));
    let mid = t.pose(T0 + 60 * MS, 0);
    assert!((mid.opacity - 0.5).abs() < 1e-4 && (mid.offset[1] - 3.0).abs() < 1e-4);
    assert!(!t.finished(T0 + 60 * MS));
    assert_eq!(t.finish_time(), Some(T0 + 110 * MS));
    assert!(t.finished(T0 + 110 * MS));
    assert_eq!(t.pose(T0 + 110 * MS, 0), to);
}

#[test]
fn closing_while_opening_starts_from_where_the_panel_is() {
    let from = Pose {
        opacity: 0.4,
        offset: [0.0, 10.0],
    };
    let mut t = Transition::opening(from, LINEAR_100);
    t.pose(T0, 0);
    t.presented(T0, 0);
    let half = t.pose(T0 + 50 * MS, 0);
    assert!(t.moving(T0 + 50 * MS));
    t.close(Pose::REST, LINEAR_100, T0 + 50 * MS);
    assert_eq!(t.pose(T0 + 50 * MS, 0), half, "no jump");
}

#[test]
fn an_instant_close_finishes_at_once() {
    let mut t = Transition::resting();
    t.close(Pose::REST, Curve::INSTANT, T0);
    assert!(t.finished(T0));
}

#[test]
fn the_first_target_of_a_glide_is_taken_at_once() {
    let mut g = Glide::new(LINEAR_100);
    assert_eq!(g.toward(Some(40.0), T0, 10 * MS), (Some(40.0), false));
    assert_eq!(g.toward(Some(40.0), T0 + MS, 10 * MS), (Some(40.0), false));
}

#[test]
fn a_new_target_glides_there_starting_one_interval_back() {
    let mut g = Glide::new(LINEAR_100);
    g.toward(Some(0.0), T0, 10 * MS);
    let (at, moving) = g.toward(Some(100.0), T0 + 200 * MS, 10 * MS);
    assert!(moving);
    assert!((at.unwrap() - 10.0).abs() < 1e-3, "{at:?}: already moving");
    let (at, _) = g.toward(Some(100.0), T0 + 240 * MS, 10 * MS);
    assert!((at.unwrap() - 50.0).abs() < 1e-3, "{at:?}");
    assert_eq!(
        g.toward(Some(100.0), T0 + 290 * MS, 10 * MS),
        (Some(100.0), false)
    );
    assert_eq!(
        g.toward(Some(100.0), T0 + 400 * MS, 10 * MS),
        (Some(100.0), false)
    );
}

#[test]
fn retargeting_mid_glide_continues_from_where_the_band_is() {
    let mut g = Glide::new(LINEAR_100);
    g.toward(Some(0.0), T0, 0);
    g.toward(Some(100.0), T0, 0);
    let (at, _) = g.toward(Some(100.0), T0 + 50 * MS, 0);
    assert!((at.unwrap() - 50.0).abs() < 1e-3);
    let (at, moving) = g.toward(Some(0.0), T0 + 50 * MS, 0);
    assert!(moving);
    assert!((at.unwrap() - 50.0).abs() < 1e-3, "{at:?}: no jump");
}

#[test]
fn no_target_resets_the_glide() {
    let mut g = Glide::new(LINEAR_100);
    g.toward(Some(0.0), T0, 0);
    assert_eq!(g.toward(None, T0 + MS, 0), (None, false));
    assert_eq!(g.toward(Some(80.0), T0 + 2 * MS, 0), (Some(80.0), false));
}

#[test]
fn an_instant_glide_jumps() {
    let mut g = Glide::new(Curve::INSTANT);
    g.toward(Some(0.0), T0, 0);
    assert_eq!(g.toward(Some(52.0), T0 + MS, 0), (Some(52.0), false));
}

#[test]
fn a_reset_glide_takes_its_next_target_at_once() {
    let mut g = Glide::new(LINEAR_100);
    g.toward(Some(0.0), T0, 0);
    g.toward(Some(100.0), T0, 0);
    g.reset();
    assert_eq!(g.toward(Some(52.0), T0 + 10 * MS, 0), (Some(52.0), false));
}
