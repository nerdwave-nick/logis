# logis

A clean-room Wayland launcher and picker that looks like a cogitator of the Machine
God: a GPU-rendered, Adeptus Mechanicus-styled window with animated glow,
scanlines, grain and blur behind it.

logis is meant to be:

- a drop-in `dmenu` for scripts (`logis --dmenu`, fuzzel/dmenu-compatible flags and
  exit codes);
- an application launcher for desktop entries, with icons and frecency;
- a first-class front end for [servitor](https://github.com/nerdwave-nick/servitor):
  choose a rite, an aspect and its inscriptions, and set the invocation going
  (servitor announces the verdict itself);
- instant to open, because a resident daemon keeps the GPU, fonts and app index
  warm while every invocation is a thin client.

## Status

**Early.** `logis` (the app launcher), `logis --dmenu`, `logis --provider` and
`logis --servitor` work. The binary does this much:

- `logis --version` prints the version.
- `logis --dmenu` (or `--dmenu0`) is a drop-in for `fuzzel --dmenu`: see
  [dmenu](#dmenu). Its window takes the [keyboard](#keyboard), the
  [pointer and paste](#pointer-and-paste).
- `logis` (or `logis --apps`) is the app launcher, as in fuzzel: see
  [App launcher](#app-launcher).
- `logis --provider PROGRAM [ARG]...` shows what an external provider says, in
  one or more stages: see [Providers](#providers).
- `logis --servitor` is servitor mode: every rite at once, its aspect and its
  inscriptions, then `servitor invoke …` started detached: see
  [servitor mode](#servitor-mode).
- `logis daemon` is the resident daemon. It checks for a Wayland session whose
  compositor offers wlr-layer-shell (exit 2 with a message on stderr if there is
  none), listens on `$XDG_RUNTIME_DIR/logis.sock`, and wakes its window at start:
  the compositor connection (the only one it opens), the GPU and the fonts. When
  the compositor goes away it exits 2. `--idle-timeout SECONDS` lets it exit once
  no client has been connected for that long (off by default); it outranks the
  config's `idle-timeout`, and `--idle-timeout 0` keeps the daemon resident
  whatever the config says. It reads its [configuration](#configuration) at
  start (an invalid one is an error, exit 2) and again whenever the file changes.
- `logis`, `logis --dmenu`, `logis --apps`, `logis --provider` and `logis --servitor` are clients. When no daemon answers they start one
  (`logis daemon --detach`, whose later diagnostics go to
  `$XDG_RUNTIME_DIR/logis.log`); if it cannot start, they exit 2 with its reason.
  To only make sure a daemon runs, without opening a window, start it yourself:
  `logis daemon --detach &` (it exits 0 at once if a daemon already answers).
- `logis --check-config [--config PATH]` validates the configuration and the
  [theme](#themes) it names without touching Wayland or the daemon: exit 0 if
  they are sound (or absent), 2 with the error's file, line and column (or key)
  on stderr.
- Every window wears a [theme](#themes): forge crimson & brass by default, or
  cogitator phosphor, or one of your own, edited live.
- `logis daemon --debug-panel [--config PATH]` does not serve sessions: it shows a
  static panel drawn by the renderer core (a GPU-rendered overlay layer surface at
  the compositor's fractional scale, with text rows in several scripts and colour
  emoji) and a query line. It takes exclusive keyboard focus and uses the whole
  [keyboard model](#keyboard): typing edits the query, the selection keys move,
  Return prints the selected row and closes it, and the dismissal keys (or focus
  loss, per the config's `focus-loss`) close it, as does the compositor or ending
  the process.

Client and daemon speak length-prefixed JSON frames, one session per connection;
the protocol is described in [`src/protocol/mod.rs`](src/protocol/mod.rs). While a
session is visible, a new dmenu session queues (each caller gets its own answer,
in turn) and a new app, servitor or provider session replaces it.

## dmenu

```sh
printf 'rite of oil\nrite of ignition\nlitany\n' | logis --dmenu
```

stdin streams into the window as it arrives, one item per line (per NUL-ended
item with `--dmenu0`), so the window opens before the input ends. Typing filters
the items with fzf-like fuzzy matching (nucleo), ignoring case and accents (`e`
matches `é`; a typed `é` means `é`), with fzf's syntax: `^prefix`, `suffix$`,
`'exact`, `!not`, and words matched separately. Matched characters are drawn in
brass; tabs advance to tab stops, so tab-separated columns line up.

Return prints the selected item's original bytes, exactly as read, and exits 0.
When nothing matches, Return prints the typed text as typed (unless
`--only-match`). Escape (or Ctrl+G, Ctrl+[, or focus loss) prints nothing and
exits 1. Errors exit 2 with the message on stderr.

| Rune | Effect |
|---|---|
| `--dmenu`, `-d` / `--dmenu0` | items separated by newlines / NUL |
| `--prompt=TEXT`, `-p` | shown before the query (default `> `) |
| `--prompt-only=TEXT` | a prompt and nothing else: stdin is not read, no rows |
| `--placeholder=TEXT` | shown while the query is empty |
| `--password[=C]` | shows each typed character as `C` (default `*`) |
| `--lines=N`, `-l` | rows shown at most (default 15); the window's height |
| `--width=N`, `-w` | the window's width in characters (default 30, an estimate) |
| `--counter` | shows matching/total items |
| `--search=TEXT` | the query the window opens with |
| `--select=TEXT` | preselects the first item equal to TEXT, ignoring case |
| `--select-index=N` | preselects item N (from 0); not with `--select` |
| `--only-match` | Return accepts only an item, never typed text |
| `--no-run-if-empty`, `-R` | empty stdin: exit 1 at once, no window |
| `--index` | prints the chosen item's index (from 0) instead of its text |
| `--with-nth=N\|FMT` | shows column N (or a format) of each line |
| `--accept-nth=N\|FMT` | prints column N (or a format) of the chosen line |
| `--match-nth=N\|FMT` | matches against column N (or a format) instead of what is shown |
| `--nth-delimiter=C` | the column delimiter, one ASCII character (default tab) |
| `--config=PATH` | the config a daemon this client starts reads |
| `-i` | ignored (dmenu's case switch; logis always ignores case) |

Columns count from 1; a format is free text in which `{N}`, `{N..M}` and `{N..}`
expand to columns (ranges joined by the delimiter), and `0` means the rune is not
given. Values may follow as `--rune=V` or `--rune V` (`--password` only takes
`=C`). The list runes need `--dmenu`.

Where fuzzel's documentation leaves a choice open, logis decides so:
- typed text is printed as typed even with `--index` or `--accept-nth` (fuzzel:
  free text "is printed as is");
- `--select` means equal ignoring case, not a prefix or a fuzzy match; a
  preselection applies when its item arrives, until you move or type;
- `--no-run-if-empty` exits 1 (nothing was chosen);
- a format that names no column is an error, not a constant output.

## App launcher

```sh
logis          # or: logis --apps
```

The daemon keeps an index of the installed applications resident and opens it in
a few milliseconds. Type to filter (the name, the generic name, the keywords and
the program are matched; matches in the name are drawn in brass), Return
launches the selected app, and the client exits 0 (1 when dismissed, 2 on error).
**Shift+Return** opens the app's desktop actions as a second stage in the same
window (the app itself first, then each action); Return there launches the one
chosen. Apps you launch often and lately come first and rank higher when you
type (frecency, kept in `$XDG_STATE_HOME/logis/frecency.json`).

- **Index:** desktop entries from `$XDG_DATA_HOME/applications`
  (`~/.local/share/applications`), then `applications` in each of
  `$XDG_DATA_DIRS`, read by logis's own implementation of the Desktop Entry
  Specification (not GIO). Earlier directories override later ones by
  desktop-file ID (`sub/app.desktop` is `sub-app.desktop`); an override with
  `Hidden=true` deletes the entry. Hidden are `NoDisplay` entries, entries whose
  `OnlyShowIn`/`NotShowIn` exclude the current desktops
  (`$XDG_CURRENT_DESKTOP`) and entries whose `TryExec` program is missing.
  Names, generic names and keywords are localised (`LC_ALL`, `LC_MESSAGES`,
  `LANG`). The daemon watches the directories (inotify) and reads them again
  when they change, so newly installed apps appear without a restart.
- **Icons:** from the config's `icon-theme`, the themes it inherits, hicolor
  and the pixmaps directories, drawn crisp at the output's scale.
- **Exec:** field codes per the specification (`%f %F %u %U` vanish, as no files
  are opened; `%i` is `--icon NAME`, `%c` the name, `%k` the file).
  `Terminal=true` apps run behind the config's `terminal` (or `$TERMINAL -e`).
- **Launch:** the app gets an xdg-activation token from the compositor
  (`XDG_ACTIVATION_TOKEN`, `DESKTOP_STARTUP_ID`), so it opens focused. It runs
  in the session environment: the systemd user manager's
  (`systemctl --user show-environment`), or, without one, the environment of the
  `logis --apps` client; never the daemon's own, frozen at its start. Where a
  systemd user manager answers, it runs in a transient scope of its own,
  `app-logis-<app id>-<pid>.scope` in `app.slice`, so it is accounted and
  stopped apart from logis. It runs in a session of its own with no
  controlling terminal, so it outlives `logis` and the daemon.
  `DBusActivatable=true` apps are started through D-Bus
  (`org.freedesktop.Application`), or by their `Exec` if the bus cannot.
  logis talks to the session bus itself (a few calls, no D-Bus library).

## Providers

Everything logis shows comes from a provider: the built-in dmenu provider (stdin),
servitor mode (logis itself as a provider), the app launcher, and
external providers, programs
that speak [the provider protocol](docs/provider-protocol.md): JSON lines over
their stdin and stdout.

```sh
logis --provider ./my-provider --some-arg
```

The daemon runs the provider as this client would (its working directory and
environment) in a process group of its own. The provider fills the window with
stages (lists whose items have stable ids, columns, a glyph or icon, badges and a
pending state), updates and removes items as its data settles, learns what the
user chose (`activate`, or `submit` for typed text), moves on to the next stage in
the same window, and can show toasts. Pending rows are never chosen or
preselected. The client exits 0 when the provider closes its flow, 1 when the
user dismisses it, 2 on error. Dismissal never kills an action the provider
started: logis signals only a provider it never sent a choice to, and never its
children.

## servitor mode

```sh
logis --servitor
```

opens servitor mode, built into logis and spoken to only through servitor's CLI
(`servitor` must be on the caller's `PATH`):

1. **Rites.** `servitor census --vigil` streams every rite at once: its name, the
   aspect it stands in (`·` while unknown), its purpose, and badges for an
   `edict` or a rite in `heresy`, `corrupted`, `tainted` or `desecrated`
   standing. A rite whose auspex has not settled yet is drawn pending
   ("divining…") and cannot be chosen; it settles in place as soon as servitor
   tells it again.
2. **Aspect.** The chosen rite's aspects in written order; the one it stands in
   is marked `current` and selected, but only once its auspex has settled, never
   from a stale or forgone reading. An edict has no aspect and skips this.
3. **Inscriptions.** One prompt per inscription the rite declares (its `canon`,
   in written order), with its purpose as the placeholder and the decree for the
   chosen aspect (or the edict's decree) already typed in. A mandatory
   inscription (`key*:`) needs a word before Return goes on.
4. **Invocation.** logis starts `servitor invoke <rite> [<aspect>] [--<key>
   <word>]...` detached (a session of its own, stdin, stdout and stderr on
   `/dev/null`, the caller's environment and working directory) and closes at
   once. It never waits for the invocation, reads its outcome or signals it:
   servitor's own desktop notifications announce the verdict, and nothing that
   happens to logis afterwards (dismissing a later window, the daemon ending)
   cuts it short.

The client exits 0 once the invocation is started, 1 when dismissed or
replaced, 2 on error. Troubles of logis's own (no servitor on the `PATH`, a
census that breaks off, a rite in heresy, an invocation that cannot start) show
as a toast in the window; they are never an invocation's outcome.

Under the hood `logis --servitor` is a provider session whose provider is logis
itself, `logis provide servitor`, speaking [the provider
protocol](docs/provider-protocol.md#servitor-mode-on-this-protocol): the daemon
runs it as the caller would have, and its rules (never signalling a provider
that was sent a choice, nor anything it started) protect the invocation.

## Caller contract

What scripts, servitor and other callers of `logis --dmenu` and `logis
--provider` can rely on. The man page says the same without the test names:
[`docs/man/logis.1`](docs/man/logis.1) (`man -l docs/man/logis.1`). Every
statement here is pinned by the tests named beside it as `file::test`
(`tests/docs.rs` checks that they exist); those marked *E2E* run only in a test
compositor (`LOGIS_E2E`), the rest in every `cargo test`. Anything not stated
here (message wording, timings, the socket protocol) may change.

### Exit status

| Status | stdout | When | Pinned by |
|---|---|---|---|
| 0 | the choice, then `\n` | `--dmenu`: an item or typed text was accepted | `tests/daemon.rs::the_original_bytes_of_the_chosen_item_are_returned_exactly`, `tests/dmenu.rs::typing_filters_and_return_prints_the_chosen_line` (E2E) |
| 0 | empty | `--provider`: the provider closed its flow | `tests/provider.rs::a_two_stage_flow_runs_to_the_providers_close` |
| 0 | empty | `--servitor`: `servitor invoke …` was started (logis does not wait for it) | `tests/servitor.rs::servitor_mode_invokes_detached_once_settled_and_the_caller_does_not_wait`, `tests/servitor_window.rs::rites_wait_pending_settle_live_and_lead_to_a_detached_invocation` (E2E) |
| 0 | empty | plain `logis` (or `--apps`): an app was launched | `tests/apps.rs::plain_logis_is_the_app_launcher`, `tests/apps.rs::the_chosen_app_is_launched_with_its_token_and_counts_for_frecency`, `tests/apps_window.rs::return_launches_the_app_detached_with_an_activation_token` (E2E) |
| 0 | the daemon's readiness line | `logis daemon --detach` when a daemon already answers (it exits at once) | `tests/cli.rs::daemon_detach_exits_0_at_once_when_a_daemon_already_answers` |
| 1 | empty | dismissed: Escape, Ctrl+G, Ctrl+[, focus loss (by default) | `tests/daemon.rs::a_dismissed_dmenu_session_exits_1_with_empty_stdout`, `tests/dmenu.rs::escape_dismisses_with_empty_stdout_and_exit_1` (E2E), `tests/provider_window.rs::escape_after_an_activation_never_kills_the_action_it_started` (E2E), `tests/apps_window.rs::plain_logis_opens_the_launcher_and_escape_exits_1` (E2E), `src/input/bindings.rs::escape_ctrl_g_and_ctrl_bracket_dismiss`, `src/input/keyboard.rs::focus_loss_dismisses_by_default_and_keeps_on_request` |
| 1 | empty | replaced by an interactive session (see [Sessions](#sessions)) | `tests/daemon.rs::a_replaced_dmenu_caller_exits_1_with_empty_stdout`, `tests/provider.rs::a_replaced_provider_caller_exits_1_with_empty_stdout` |
| 1 | empty | `--no-run-if-empty` and stdin is empty (no window, no daemon) | `tests/dmenu_runes.rs::no_run_if_empty_exits_1_at_once_without_a_daemon` |
| 2 | empty | any error, its message on stderr: bad runes, no usable session, the daemon cannot start, fails or refuses, the provider fails | `tests/cli.rs::unknown_argument_exits_2_naming_it`, `src/cli/dmenu.rs::bad_values_are_named_with_their_flag`, `tests/dmenu_runes.rs::list_runes_without_dmenu_are_an_error`, and [below](#when-logis-cannot-serve) |

Exit 1 does not tell a dismissal from a replacement; both mean "nothing was
chosen". stdout carries nothing but the choice and its newline, also after
`--dmenu0` (`tests/dmenu_runes.rs::dmenu0_reads_nul_separated_items_with_newlines_inside`).
`--check-config` exits 0 or 2 likewise
(`tests/config.rs::check_config_accepts_a_sound_config_and_exits_0`,
`tests/config.rs::check_config_with_a_missing_named_file_exits_2`).

### dmenu input and output

- stdin is read as it arrives; the window opens before it ends, and logis may
  answer and exit before reading all of it, so a producer can meet a closed
  pipe (`tests/dmenu.rs::the_window_opens_before_the_input_ends_and_takes_streamed_lines`
  (E2E), `src/client/dmenu/stream.rs::lines_are_sent_as_they_arrive_then_end`).
- A chosen item comes back as its original bytes, exactly as read, whatever
  their encoding and whatever `--with-nth` showed
  (`tests/dmenu_runes.rs::the_original_bytes_survive_columns_and_invalid_utf8`,
  `tests/daemon.rs::the_original_bytes_of_the_chosen_item_are_returned_exactly`).
- Return with no match prints the typed text as typed, unless `--only-match`
  (`tests/dmenu.rs::return_without_a_match_prints_the_typed_text` (E2E),
  `src/list/view.rs::return_without_a_match_accepts_the_typed_text_unless_only_match`,
  `tests/dmenu.rs::only_match_refuses_free_text_until_a_match_is_chosen` (E2E)).
- Matching ignores case and accents and takes fzf's syntax
  (`src/list/filter.rs::matching_ignores_case_and_accents`,
  `src/list/filter.rs::fzf_syntax_applies`,
  `src/list/filter.rs::fzf_suffix_and_exact_terms_apply`).

### dmenu runes

The runes, their defaults and their fuzzel semantics are in [dmenu](#dmenu)
(`src/cli/dmenu.rs::the_defaults_are_fuzzels`,
`src/cli/dmenu.rs::values_follow_their_flag_or_an_equals_sign`):

| Rune | Pinned by |
|---|---|
| `--dmenu`, `-d`, `--dmenu0` | `src/cli/mod.rs::dmenu_asks_for_a_dmenu_session`, `tests/dmenu_runes.rs::dmenu0_reads_nul_separated_items_with_newlines_inside` |
| `--prompt`, `-p` | `src/list/scene/tests.rs::a_session_prompt_shows_and_the_default_one_gives_way_to_the_label` |
| `--prompt-only` | `tests/dmenu_runes.rs::prompt_only_never_waits_for_stdin`, `src/cli/dmenu.rs::prompt_only_reads_nothing_and_shows_no_rows` |
| `--placeholder` | `src/list/view.rs::an_empty_query_shows_the_placeholder_without_a_cursor` |
| `--password[=C]` | `src/cli/dmenu.rs::password_takes_an_optional_character`, `src/list/view.rs::a_password_is_masked_per_character` |
| `--lines`, `-l`, `--width`, `-w` | `src/list/scene/tests.rs::lines_and_width_in_the_options_size_the_window`, `src/list/scene/tests.rs::without_a_size_the_window_is_the_themes` |
| `--counter` | `src/list/view.rs::the_counter_counts_matches_of_all` |
| `--search` | `src/list/view.rs::search_opens_with_a_query` |
| `--select` | `src/list/view.rs::select_preselects_the_first_equal_item_ignoring_case_when_it_arrives` |
| `--select-index` | `src/list/view.rs::select_index_preselects_once_it_arrives_unless_the_user_moved_first`, `src/cli/dmenu.rs::preselection_is_by_text_or_by_index_not_both` |
| `--only-match` | see typed text above |
| `--no-run-if-empty`, `-R` | `tests/dmenu_runes.rs::no_run_if_empty_exits_1_at_once_without_a_daemon` |
| `--index` | `tests/dmenu_runes.rs::index_prints_the_position_of_the_choice`, `tests/dmenu.rs::index_and_columns_print_what_was_asked` (E2E) |
| `--with-nth` | `tests/dmenu_runes.rs::with_nth_shows_a_column_and_the_whole_line_comes_back` |
| `--accept-nth`, `--nth-delimiter` | `tests/dmenu_runes.rs::accept_nth_prints_columns_split_at_the_nth_delimiter` |
| `--match-nth` | `src/client/dmenu/stream.rs::with_nth_shapes_the_text_and_match_nth_the_match_text`, `src/list/view.rs::items_matched_by_another_text_are_not_highlighted` |
| formats `{N}`, `{N..M}`, `{N..}`; `0` | `src/nth.rs::a_format_expands_columns_inside_free_text`, `src/nth.rs::ranges_join_their_columns_with_the_delimiter`, `src/nth.rs::zero_means_no_selection` |
| `--config` | `tests/config.rs::a_client_hands_its_config_to_the_daemon_it_starts`, `tests/config.rs::a_running_daemon_keeps_its_config_whatever_a_caller_names` |
| `-i` (ignored) | `src/cli/mod.rs::list_runes_go_with_dmenu_in_any_order` |

`--config PATH` reaches only a daemon that this caller starts; a daemon
already running keeps its own configuration.

### fuzzel compatibility

logis takes fuzzel's dmenu runes (fuzzel(1) 1.15) and exit statuses 0 and 1.
Where fuzzel's documentation leaves a choice open, or logis differs on
purpose:

- typed text is printed as typed, even with `--index` or `--accept-nth`
  (`src/client/dmenu/mod.rs::typed_text_prints_as_typed_whatever_the_output_runes`);
- `--select` picks the first item equal to its text ignoring case, not a
  prefix or fuzzy match, and applies when that item arrives, until the user
  moves the selection or edits the query
  (`src/list/view.rs::select_preselects_the_first_equal_item_ignoring_case_when_it_arrives`,
  `src/list/view.rs::select_index_preselects_once_it_arrives_unless_the_user_moved_first`,
  `src/list/view.rs::typing_ends_a_preselection_still_waiting_for_its_item`);
- `--no-run-if-empty` exits 1 (nothing was chosen);
- a format that names no column is an error, exit 2, not a constant output
  (`src/nth.rs::column_zero_and_backward_ranges_are_refused`);
- list runes without `--dmenu` are an error, exit 2;
- errors exit 2, apart from a dismissal's 1, where fuzzel exits 1 (its
  `--check-config`, for one); logis's `--check-config` exits 2
  (`tests/config.rs::check_config_with_a_missing_named_file_exits_2`);
- Shift+Insert pastes the clipboard, not the primary selection; the middle
  button pastes the primary (`src/input/bindings.rs::ctrl_v_and_shift_insert_paste_the_clipboard`,
  `tests/pointer.rs::the_middle_button_pastes_the_primary_selection` (E2E)).

### Sessions

Each caller holds one session on its own connection to the daemon, and the
answer goes to that caller alone
(`src/daemon/core.rs::the_result_goes_only_to_the_session_that_was_answered`).
At most one session is visible:

- **dmenu callers queue.** A dmenu session that arrives while another session
  (of any kind) is visible waits, and the queue is shown first in, first out;
  each caller gets its own answer
  (`tests/daemon.rs::dmenu_sessions_queue_and_each_caller_gets_its_own_answer_in_order`,
  `tests/daemon.rs::a_dmenu_caller_waits_in_the_queue_for_its_turn`,
  `tests/dmenu.rs::queued_callers_each_get_their_own_answer_in_turn` (E2E),
  `src/daemon/sessions.rs::a_dmenu_session_queues_behind_an_interactive_one`).
  A queued caller waits as long as the sessions ahead of it take; to bound
  that, run it under `timeout(1)`, since leaving cancels (below).
- **Interactive sessions replace.** An app, servitor or provider session takes
  the visible session's place; that caller exits 1. Queued dmenu callers stay
  queued and are shown once the interactive session ends
  (`tests/daemon.rs::an_interactive_session_replaces_the_visible_one`,
  `tests/daemon.rs::a_replaced_dmenu_caller_exits_1_with_empty_stdout`,
  `src/daemon/sessions.rs::an_interactive_session_replaces_the_visible_one_and_keeps_the_queue`).
- **Leaving cancels.** A caller that exits or is killed (`timeout(1)`, Ctrl+C)
  cancels its session: a visible one closes and the next queued one is shown,
  a queued one is dropped
  (`tests/daemon.rs::a_client_disconnect_cancels_its_session_and_the_next_one_is_shown`,
  `src/daemon/sessions.rs::closing_a_queued_session_drops_it_silently`). A
  provider is then sent `cancel`; one that was never sent a choice is ended,
  one that was is left to finish
  (`tests/provider.rs::a_provider_that_never_acted_and_lingers_after_cancel_is_ended`,
  `tests/provider.rs::a_dismissed_provider_that_acted_is_never_signalled_nor_its_action`).

**Ordering caveat.** Connections are not ordered among themselves: the queue
follows the order in which the daemon *receives* sessions, not the order in
which callers were started, so two callers started a moment apart may be
answered in either order. A session opened just after another caller left may
wait in the queue until the daemon has handled that departure (the protocol
notes in [`src/protocol/mod.rs`](src/protocol/mod.rs)). Callers that need an
order should run one after another.

The daemon a caller starts outlives it and serves later callers
(`tests/daemon.rs::a_client_starts_the_daemon_when_none_answers_and_later_clients_reuse_it`,
`tests/daemon.rs::an_auto_started_daemon_outlives_its_starter_and_logs_to_the_runtime_dir`).

### Providers

`logis --provider PROGRAM [ARG]...` runs a provider that speaks
[the provider protocol](docs/provider-protocol.md) (version 1: its messages,
rules, signals and examples). The daemon runs it as this caller would have,
in its working directory and with its environment
(`tests/provider.rs::a_two_stage_flow_runs_to_the_providers_close`), and shows
its stages, pending rows and toasts in one window
(`tests/provider_window.rs::a_two_stage_flow_with_a_pending_row_ends_in_a_toast`
(E2E)). For the caller it is an interactive session: exit 0 when the provider
sends `close`, 1 when dismissed or replaced, 2 when the provider cannot start,
breaks the protocol, shows no stage within 5 s or exits early
(`tests/provider.rs::a_provider_that_cannot_start_is_an_error`,
`tests/provider.rs::a_provider_that_breaks_the_protocol_or_departs_fails_its_session`,
`tests/provider.rs::a_provider_that_shows_nothing_in_time_fails_its_session`).

### servitor mode

`logis --servitor` ([servitor mode](#servitor-mode)) is an interactive session
like a provider's, with the same exit statuses, and promises:

- A rite whose auspex is still awaited is never chosen nor invoked, however
  long it takes; leaving ends the census
  (`tests/servitor.rs::a_rite_that_never_settles_is_never_invoked_and_leaving_ends_the_census`,
  `src/servitor/flow/tests.rs::a_pending_rite_is_never_taken`).
- The current aspect is marked and selected only from a settled reading
  (`src/servitor/flow/tests.rs::a_settled_rite_offers_its_aspects_with_the_current_marked_and_selected`,
  `src/servitor/flow/tests.rs::a_stale_or_unknown_aspect_is_never_marked_or_selected`).
- Inscriptions come from the rite's `canon`: the decree prefilled, a mandatory
  one filled before invoking; a cleared decree is passed as `--<key> ""`, an
  empty word without a decree is left out
  (`src/servitor/flow/tests.rs::inscriptions_are_asked_in_written_order_from_the_canon`,
  `src/servitor/flow/tests.rs::a_mandatory_inscription_must_be_filled_before_invoking`,
  `src/servitor/flow/tests.rs::a_cleared_decree_is_passed_and_an_empty_word_without_one_is_left_out`,
  `tests/servitor.rs::an_edict_is_invoked_without_an_aspect`).
- `servitor invoke` runs detached: its own session and process group, stdio on
  `/dev/null`, the caller's environment and working directory, unawaited
  (`tests/servitor.rs::servitor_mode_invokes_detached_once_settled_and_the_caller_does_not_wait`,
  `src/servitor/invoke.rs::invoke_runs_in_a_session_of_its_own_with_stdio_on_dev_null_and_unawaited`).
- logis shows no outcome
  (`src/servitor/flow/tests.rs::nothing_the_flow_says_is_an_outcome_toast`), and
  neither dismissal nor the daemon's end aborts a running invocation
  (`tests/servitor.rs::no_dismissal_or_departure_of_logis_aborts_a_running_invocation`).

### When logis cannot serve

Every case exits 2 with an empty stdout and the reason on stderr. The
message is for people: its wording may change, so scripts branch on the exit
status only. It does name the plain fact:

| Case | stderr names | Pinned by |
|---|---|---|
| no Wayland session: `WAYLAND_DISPLAY` unset, or its socket unreachable | `WAYLAND_DISPLAY is not set` / the socket's path | `tests/cli.rs::a_dmenu_caller_without_a_usable_session_exits_2_naming_why`, `tests/daemon.rs::a_client_whose_daemon_cannot_start_exits_2_with_the_reason`, `tests/cli.rs::with_nonexistent_wayland_socket_exits_2_naming_the_socket` |
| a compositor without layer shell (GNOME/Mutter) | `zwlr_layer_shell_v1` | `tests/cli.rs::a_dmenu_caller_without_a_usable_session_exits_2_naming_why`, `tests/cli.rs::on_compositor_without_layer_shell_exits_2_naming_the_protocol` |
| a compositor without fractional scaling | `wp_fractional_scale_manager_v1` | `tests/cli.rs::on_compositor_without_fractional_scale_exits_2_naming_the_protocol` |
| no runtime directory for the daemon's socket | `XDG_RUNTIME_DIR` | `tests/cli.rs::a_dmenu_caller_without_a_usable_session_exits_2_naming_why` |
| an invalid config stops the daemon starting | the file, line and column | `tests/config.rs::a_daemon_refuses_to_start_on_an_invalid_config_and_its_client_exits_2` |
| the daemon dies during the session (the compositor left) | that the daemon fell silent | `tests/daemon.rs::a_dmenu_caller_whose_daemon_dies_exits_2_with_empty_stdout`, `tests/daemon.rs::losing_the_wayland_connection_makes_the_daemon_exit_non_zero` |
| the daemon refuses the caller (another version, left running across an upgrade) | the daemon's reason: restart it | `tests/daemon.rs::a_caller_the_daemon_refuses_exits_2_with_the_daemons_reason`, `tests/daemon.rs::a_greeting_in_another_protocol_version_is_refused` |

A daemon that lost its compositor exits and removes its socket; the next
caller starts a new one
(`tests/daemon.rs::losing_the_wayland_connection_makes_the_daemon_exit_non_zero`,
`tests/daemon.rs::a_client_starts_the_daemon_when_none_answers_and_later_clients_reuse_it`).
Plain `logis` is the app launcher and opens a window. To use logis where it can
serve and fall back elsewhere, let the dmenu caller's exit status decide:
2 means logis cannot serve here, for each case above, its reason on stderr
(`tests/cli.rs::a_dmenu_caller_without_a_usable_session_exits_2_naming_why`,
`tests/cli.rs::without_wayland_display_exits_2_with_flavoured_message`,
`tests/cli.rs::on_compositor_without_layer_shell_exits_2_naming_the_protocol`).
To start a daemon ahead of time without a window, run `logis daemon --detach &`;
it exits 0 at once if one already answers
(`tests/cli.rs::daemon_detach_exits_0_at_once_when_a_daemon_already_answers`).

```sh
items='rite of oil
rite of ignition'
choice=$(printf '%s\n' "$items" | logis --dmenu)
case $? in
0) printf 'chosen: %s\n' "$choice" ;;
1) ;;   # nothing chosen: dismissed, or replaced
*) choice=$(printf '%s\n' "$items" | fuzzel --dmenu) ;;   # logis cannot serve here
esac
```

### Keys, daemon, environment and files

What the man page adds, pinned as well:

- Accept: Return, keypad Enter, a click on a row
  (`src/input/bindings.rs::return_and_keypad_enter_accept`,
  `tests/pointer.rs::hovering_selects_a_row_and_a_click_on_it_accepts_it` (E2E)).
  Dismiss: see [Exit status](#exit-status). Moving and editing, as in
  [Keyboard](#keyboard): `src/input/bindings.rs::up_down_tab_and_ctrl_n_p_move`,
  `src/input/bindings.rs::editing_keys_edit`,
  `src/input/bindings.rs::left_right_home_and_end_move_the_cursor`,
  `tests/e2e.rs::every_editing_and_selection_key_does_its_work` (E2E).
- `logis daemon` checks the session and the config at start and exits 2 if
  either is unusable, or when the compositor goes away (see the table above);
  a second daemon finds the first listening and exits 0
  (`tests/daemon.rs::a_second_daemon_defers_to_the_one_already_listening`).
  `--idle-timeout SECONDS` ends it once no client has been connected for that
  long, off by default, `0` keeps it resident, and it outranks the config
  (`tests/daemon.rs::with_an_idle_timeout_the_daemon_exits_once_no_client_is_connected`,
  `tests/daemon.rs::without_an_idle_timeout_the_daemon_stays`,
  `tests/config.rs::the_idle_timeout_flag_outranks_the_config`). `--detach`
  (how clients start it) sends its later diagnostics to
  `$XDG_RUNTIME_DIR/logis.log`
  (`tests/daemon.rs::an_auto_started_daemon_outlives_its_starter_and_logs_to_the_runtime_dir`).
- `WAYLAND_DISPLAY` is the compositor's socket, relative to `XDG_RUNTIME_DIR`
  or absolute, with no fallback to `wayland-0`
  (`src/wayland.rs::a_relative_display_is_joined_to_the_runtime_dir`,
  `src/wayland.rs::an_absolute_display_is_used_as_is`,
  `src/wayland.rs::an_unset_or_empty_display_means_no_session`). The daemon's
  socket is `$XDG_RUNTIME_DIR/logis.sock`, which must be absolute
  (`src/protocol/mod.rs::the_socket_lives_in_an_absolute_runtime_dir`). The
  config is `$XDG_CONFIG_HOME/logis/logis.jsonc`, `~/.config` by default
  (`src/config/source.rs::the_default_lives_in_xdg_config_home`,
  `src/config/source.rs::without_a_usable_xdg_config_home_the_default_is_under_home`).
- `logis --version` prints `logis` and its version, exit 0
  (`tests/cli.rs::version_flag_prints_name_and_version_and_succeeds`).

## Configuration

logis reads `$XDG_CONFIG_HOME/logis/logis.jsonc` (`~/.config/logis/logis.jsonc`), or
the file given with `--config PATH`. It is JSON with comments and trailing commas
(JSONC), described by the JSON Schema in
[`schema/logis.schema.json`](schema/logis.schema.json); name it under `"$schema"` and
editors complete and check every key. Every key is optional; a missing file means
the defaults.

```jsonc
{
  "$schema": "https://raw.githubusercontent.com/nerdwave-nick/logis/main/schema/logis.schema.json",
  "theme": "forge-crimson-brass",  // a bundled theme or themes/<name>.jsonc
  "idle-timeout": 0,               // seconds without a client before the daemon exits; 0 = never
  "terminal": ["foot", "-e"],      // runs Terminal=true apps; absent: $TERMINAL -e
  "icon-theme": "hicolor",         // freedesktop icon theme, falling back to hicolor
  "focus-loss": "dismiss",         // or "keep": what losing keyboard focus does
  "blur": true,                    // blur what lies behind the panel, where offered
  "reduce-motion": false,          // true stills the theme's pulse, highlight, flicker
}
```

Unknown keys and values of the wrong kind are errors naming the key, its line and
its column. The daemon watches the file (and its directory, so editors that save by
replacing the file are seen too) and applies a sound edit at once. An invalid edit
changes nothing: the last sound configuration stays in force, and the error goes to
the daemon's log and, while a list is shown, to a themed notice over it (3 s).

`--config PATH` given to a client reaches the daemon only when that client starts
it; a daemon already running keeps the file it was started with. The window
follows `focus-loss`, `blur`, `icon-theme`, `theme` and `reduce-motion`, the app
launcher `terminal`.

`reduce-motion` is off by default, as decided for the bundled themes (their glow
pulses and a highlight travels round the border; the flicker is very subtle).
Turned on, it stills every continuous effect of the theme (glow pulse, travelling
highlight, flicker, a pending row's shimmer, and the grain stops changing); the
open, close and selection transitions stay (`LOGIS_MOTION=off` stills those).

## Themes

A theme is the cogitator's whole look (spec §4.9, the model decided on 6217c721):

- a **palette** of named colours with alpha (`"brass": "#d4a017"`, or several:
  `"soot": ["#170e0b", "#0b0807"]`, named `soot.0`, `soot.1`);
- the **elements** that refer to it: `window` (default size and rows), `glow`,
  `border` (gradient of up to four stops, travelling highlight), `panel`,
  `decorations` (corner rivets), `prompt` (label, sigil), `query` (caret),
  `counter`, `rule`, `row` (font, height, colour per tab-separated column),
  `selection`, `match` (colour, weight, underline), `badge`, `pending` — and
  `fonts`, the font roles they name;
- **effects and motion**: one post-processing pass over the body (`scanlines`,
  `grain`, `flicker`, `vignette`, `bloom`, `curvature`, `aberration`, each a
  strength where 0 is off) and the transition timings.

Colours are `#rrggbb`, `#rrggbbaa`, `transparent` or a palette name, each
optionally with `/alpha` (`"crimson/0.55"`). Lengths are logical pixels: a theme
looks the same at scales 1.0, 1.3 and 2.0. The JSON Schema is
[`schema/theme.schema.json`](schema/theme.schema.json); its defaults are the
bundled default's values.

Two themes are bundled (compiled in): **`forge-crimson-brass`** (the default:
[themes/forge-crimson-brass.jsonc](themes/forge-crimson-brass.jsonc), after the
research spike) and **`cogitator-phosphor`**, a green CRT (curved glass, bloom,
inverse selection, underlined matches:
[themes/cogitator-phosphor.jsonc](themes/cogitator-phosphor.jsonc)). Choose one
with the config's `"theme"`. Your own are `themes/<name>.jsonc` beside
`logis.jsonc` (`~/.config/logis/themes/`); a file named like a bundled theme
overrides it.

```jsonc
// ~/.config/logis/themes/amber.jsonc: phosphor, in amber
{
  "$schema": "https://raw.githubusercontent.com/nerdwave-nick/logis/main/schema/theme.schema.json",
  "extends": "cogitator-phosphor",
  "palette": { "phosphor": "#ffb000", "text": "#e0a000", "dim": "#8a6000", "edge": "#ffb000" },
  "prompt": { "sigil": { "image": "sigils/skull.svg", "size": 24 } }
}
```

- **Extending and defaults:** `"extends"` names one other theme (bundled or
  yours) whose keys a theme starts from: one level only (the theme it extends
  extends nothing), and never in a circle. A theme extending its own name
  amends the bundled theme of that name. Whatever a theme and its base leave out
  comes from forge-crimson-brass. A part that may be off (`rivets`, `sigil`,
  `highlight`, `ornament`, `dash`) is turned off with `false`.
- **Fonts** are fontconfig families in order of preference, generic families and
  your aliases included; a family that is not installed falls through to the
  next: forge uses IBM Plex Mono → monospace for rows and Cinzel → serif for the
  prompt's label, phosphor VT323 → Terminus → monospace. Only the resolved files
  are loaded.
- **Sigils and images:** `"gear"` is logis's own (drawn in the sigil's colour);
  any other `image` is an SVG or PNG file, relative to the theme's directory
  (or absolute). Original motifs only in the bundled themes; your personal
  themes may load whatever images you like.
- **Size:** a session that names no size (the app launcher, servitor mode, a
  plain `logis --dmenu`) opens in the theme's `window`: 820×520 logical pixels,
  centred, nine rows for both bundled themes. `--lines` and `--width` (in
  average characters of the row font) size it instead.
- **Live:** the daemon watches `themes/*.jsonc` and the config and applies an
  edited theme at once, a visible window included (its colours, fonts, effects,
  motion timings and, when the session named no size, its size). An invalid
  theme changes nothing: the last sound one stays, and the error (file, line and
  column, or the key: `border.paint[1]: no colour named 'embr' in the palette`)
  goes to the log and a notice over a shown list. A theme that cannot be worn at
  start leaves the bundled default in force. `logis --check-config` checks the
  theme the config names.
- **Continuous effects** (glow pulse, travelling highlight, flicker, a pending
  row's shimmer) keep a visible window drawing at the output's refresh rate:
  measured on the owner's machine (Intel Arc, nested niri at 60 Hz, release
  build) a visible forge window draws 60 frames/s with about 70 daemon
  wake-ups/s and 6 % of one CPU core; with `reduce-motion` it draws nothing and
  wakes nothing once settled, as any idle window.

## Motion and blur

The window moves only when something changes, paced by the compositor's frame
callbacks (and timed by wp_presentation where offered); once a movement has
settled it draws nothing until the next change.

- **Open:** the panel rises 10 px into place and fades in from 40 % over 150 ms
  (ease out). Its first frame already shows it; the open starts when that frame
  is on screen.
- **Close:** when a session ends, its answer goes to the caller at once; then the
  panel gives up keyboard and pointer, sinks 6 px and fades out over 110 ms (ease
  in). A session that comes next is shown at once, cutting the fade short.
- **Selection:** the selection band glides to the newly selected row in 90 ms
  (ease out).

- **Provider stages and toasts:** a provider's next stage is not a close and an
  open: the window stays in place, only its list changes, and the band takes the
  new list's row at once. A toast is drawn inside the panel, so it opens, moves
  and fades with it; a window lingering on its toast after the session ends
  closes with the same close transition when the daemon takes it away.
- **Icons** move and fade with the panel like everything else in it.
- **Launching an app:** the activation token is asked for while the panel is
  still shown and focused; the launch happens once it comes, then the panel
  closes and fades. The launched app gets the keyboard at once.

The durations, start and end poses and easing curves (CSS `cubic-bezier`) are the
parameters of `logis::motion::Motion`; a theme sets the durations (`"motion":
{ "open-ms", "close-ms", "select-ms" }`). `LOGIS_MOTION=off` in the daemon's
environment turns motion off, and `LOGIS_MOTION=4` runs it four times slower (any
factor up to 100); either outranks the theme.

With `"blur": true` (the default) the compositor blurs what lies behind the
panel's body, a rounded rectangle, through ext-background-effect-v1 (niri offers
it). A compositor without that protocol shows the panel unblurred; the setting
changes the visible window at once.

## Keyboard

The keyboard follows your layout as the compositor sends it (libxkbcommon), with
key repeat at the compositor's rate (a rate of 0 turns it off) and Compose
sequences from your locale's Compose table, or `XCOMPOSEFILE`, or
`$XDG_CONFIG_HOME/XCompose`, or `~/.XCompose`, as libxkbcommon finds them (read
when the window code starts; edits need a restart).

| Keys | Action |
|---|---|
| Escape, Ctrl+G, Ctrl+[ | dismiss |
| Return, keypad Enter | accept |
| Shift+Return | open the selected app's desktop actions (elsewhere: accept) |
| Up, Shift+Tab, Ctrl+P | select the previous item |
| Down, Tab, Ctrl+N | select the next item |
| Backspace / Delete | delete the character (grapheme) before / after the cursor |
| Ctrl+W | delete the word before the cursor |
| Ctrl+U | clear the query |
| Ctrl+V, Shift+Insert | paste the clipboard at the cursor |
| Left, Right, Home, End | move the cursor |

Shortcuts ignore modifiers the key itself needs (consumed modifiers): Ctrl+[ works
where `[` is AltGr+8, Caps Lock and Num Lock never change a shortcut, and while a
second layout is active the first layout's letters still work with Ctrl. Losing
keyboard focus dismisses unless the config says `"focus-loss": "keep"`.

## Pointer and paste

- **Hover** selects the row under the pointer once the pointer moves; a window
  that opens under a pointer at rest keeps its selection (and a `--select`).
- **Click** (primary button pressed and released on the same row) accepts that
  row. A press dragged to another row accepts nothing.
- **Scroll** moves the rows in view, and the selection stays on the row under
  the pointer: a wheel notch scrolls three rows (high-resolution wheels add up
  their fractions), a touchpad finger one row per row height, so the rows follow
  the finger.
- **Middle button** pastes the primary selection; **Ctrl+V** and
  **Shift+Insert** paste the clipboard. A paste inserts at the query's cursor
  the first non-empty line of the text (dmenu's rule; tabs become spaces, other
  control characters are dropped, at most 64 KiB are read), choosing
  `text/plain;charset=utf-8` over `UTF8_STRING`, `text/plain`, `TEXT` and
  `STRING`. The text is read without ever blocking the window.
- **Cursor**: the compositor's default arrow, through `wp_cursor_shape_v1`. A
  compositor without it keeps showing the cursor it had (logis loads no cursor
  theme of its own).

## Building

Requires Rust 1.91 or later, `pkg-config`, and the libwayland-client,
libfontconfig and libxkbcommon development files (`wayland`, `fontconfig` and
`libxkbcommon` on Arch; `libwayland-dev`, `libfontconfig-dev` and
`libxkbcommon-dev` on Debian/Ubuntu). The keyboard's unit tests compile keymaps
from xkeyboard-config (`xkeyboard-config` / `xkb-data`).

```sh
cargo build --release
cargo test
```

`cargo test` renders offscreen and needs a GPU adapter (hardware, or Mesa lavapipe
from `vulkan-swrast` / `mesa-vulkan-drivers`); without one, the render tests print
`SKIPPED` unless their backend is listed in `LOGIS_TEST_REQUIRE_BACKENDS`
(`auto,vulkan,gl,software`), as CI does.

### Fonts and text goldens

logis asks libfontconfig, in-process, for the theme's faces (each a list of
families in order of preference at a weight, after the user's aliases; without
a theme the default family `sans-serif`) and, per character, for the first font
in fontconfig's fallback order that covers it; only those files are loaded. cosmic-text's full
system scan (`FontSystem::new()`) is forbidden by `clippy.toml`. The daemon
resolves its fonts (fontconfig's configuration and caches, the default face and
the fallback order) on a thread of their own while it connects and wakes the GPU,
so with thousands of installed faces they stay off the first frame's path.

The text tests (`tests/text.rs`) pin their fonts: an isolated fontconfig setup that
knows only the OFL subsets in `tests/fonts/` (regenerated by
`tests/fonts/subset.sh`), so the goldens in `tests/goldens/text/` do not depend on
installed fonts. A pixel matches when each channel is within 2 of the golden.
After a deliberate change, rewrite and review the goldens:

```sh
LOGIS_UPDATE_GOLDENS=1 cargo test --test text
```

A mismatch leaves the render and a difference image under
`target/tmp/golden-failures/`.

The themes have goldens of their own: `tests/list.rs` draws the same lists in
each bundled theme at scales 1.0, 1.3 and 2.0 into `tests/goldens/themes/<theme>/`,
the frame's time frozen (glow at full strength, the highlight at the top, grain
and flicker at time 0), with the pinned Noto Sans Mono and Noto Serif standing in
for the themes' own families; `tests/faces.rs`, `tests/primitives.rs` and
`tests/post.rs` cover faces and carets, gradients and the highlight, and each
post-processing effect. The post pass reads pixels with `textureLoad` and draws
its noise from an integer hash, so its goldens hold on any GPU.
`LOGIS_UPDATE_GOLDENS=1 cargo test --test list` rewrites them.

`tests/icons.rs` compares icon renders with the PNG goldens in
`tests/goldens/icons/` (straight alpha, each channel within 3);
`LOGIS_UPDATE_GOLDENS=1 cargo test --test icons` rewrites them after an intended
change.

### Test infrastructure

Shared helpers live in [`tests/support/`](tests/support/); a test crate adopts them
with `mod support;`.

- **Goldens** ([`golden.rs`](tests/support/golden.rs)): `golden::check(set, name,
  &image)` compares an image with `tests/goldens/<set>/<name>.png` (each channel
  within 2); `golden::check_scene(&gpu, set, name, &scene, logical, &scales)`
  renders a scene through `render::render_offscreen` at each scale
  (`<name>@1.3` …). `Goldens::checked_in()` takes another `Tolerance` (channel
  difference plus an allowed fraction of outlier pixels) or `Alpha::Straight`.
  `LOGIS_UPDATE_GOLDENS=1` rewrites goldens; mismatches leave
  `<name>.actual.png` and `<name>.diff.png` under `target/tmp/golden-failures/`.
- **End to end** ([`e2e/`](tests/support/e2e/)): `TestCompositor::start(kind)`
  runs a compositor the harness owns, in a scratch `XDG_RUNTIME_DIR`: a nested
  niri window locally (`LOGIS_E2E=niri`, its own generated config, scale 1, never
  `--session`), sway on wlroots' headless backend in CI (`LOGIS_E2E=sway`).
  `compositor.logis()` is a `logis` command inside it (pinned fonts);
  `Traced::spawn(command)` runs it with `LOGIS_TEST_TRACE=1` and waits for its
  trace (`wait_for_focus()`, `wait_for_commit()` …). Clients get a Compose table
  of the harness's own (`XCOMPOSEFILE`, never anyone's `~/.XCompose`) and a
  scratch `XDG_CONFIG_HOME`; both compositors and the injector use the US layout
  with Menu as Compose (`compose:menu`) and a repeat of 30/s after 300 ms. `Harness::connect(&compositor)`
  refuses any socket whose server is not that compositor process (peer
  credentials), then offers `virtual_keyboard()`, `type_text`, `tap`, `hold`/
  `release` (real modifier keys plus masks), `toggle` (Caps/Num Lock) — all
  requiring the `Focused` proof from `wait_for_focus()` — and `capture()`
  (wlr-screencopy, upright). `virtual_pointer()` (zwlr_virtual_pointer_v1) adds
  `move_to`, `button`/`click`, `wheel` (notches) and `finger` (touchpad pixels);
  a `Panel` maps output points onto a client's surface, calibrated from where
  the client says the pointer entered it. `SelectionOwner::start(&compositor,
  clipboard, primary)` owns the test compositor's clipboard and primary
  selection (wlr-data-control, no `wl-copy`) and records which MIME types were
  asked for; it connects through the same isolation guard
  (`wire::connect_verified`). Without `LOGIS_E2E` these tests print `SKIPPED`.
- **Latency** ([`tests/latency.rs`](tests/latency.rs),
  [`latency.rs`](tests/support/latency.rs)): the research/bench `T0_NS` method:
  the harness reads `CLOCK_MONOTONIC` just before starting the client, the
  client stamps `commit` (and key) events with the same clock. Start → first
  commit is measured with the pinned fonts and with the system's own, run by run
  in turn. Each series is reported against its spec §4.10 budget; `LOGIS_LATENCY_REPORT=<file>` appends
  JSON lines and `LOGIS_LATENCY_BUDGETS=enforce` fails a median over budget.
  The dmenu series run a real daemon: warm open (`logis --dmenu` from exec to
  the window's first committed frame, ≤ 15 ms) and keystroke → commit with 3
  and with 100 000 items (one frame). With motion, "first committed frame" is
  the first frame of the open transition, not its end; the end is a series of
  its own (warm open → open settled). Each warm open starts 100 ms after the
  previous window is gone (`faded`), not merely hidden: a nested niri that is
  still drawing a closing window answers the new window's setup round trips
  up to two parent frames late (a real niri does not).
- **Motion** ([`tests/motion.rs`](tests/motion.rs)): the open and the band's
  glide take several frames and then nothing is drawn, the caller has its
  answer while the window still fades, a queued session does not wait for the
  fade, blur follows the body where offered (not when the config says no), and
  an idle window wakes no thread of the daemon (context switches unchanged).
  `Cogitator::start_with(env, config)` starts the daemon with extra
  environment (`LOGIS_MOTION`) or a config.
- **dmenu** ([`tests/dmenu.rs`](tests/dmenu.rs),
  [`cogitator.rs`](tests/support/e2e/cogitator.rs)): a traced `logis daemon` in
  the test compositor and `logis --dmenu` clients with piped stdin and stdout:
  select, dismiss, index and column output, `--only-match`, typed text, queued
  callers, streamed input and the compositor's loss.
  [`tests/dmenu_runes.rs`](tests/dmenu_runes.rs) checks the runes over a real
  daemon with the scripted resolver (no window), and
  [`tests/list.rs`](tests/list.rs) the list's goldens offscreen.
- **Pointer and paste** ([`tests/pointer.rs`](tests/pointer.rs)): hover, click,
  a press dragged off its row, wheel and finger scrolling, the cursor shape,
  Ctrl+V and Shift+Insert, and the middle button's primary paste, against the
  same daemon and `logis --dmenu` clients.
- **Providers** ([`tests/provider.rs`](tests/provider.rs),
  [`tests/provider_window.rs`](tests/provider_window.rs)): the fixture provider
  [`forge.sh`](tests/fixtures/providers/forge.sh) (POSIX sh) through a real
  daemon: with the scripted resolver (a two-stage flow, cancellation and the
  signal rules, timeouts and protocol errors) and in the test compositor (a
  pending row Return cannot take, a second stage in the same window, a toast the
  window lingers on, Escape after an activation sparing the action).
- **App launcher** ([`tests/launch.rs`](tests/launch.rs),
  [`tests/apps.rs`](tests/apps.rs), [`tests/apps_window.rs`](tests/apps_window.rs)):
  fixture desktop entries ([`tests/fixtures/apps/`](tests/fixtures/apps/)) and
  the fixture program [`anoint`](tests/fixtures/apps/bin/anoint), which records
  its arguments, environment, activation token and session, then waits. Launches
  with a fake session bus ([`fake_bus.rs`](tests/support/fake_bus.rs): the user
  manager's environment, the transient scope, D-Bus activation; the real bus is
  never reached), through a real daemon with the scripted resolver (the launch,
  frecency, an app installed while it runs), and in the test compositor (a real
  activation token, the app outliving the killed daemon, desktop actions as a
  second stage). The D-Bus messages logis writes are compared byte for byte with
  GLib's ([`glib-reference.py`](tests/fixtures/dbus/glib-reference.py)). Test
  daemons run without `DBUS_SESSION_BUS_ADDRESS` in a scratch runtime dir, so
  they find no session bus.
- **Themes** ([`tests/theme_window.rs`](tests/theme_window.rs)): in the test
  compositor, an edited theme reaches the visible window (`theme <name>`), a
  broken one keeps the last sound theme and shows its notice (`toast …`,
  `untoast`), and a window keeps drawing only while the theme's continuous
  effects move (it reports frames, wake-ups and CPU for both). Every other E2E
  daemon runs with `reduce-motion` unless its test's config names it, so its
  windows settle and screen captures agree.
- **servitor mode** ([`tests/servitor.rs`](tests/servitor.rs),
  [`tests/servitor_window.rs`](tests/servitor_window.rs)): a fake `servitor`
  ([`tests/fixtures/servitor/servitor`](tests/fixtures/servitor/servitor), POSIX
  sh) on the caller's `PATH` tells census lines recorded from the real servitor
  ([`vigil.jsonl`](tests/fixtures/servitor/vigil.jsonl) and fixtures derived
  from it, with timed and gated pending cases) and records how `servitor invoke`
  was launched (argv, session and process group, stdio, environment, working
  directory, when it started and ended). With the scripted resolver and in the
  test compositor (pending rows, live settling, the current aspect, a prefilled
  decree).

`LOGIS_TEST_TRACE=1` is the test-only observable in the window code: stderr lines
`logis-trace: <monotonic ns> <event>` for `keyboard-enter`/`keyboard-leave`,
`keymap`, `repeat-info <rate> <delay>`, `key <evdev code>
pressed|released|repeated`, `modifiers <depressed> <latched> <locked> <group>`,
`pointer-enter <x> <y>`, `pointer-motion <x> <y>`, `pointer-leave`, `cursor
default|unchanged`, `button <code> pressed|released`, `scroll wheel <120ths>|pixels
<px>|stop`, `paste <selection> <MIME type>|none|unavailable`, `pasted <selection>
<bytes>`, `commit` (a frame of the visible window), `commit closing` (a frame of
one fading out), `settled` (the first frame after which nothing moves), `faded` (a
closed window is gone, at once without motion), `blur <rectangles>` (a blur region was set) and
`blur-capable true|false` (the compositor's word), `cut` (a window still closing
was dropped after the next window's first frame); the debug panel adds what each key did: `query <JSON string>`, `select
<row>`, `accept <row>` and `dismiss`. The daemon's window reports its sessions:
`show <session>`, `stage <session>` (a provider's next stage), `items <total>`,
`update <item>`, `remove <item>`, `toast <JSON string>`, `untoast`, `ready`,
`query <JSON string>`, `select <item>` (or `select none`), `accept <item>`,
`descend <item>` (Shift+Return), `typed <JSON string>`, `dismiss`, `token
<session>` (an activation token came), `linger <session>`, `hide <session>` and
`theme <name>` (the window put a theme on).

Locally (inside a running Wayland session; a nested niri window opens briefly):

```sh
LOGIS_E2E=niri cargo test --test e2e --test dmenu --test pointer --test provider_window --test apps_window --test servitor_window --test motion --test theme_window
LOGIS_E2E=niri LOGIS_LATENCY_BUDGETS=enforce cargo test --release --test latency -- --nocapture
```

CI runs the unit and golden tests in one job and the E2E and latency suites in
headless sway in another, recording latency as a trend (job summary and a
`latency.jsonl` artefact) without enforcing the budgets.

### GPU backends

logis tries Vulkan hardware, then GL hardware, then lavapipe, then llvmpipe.
`LOGIS_GPU_BACKEND=vulkan|gl|software` forces one; the render tests force each in
turn. Manual check on a live session (each prints the adapter it woke):

```sh
for b in vulkan gl software; do
  LOGIS_GPU_BACKEND=$b timeout 3 logis daemon --debug-panel
done
# llvmpipe (GL software) instead of lavapipe:
LIBGL_ALWAYS_SOFTWARE=1 LOGIS_GPU_BACKEND=gl timeout 3 logis daemon --debug-panel
```

logis targets the niri compositor; other compositors with wlr-layer-shell are best
effort. X11 and compositors without layer shell (such as GNOME/Mutter) are not
supported.

### Starting the daemon with systemd (optional)

The first client starts the daemon by itself. To have systemd start it on the
first connection instead, install the user units from
[`contrib/systemd/`](contrib/systemd/) (they expect the binary at
`/usr/bin/logis`; adjust `ExecStart` otherwise) and enable the socket:

```sh
cp contrib/systemd/logis.socket contrib/systemd/logis.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now logis.socket
```

The daemon needs `WAYLAND_DISPLAY` in the user manager's environment, which niri
and most session setups import.

## Repository

- [`CONTEXT.md`](CONTEXT.md): domain vocabulary, including the grimdark words used in
  help, error and UI text.
- [`docs/adr/`](docs/adr/): architecture decisions: Rust on wgpu with logis's own
  renderer (0001), a resident daemon and a thin client (0002).
- [`docs/research/`](docs/research/): the stack research (`stack.md`) and the
  launcher landscape (`landscape.md`) that the decisions rest on.
- [`research/`](research/): disposable research code (benchmarks, Rust prototypes,
  the GPU/daemon spike, screenshots). It is evidence, not a foundation, and is not
  part of the build.

## Licence

[MIT](LICENSE).
