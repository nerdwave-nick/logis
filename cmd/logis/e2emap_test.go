package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Every Rust end-to-end scenario carries over to the Go build: each test
// function of a Rust test file that drives a compositor through
// tests/support/e2e (tests/e2e.rs, dmenu.rs, apps_window.rs, …, latency.rs)
// has its Go port listed in e2ePorts, and every Go test listed there exists
// as a top-level `func TestX(t *testing.T)` in the file named. A Rust test
// that needs a compositor (it returns early without LOGIS_E2E) must have a
// port named TestE2E… or TestLatency…, so that the nested-niri gate
// (`-run E2E`) or the latency round (`-run Latency`) runs it. A new or
// renamed Rust test, or a renamed or deleted Go port, fails this test.

// e2ePort is the Go side of one Rust E2E test.
type e2ePort struct {
	// tests are the Go tests asserting the same observable behaviour at the
	// same boundary, as `path_test.go::TestName` from the repository root.
	tests []string
	// note says how the port differs from the Rust test, where it does.
	note string
}

// e2ePorts maps each Rust E2E test, as `tests/file.rs::name`, to its port.
var e2ePorts = map[string]e2ePort{
	// tests/e2e.rs: the harness's pure parts and isolation guard.
	"tests/e2e.rs::the_injector_refuses_a_socket_served_by_any_other_process": {tests: []string{
		"internal/e2e/pure_test.go::TestTheInjectorRefusesASocketServedByAnyOtherProcess",
		"internal/e2e/e2e_test.go::TestE2ETheGuardRefusesTheLiveSessionAndStrangers"}},
	"tests/e2e.rs::the_compositor_choice_is_niri_sway_or_none":                    {tests: []string{"internal/e2e/pure_test.go::TestTheCompositorChoiceIsNiriSwayOrNone"}},
	"tests/e2e.rs::trace_lines_parse_into_time_and_event":                         {tests: []string{"internal/trace/trace_test.go::TestTraceLinesParseIntoTimeAndEvent"}},
	"tests/e2e.rs::the_us_layout_types_shifted_characters_with_shift":             {tests: []string{"internal/e2e/pure_test.go::TestTheUSLayoutTypesShiftedCharactersWithShift"}},
	"tests/e2e.rs::captures_convert_from_both_byte_orders_and_flip_when_inverted": {tests: []string{"internal/e2e/pure_test.go::TestCapturesConvertFromBothByteOrdersAndFlipWhenInverted"}},
	"tests/e2e.rs::output_transforms_turn_captures_upright":                       {tests: []string{"internal/e2e/pure_test.go::TestOutputTransformsTurnCapturesUpright"}},
	"tests/e2e.rs::a_centred_crop_settles_an_odd_remainder_by_the_reference":      {tests: []string{"internal/e2e/pure_test.go::TestACentredCropSettlesAnOddRemainderByTheReference"}},
	// tests/e2e.rs: the debug panel and the keyboard model.
	"tests/e2e.rs::the_debug_panel_takes_focus_receives_keys_and_shows_them": {tests: []string{
		"cmd/logis/keyboard_e2e_test.go::TestE2ETheDebugPanelTakesFocusReceivesKeysAndShowsThem",
		"internal/e2e/e2e_test.go::TestE2EThePanelTakesFocusReceivesKeysAndShowsThem"}},
	"tests/e2e.rs::compose_locks_and_the_keypad_type_what_the_layout_says":          {tests: []string{"cmd/logis/keyboard_e2e_test.go::TestE2EComposeLocksAndTheKeypadTypeWhatTheLayoutSays"}},
	"tests/e2e.rs::every_editing_and_selection_key_does_its_work":                   {tests: []string{"cmd/logis/keyboard_e2e_test.go::TestE2EEveryEditingAndSelectionKeyDoesItsWork"}},
	"tests/e2e.rs::escape_ctrl_g_and_ctrl_bracket_each_dismiss":                     {tests: []string{"cmd/logis/keyboard_e2e_test.go::TestE2EEscapeCtrlGAndCtrlBracketEachDismiss"}},
	"tests/e2e.rs::a_held_key_repeats_at_the_compositors_rate_and_stops_on_release": {tests: []string{"cmd/logis/keyboard_e2e_test.go::TestE2EAHeldKeyRepeatsAtTheCompositorsRateAndStopsOnRelease"}},
	"tests/e2e.rs::home_xcompose_is_honoured_when_xcomposefile_is_unset":            {tests: []string{"cmd/logis/keyboard_e2e_test.go::TestE2EHomeXComposeIsHonouredWhenXComposeFileIsUnset"}},

	// tests/dmenu.rs
	"tests/dmenu.rs::typing_filters_and_return_prints_the_chosen_line":                {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2ETypingFiltersAndReturnPrintsTheChosenLine"}},
	"tests/dmenu.rs::escape_dismisses_with_empty_stdout_and_exit_1":                   {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2EEscapeDismissesWithEmptyStdoutAndExit1"}},
	"tests/dmenu.rs::index_and_columns_print_what_was_asked":                          {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2EIndexAndColumnsPrintWhatWasAsked"}},
	"tests/dmenu.rs::return_without_a_match_prints_the_typed_text":                    {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2EReturnWithoutAMatchPrintsTheTypedText"}},
	"tests/dmenu.rs::only_match_refuses_free_text_until_a_match_is_chosen":            {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2EOnlyMatchRefusesFreeTextUntilAMatchIsChosen"}},
	"tests/dmenu.rs::queued_callers_each_get_their_own_answer_in_turn":                {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2EQueuedCallersEachGetTheirOwnAnswerInTurn"}},
	"tests/dmenu.rs::the_window_opens_before_the_input_ends_and_takes_streamed_lines": {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2ETheWindowOpensBeforeTheInputEndsAndTakesStreamedLines"}},
	"tests/dmenu.rs::the_list_is_drawn_with_its_matches_highlighted":                  {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2ETheListIsDrawnWithItsMatchesHighlighted"}},
	"tests/dmenu.rs::losing_the_compositor_ends_the_daemon_with_exit_2":               {tests: []string{"cmd/logis/dmenu_window_e2e_test.go::TestE2ELosingTheCompositorEndsTheDaemonWithExit2"}},

	// tests/apps_window.rs
	"tests/apps_window.rs::return_launches_the_app_detached_with_an_activation_token": {tests: []string{"cmd/logis/apps_window_e2e_test.go::TestE2EReturnLaunchesTheAppDetachedWithAnActivationToken"}},
	"tests/apps_window.rs::shift_return_opens_the_desktop_actions_as_a_second_stage":  {tests: []string{"cmd/logis/apps_window_e2e_test.go::TestE2EShiftReturnOpensTheDesktopActionsAsASecondStage"}},
	"tests/apps_window.rs::plain_logis_opens_the_launcher_and_escape_exits_1":         {tests: []string{"cmd/logis/apps_window_e2e_test.go::TestE2EPlainLogisOpensTheLauncherAndEscapeExits1"}},

	// tests/provider_window.rs
	"tests/provider_window.rs::a_two_stage_flow_with_a_pending_row_ends_in_a_toast":          {tests: []string{"cmd/logis/provider_window_e2e_test.go::TestE2EATwoStageFlowWithAPendingRowEndsInAToast"}},
	"tests/provider_window.rs::escape_after_an_activation_never_kills_the_action_it_started": {tests: []string{"cmd/logis/provider_window_e2e_test.go::TestE2EEscapeAfterAnActivationNeverKillsTheActionItStarted"}},

	// tests/servitor_window.rs
	"tests/servitor_window.rs::rites_wait_pending_settle_live_and_lead_to_a_detached_invocation": {tests: []string{"cmd/logis/servitor_window_e2e_test.go::TestE2ERitesWaitPendingSettleLiveAndLeadToADetachedInvocation"}},

	// tests/theme_window.rs
	"tests/theme_window.rs::an_edited_theme_reaches_the_visible_window_and_a_broken_one_is_reported": {tests: []string{"cmd/logis/theme_window_e2e_test.go::TestE2EAnEditedThemeReachesTheVisibleWindowAndABrokenOneIsReported"}},
	"tests/theme_window.rs::continuous_effects_keep_a_window_drawing_until_motion_is_reduced":        {tests: []string{"cmd/logis/theme_window_e2e_test.go::TestE2EContinuousEffectsKeepAWindowDrawingUntilMotionIsReduced"}},

	// tests/motion.rs
	"tests/motion.rs::the_window_opens_over_several_frames_then_draws_nothing":   {tests: []string{"cmd/logis/motion_e2e_test.go::TestE2ETheWindowOpensOverSeveralFramesThenDrawsNothing"}},
	"tests/motion.rs::moving_the_selection_glides_over_frames_and_settles":       {tests: []string{"cmd/logis/motion_e2e_test.go::TestE2EMovingTheSelectionGlidesOverFramesAndSettles"}},
	"tests/motion.rs::the_answer_is_sent_at_accept_while_the_window_still_fades": {tests: []string{"cmd/logis/motion_e2e_test.go::TestE2ETheAnswerIsSentAtAcceptWhileTheWindowStillFades"}},
	"tests/motion.rs::a_waiting_session_is_shown_without_waiting_for_the_close":  {tests: []string{"cmd/logis/motion_e2e_test.go::TestE2EAWaitingSessionIsShownWithoutWaitingForTheClose"}},
	"tests/motion.rs::blur_follows_the_body_where_the_compositor_offers_it":      {tests: []string{"cmd/logis/motion_e2e_test.go::TestE2EBlurFollowsTheBodyWhereTheCompositorOffersIt"}},
	"tests/motion.rs::blur_off_in_the_config_sets_no_region":                     {tests: []string{"cmd/logis/motion_e2e_test.go::TestE2EBlurOffInTheConfigSetsNoRegion"}},
	"tests/motion.rs::a_config_edit_turns_blur_on_for_the_visible_window":        {tests: []string{"cmd/logis/motion_e2e_test.go::TestE2EAConfigEditTurnsBlurOnForTheVisibleWindow"}},
	"tests/motion.rs::an_idle_window_wakes_no_thread_of_the_daemon": {tests: []string{"cmd/logis/motion_e2e_test.go::TestE2EAnIdleWindowWakesNoThreadOfTheDaemon"},
		note: "watched 2.5 s instead of 1 s (past the last reply's 2 s write deadline), with the daemon's garbage collector off so the Go runtime's own housekeeping after the open is not counted, and from when the compositor has answered every frame instead of 200 ms after the open settles"},

	// tests/pointer.rs
	"tests/pointer.rs::the_pointer_and_the_selection_owner_refuse_a_socket_served_by_any_other_process": {tests: []string{
		"internal/e2e/pure_test.go::TestTheInjectorRefusesASocketServedByAnyOtherProcess",
		"internal/e2e/e2e_test.go::TestE2ETheGuardRefusesTheLiveSessionAndStrangers"},
		note: "the pointer, the selection owner and the keyboard share one guard (ConnectVerified), tested once"},
	"tests/pointer.rs::hovering_selects_a_row_and_a_click_on_it_accepts_it":        {tests: []string{"cmd/logis/pointer_e2e_test.go::TestE2EHoveringSelectsARowAndAClickOnItAcceptsIt"}},
	"tests/pointer.rs::the_wheel_scrolls_the_list_three_rows_a_notch":              {tests: []string{"cmd/logis/pointer_e2e_test.go::TestE2ETheWheelScrollsTheListThreeRowsANotch"}},
	"tests/pointer.rs::a_finger_scrolls_a_row_per_row_height_and_carries_the_rest": {tests: []string{"cmd/logis/pointer_e2e_test.go::TestE2EAFingerScrollsARowPerRowHeightAndCarriesTheRest"}},
	"tests/pointer.rs::ctrl_v_and_shift_insert_paste_the_clipboard_at_the_cursor":  {tests: []string{"cmd/logis/pointer_e2e_test.go::TestE2ECtrlVAndShiftInsertPasteTheClipboardAtTheCursor"}},
	"tests/pointer.rs::the_middle_button_pastes_the_primary_selection":             {tests: []string{"cmd/logis/pointer_e2e_test.go::TestE2ETheMiddleButtonPastesThePrimarySelection"}},

	// tests/latency.rs: the statistics, then the measurements (their
	// budgets are enforced only with LOGIS_LATENCY_BUDGETS=enforce).
	"tests/latency.rs::statistics_use_the_nearest_rank":                                   {tests: []string{"internal/e2e/latency/latency_test.go::TestStatisticsUseTheNearestRank"}},
	"tests/latency.rs::milliseconds_between_monotonic_stamps":                             {tests: []string{"internal/e2e/latency/latency_test.go::TestMillisecondsBetweenMonotonicStamps"}},
	"tests/latency.rs::the_summary_shows_each_series_against_its_budget":                  {tests: []string{"internal/e2e/latency/latency_test.go::TestTheSummaryShowsEachSeriesAgainstItsBudget"}},
	"tests/latency.rs::round_trips_are_the_sync_requests_of_a_wayland_debug_log":          {tests: []string{"internal/e2e/latency/latency_test.go::TestRoundTripsAreTheSyncRequestsOfAWaylandDebugLog"}},
	"tests/latency.rs::a_slow_open_names_the_compositor_only_when_its_stall_made_it_slow": {tests: []string{"internal/e2e/latency/latency_test.go::TestASlowOpenNamesTheCompositorOnlyWhenItsStallMadeItSlow"}},
	"tests/latency.rs::a_median_over_budget_is_out_of_budget":                             {tests: []string{"internal/e2e/latency/latency_test.go::TestAMedianOverBudgetIsOutOfBudget"}},
	"tests/latency.rs::debug_panel_latency": {tests: []string{"cmd/logis/latency_e2e_test.go::TestLatencyDebugPanel"},
		note: "keystroke → commit as in Rust; the panel's open is timed from its show, not from exec: exec → first commit (pinned and system fonts) is cold start, which the owner benchmarks himself (spec D5)"},
	"tests/latency.rs::a_warm_open_waits_on_no_round_trip_before_it_is_shown": {tests: []string{"cmd/logis/dmenu_latency_e2e_test.go::TestE2EAWarmOpenWaitsOnNoRoundTripBeforeItIsShown"}},
	"tests/latency.rs::dmenu_latency":                                         {tests: []string{"cmd/logis/dmenu_latency_e2e_test.go::TestLatencyDmenu"}},
	"tests/latency.rs::apps_latency":                                          {tests: []string{"cmd/logis/apps_latency_e2e_test.go::TestLatencyApps"}},
	"tests/latency.rs::dmenu_latency_with_continuous_effects": {tests: []string{"cmd/logis/theme_latency_e2e_test.go::TestLatencyThemedWarmOpen"},
		note: "its forge-crimson-brass subtest; cogitator-phosphor is measured beside it"},
}

// rustTestAttr starts a Rust test function: #[test], then the fn.
var rustTestAttr = regexp.MustCompile(`(?m)^#\[test\]\s*\n\s*fn (\w+)\(`)

// needsCompositor matches the early return of a Rust test that runs only
// in a test compositor: `let Some(…) = … else { return; }`.
var needsCompositor = regexp.MustCompile(`let Some\([^)]*\) = [^;]*?else \{\s*return;?\s*\}`)

// rustE2ETests are the test functions of the Rust test files under root
// that use tests/support/e2e, as `tests/file.rs::name`, each with whether
// it needs a compositor.
func rustE2ETests(t *testing.T, root string) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "tests", "*.rs"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "support::e2e") {
			continue
		}
		for name, compositor := range rustTests(string(data)) {
			out["tests/"+filepath.Base(file)+"::"+name] = compositor
		}
	}
	return out
}

// rustTests are the #[test] functions of a Rust source, each with whether
// its body (up to its closing brace) needs a compositor.
func rustTests(source string) map[string]bool {
	out := map[string]bool{}
	for _, m := range rustTestAttr.FindAllStringSubmatchIndex(source, -1) {
		out[source[m[2]:m[3]]] = needsCompositor.MatchString(rustBody(source[m[1]:]))
	}
	return out
}

// rustBody is the function body that starts at the first '{' of s, to its
// matching '}' (braces inside the tests' string literals come in pairs).
func rustBody(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

func TestEveryRustE2ETestHasAGoPortThatExists(t *testing.T) {
	root := repoRoot(t)
	rust := rustE2ETests(t, root)
	if len(rust) < 50 {
		t.Fatalf("found only %d Rust E2E tests", len(rust))
	}
	for ref := range rust {
		if len(e2ePorts[ref].tests) == 0 {
			t.Errorf("%s: no Go port listed in e2ePorts", ref)
		}
	}
	for ref := range e2ePorts {
		if _, ok := rust[ref]; !ok {
			t.Errorf("%s: listed in e2ePorts but no such Rust E2E test", ref)
		}
	}
	sources := map[string]string{}
	for _, ref := range slices.Sorted(maps.Keys(e2ePorts)) {
		port, gated := e2ePorts[ref], false
		for _, p := range port.tests {
			file, name, ok := strings.Cut(p, "::")
			if !ok || !strings.HasSuffix(file, "_test.go") {
				t.Errorf("%s: %q is not path_test.go::TestName", ref, p)
				continue
			}
			source, read := sources[file]
			if !read {
				data, err := os.ReadFile(filepath.Join(root, file))
				if err != nil {
					t.Errorf("%s: %v", ref, err)
					continue
				}
				source, sources[file] = string(data), string(data)
			}
			if !declaresTest(t, file, source, name) {
				t.Errorf("%s: no such Go test %s", ref, p)
			}
			gated = gated || strings.HasPrefix(name, "TestE2E") || strings.HasPrefix(name, "TestLatency")
		}
		if rust[ref] && !gated {
			t.Errorf("%s needs a compositor, but no port is a TestE2E… or TestLatency… test the gates run", ref)
		}
		t.Logf("%s → %s%s", ref, strings.Join(port.tests, ", "), map[bool]string{true: " (" + port.note + ")"}[port.note != ""])
	}
}

func TestARustTestNeedsACompositorWhenItReturnsEarlyWithoutOne(t *testing.T) {
	source := "use x;\n\n#[test]\nfn pure() {\n    let s = format!(\"{a}\");\n    assert!(s.is_empty());\n}\n\n" +
		"fn helper() -> Option<u8> { None }\n\n" +
		"#[test]\nfn in_niri() {\n    let Some(mut c) = Cogitator::start_with(&[(\"A\", \"4\")], None) else {\n        return;\n    };\n    c.go();\n}\n\n" +
		"#[test]\nfn in_sway() {\n    let Some(c) = Cogitator::start() else { return };\n    drop(c);\n}\n\n" +
		"/// Docs.\nfn not_a_test() {\n    let Some(kind) = Kind::requested() else {\n        return;\n    };\n}\n"
	want := map[string]bool{"pure": false, "in_niri": true, "in_sway": true}
	if got := rustTests(source); !maps.Equal(got, want) {
		t.Errorf("rustTests = %v, want %v", got, want)
	}
}
