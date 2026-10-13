package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The caller contract stays pinned on the Go build (Rust tests/docs.rs):
// every test the README's "Caller contract" section names as
// `path.rs::test_name` has its Go port listed in goPorts, and every Go test
// listed there exists as a top-level `func TestX(t *testing.T)` in the file
// named, so no promise of the contract is left without a Go test, and a
// renamed or deleted Go test cannot leave one without its proof.

// goPorts maps each Rust test the caller contract names to the Go tests that
// pin the same promise, as `path_test.go::TestName` from the repository
// root.
var goPorts = map[string][]string{
	"tests/daemon.rs::the_original_bytes_of_the_chosen_item_are_returned_exactly":                        {"cmd/logis/contract_test.go::TestTheOriginalBytesOfTheChosenItemAreReturnedExactly"},
	"tests/dmenu.rs::typing_filters_and_return_prints_the_chosen_line":                                   {"cmd/logis/dmenu_window_e2e_test.go::TestE2ETypingFiltersAndReturnPrintsTheChosenLine"},
	"tests/provider.rs::a_two_stage_flow_runs_to_the_providers_close":                                    {"cmd/logis/provider_e2e_test.go::TestATwoStageFlowRunsToTheProvidersClose"},
	"tests/servitor.rs::servitor_mode_invokes_detached_once_settled_and_the_caller_does_not_wait":        {"cmd/logis/servitor_e2e_test.go::TestServitorModeInvokesDetachedOnceSettledAndTheCallerDoesNotWait"},
	"tests/servitor_window.rs::rites_wait_pending_settle_live_and_lead_to_a_detached_invocation":         {"cmd/logis/servitor_window_e2e_test.go::TestE2ERitesWaitPendingSettleLiveAndLeadToADetachedInvocation"},
	"tests/apps.rs::plain_logis_is_the_app_launcher":                                                     {"cmd/logis/apps_e2e_test.go::TestPlainLogisIsTheAppLauncher"},
	"tests/apps.rs::the_chosen_app_is_launched_with_its_token_and_counts_for_frecency":                   {"cmd/logis/apps_e2e_test.go::TestTheChosenAppIsLaunchedWithItsTokenInAScopeAndCountsForFrecency"},
	"tests/apps_window.rs::return_launches_the_app_detached_with_an_activation_token":                    {"cmd/logis/apps_window_e2e_test.go::TestE2EReturnLaunchesTheAppDetachedWithAnActivationToken"},
	"tests/cli.rs::daemon_detach_exits_0_at_once_when_a_daemon_already_answers":                          {"cmd/logis/contract_test.go::TestDaemonDetachExits0AtOnceWhenADaemonAlreadyAnswers", "cmd/logis/daemon_e2e_test.go::TestE2EDaemonDetachOnlyEnsuresTheDaemon"},
	"tests/daemon.rs::a_dismissed_dmenu_session_exits_1_with_empty_stdout":                               {"cmd/logis/dmenu_e2e_test.go::TestE2EAnUnmarkedListIsDismissedWithExit1AndEmptyStdout"},
	"tests/dmenu.rs::escape_dismisses_with_empty_stdout_and_exit_1":                                      {"cmd/logis/dmenu_window_e2e_test.go::TestE2EEscapeDismissesWithEmptyStdoutAndExit1"},
	"tests/provider_window.rs::escape_after_an_activation_never_kills_the_action_it_started":             {"cmd/logis/provider_window_e2e_test.go::TestE2EEscapeAfterAnActivationNeverKillsTheActionItStarted"},
	"tests/apps_window.rs::plain_logis_opens_the_launcher_and_escape_exits_1":                            {"cmd/logis/apps_window_e2e_test.go::TestE2EPlainLogisOpensTheLauncherAndEscapeExits1"},
	"src/input/bindings.rs::escape_ctrl_g_and_ctrl_bracket_dismiss":                                      {"internal/input/bindings_test.go::TestEscapeCtrlGAndCtrlBracketDismiss"},
	"src/input/keyboard.rs::focus_loss_dismisses_by_default_and_keeps_on_request":                        {"internal/input/keyboard_test.go::TestFocusLossDismissesByDefaultAndKeepsOnRequest"},
	"tests/daemon.rs::a_replaced_dmenu_caller_exits_1_with_empty_stdout":                                 {"cmd/logis/contract_test.go::TestAReplacedDmenuCallerExits1WithEmptyStdout"},
	"tests/provider.rs::a_replaced_provider_caller_exits_1_with_empty_stdout":                            {"cmd/logis/provider_e2e_test.go::TestAReplacedProviderCallerExits1WithEmptyStdout"},
	"tests/dmenu_runes.rs::no_run_if_empty_exits_1_at_once_without_a_daemon":                             {"cmd/logis/dmenu_e2e_test.go::TestE2ENoRunIfEmptyExits1AtOnceWithoutADaemon"},
	"tests/cli.rs::unknown_argument_exits_2_naming_it":                                                   {"cmd/logis/contract_test.go::TestUnknownArgumentExits2NamingIt", "cmd/logis/main_test.go::TestUnknownRunesAreNamedAndExit2"},
	"src/cli/dmenu.rs::bad_values_are_named_with_their_flag":                                             {"cmd/logis/cli_test.go::TestBadValuesAreNamedWithTheirFlag"},
	"tests/dmenu_runes.rs::list_runes_without_dmenu_are_an_error":                                        {"cmd/logis/dmenu_e2e_test.go::TestE2EListRunesWithoutDmenuAreAnError"},
	"tests/dmenu_runes.rs::dmenu0_reads_nul_separated_items_with_newlines_inside":                        {"cmd/logis/dmenu_e2e_test.go::TestE2EDmenu0ReadsNulSeparatedItemsWithNewlinesInside"},
	"tests/config.rs::check_config_accepts_a_sound_config_and_exits_0":                                   {"cmd/logis/config_test.go::TestCheckConfigAcceptsASoundConfigAndExits0"},
	"tests/config.rs::check_config_with_a_missing_named_file_exits_2":                                    {"cmd/logis/config_test.go::TestCheckConfigWithAMissingNamedFileExits2"},
	"tests/dmenu.rs::the_window_opens_before_the_input_ends_and_takes_streamed_lines":                    {"cmd/logis/dmenu_window_e2e_test.go::TestE2ETheWindowOpensBeforeTheInputEndsAndTakesStreamedLines"},
	"src/client/dmenu/stream.rs::lines_are_sent_as_they_arrive_then_end":                                 {"cmd/logis/dmenu_test.go::TestLinesAreSentAsTheyArriveThenEnd"},
	"tests/dmenu_runes.rs::the_original_bytes_survive_columns_and_invalid_utf8":                          {"cmd/logis/dmenu_e2e_test.go::TestE2ETheOriginalBytesSurviveColumnsAndInvalidUTF8"},
	"tests/dmenu.rs::return_without_a_match_prints_the_typed_text":                                       {"cmd/logis/dmenu_window_e2e_test.go::TestE2EReturnWithoutAMatchPrintsTheTypedText"},
	"src/list/view.rs::return_without_a_match_accepts_the_typed_text_unless_only_match":                  {"internal/list/view_test.go::TestReturnWithoutAMatchAcceptsTheTypedTextUnlessOnlyMatch"},
	"tests/dmenu.rs::only_match_refuses_free_text_until_a_match_is_chosen":                               {"cmd/logis/dmenu_window_e2e_test.go::TestE2EOnlyMatchRefusesFreeTextUntilAMatchIsChosen"},
	"src/list/filter.rs::matching_ignores_case_and_accents":                                              {"internal/list/filter_test.go::TestMatchingIgnoresCaseAndAccents"},
	"src/list/filter.rs::fzf_syntax_applies":                                                             {"internal/list/filter_test.go::TestFzfSyntaxApplies"},
	"src/list/filter.rs::fzf_suffix_and_exact_terms_apply":                                               {"internal/list/filter_test.go::TestFzfSuffixAndExactTermsApply"},
	"src/cli/dmenu.rs::the_defaults_are_fuzzels":                                                         {"cmd/logis/cli_test.go::TestTheDmenuDefaultsAreFuzzels"},
	"src/cli/dmenu.rs::values_follow_their_flag_or_an_equals_sign":                                       {"cmd/logis/cli_test.go::TestValuesFollowTheirFlagOrAnEqualsSign"},
	"src/cli/mod.rs::dmenu_asks_for_a_dmenu_session":                                                     {"cmd/logis/cli_test.go::TestDmenuAsksForADmenuSession"},
	"src/list/scene/tests.rs::a_session_prompt_shows_and_the_default_one_gives_way_to_the_label":         {"internal/list/scene/scene_test.go::TestASessionPromptShowsAndTheDefaultOneGivesWayToTheLabel"},
	"tests/dmenu_runes.rs::prompt_only_never_waits_for_stdin":                                            {"cmd/logis/dmenu_e2e_test.go::TestE2EPromptOnlyNeverWaitsForStdin"},
	"src/cli/dmenu.rs::prompt_only_reads_nothing_and_shows_no_rows":                                      {"cmd/logis/cli_test.go::TestPromptOnlyReadsNothingAndShowsNoRows"},
	"src/list/view.rs::an_empty_query_shows_the_placeholder_without_a_cursor":                            {"internal/list/view_test.go::TestAnEmptyQueryShowsThePlaceholderWithoutACursor"},
	"src/cli/dmenu.rs::password_takes_an_optional_character":                                             {"cmd/logis/cli_test.go::TestPasswordTakesAnOptionalCharacter"},
	"src/list/view.rs::a_password_is_masked_per_character":                                               {"internal/list/view_test.go::TestAPasswordIsMaskedPerCharacter"},
	"src/list/scene/tests.rs::lines_and_width_in_the_options_size_the_window":                            {"internal/list/scene/scene_test.go::TestLinesAndWidthInTheOptionsSizeTheWindow"},
	"src/list/scene/tests.rs::without_a_size_the_window_is_the_themes":                                   {"internal/list/scene/scene_test.go::TestWithoutASizeTheWindowIsTheThemes"},
	"src/list/view.rs::the_counter_counts_matches_of_all":                                                {"internal/list/view_test.go::TestTheCounterCountsMatchesOfAll"},
	"src/list/view.rs::search_opens_with_a_query":                                                        {"internal/list/view_test.go::TestSearchOpensWithAQuery"},
	"src/list/view.rs::select_preselects_the_first_equal_item_ignoring_case_when_it_arrives":             {"internal/list/view_test.go::TestSelectPreselectsTheFirstEqualItemIgnoringCaseWhenItArrives"},
	"src/list/view.rs::select_index_preselects_once_it_arrives_unless_the_user_moved_first":              {"internal/list/view_test.go::TestSelectIndexPreselectsOnceItArrivesUnlessTheUserMovedFirst"},
	"src/cli/dmenu.rs::preselection_is_by_text_or_by_index_not_both":                                     {"cmd/logis/cli_test.go::TestPreselectionIsByTextOrByIndexNotBoth"},
	"tests/dmenu_runes.rs::index_prints_the_position_of_the_choice":                                      {"cmd/logis/dmenu_e2e_test.go::TestE2EIndexPrintsThePositionOfTheChoice"},
	"tests/dmenu.rs::index_and_columns_print_what_was_asked":                                             {"cmd/logis/dmenu_window_e2e_test.go::TestE2EIndexAndColumnsPrintWhatWasAsked"},
	"tests/dmenu_runes.rs::with_nth_shows_a_column_and_the_whole_line_comes_back":                        {"cmd/logis/dmenu_e2e_test.go::TestE2EWithNthShowsAColumnAndTheWholeLineComesBack"},
	"tests/dmenu_runes.rs::accept_nth_prints_columns_split_at_the_nth_delimiter":                         {"cmd/logis/dmenu_e2e_test.go::TestE2EAcceptNthPrintsColumnsSplitAtTheNthDelimiter"},
	"src/client/dmenu/stream.rs::with_nth_shapes_the_text_and_match_nth_the_match_text":                  {"cmd/logis/dmenu_test.go::TestWithNthShapesTheTextAndMatchNthTheMatchText"},
	"src/list/view.rs::items_matched_by_another_text_are_not_highlighted":                                {"internal/list/view_test.go::TestItemsMatchedByAnotherTextAreNotHighlighted"},
	"src/nth.rs::a_format_expands_columns_inside_free_text":                                              {"internal/nth/nth_test.go::TestAFormatExpandsColumnsInsideFreeText"},
	"src/nth.rs::ranges_join_their_columns_with_the_delimiter":                                           {"internal/nth/nth_test.go::TestRangesJoinTheirColumnsWithTheDelimiter"},
	"src/nth.rs::zero_means_no_selection":                                                                {"internal/nth/nth_test.go::TestZeroMeansNoSelection"},
	"tests/config.rs::a_client_hands_its_config_to_the_daemon_it_starts":                                 {"cmd/logis/config_e2e_test.go::TestE2EAClientHandsItsConfigToTheDaemonItStarts"},
	"tests/config.rs::a_running_daemon_keeps_its_config_whatever_a_caller_names":                         {"cmd/logis/config_e2e_test.go::TestE2EARunningDaemonKeepsItsConfigWhateverACallerNames"},
	"src/cli/mod.rs::list_runes_go_with_dmenu_in_any_order":                                              {"cmd/logis/cli_test.go::TestListRunesGoWithDmenuInAnyOrder"},
	"src/client/dmenu/mod.rs::typed_text_prints_as_typed_whatever_the_output_runes":                      {"cmd/logis/dmenu_test.go::TestTypedTextPrintsAsTypedWhateverTheOutputRunes"},
	"src/list/view.rs::typing_ends_a_preselection_still_waiting_for_its_item":                            {"internal/list/view_test.go::TestTypingEndsAPreselectionStillWaitingForItsItem"},
	"src/nth.rs::column_zero_and_backward_ranges_are_refused":                                            {"internal/nth/nth_test.go::TestColumnZeroAndBackwardRangesAreRefused"},
	"src/input/bindings.rs::ctrl_v_and_shift_insert_paste_the_clipboard":                                 {"internal/input/bindings_test.go::TestCtrlVAndShiftInsertPasteTheClipboard"},
	"tests/pointer.rs::the_middle_button_pastes_the_primary_selection":                                   {"cmd/logis/pointer_e2e_test.go::TestE2ETheMiddleButtonPastesThePrimarySelection", "internal/input/pointer_test.go::TestTheMiddleButtonPastesThePrimarySelectionOnPress"},
	"src/daemon/core.rs::the_result_goes_only_to_the_session_that_was_answered":                          {"internal/daemon/core_test.go::TestTheResultGoesOnlyToTheSessionThatWasAnswered"},
	"tests/daemon.rs::dmenu_sessions_queue_and_each_caller_gets_its_own_answer_in_order":                 {"internal/daemon/serve_test.go::TestDmenuSessionsQueueAndEachCallerGetsItsOwnAnswerInOrder"},
	"tests/daemon.rs::a_dmenu_caller_waits_in_the_queue_for_its_turn":                                    {"cmd/logis/contract_test.go::TestADmenuCallerWaitsInTheQueueForItsTurn"},
	"tests/dmenu.rs::queued_callers_each_get_their_own_answer_in_turn":                                   {"cmd/logis/dmenu_window_e2e_test.go::TestE2EQueuedCallersEachGetTheirOwnAnswerInTurn"},
	"src/daemon/sessions.rs::a_dmenu_session_queues_behind_an_interactive_one":                           {"internal/daemon/sessions_test.go::TestADmenuSessionQueuesBehindAnInteractiveOne"},
	"tests/daemon.rs::an_interactive_session_replaces_the_visible_one":                                   {"internal/daemon/core_test.go::TestAnInteractiveSessionReplacesTheVisibleOne"},
	"src/daemon/sessions.rs::an_interactive_session_replaces_the_visible_one_and_keeps_the_queue":        {"internal/daemon/sessions_test.go::TestAnInteractiveSessionReplacesTheVisibleOneAndKeepsTheQueue"},
	"tests/daemon.rs::a_client_disconnect_cancels_its_session_and_the_next_one_is_shown":                 {"internal/daemon/serve_test.go::TestAClientDisconnectCancelsItsSessionAndTheNextOneIsShown"},
	"src/daemon/sessions.rs::closing_a_queued_session_drops_it_silently":                                 {"internal/daemon/sessions_test.go::TestClosingAQueuedSessionDropsItSilently"},
	"tests/provider.rs::a_provider_that_never_acted_and_lingers_after_cancel_is_ended":                   {"cmd/logis/provider_e2e_test.go::TestAProviderThatNeverActedAndLingersAfterCancelIsEnded"},
	"tests/provider.rs::a_dismissed_provider_that_acted_is_never_signalled_nor_its_action":               {"cmd/logis/provider_e2e_test.go::TestADismissedProviderThatActedIsNeverSignalledNorItsAction"},
	"tests/daemon.rs::a_client_starts_the_daemon_when_none_answers_and_later_clients_reuse_it":           {"internal/client/client_test.go::TestAClientStartsTheDaemonWhenNoneAnswersAndLaterClientsReuseIt"},
	"tests/daemon.rs::an_auto_started_daemon_outlives_its_starter_and_logs_to_the_runtime_dir":           {"cmd/logis/contract_test.go::TestAnAutoStartedDaemonOutlivesItsStarterAndLogsToTheRuntimeDir", "cmd/logis/daemon_e2e_test.go::TestE2EADetachedDaemonLeavesItsStartersStdioAndLogsToTheRuntimeDir"},
	"tests/provider_window.rs::a_two_stage_flow_with_a_pending_row_ends_in_a_toast":                      {"cmd/logis/provider_window_e2e_test.go::TestE2EATwoStageFlowWithAPendingRowEndsInAToast"},
	"tests/provider.rs::a_provider_that_cannot_start_is_an_error":                                        {"cmd/logis/provider_e2e_test.go::TestAProviderThatCannotStartIsAnError"},
	"tests/provider.rs::a_provider_that_breaks_the_protocol_or_departs_fails_its_session":                {"cmd/logis/provider_e2e_test.go::TestAProviderThatBreaksTheProtocolOrDepartsFailsItsSession"},
	"tests/provider.rs::a_provider_that_shows_nothing_in_time_fails_its_session":                         {"cmd/logis/provider_e2e_test.go::TestAProviderThatShowsNothingInTimeFailsItsSession"},
	"tests/servitor.rs::a_rite_that_never_settles_is_never_invoked_and_leaving_ends_the_census":          {"cmd/logis/servitor_e2e_test.go::TestARiteThatNeverSettlesIsNeverInvokedAndLeavingEndsTheCensus"},
	"src/servitor/flow/tests.rs::a_pending_rite_is_never_taken":                                          {"internal/servitor/flow_test.go::TestAPendingRiteIsNeverTaken"},
	"src/servitor/flow/tests.rs::a_settled_rite_offers_its_aspects_with_the_current_marked_and_selected": {"internal/servitor/flow_test.go::TestASettledRiteOffersItsAspectsWithTheCurrentMarkedAndSelected"},
	"src/servitor/flow/tests.rs::a_stale_or_unknown_aspect_is_never_marked_or_selected":                  {"internal/servitor/flow_test.go::TestAStaleOrUnknownAspectIsNeverMarkedOrSelected"},
	"src/servitor/flow/tests.rs::inscriptions_are_asked_in_written_order_from_the_canon":                 {"internal/servitor/flow_test.go::TestInscriptionsAreAskedInWrittenOrderFromTheCanon"},
	"src/servitor/flow/tests.rs::a_mandatory_inscription_must_be_filled_before_invoking":                 {"internal/servitor/flow_test.go::TestAMandatoryInscriptionMustBeFilledBeforeInvoking"},
	"src/servitor/flow/tests.rs::a_cleared_decree_is_passed_and_an_empty_word_without_one_is_left_out":   {"internal/servitor/flow_test.go::TestAClearedDecreeIsPassedAndAnEmptyWordWithoutOneIsLeftOut"},
	"tests/servitor.rs::an_edict_is_invoked_without_an_aspect":                                           {"cmd/logis/servitor_e2e_test.go::TestAnEdictIsInvokedWithoutAnAspect"},
	"src/servitor/invoke.rs::invoke_runs_in_a_session_of_its_own_with_stdio_on_dev_null_and_unawaited":   {"internal/servitor/invoke_test.go::TestInvokeRunsInASessionOfItsOwnWithStdioOnDevNullAndUnawaited"},
	"src/servitor/flow/tests.rs::nothing_the_flow_says_is_an_outcome_toast":                              {"internal/servitor/flow_test.go::TestNothingTheFlowSaysIsAnOutcomeToast"},
	"tests/servitor.rs::no_dismissal_or_departure_of_logis_aborts_a_running_invocation":                  {"cmd/logis/servitor_e2e_test.go::TestNoDismissalOrDepartureOfLogisAbortsARunningInvocation"},
	"tests/cli.rs::a_dmenu_caller_without_a_usable_session_exits_2_naming_why":                           {"cmd/logis/dmenu_e2e_test.go::TestE2EADmenuCallerWithoutAUsableSessionExits2NamingWhy"},
	"tests/daemon.rs::a_client_whose_daemon_cannot_start_exits_2_with_the_reason":                        {"cmd/logis/dmenu_e2e_test.go::TestE2EADmenuCallerWithoutAUsableSessionExits2NamingWhy", "cmd/logis/dmenu_e2e_test.go::TestE2EADaemonThatCannotStartIsReportedWithExit2", "internal/client/client_test.go::TestADaemonThatCannotStartIsReportedWithItsReason"},
	"tests/cli.rs::with_nonexistent_wayland_socket_exits_2_naming_the_socket":                            {"cmd/logis/session_test.go::TestWithNonexistentWaylandSocketExits2NamingTheSocket"},
	"tests/cli.rs::on_compositor_without_layer_shell_exits_2_naming_the_protocol":                        {"cmd/logis/session_test.go::TestOnCompositorWithoutLayerShellExits2NamingTheProtocol"},
	"tests/cli.rs::on_compositor_without_fractional_scale_exits_2_naming_the_protocol":                   {"cmd/logis/session_test.go::TestOnCompositorWithoutFractionalScaleExits2NamingTheProtocol"},
	"tests/config.rs::a_daemon_refuses_to_start_on_an_invalid_config_and_its_client_exits_2":             {"cmd/logis/config_e2e_test.go::TestE2EADaemonRefusesToStartOnAnInvalidConfigAndItsClientFails"},
	"tests/daemon.rs::a_dmenu_caller_whose_daemon_dies_exits_2_with_empty_stdout":                        {"cmd/logis/contract_test.go::TestADmenuCallerWhoseDaemonDiesExits2WithEmptyStdout", "cmd/logis/dmenu_window_e2e_test.go::TestE2ELosingTheCompositorEndsTheDaemonWithExit2"},
	"tests/daemon.rs::losing_the_wayland_connection_makes_the_daemon_exit_non_zero":                      {"cmd/logis/dmenu_window_e2e_test.go::TestE2ELosingTheCompositorEndsTheDaemonWithExit2", "cmd/logis/e2e_test.go::TestE2ELosingTheCompositorExits2"},
	"tests/daemon.rs::a_caller_the_daemon_refuses_exits_2_with_the_daemons_reason":                       {"cmd/logis/contract_test.go::TestACallerTheDaemonRefusesExits2WithTheDaemonsReason", "internal/client/client_test.go::TestADaemonsRefusalIsTheClientsError"},
	"tests/daemon.rs::a_greeting_in_another_protocol_version_is_refused":                                 {"internal/daemon/serve_test.go::TestOverTheSocketAGreetingInAnotherVersionIsRefused", "internal/daemon/core_test.go::TestAGreetingOfAnotherVersionIsRefused"},
	"tests/cli.rs::without_wayland_display_exits_2_with_flavoured_message":                               {"cmd/logis/session_test.go::TestWithoutWaylandDisplayExits2WithFlavouredMessage"},
	"src/input/bindings.rs::return_and_keypad_enter_accept":                                              {"internal/input/bindings_test.go::TestReturnAndKeypadEnterAcceptShiftReturnDescends"},
	"tests/pointer.rs::hovering_selects_a_row_and_a_click_on_it_accepts_it":                              {"cmd/logis/pointer_e2e_test.go::TestE2EHoveringSelectsARowAndAClickOnItAcceptsIt"},
	"src/input/bindings.rs::up_down_tab_and_ctrl_n_p_move":                                               {"internal/input/bindings_test.go::TestUpDownTabAndCtrlNPMove"},
	"src/input/bindings.rs::editing_keys_edit":                                                           {"internal/input/bindings_test.go::TestEditingKeysEdit"},
	"src/input/bindings.rs::left_right_home_and_end_move_the_cursor":                                     {"internal/input/bindings_test.go::TestEditingKeysEdit"},
	"tests/e2e.rs::every_editing_and_selection_key_does_its_work":                                        {"cmd/logis/keyboard_e2e_test.go::TestE2EEveryEditingAndSelectionKeyDoesItsWork"},
	"tests/daemon.rs::a_second_daemon_defers_to_the_one_already_listening":                               {"internal/daemon/serve_test.go::TestASecondDaemonDefersToTheOneAlreadyListening"},
	"tests/daemon.rs::with_an_idle_timeout_the_daemon_exits_once_no_client_is_connected":                 {"cmd/logis/daemon_e2e_test.go::TestE2EWithAnIdleTimeoutTheDaemonExitsOnceNoClientIsConnected"},
	"tests/daemon.rs::without_an_idle_timeout_the_daemon_stays":                                          {"internal/daemon/serve_test.go::TestWithoutAnIdleTimeoutTheDaemonStays"},
	"tests/config.rs::the_idle_timeout_flag_outranks_the_config":                                         {"internal/daemon/config_test.go::TestTheIdleTimeoutFlagOutranksTheConfig"},
	"src/wayland.rs::a_relative_display_is_joined_to_the_runtime_dir":                                    {"internal/window/connect_test.go::TestARelativeDisplayIsJoinedToTheRuntimeDir"},
	"src/wayland.rs::an_absolute_display_is_used_as_is":                                                  {"internal/window/connect_test.go::TestAnAbsoluteDisplayIsUsedAsIs"},
	"src/wayland.rs::an_unset_or_empty_display_means_no_session":                                         {"internal/window/connect_test.go::TestAnUnsetOrEmptyDisplayMeansNoSession"},
	"src/protocol/mod.rs::the_socket_lives_in_an_absolute_runtime_dir":                                   {"internal/protocol/protocol_test.go::TestTheSocketLivesInAnAbsoluteRuntimeDir"},
	"src/config/source.rs::the_default_lives_in_xdg_config_home":                                         {"internal/config/source_test.go::TestTheDefaultLivesInXDGConfigHome"},
	"src/config/source.rs::without_a_usable_xdg_config_home_the_default_is_under_home":                   {"internal/config/source_test.go::TestWithoutAUsableXDGConfigHomeTheDefaultIsUnderHome"},
	"tests/cli.rs::version_flag_prints_name_and_version_and_succeeds":                                    {"cmd/logis/contract_test.go::TestVersionFlagPrintsNameAndVersionAndSucceeds", "cmd/logis/main_test.go::TestVersionRunesPrintNameAndVersion"},
}

// contractSection is the README's section under `## Caller contract`, up
// to the next `## `.
func contractSection(t *testing.T, readme string) string {
	t.Helper()
	start := strings.Index(readme, "\n## Caller contract\n")
	if start < 0 {
		t.Fatal("README.md has no '## Caller contract' section")
	}
	body := readme[start+1:]
	if end := strings.Index(body[3:], "\n## "); end >= 0 {
		return body[:end+3]
	}
	return body
}

// referenceWord reports whether s is a path or name of a reference: ASCII
// letters, digits, '_' and '/'.
func referenceWord(s string) bool {
	if s == "" {
		return false
	}
	for _, b := range []byte(s) {
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '/') {
			return false
		}
	}
	return true
}

// references are the `path.rs::name` written inside backticks, in order,
// each once.
func references(text string) []string {
	var refs []string
	spans := strings.Split(text, "`")
	for i := 1; i < len(spans); i += 2 {
		path, name, ok := strings.Cut(spans[i], ".rs::")
		if !ok || !referenceWord(path) || !referenceWord(name) {
			continue
		}
		if ref := path + ".rs::" + name; !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	return refs
}

// declaresTest reports whether the Go source declares name as a top-level
// test function, `func name(t *testing.T)`.
func declaresTest(t *testing.T, file, source, name string) bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != name || !strings.HasPrefix(name, "Test") {
			continue
		}
		params := fn.Type.Params.List
		if len(params) != 1 || len(params[0].Names) > 1 || fn.Type.Results != nil {
			continue
		}
		star, ok := params[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if ok && sel.Sel.Name == "T" {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "testing" {
				return true
			}
		}
	}
	return false
}

func TestEveryTestTheCallerContractNamesHasAGoPortThatExists(t *testing.T) {
	root := repoRoot(t)
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	refs := references(contractSection(t, string(readme)))
	if len(refs) < 20 {
		t.Fatalf("the caller contract names only %d tests", len(refs))
	}
	for _, ref := range refs {
		if len(goPorts[ref]) == 0 {
			t.Errorf("%s: no Go port listed in goPorts", ref)
		}
	}
	for ref := range goPorts {
		if !slices.Contains(refs, ref) {
			t.Errorf("%s: listed in goPorts but not named by the caller contract", ref)
		}
	}
	sources := map[string]string{}
	for _, ref := range refs {
		for _, port := range goPorts[ref] {
			file, name, ok := strings.Cut(port, "::")
			if !ok || !strings.HasSuffix(file, "_test.go") {
				t.Errorf("%s: %q is not path_test.go::TestName", ref, port)
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
				t.Errorf("%s: no such Go test %s", ref, port)
			}
		}
	}
}

func TestReferencesAreReadFromCodeSpansOnly(t *testing.T) {
	text := "see `tests/a.rs::one`, `src/x/y.rs::two` and tests/b.rs::three; `--dmenu`, `tests/a.rs::one` again"
	if got, want := references(text), []string{"tests/a.rs::one", "src/x/y.rs::two"}; !slices.Equal(got, want) {
		t.Errorf("references %q, want %q", got, want)
	}
}

func TestATestCountsOnlyAsATopLevelTestFunction(t *testing.T) {
	source := "package p\nimport \"testing\"\n" +
		"func TestPinned(t *testing.T) {}\n" +
		"func helper(t *testing.T) {}\n" +
		"func TestPinnedTooLong(t *testing.T) {}\n" +
		"func (r *rig) TestMethod(t *testing.T) {}\n" +
		"func TestBenchmarkLike(b *testing.B) {}\n" +
		"func TestNoT() {}\n"
	for name, want := range map[string]bool{
		"TestPinned": true, "TestPinnedTooLong": true, "helper": false, "TestPinnedToo": false,
		"TestMethod": false, "TestBenchmarkLike": false, "TestNoT": false,
	} {
		if got := declaresTest(t, "p_test.go", source, name); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}
