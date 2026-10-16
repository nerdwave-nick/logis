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

**1.0.0** (prepared; see [CHANGELOG.md](CHANGELOG.md)). `logis` (the app
launcher), `logis --dmenu`, `logis --provider` and `logis --servitor` work; to
install it, see [Installing](#installing). The binary does this much:

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
  on stderr. A [custom shader](#custom-shaders) they name is compiled on an
  offscreen GPU context, its errors at the shader's own lines.
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
the protocol is described in [`internal/protocol`](internal/protocol/protocol.go). While a
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
brass. Tab-separated columns line up: each is as wide as the widest text that
has stood in it in a row shown so far (measured with the row font), at most the
theme's share of the row (wider text ends in `…`), and the last takes the rest.
Widths only grow while the window is open, so filtering never makes the columns
jump back; a wider row coming into view widens its column once. This is display
only: Return still prints the line's original bytes.

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
| `--lines=N`, `-l` | rows shown, and so the window's height (default: the theme's `window.rows`, eleven for the bundled themes) |
| `--width=N`, `-w` | the window's width in characters of the row font (an estimate; default: the theme's window, 820 px) |
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

**Tabs (the Go build; a divergence from logis v1).** Plain `logis` with
`servitor` on its PATH opens a tabbed launcher: two plates above the panel,
**Applications** on the left and **Rites** (servitor mode's rites) on the right,
each counting what the query matches, and a TAB/SWITCH hint at the strip's end.
Tab or Shift+Tab, or a click on a plate, switches tabs; the plates stay in place
and only the lit one changes. The query stays across the tabs; Up, Down, Ctrl+N
and Ctrl+P move the selection. Return acts in the shown tab as `logis --apps` and
`logis --servitor` do: an app is launched, a rite leads through its aspects and
inscriptions to servitor's invocation; either way the client exits 0 (1 when
dismissed). A tab with nothing for the query says how much the other has
("nothing answers here · 3 among the rites — Tab"). It opens on the tab last
used, kept in `$XDG_STATE_HOME/logis/tab`. `logis --apps` and `logis --servitor`
stay the single lists they were (no plates; Tab selects the next item) and leave
the remembered tab alone; without servitor on the PATH plain `logis` is the app
launcher alone. The plates take their look from the theme (the lit one is the
selection, an idle one the panel; their corners are its `tab.radius`), and the
CRT and glitch passes cover them as they cover the panel; a switch fires the
glitch's tab burst.

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
statement here is pinned by the Go tests named beside it as
`<file>_test.go::Test…` (`cmd/logis/docs_test.go` checks that they exist).
The `TestE2E…` tests run only in a test compositor (`LOGIS_E2E`: a nested niri
locally, headless sway in CI), the rest in every `go test ./...`. Anything not
stated here (message wording, timings, the socket protocol) may change.

### Exit status

| Status | stdout | When | Pinned by |
|---|---|---|---|
| 0 | the choice, then `\n` | `--dmenu`: an item or typed text was accepted | `cmd/logis/contract_test.go::TestTheOriginalBytesOfTheChosenItemAreReturnedExactly`, `cmd/logis/dmenu_window_e2e_test.go::TestE2ETypingFiltersAndReturnPrintsTheChosenLine` |
| 0 | empty | `--provider`: the provider closed its flow | `cmd/logis/provider_e2e_test.go::TestATwoStageFlowRunsToTheProvidersClose` |
| 0 | empty | `--servitor`: `servitor invoke …` was started (logis does not wait for it) | `cmd/logis/servitor_e2e_test.go::TestServitorModeInvokesDetachedOnceSettledAndTheCallerDoesNotWait`, `cmd/logis/servitor_window_e2e_test.go::TestE2ERitesWaitPendingSettleLiveAndLeadToADetachedInvocation` |
| 0 | empty | plain `logis` (or `--apps`): an app was launched; in the Go build's tabbed launcher also a rite invoked | `cmd/logis/apps_e2e_test.go::TestPlainLogisIsTheAppLauncher`, `cmd/logis/apps_e2e_test.go::TestTheChosenAppIsLaunchedWithItsTokenInAScopeAndCountsForFrecency`, `cmd/logis/apps_window_e2e_test.go::TestE2EReturnLaunchesTheAppDetachedWithAnActivationToken` |
| 0 | the daemon's readiness line | `logis daemon --detach` when a daemon already answers (it exits at once) | `cmd/logis/contract_test.go::TestDaemonDetachExits0AtOnceWhenADaemonAlreadyAnswers`, `cmd/logis/daemon_e2e_test.go::TestE2EDaemonDetachOnlyEnsuresTheDaemon` |
| 1 | empty | dismissed: Escape, Ctrl+G, Ctrl+[, focus loss (by default) | `cmd/logis/dmenu_e2e_test.go::TestE2EAnUnmarkedListIsDismissedWithExit1AndEmptyStdout`, `cmd/logis/dmenu_window_e2e_test.go::TestE2EEscapeDismissesWithEmptyStdoutAndExit1`, `cmd/logis/provider_window_e2e_test.go::TestE2EEscapeAfterAnActivationNeverKillsTheActionItStarted`, `cmd/logis/apps_window_e2e_test.go::TestE2EPlainLogisOpensTheLauncherAndEscapeExits1`, `internal/input/bindings_test.go::TestEscapeCtrlGAndCtrlBracketDismiss`, `internal/input/keyboard_test.go::TestFocusLossDismissesByDefaultAndKeepsOnRequest` |
| 1 | empty | replaced by an interactive session (see [Sessions](#sessions)) | `cmd/logis/contract_test.go::TestAReplacedDmenuCallerExits1WithEmptyStdout`, `cmd/logis/provider_e2e_test.go::TestAReplacedProviderCallerExits1WithEmptyStdout` |
| 1 | empty | `--no-run-if-empty` and stdin is empty (no window, no daemon) | `cmd/logis/dmenu_e2e_test.go::TestE2ENoRunIfEmptyExits1AtOnceWithoutADaemon` |
| 2 | empty | any error, its message on stderr: bad runes, no usable session, the daemon cannot start, fails or refuses, the provider fails | `cmd/logis/contract_test.go::TestUnknownArgumentExits2NamingIt`, `cmd/logis/main_test.go::TestUnknownRunesAreNamedAndExit2`, `cmd/logis/cli_test.go::TestBadValuesAreNamedWithTheirFlag`, `cmd/logis/dmenu_e2e_test.go::TestE2EListRunesWithoutDmenuAreAnError`, and [below](#when-logis-cannot-serve) |

Exit 1 does not tell a dismissal from a replacement; both mean "nothing was
chosen". stdout carries nothing but the choice and its newline, also after
`--dmenu0` (`cmd/logis/dmenu_e2e_test.go::TestE2EDmenu0ReadsNulSeparatedItemsWithNewlinesInside`).
`--check-config` exits 0 or 2 likewise
(`cmd/logis/config_test.go::TestCheckConfigAcceptsASoundConfigAndExits0`,
`cmd/logis/config_test.go::TestCheckConfigWithAMissingNamedFileExits2`).

### dmenu input and output

- stdin is read as it arrives; the window opens before it ends, and logis may
  answer and exit before reading all of it, so a producer can meet a closed
  pipe (`cmd/logis/dmenu_window_e2e_test.go::TestE2ETheWindowOpensBeforeTheInputEndsAndTakesStreamedLines`, `cmd/logis/dmenu_test.go::TestLinesAreSentAsTheyArriveThenEnd`).
- A chosen item comes back as its original bytes, exactly as read, whatever
  their encoding and whatever `--with-nth` showed
  (`cmd/logis/dmenu_e2e_test.go::TestE2ETheOriginalBytesSurviveColumnsAndInvalidUTF8`,
  `cmd/logis/contract_test.go::TestTheOriginalBytesOfTheChosenItemAreReturnedExactly`).
- Return with no match prints the typed text as typed, unless `--only-match`
  (`cmd/logis/dmenu_window_e2e_test.go::TestE2EReturnWithoutAMatchPrintsTheTypedText`,
  `internal/list/view_test.go::TestReturnWithoutAMatchAcceptsTheTypedTextUnlessOnlyMatch`,
  `cmd/logis/dmenu_window_e2e_test.go::TestE2EOnlyMatchRefusesFreeTextUntilAMatchIsChosen`).
- Matching ignores case and accents and takes fzf's syntax
  (`internal/list/filter_test.go::TestMatchingIgnoresCaseAndAccents`,
  `internal/list/filter_test.go::TestFzfSyntaxApplies`,
  `internal/list/filter_test.go::TestFzfSuffixAndExactTermsApply`).

### dmenu runes

The runes, their defaults and their fuzzel semantics are in [dmenu](#dmenu)
(`cmd/logis/cli_test.go::TestTheDmenuDefaultsAreFuzzels`,
`cmd/logis/cli_test.go::TestValuesFollowTheirFlagOrAnEqualsSign`):

| Rune | Pinned by |
|---|---|
| `--dmenu`, `-d`, `--dmenu0` | `cmd/logis/cli_test.go::TestDmenuAsksForADmenuSession`, `cmd/logis/dmenu_e2e_test.go::TestE2EDmenu0ReadsNulSeparatedItemsWithNewlinesInside` |
| `--prompt`, `-p` | `internal/list/scene/scene_test.go::TestASessionPromptShowsAndTheDefaultOneGivesWayToTheLabel` |
| `--prompt-only` | `cmd/logis/dmenu_e2e_test.go::TestE2EPromptOnlyNeverWaitsForStdin`, `cmd/logis/cli_test.go::TestPromptOnlyReadsNothingAndShowsNoRows` |
| `--placeholder` | `internal/list/view_test.go::TestAnEmptyQueryShowsThePlaceholderWithoutACursor` |
| `--password[=C]` | `cmd/logis/cli_test.go::TestPasswordTakesAnOptionalCharacter`, `internal/list/view_test.go::TestAPasswordIsMaskedPerCharacter` |
| `--lines`, `-l`, `--width`, `-w` | `internal/list/scene/scene_test.go::TestLinesAndWidthInTheOptionsSizeTheWindow`, `internal/list/scene/scene_test.go::TestWithoutASizeTheWindowIsTheThemes` |
| `--counter` | `internal/list/view_test.go::TestTheCounterCountsMatchesOfAll` |
| `--search` | `internal/list/view_test.go::TestSearchOpensWithAQuery` |
| `--select` | `internal/list/view_test.go::TestSelectPreselectsTheFirstEqualItemIgnoringCaseWhenItArrives` |
| `--select-index` | `internal/list/view_test.go::TestSelectIndexPreselectsOnceItArrivesUnlessTheUserMovedFirst`, `cmd/logis/cli_test.go::TestPreselectionIsByTextOrByIndexNotBoth` |
| `--only-match` | see typed text above |
| `--no-run-if-empty`, `-R` | `cmd/logis/dmenu_e2e_test.go::TestE2ENoRunIfEmptyExits1AtOnceWithoutADaemon` |
| `--index` | `cmd/logis/dmenu_e2e_test.go::TestE2EIndexPrintsThePositionOfTheChoice`, `cmd/logis/dmenu_window_e2e_test.go::TestE2EIndexAndColumnsPrintWhatWasAsked` |
| `--with-nth` | `cmd/logis/dmenu_e2e_test.go::TestE2EWithNthShowsAColumnAndTheWholeLineComesBack` |
| `--accept-nth`, `--nth-delimiter` | `cmd/logis/dmenu_e2e_test.go::TestE2EAcceptNthPrintsColumnsSplitAtTheNthDelimiter` |
| `--match-nth` | `cmd/logis/dmenu_test.go::TestWithNthShapesTheTextAndMatchNthTheMatchText`, `internal/list/view_test.go::TestItemsMatchedByAnotherTextAreNotHighlighted` |
| formats `{N}`, `{N..M}`, `{N..}`; `0` | `internal/nth/nth_test.go::TestAFormatExpandsColumnsInsideFreeText`, `internal/nth/nth_test.go::TestRangesJoinTheirColumnsWithTheDelimiter`, `internal/nth/nth_test.go::TestZeroMeansNoSelection` |
| `--config` | `cmd/logis/config_e2e_test.go::TestE2EAClientHandsItsConfigToTheDaemonItStarts`, `cmd/logis/config_e2e_test.go::TestE2EARunningDaemonKeepsItsConfigWhateverACallerNames` |
| `-i` (ignored) | `cmd/logis/cli_test.go::TestListRunesGoWithDmenuInAnyOrder` |

`--config PATH` reaches only a daemon that this caller starts; a daemon
already running keeps its own configuration.

### fuzzel compatibility

logis takes fuzzel's dmenu runes (fuzzel(1) 1.15) and exit statuses 0 and 1.
Where fuzzel's documentation leaves a choice open, or logis differs on
purpose:

- typed text is printed as typed, even with `--index` or `--accept-nth`
  (`cmd/logis/dmenu_test.go::TestTypedTextPrintsAsTypedWhateverTheOutputRunes`);
- `--select` picks the first item equal to its text ignoring case, not a
  prefix or fuzzy match, and applies when that item arrives, until the user
  moves the selection or edits the query
  (`internal/list/view_test.go::TestSelectPreselectsTheFirstEqualItemIgnoringCaseWhenItArrives`,
  `internal/list/view_test.go::TestSelectIndexPreselectsOnceItArrivesUnlessTheUserMovedFirst`,
  `internal/list/view_test.go::TestTypingEndsAPreselectionStillWaitingForItsItem`);
- `--no-run-if-empty` exits 1 (nothing was chosen);
- a format that names no column is an error, exit 2, not a constant output
  (`internal/nth/nth_test.go::TestColumnZeroAndBackwardRangesAreRefused`);
- list runes without `--dmenu` are an error, exit 2;
- errors exit 2, apart from a dismissal's 1, where fuzzel exits 1 (its
  `--check-config`, for one); logis's `--check-config` exits 2
  (`cmd/logis/config_test.go::TestCheckConfigWithAMissingNamedFileExits2`);
- Shift+Insert pastes the clipboard, not the primary selection; the middle
  button pastes the primary (`internal/input/bindings_test.go::TestCtrlVAndShiftInsertPasteTheClipboard`,
  `cmd/logis/pointer_e2e_test.go::TestE2ETheMiddleButtonPastesThePrimarySelection`, `internal/input/pointer_test.go::TestTheMiddleButtonPastesThePrimarySelectionOnPress`).

### Sessions

Each caller holds one session on its own connection to the daemon, and the
answer goes to that caller alone
(`internal/daemon/core_test.go::TestTheResultGoesOnlyToTheSessionThatWasAnswered`).
At most one session is visible:

- **dmenu callers queue.** A dmenu session that arrives while another session
  (of any kind) is visible waits, and the queue is shown first in, first out;
  each caller gets its own answer
  (`internal/daemon/serve_test.go::TestDmenuSessionsQueueAndEachCallerGetsItsOwnAnswerInOrder`,
  `cmd/logis/contract_test.go::TestADmenuCallerWaitsInTheQueueForItsTurn`,
  `cmd/logis/dmenu_window_e2e_test.go::TestE2EQueuedCallersEachGetTheirOwnAnswerInTurn`,
  `internal/daemon/sessions_test.go::TestADmenuSessionQueuesBehindAnInteractiveOne`).
  A queued caller waits as long as the sessions ahead of it take; to bound
  that, run it under `timeout(1)`, since leaving cancels (below).
- **Interactive sessions replace.** An app, servitor or provider session takes
  the visible session's place; that caller exits 1. Queued dmenu callers stay
  queued and are shown once the interactive session ends
  (`internal/daemon/core_test.go::TestAnInteractiveSessionReplacesTheVisibleOne`,
  `cmd/logis/contract_test.go::TestAReplacedDmenuCallerExits1WithEmptyStdout`,
  `internal/daemon/sessions_test.go::TestAnInteractiveSessionReplacesTheVisibleOneAndKeepsTheQueue`).
- **Leaving cancels.** A caller that exits or is killed (`timeout(1)`, Ctrl+C)
  cancels its session: a visible one closes and the next queued one is shown,
  a queued one is dropped
  (`internal/daemon/serve_test.go::TestAClientDisconnectCancelsItsSessionAndTheNextOneIsShown`,
  `internal/daemon/sessions_test.go::TestClosingAQueuedSessionDropsItSilently`). A
  provider is then sent `cancel`; one that was never sent a choice is ended,
  one that was is left to finish
  (`cmd/logis/provider_e2e_test.go::TestAProviderThatNeverActedAndLingersAfterCancelIsEnded`,
  `cmd/logis/provider_e2e_test.go::TestADismissedProviderThatActedIsNeverSignalledNorItsAction`).

**Ordering caveat.** Connections are not ordered among themselves: the queue
follows the order in which the daemon *receives* sessions, not the order in
which callers were started, so two callers started a moment apart may be
answered in either order. A session opened just after another caller left may
wait in the queue until the daemon has handled that departure (the protocol
notes in [`internal/protocol`](internal/protocol/protocol.go)). Callers that need an
order should run one after another.

The daemon a caller starts outlives it and serves later callers
(`internal/client/client_test.go::TestAClientStartsTheDaemonWhenNoneAnswersAndLaterClientsReuseIt`,
`cmd/logis/contract_test.go::TestAnAutoStartedDaemonOutlivesItsStarterAndLogsToTheRuntimeDir`, `cmd/logis/daemon_e2e_test.go::TestE2EADetachedDaemonLeavesItsStartersStdioAndLogsToTheRuntimeDir`).

### Providers

`logis --provider PROGRAM [ARG]...` runs a provider that speaks
[the provider protocol](docs/provider-protocol.md) (version 1: its messages,
rules, signals and examples). The daemon runs it as this caller would have,
in its working directory and with its environment
(`cmd/logis/provider_e2e_test.go::TestATwoStageFlowRunsToTheProvidersClose`), and shows
its stages, pending rows and toasts in one window
(`cmd/logis/provider_window_e2e_test.go::TestE2EATwoStageFlowWithAPendingRowEndsInAToast`). For the caller it is an interactive session: exit 0 when the provider
sends `close`, 1 when dismissed or replaced, 2 when the provider cannot start,
breaks the protocol, shows no stage within 5 s or exits early
(`cmd/logis/provider_e2e_test.go::TestAProviderThatCannotStartIsAnError`,
`cmd/logis/provider_e2e_test.go::TestAProviderThatBreaksTheProtocolOrDepartsFailsItsSession`,
`cmd/logis/provider_e2e_test.go::TestAProviderThatShowsNothingInTimeFailsItsSession`).

### servitor mode

`logis --servitor` ([servitor mode](#servitor-mode)) is an interactive session
like a provider's, with the same exit statuses, and promises:

- A rite whose auspex is still awaited is never chosen nor invoked, however
  long it takes; leaving ends the census
  (`cmd/logis/servitor_e2e_test.go::TestARiteThatNeverSettlesIsNeverInvokedAndLeavingEndsTheCensus`,
  `internal/servitor/flow_test.go::TestAPendingRiteIsNeverTaken`).
- The current aspect is marked and selected only from a settled reading
  (`internal/servitor/flow_test.go::TestASettledRiteOffersItsAspectsWithTheCurrentMarkedAndSelected`,
  `internal/servitor/flow_test.go::TestAStaleOrUnknownAspectIsNeverMarkedOrSelected`).
- Inscriptions come from the rite's `canon`: the decree prefilled, a mandatory
  one filled before invoking; a cleared decree is passed as `--<key> ""`, an
  empty word without a decree is left out
  (`internal/servitor/flow_test.go::TestInscriptionsAreAskedInWrittenOrderFromTheCanon`,
  `internal/servitor/flow_test.go::TestAMandatoryInscriptionMustBeFilledBeforeInvoking`,
  `internal/servitor/flow_test.go::TestAClearedDecreeIsPassedAndAnEmptyWordWithoutOneIsLeftOut`,
  `cmd/logis/servitor_e2e_test.go::TestAnEdictIsInvokedWithoutAnAspect`).
- `servitor invoke` runs detached: its own session and process group, stdio on
  `/dev/null`, the caller's environment and working directory, unawaited
  (`cmd/logis/servitor_e2e_test.go::TestServitorModeInvokesDetachedOnceSettledAndTheCallerDoesNotWait`,
  `internal/servitor/invoke_test.go::TestInvokeRunsInASessionOfItsOwnWithStdioOnDevNullAndUnawaited`).
- logis shows no outcome
  (`internal/servitor/flow_test.go::TestNothingTheFlowSaysIsAnOutcomeToast`), and
  neither dismissal nor the daemon's end aborts a running invocation
  (`cmd/logis/servitor_e2e_test.go::TestNoDismissalOrDepartureOfLogisAbortsARunningInvocation`).

### When logis cannot serve

Every case exits 2 with an empty stdout and the reason on stderr. The
message is for people: its wording may change, so scripts branch on the exit
status only. It does name the plain fact:

| Case | stderr names | Pinned by |
|---|---|---|
| no Wayland session: `WAYLAND_DISPLAY` unset, or its socket unreachable | `WAYLAND_DISPLAY is not set` / the socket's path | `cmd/logis/dmenu_e2e_test.go::TestE2EADmenuCallerWithoutAUsableSessionExits2NamingWhy`, `cmd/logis/dmenu_e2e_test.go::TestE2EADaemonThatCannotStartIsReportedWithExit2`, `internal/client/client_test.go::TestADaemonThatCannotStartIsReportedWithItsReason`, `cmd/logis/session_test.go::TestWithNonexistentWaylandSocketExits2NamingTheSocket` |
| a compositor without layer shell (GNOME/Mutter) | `zwlr_layer_shell_v1` | `cmd/logis/dmenu_e2e_test.go::TestE2EADmenuCallerWithoutAUsableSessionExits2NamingWhy`, `cmd/logis/session_test.go::TestOnCompositorWithoutLayerShellExits2NamingTheProtocol` |
| a compositor without fractional scaling | `wp_fractional_scale_manager_v1` | `cmd/logis/session_test.go::TestOnCompositorWithoutFractionalScaleExits2NamingTheProtocol` |
| no runtime directory for the daemon's socket | `XDG_RUNTIME_DIR` | `cmd/logis/dmenu_e2e_test.go::TestE2EADmenuCallerWithoutAUsableSessionExits2NamingWhy` |
| an invalid config stops the daemon starting | the file, line and column | `cmd/logis/config_e2e_test.go::TestE2EADaemonRefusesToStartOnAnInvalidConfigAndItsClientFails` |
| the daemon dies during the session (the compositor left) | that the daemon fell silent | `cmd/logis/contract_test.go::TestADmenuCallerWhoseDaemonDiesExits2WithEmptyStdout`, `cmd/logis/dmenu_window_e2e_test.go::TestE2ELosingTheCompositorEndsTheDaemonWithExit2`, `cmd/logis/e2e_test.go::TestE2ELosingTheCompositorExits2` |
| the daemon refuses the caller (another version, left running across an upgrade) | the daemon's reason: restart it | `cmd/logis/contract_test.go::TestACallerTheDaemonRefusesExits2WithTheDaemonsReason`, `internal/client/client_test.go::TestADaemonsRefusalIsTheClientsError`, `internal/daemon/serve_test.go::TestOverTheSocketAGreetingInAnotherVersionIsRefused`, `internal/daemon/core_test.go::TestAGreetingOfAnotherVersionIsRefused` |

**A daemon left running across an upgrade** keeps serving, but once its binary
was replaced since it started (an upgrade renames a new file over it) it asks
to be restarted: every panel it shows carries the notice `logis updated —
…`, naming `systemctl --user restart logis` when systemd passed it the
socket and `restart the daemon: kill PID` otherwise; every caller prints
the same line on stderr, with its exit status and stdout unchanged; the
daemon's log has it once
(`cmd/logis/stale_test.go::TestADaemonWhoseBinaryWasReplacedRemindsEveryCallerAndStillServes`,
`cmd/logis/stale_test.go::TestASocketActivatedDaemonWhoseBinaryWasReplacedNamesItsUnit`,
`cmd/logis/stale_window_e2e_test.go::TestE2EADaemonWhoseBinaryWasReplacedShowsTheReminderAndStillServes`,
`internal/daemon/stale_test.go::TestTheDaemonsBinaryIsReplacedOnlyWhenAnotherFileTakesItsPlace`,
`internal/daemon/stale_test.go::TestTheReminderSaysHowToRestartThisDaemon`).
A daemon of another protocol version, whose messages differ, refuses instead
(the last row above;
`internal/daemon/stale_test.go::TestAClientOfTheVersionBeforeIsRefusedWithARestart`).
It never restarts itself: a session may be open.

A daemon that lost its compositor exits and removes its socket; the next
caller starts a new one
(`cmd/logis/dmenu_window_e2e_test.go::TestE2ELosingTheCompositorEndsTheDaemonWithExit2`, `cmd/logis/e2e_test.go::TestE2ELosingTheCompositorExits2`,
`internal/client/client_test.go::TestAClientStartsTheDaemonWhenNoneAnswersAndLaterClientsReuseIt`).
Plain `logis` is the app launcher and opens a window. To use logis where it can
serve and fall back elsewhere, let the dmenu caller's exit status decide:
2 means logis cannot serve here, for each case above, its reason on stderr
(`cmd/logis/dmenu_e2e_test.go::TestE2EADmenuCallerWithoutAUsableSessionExits2NamingWhy`,
`cmd/logis/session_test.go::TestWithoutWaylandDisplayExits2WithFlavouredMessage`,
`cmd/logis/session_test.go::TestOnCompositorWithoutLayerShellExits2NamingTheProtocol`).
To start a daemon ahead of time without a window, run `logis daemon --detach &`;
it exits 0 at once if one already answers
(`cmd/logis/contract_test.go::TestDaemonDetachExits0AtOnceWhenADaemonAlreadyAnswers`, `cmd/logis/daemon_e2e_test.go::TestE2EDaemonDetachOnlyEnsuresTheDaemon`).

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
  (`internal/input/bindings_test.go::TestReturnAndKeypadEnterAcceptShiftReturnDescends`,
  `cmd/logis/pointer_e2e_test.go::TestE2EHoveringSelectsARowAndAClickOnItAcceptsIt`).
  Dismiss: see [Exit status](#exit-status). Moving and editing, as in
  [Keyboard](#keyboard): `internal/input/bindings_test.go::TestUpDownTabAndCtrlNPMove`,
  `internal/input/bindings_test.go::TestEditingKeysEdit`,
  `cmd/logis/keyboard_e2e_test.go::TestE2EEveryEditingAndSelectionKeyDoesItsWork`.
  The Go build diverges in plain `logis` with servitor on the PATH, its tabbed
  launcher ([App launcher](#app-launcher)): there Tab and Shift+Tab switch
  between the Applications and Rites tabs, keeping the query, and a click on a
  plate does the same
  (`cmd/logis/tabs_e2e_test.go::TestE2ETabSwitchesTabsTheQueryStaysAndTheTabIsRemembered`,
  `cmd/logis/tabs_e2e_test.go::TestE2EAClickOnAPlateSelectsItAndRowsLieBelowTheStrip`,
  `internal/cogitator/tabs_test.go::TestTabAndShiftTabSwitchTabsAndKeepTheQuery`);
  `--apps`, `--servitor` and every other session keep Tab as the next item
  (`cmd/logis/tabs_e2e_test.go::TestE2EAppsIsTheLauncherAloneAndTabSelects`,
  `internal/cogitator/tabs_test.go::TestUntabbedTabStillMovesTheSelection`).
- `logis daemon` checks the session and the config at start and exits 2 if
  either is unusable, or when the compositor goes away (see the table above);
  a second daemon finds the first listening and exits 0
  (`internal/daemon/serve_test.go::TestASecondDaemonDefersToTheOneAlreadyListening`).
  `--idle-timeout SECONDS` ends it once no client has been connected for that
  long, off by default, `0` keeps it resident, and it outranks the config
  (`cmd/logis/daemon_e2e_test.go::TestE2EWithAnIdleTimeoutTheDaemonExitsOnceNoClientIsConnected`,
  `internal/daemon/serve_test.go::TestWithoutAnIdleTimeoutTheDaemonStays`,
  `internal/daemon/config_test.go::TestTheIdleTimeoutFlagOutranksTheConfig`). `--detach`
  (how clients start it) sends its later diagnostics to
  `$XDG_RUNTIME_DIR/logis.log`
  (`cmd/logis/contract_test.go::TestAnAutoStartedDaemonOutlivesItsStarterAndLogsToTheRuntimeDir`, `cmd/logis/daemon_e2e_test.go::TestE2EADetachedDaemonLeavesItsStartersStdioAndLogsToTheRuntimeDir`).
- `WAYLAND_DISPLAY` is the compositor's socket, relative to `XDG_RUNTIME_DIR`
  or absolute, with no fallback to `wayland-0`
  (`internal/window/connect_test.go::TestARelativeDisplayIsJoinedToTheRuntimeDir`,
  `internal/window/connect_test.go::TestAnAbsoluteDisplayIsUsedAsIs`,
  `internal/window/connect_test.go::TestAnUnsetOrEmptyDisplayMeansNoSession`). The daemon's
  socket is `$XDG_RUNTIME_DIR/logis.sock`, which must be absolute
  (`internal/protocol/protocol_test.go::TestTheSocketLivesInAnAbsoluteRuntimeDir`). The
  config is `$XDG_CONFIG_HOME/logis/logis.jsonc`, `~/.config` by default
  (`internal/config/source_test.go::TestTheDefaultLivesInXDGConfigHome`,
  `internal/config/source_test.go::TestWithoutAUsableXDGConfigHomeTheDefaultIsUnderHome`).
- `logis --version` prints `logis` and its version, exit 0
  (`cmd/logis/contract_test.go::TestVersionFlagPrintsNameAndVersionAndSucceeds`, `cmd/logis/main_test.go::TestVersionRunesPrintNameAndVersion`).

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
  "reduce-motion": false,          // true stills the theme's pulse, highlight, flicker, glitch
  // "custom-shader": "shaders/amber.frag", // a pass after the theme's effects (Custom shaders)
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
launcher `terminal`, and the window wears `custom-shader` over the theme's
own (see [Custom shaders](#custom-shaders)).

`reduce-motion` is off by default, as decided for the bundled themes (their glow
pulses and a highlight travels round the border; the flicker is very subtle).
Turned on, it stills every continuous effect of the theme (glow pulse, travelling
highlight, flicker, a pending row's shimmer; the grain stops changing, the
scanlines stop rolling and the colour split stops wobbling), turns the glitch
off and holds a custom shader's `iTime` at 0; the open, close and selection transitions stay (`LOGIS_MOTION=off` stills
those).

## Themes

A theme is the cogitator's whole look (spec §4.9, the model decided on 6217c721):

- a **palette** of named colours with alpha (`"brass": "#d4a017"`, or several:
  `"soot": ["#170e0b", "#0b0807"]`, named `soot.0`, `soot.1`);
- the **elements** that refer to it: `window` (default width and rows), `glow`,
  `border` (gradient of up to four stops, travelling highlight), `panel`,
  `decorations` (corner rivets), `prompt` (label, sigil), `query` (caret),
  `counter`, `rule`, `row` (font, height, colour per tab-separated column,
  `column-gap` between columns and `column-max`, the share of the row any
  column but the last may take),
  `selection`, `match` (colour, weight, underline), `badge`, `tab` (the Go
  build's: the corner radius of plain logis's tab plates, whose other looks
  follow the selection, the panel, the label and the badges), `pending` — and
  `fonts`, the font roles they name;
- **effects and motion**: the CRT pass over the body (`scanlines`, `glow`,
  `aberration`, `mask`, `roundness`, `grain`, `flicker`, each a group of settings
  with a strength where 0 is off; see **Effects** below), the `glitch` before it
  (see **Glitch**), an optional `custom-shader` after them all (see
  [Custom shaders](#custom-shaders)) and the transition timings.

Colours are `#rrggbb`, `#rrggbbaa`, `transparent` or a palette name, each
optionally with `/alpha` (`"crimson/0.55"`). Lengths are logical pixels: a theme
looks the same at scales 1.0, 1.3 and 2.0. The JSON Schema is
[`schema/theme.schema.json`](schema/theme.schema.json); its defaults are the
bundled default's values.

Two themes are bundled (compiled in): **`forge-crimson-brass`** (the default:
[themes/forge-crimson-brass.jsonc](themes/forge-crimson-brass.jsonc), after the
research spike) and **`cogitator-phosphor`**, a green CRT (curved glass, bloom,
inverse selection, underlined matches:
[themes/cogitator-phosphor.jsonc](themes/cogitator-phosphor.jsonc)), both seen on
the owner's niri session at scale 1.3 with blur behind and their glow (another
window at the right edge cropped away)
([forge](docs/evidence/theme-forge-live.png),
[phosphor](docs/evidence/theme-phosphor-live.png); IBM Plex Mono, Cinzel and
VT323 were not installed there, so the fallbacks show; the columns measured,
and phosphor's wide fallback face leaves the last column to its ellipsis).
Choose one
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
  plain `logis --dmenu`) opens in the theme's `window`: 820 logical pixels wide,
  centred, eleven rows for both bundled themes. `--lines` and `--width` (in
  average characters of the row font) size it instead.
- **Height follows the rows:** the panel is exactly as tall as its prompt line,
  rule, row gaps and rows × `row.height` (the bundled themes: 561 logical
  pixels with the glow margins), so no empty band stays below the last row. On
  an output too short for that many rows (its logical height less the glow
  margins), the panel takes the output's height, shows the rows that fit and
  spreads the leftover height evenly between them; the pointer finds rows at
  that pitch. The output's height comes from xdg-output, the output the panel
  lands on from the compositor (a panel that lands on another output is fitted
  anew); without xdg-output the panel asks for all its rows. While a panel is
  open its size stays, however few items the query leaves. **Divergence from
  Rust v1**, whose theme also had a `window.height` (820×520, nine rows, about
  three rows of empty glass below the ninth): Go reads and checks
  `window.height` so v1 themes still load, and ignores it.
- **Live:** the daemon watches `themes/*.jsonc` and the config and applies an
  edited theme at once, a visible window included (its colours, fonts, effects,
  motion timings and, when the session named no size, its size). An invalid
  theme changes nothing: the last sound one stays, and the error (file, line and
  column, or the key: `border.paint[1]: no colour named 'embr' in the palette`)
  goes to the log and a notice over a shown list. A theme that cannot be worn at
  start leaves the bundled default in force. `logis --check-config` checks the
  theme the config names.
- **Effects** (the Go build; a deliberate divergence from Rust v1, whose
  effects were seven strengths): the CRT pass is logis's own clean-room shader
  (`internal/render/crt.frag`), and each effect is a group of settings. A bare
  number is still the group's strength (the aberration's amount), and v1's
  `bloom`, `curvature` and `vignette` set `glow.strength`,
  `roundness.curvature` and `roundness.vignette`, so a v1 theme stays valid
  and, with the other settings at their defaults, looks as it did (the groups'
  other settings come from the theme's base, like any key). The settings,
  lengths in logical pixels, rates in Hz:
  - `scanlines`: `strength`, `period` (1–16, v1 3), `sharpness` (0 a soft
    wave, 0.25 v1's profile, 1 a hard one-pixel line), `roll` (px/s, −60–60,
    down when positive);
  - `glow`: `strength` (v1's bloom), `threshold` (v1 0.35), `reach` (1–32, v1
    7), `bleed` (0 a grey halo, 1 v1's colours, up to 3 saturated),
    `dim-bright` (dim pixels take more glow than bright ones);
  - `aberration`: `amount` (2 px at 1), `edges-only` (none at the centre,
    growing outwards), `wobble`, `wobble-rate` (0.05–20), `twitch` (0 smooth
    breathing, 1 calm with sudden spikes);
  - `mask`: `strength`, `size` (1–16), `kind` (`dots`, `grille` or `slot`);
  - `roundness`: `curvature`, `corners` (extra rounding of the glass, black
    beyond), `vignette`;
  - `grain`: `strength`, `rate` (0–60, v1 24, 0 frozen);
  - `flicker`: `strength`, `frequency` (0.5–30, v1 7.48).

  A value outside its range is refused with its key
  (`effects.scanlines.period: 40 is out of range (1 to 16)`), as
  `logis --check-config` reports it. The bundled themes wear the looks chosen
  in the effects lab.
- **Glitch** (the Go build; new, Rust v1 had none): a pass of its own before
  the CRT pass (logis's clean-room `internal/render/glitch.frag`) breaks the
  glass up like a failing signal, on the same screens. `effects.glitch` is a
  group of parts; a bare number is its `strength` (0 to 1), which scales the
  whole glitch, its calm included: 0 is no pass at all. Most parts have an
  amount at `rest` and one in an `episode`:
  - `episodes`: `period-s` (0–60) and `duty` (0–1): an ambient episode over
    the last `duty` share of every period, counted from the panel's opening,
    so a panel opens calm; a period of 0 leaves episodes to bursts;
  - `wobble` (`rest`, `episode`, `band-px`, `speed-hz`): bands of the picture
    jitter sideways, a few tearing far in an episode; `tracking` (`px`,
    `height-px`, `a-hz`, `b-hz`): two thin kinks sweeping down the glass;
    `fringe` (`px`, `growth`): red and blue split from green;
  - `slices` (`rest`, `episode`, `px`, `shift-px`, `group`, `rate-hz`): torn
    slices; `blocks` (`rest`, `episode`, `size`, `shift-px`, `split-px`,
    `brightness`, `rate-hz`): shifted, split, brightened rectangles; `flashes`
    (`rest`, `episode`, `gain`, `rate-hz`): lit slices;
  - `noise-band` (`rest`, `episode`, `cell-px`, `min`, `max`, `rate-hz`): a
    band of coarse noise; `static` (`rest`, `episode`, `rate-hz`): fine
    noise; `line-pulse` (`rest`, `episode`, `pitch-px`, `drift-px-s`): a dense
    drifting ripple;
  - `bursts`: `open-ms` and `tab-ms` (0–1500) are the bursts on the events
    named in `on` (`["open", "tab"]`: the panel opening, a switch of tab; no
    panel has tabs yet, so today only the opening fires one);
    `random-per-minute` (0–30) random bursts come at random intervals from
    the opening, each `random-ms` (50–1500) long; a burst rises and falls as
    sin(πt)^0.6 and `stutter` (0–1) damps that share of its 33 ms steps to a
    quarter. The glitch takes the larger of a burst and the ambient episode.

  Each part's range is in the schema; a value outside it is refused with its
  key (`effects.glitch.wobble.episode: 81 is out of range (0 to 80)`). The
  bundled themes wear the owner's looks: forge calm at rest, an episode at the
  end of every 9 s, bursts on opening and tab, six random bursts a minute;
  phosphor stronger with shorter episodes. A glitch that moves at rest (the
  bundled looks: tracking, wobble, static, line pulse) keeps the window
  drawing like any continuous effect; one that is calm at rest draws only
  through its episodes and bursts and otherwise sleeps until the next one.
  `reduce-motion` turns the glitch off.
- **Curved glass** (`roundness.curvature`) bends the whole panel as one tube
  face: border, rivets, glass and the glow around it bend together, the
  outline turns convex, and every other effect lies on the bent glass; what the
  bend uncovers at the surface's edge is transparent. Rust v1 bowed only what
  lay inside a straight border and left the unbent picture's alpha in the dark
  it uncovered (a ghost of the text over a light desktop); the Go build does
  neither.
- **Continuous effects** (glow pulse, travelling highlight, flicker, rolling
  scanlines, a wobbling colour split, changing grain, a glitch that moves at
  rest, a custom shader, a pending row's shimmer)
  keep a visible window drawing at the output's refresh rate:
  measured on the owner's machine (Intel Arc, nested niri at 60 Hz, release
  build) a visible forge window draws 60 frames/s with about 70 daemon
  wake-ups/s and 6 % of one CPU core; with `reduce-motion` it draws nothing and
  wakes nothing once settled, as any idle window.

## Custom shaders

The Go build (new beside Rust v1) can run one shader of your own after the
theme's built-in effects, which stay as they are: a theme names it under
`effects.custom-shader` (a path from the theme's file), or the config under
`custom-shader` (a path from the config's directory), which outranks the
theme's. `""` names none, so a theme that extends another can drop its shader.

The shader is GLSL ES 3.00 (OpenGL ES 3, what logis draws with), written the
way Shadertoy writes its image shaders: it defines

```glsl
void mainImage(out vec4 fragColor, in vec2 fragCoord)
```

and logis declares everything else before it:

| Input | What it is |
|---|---|
| `iChannel0` | the frame after the built-in effects (glitch, CRT): premultiplied alpha, read linearly and held at its edges |
| `iResolution` | `vec3(width, height, 1.0)` in buffer pixels |
| `iTime` | seconds since the panel opened (starting again near each hour); frozen at 0 by `reduce-motion` |
| `iFrame` | frames the panel has drawn before this one |
| `iGlitch` | the theme's glitch at this moment, 0 to 1 (episode or burst, times its strength; 0 without a glitch) |

`fragCoord` is the pixel's centre counted from the bottom-left corner, as on
Shadertoy, so `texture(iChannel0, fragCoord / iResolution.xy)` is the pixel
as the built-in effects left it. The frame and `fragColor` are
**premultiplied**: keep `iChannel0`'s alpha (and keep the colour at or below
it), or the panel's transparent margin turns opaque. A minimal shader, which
warms the colours towards the bottom and dims every third row of pixels:

```glsl
// ~/.config/logis/themes/shaders/amber.frag
void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 c = texture(iChannel0, uv);
    vec3 amber = c.rgb * mix(vec3(1.0, 0.82, 0.55), vec3(1.0), uv.y);
    float row = mod(floor(fragCoord.y), 3.0) == 0.0 ? 0.75 : 1.0;
    fragColor = vec4(amber * row, c.a);
}
```

```jsonc
// ~/.config/logis/themes/mine.jsonc
{ "extends": "forge-crimson-brass", "effects": { "custom-shader": "shaders/amber.frag" } }
```

The pass runs over the whole surface, glow included, and a panel with a
custom shader keeps drawing while it is shown (the shader may use its time);
with `reduce-motion` its `iTime` stays 0 and the panel rests between changes.
logis reads the file when it reads the theme or the config, so after editing
the shader save the theme or the config again (or restart the daemon). A
shader that cannot be read, compiled or linked is skipped with one line in the
daemon's log; the built-in effects still draw. `logis --check-config` compiles
it and names the line at fault in your file, not in logis's prelude
(`the custom shader is heretical: …/amber.frag:6:24: error: …`). A
`#version 300 es` line of your own is taken as logis's; any other version is
refused.

**Porting Ghostty or Shadertoy shaders.** Both use the same `mainImage`
shape, so the body usually carries over; what differs:

- Ghostty compiles its shaders as desktop GLSL behind a prelude of its own,
  logis as GLSL ES 3.00 behind its prelude: leave out any `#version` line and
  uniform declarations of the inputs, write float literals with a point (`2.0`,
  not `2`, where a float is meant: ES converts no ints implicitly), use
  `texture`, not `texture2D`, and give up desktop-only built-ins and `double`.
- Only the inputs above exist: replace `iMouse`, `iDate`, `iTimeDelta`,
  `iChannelResolution` (use `iResolution`) and Ghostty's cursor uniforms
  (`iCurrentCursor`, `iPreviousCursor`, …), and drop `iChannel1` and up.
- `iChannel0` is the panel, not a terminal, and it is premultiplied with a
  transparent margin around the glow: a shader that writes `alpha = 1.0` or
  brightens colour past alpha paints that margin.
- Mind the licence of a shader you did not write before you share a theme
  with it; logis ships no third-party shader.

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
| Up, Shift+Tab, Ctrl+P | select the previous item (Shift+Tab in plain logis's tabbed launcher: switch tabs) |
| Down, Tab, Ctrl+N | select the next item (Tab in plain logis's tabbed launcher: switch tabs) |
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

## Installing

logis is not in the AUR (AUR registrations are closed). The repository carries a
PKGBUILD instead, in [`packaging/arch/`](packaging/arch/PKGBUILD); on Arch Linux:

```sh
cd packaging/arch
makepkg -si
```

It builds the commit checked out in the repository (`HEAD`; uncommitted changes
are not built): `./cmd/logis` with cgo, as Arch's Go package guidelines ask
(makepkg's C flags, a PIE linked by the system linker, `-trimpath`). It needs
`go`, `pkgconf` and `git` to build, and `wayland`, `libglvnd`, an
`opengl-driver` (Mesa's, NVIDIA's or another vendor's EGL and OpenGL ES
driver), `fontconfig` and `freetype2` to run. `prepare()`
downloads the `golang.org/x` modules `go.sum` names; `check()` runs `go test
-short ./...` (no compositor needed: the E2E and latency tests skip, and so do
the GL tests where no EGL display serves; `-short` leaves out the wall-clock
timing checks and the GPU pixel tests, the offscreen goldens and pixels read
back, which a busy machine or another GPU driver can fail and which CI and a
plain `go test ./...` still run); then it installs what is listed below.
The build takes about 3 minutes with tests on the owner's machine (under 2 with
a warm Go build cache) and about 170 MB in `packaging/arch/src/`, the module
cache included; `makepkg -sic` removes that afterwards, `makepkg -si --nocheck`
skips the tests. Go's own build cache (`~/.cache/go-build`, about 260 MB more
for logis) stays to speed up the next build; `go clean -cache` empties it.

The package installs:

| What | Where |
| --- | --- |
| the binary | `/usr/bin/logis` |
| the man page | `/usr/share/man/man1/logis.1.gz` (`man logis`) |
| completions | bash `/usr/share/bash-completion/completions/logis`, zsh `/usr/share/zsh/site-functions/_logis`, fish `/usr/share/fish/vendor_completions.d/logis.fish` |
| systemd user units | `/usr/lib/systemd/user/logis.socket` and `logis.service`, **not enabled** (see [below](#starting-the-daemon-with-systemd-optional)) |
| JSON Schemas | `/usr/share/logis/schema/logis.schema.json` and `theme.schema.json` |
| the bundled themes, to copy and edit | `/usr/share/logis/themes/*.jsonc` |
| README, changelog, provider protocol | `/usr/share/doc/logis/` |
| the licence | `/usr/share/licenses/logis/LICENSE` |

Once a version is tagged, the PKGBUILD can build the tagged GitHub tarball
instead of the checkout; its header says which two lines to change.
`cmd/logis/packaging_test.go` keeps the PKGBUILD, its `.SRCINFO` (`makepkg
--printsrcinfo > .SRCINFO` after editing), the man page and the changelog on the
version logis reports (`internal/version`, its single source), the package's
`depends` on the libraries the binary links and an `opengl-driver`, and the
package without `optdepends`.

The completions in [`contrib/completions/`](contrib/completions/) are written by
hand; `cmd/logis/completions_test.go` checks that
each offers exactly the runes the parser accepts (asking bash, zsh and fish where they are installed). Without the
package, source `logis.bash`, put `_logis` on zsh's `fpath`, or copy `logis.fish`
to `~/.config/fish/completions/`.

## Building

logis is written in Go (ADR 0003, ADR 0004). It needs Go 1.27 or later, a C
compiler, `pkg-config`, and the development files of the six C libraries it
links: libwayland-client (1.22 or later), libwayland-egl, libEGL, libGLESv2,
libfontconfig and libfreetype (`wayland`, `libglvnd`, `fontconfig` and
`freetype2` on Arch; `libwayland-dev`, `libegl-dev`, `libgles-dev`,
`libfontconfig-dev` and `libfreetype-dev` on Debian/Ubuntu; Ubuntu 24.04's
libwayland 1.22 is enough). At run time libglvnd needs a vendor's EGL and
OpenGL ES driver (see [GPU backends](#gpu-backends)). From the repository
root:

```sh
go build -o logis ./cmd/logis
go test ./...
```

`gofmt -l .`, `go vet ./...` and `go test -race ./...` are what CI checks.
Tests that hold work to a wall-clock limit skip it under `go test -short`, as
the PKGBUILD's `check()` runs them; hang guards (timeouts for what never
happens) stay. So do the tests whose verdict rests on pixels the GPU drew
(goldens and pixels read back through `rendertest.Headless`): GPUs and drivers
round differently. Tests that use the GPU for sizes, errors or text measures
(`rendertest.HeadlessForStructure`) run under `-short` too. The GL tests draw
on a surfaceless EGL context and skip where none serves, unless
`LOGIS_REQUIRE_EGL` is set, as CI sets it; `LIBGL_ALWAYS_SOFTWARE=1` runs them
on Mesa's llvmpipe, as CI does.

The keyboard's differential suite checks the pure-Go XKB against
libxkbcommon, a test-only oracle that builds only with a tag and is never
linked into logis: `go test -tags xkbref ./internal/xkb/...` (needs
libxkbcommon, xkeyboard-config and libx11's Compose tables).

The Wayland protocol glue under `internal/wlproto` is generated by
`wayland-scanner` and committed; after changing a protocol XML file, run
`go generate ./...` (`wayland-scanner` on `PATH`) and commit the result. The
core protocol's `protocols/wayland.xml` stays libwayland 1.22.0's, whatever
libwayland the build machine has: bindings from a newer one need interfaces
older libwayland lacks (`wl_fixes` since 1.24), and
`internal/wlproto/floor_test.go` fails on them.

`cmd/logis/parity_test.go` holds every rune's exit status, stdout and stderr
to Rust logis v1's, recorded in `cmd/logis/testdata/parity.json` (the frozen
v1 record, from the last Rust commit, `main` 2ffdd02); the deliberate
differences are listed there with their reason. With `LOGIS_RUST_LOGIS`
naming a Rust logis v1 binary built from that commit it asks that binary too,
and `-update-parity` records its answers anew.

Two recorded oracles are standalone Rust programs, kept beside the data they
wrote because the libraries they ran cannot be linked into Go tests:
`internal/fuzzy/testdata/oracle` (nucleo-matcher 0.3.1, the matcher of v1)
and `internal/text/testdata/cosmic-oracle` (cosmic-text 0.19, v1's line
layout). Only regenerating `nucleo.json` or `cosmic.json` needs cargo; the
command is in each program's `Cargo.toml` and beside the Go test that reads
it. Rust logis v1 itself is in the history only (`main` up to 2ffdd02).

### Goldens and pinned fonts

logis asks libfontconfig, in-process, for the theme's faces (each a list of
families in order of preference at a weight, after the user's aliases; without
a theme the default family `sans-serif`) and, per character, for the first
font in fontconfig's fallback order that covers it; only those files are
loaded, never the whole system's fonts. The daemon resolves its fonts
(fontconfig's configuration and caches, the default face and the fallback
order) beside connecting and waking the GPU, so with thousands of installed
faces they stay off the first frame's path.

The pixel tests pin their fonts (`rendertest.PinnedFonts`): an isolated
fontconfig setup that knows only the OFL subsets in `tests/fonts/`
(regenerated by `tests/fonts/subset.sh`), so the goldens do not depend on
installed fonts.

The goldens live in `tests/goldens/`. `tests/goldens/go/<set>/` holds the Go
build's own; `tests/goldens/<set>/` holds the frozen goldens of Rust logis v1
that a Go test still reads: the text corpus and faces and the icons (the Go
renders are bounded against them), forge's window (the scene test's
oracle for the row layout), the debug panel's captures and the golden
harness's reference. The harness, `internal/render/rendertest`
(`Goldens.Check`, `CheckScene`), compares an image with the Go golden where
there is one and with v1's otherwise, each channel within a tolerance (an
allowed fraction of outlier pixels for E2E captures); the icon and theme
goldens store straight alpha, so a viewer shows the translucent glow in its
colour, and are compared premultiplied. The theme goldens draw the same
lists in each bundled theme at scales 1.0, 1.3 and 2.0 with the frame's time
frozen (glow at full strength, the highlight at the top, grain, flicker and
glitch at time 0), with the pinned Noto Sans Mono and Noto Serif standing in
for the themes' own families.

After a deliberate change, rewrite and review the goldens:

```sh
LOGIS_UPDATE_GOLDENS=1 go test ./internal/render/ ./internal/list/scene/ ./internal/icons/
LOGIS_E2E=niri LOGIS_UPDATE_GOLDENS=1 go test -count=1 -run E2E ./cmd/logis/
```

Update mode writes only Go goldens, never one of v1's: an image within
tolerance of v1's golden needs none (a stale Go golden is removed), any other
is written under `tests/goldens/go/` and the test log says how it differs
from v1's. A mismatch leaves `<name>.actual.png` and `<name>.diff.png`
(outliers magenta over the dimmed golden) under `$LOGIS_GOLDEN_FAILURES`
(by default the temp dir's `logis-golden-failures`); a passing check removes
an earlier failure's.

### Test infrastructure

Test-only packages, imported by tests alone and never linked into logis:

- **Goldens** (`internal/render/rendertest`): headless renderers, PNG
  reading and writing, comparison under a `Tolerance`, the golden harness
  above and the pinned fonts.
- **Fake compositor** (`internal/wltest`): just enough of the Wayland wire
  protocol to offer chosen globals, so a test can hand logis a compositor
  without layer shell or fractional scaling, or one that hangs up.
- **Fake session bus** (`internal/dbus/dbustest`): the user manager's
  environment, the transient scope and D-Bus activation for the launch
  tests, recording every call; the real bus is never reached. The D-Bus
  messages logis writes are compared byte for byte with GLib's
  ([`glib-reference.py`](tests/fixtures/dbus/glib-reference.py)).
- **End to end** (`internal/e2e`): `e2e.Start(t, kind)` runs a compositor the
  harness owns, in a scratch `XDG_RUNTIME_DIR`: a nested niri window locally
  (`LOGIS_E2E=niri`, its own generated config, scale 1, never `--session`),
  sway on wlroots' headless backend in CI (`LOGIS_E2E=sway`). `c.Logis(…)`
  is a `logis` command inside it (pinned fonts); `e2e.Spawn(t, cmd)` runs it
  with `LOGIS_TEST_TRACE=1` and follows its trace (`WaitForFocus`,
  `WaitForCommit` …). Clients get a Compose table of the harness's own
  (`XCOMPOSEFILE`, never anyone's `~/.XCompose`) and a scratch
  `XDG_CONFIG_HOME`; both compositors and the injector use the US layout
  with Menu as Compose (`compose:menu`). `e2e.Connect(t, c)` refuses any
  socket whose server is not that compositor process (peer credentials,
  `ConnectVerified`), then offers `VirtualKeyboard()` (`Type`, `Tap`,
  `Hold`/`Release` with real modifier keys, `Toggle` for Caps and Num Lock,
  all requiring the `Focused` proof from `WaitForFocus`), `VirtualPointer()`
  (`MoveTo`, `Button`/`Click`, `Wheel` in notches, `Finger` in touchpad
  pixels; `Calibrate` maps output points onto a client's surface) and
  `Capture()` (wlr-screencopy, upright). `e2e.OwnSelection` owns the test
  compositor's clipboard and primary selection (wlr-data-control, no
  `wl-copy`) through the same guard, and `e2e.StartWatchdog` makes a round
  trip to the compositor every 10 ms to tell a stalled compositor from a slow
  logis. `e2e.StartCogitator` runs a traced `logis daemon` in the compositor
  with `logis --dmenu`, `--provider`, `--servitor` and `--apps` clients on
  piped stdin and stdout. Without `LOGIS_E2E` these tests skip.
- **Latency** (`internal/e2e/latency`): the research/bench `T0_NS` method:
  the harness reads `CLOCK_MONOTONIC` just before it acts (starts a client,
  injects a key) and the client stamps `commit` (and key) events with the
  same clock. Each series is reported against its budget;
  `LOGIS_LATENCY_REPORT=<file>` appends JSON lines, `LOGIS_LATENCY_RUNS`
  sets the runs, and `LOGIS_LATENCY_BUDGETS=enforce` fails a median over
  budget. The series run a real daemon: warm open (from exec to the window's
  first committed frame, ≤ 15 ms; with motion that is the open's first frame,
  and the settled open is a series of its own) and keystroke → commit with 3
  and with 100 000 items, for dmenu, the app launcher and both themes. A warm
  open asks the compositor nothing before it is shown; the test counts the
  daemon's `wl_display.sync` requests in its `WAYLAND_DEBUG` log. Cold start
  (exec to the first window of a fresh daemon) is not measured here.

The suites, in `cmd/logis/` unless named otherwise: the keyboard model and
the debug panel (`keyboard_e2e_test.go`, `internal/e2e`), dmenu
(`dmenu_window_e2e_test.go` in the compositor; `dmenu_e2e_test.go` and
`contract_test.go` over a real daemon with the scripted resolver, no window),
pointer and paste (`pointer_e2e_test.go`), motion and blur
(`motion_e2e_test.go`), providers with the fixture provider
[`forge.sh`](tests/fixtures/providers/forge.sh) (`provider_e2e_test.go`,
`provider_window_e2e_test.go`), the app launcher with fixture desktop entries
([`tests/fixtures/apps/`](tests/fixtures/apps/)) and the fixture program
[`anoint`](tests/fixtures/apps/bin/anoint), which records its arguments,
environment, activation token and session (`apps_e2e_test.go`,
`apps_window_e2e_test.go`, `internal/launch`), the tabbed launcher
(`tabs_e2e_test.go`), themes and their live edits (`theme_window_e2e_test.go`),
the glitch and the custom shader (`glitch_e2e_test.go`, `custom_e2e_test.go`),
the row fit (`rowfit_e2e_test.go`) and servitor mode with a fake `servitor`
([`tests/fixtures/servitor/servitor`](tests/fixtures/servitor/servitor), POSIX
sh) that tells census lines recorded from the real servitor
([`vigil.jsonl`](tests/fixtures/servitor/vigil.jsonl) and fixtures derived
from it) and records how `servitor invoke` was launched
(`servitor_e2e_test.go`, `servitor_window_e2e_test.go`). Test daemons run
without `DBUS_SESSION_BUS_ADDRESS` in a scratch runtime dir, so they find no
session bus, and with `reduce-motion` unless their test's config names it, so
their windows settle and screen captures agree. `cmd/logis/e2emap_test.go`
keeps each of Rust v1's 55 E2E tests mapped to its Go port.

`LOGIS_TEST_TRACE=1` is the test-only observable in the window code: stderr
lines `logis-trace: <monotonic ns> <event>` for `keyboard-enter`/`keyboard-leave`,
`keymap`, `repeat-info <rate> <delay>`, `key <evdev code>
pressed|released|repeated`, `modifiers <depressed> <latched> <locked> <group>`,
`pointer-enter <x> <y>`, `pointer-motion <x> <y>`, `pointer-leave`, `cursor
default|unchanged`, `button <code> pressed|released`, `scroll wheel <120ths>|pixels
<px>|stop`, `paste <selection> <MIME type>|none|unavailable`, `pasted <selection>
<bytes>`, `commit` (a frame of the visible window), `commit closing` (a frame of
one fading out), `settled` (the first frame after which nothing moves),
`frame-done` and `presented`/`discarded` (the compositor's frame callback and
presentation feedback for a frame of the visible window), `faded` (a closed
window is gone, at once without motion), `blur <rectangles>` (a blur region
was set) and `blur-capable true|false` (the compositor's word), `cut` (a
window still closing was dropped after the next window's first frame); the
debug panel adds what each key did: `query <JSON string>`, `select <row>`,
`accept <row>` and `dismiss`. The daemon's window reports its sessions:
`show <session>`, `stage <session>` (a provider's next stage), `items
<total>`, `update <item>`, `remove <item>`, `toast <JSON string>`, `untoast`,
`ready`, `query <JSON string>`, `select <item>` (or `select none`), `accept
<item>`, `descend <item>` (Shift+Return), `typed <JSON string>`, `dismiss`,
`token <session>` (an activation token came), `linger <session>`, `hide
<session>`, `theme <name>` (the window put a theme on) and, in the tabbed
launcher, `tab <name>` (the lit tab) and the rites tab's own feed as `rites
<event>`.

Locally (inside a running Wayland session; a nested niri window opens
briefly):

```sh
LOGIS_E2E=niri go test -count=1 -run E2E ./...
LOGIS_E2E=niri LOGIS_LATENCY_BUDGETS=enforce go test -count=1 -run Latency -v ./cmd/logis/
```

A warm open slower than 100 ms prints `SLOW OPEN <ms>` with the window's
trace between the client's start and its first frame, and whether the test
compositor itself stopped answering meanwhile (the watchdog), so an outlier
names its cause. With `LOGIS_LATENCY_BUDGETS=enforce` such an open fails the
run; keep the nested niri window in view while it runs (a nested niri the
session does not draw gets frame callbacks about once a second).

CI ([`.github/workflows/go.yml`](.github/workflows/go.yml)) runs gofmt, vet
and the tests with the race detector on llvmpipe in one job, the keyboard's
differential suite in another, and the E2E and latency suites in headless
sway in a third, recording latency as a trend (job summary and a
`latency.jsonl` artefact) without enforcing the budgets.

### GPU backends

logis draws with OpenGL ES 3 through EGL, presented with the EGL Wayland
platform; there is no Vulkan path and no CPU renderer. libglvnd dispatches to
the vendor driver the system provides (the package depends on
`opengl-driver`: Mesa, NVIDIA's or another). Without one that answers, the
daemon exits 2 (`no machine spirit answers …`). Software rendering is Mesa's
llvmpipe. Manual check on a live session:

```sh
timeout 3 logis daemon --debug-panel
# llvmpipe (software) instead of the GPU:
LIBGL_ALWAYS_SOFTWARE=1 timeout 3 logis daemon --debug-panel
# one EGL vendor of several (libglvnd), here Mesa's:
__EGL_VENDOR_LIBRARY_FILENAMES=/usr/share/glvnd/egl_vendor.d/50_mesa.json timeout 3 logis daemon --debug-panel
```

logis targets the niri compositor; other compositors with wlr-layer-shell are best
effort. X11 and compositors without layer shell (such as GNOME/Mutter) are not
supported.

### Starting the daemon with systemd (optional)

The first client starts the daemon by itself. To have systemd start it on the
first connection instead, enable the socket. The package installs the units;
without it, copy them from [`contrib/systemd/`](contrib/systemd/) first (they
expect the binary at `/usr/bin/logis`; adjust `ExecStart` otherwise):

```sh
# without the package only:
cp contrib/systemd/logis.socket contrib/systemd/logis.service ~/.config/systemd/user/
systemctl --user daemon-reload
# either way:
systemctl --user enable --now logis.socket
```

The daemon needs `WAYLAND_DISPLAY` in the user manager's environment, which niri
and most session setups import.

## Repository

- [`CONTEXT.md`](CONTEXT.md): domain vocabulary, including the grimdark words used in
  help, error and UI text.
- [`docs/adr/`](docs/adr/): architecture decisions: logis's own GPU renderer, no
  CPU renderer (0001, written for Rust v1 on wgpu), a resident daemon and a thin
  client (0002), the Go rewrite's stack (0003) and the switch-over from Rust to
  Go (0004).
- [`docs/research/`](docs/research/): the stack research (`stack.md`, for Rust
  v1), the launcher landscape (`landscape.md`) and the Go rewrite's research
  (`go-rendering.md`, `go-text-input.md`) that the decisions rest on.
- [`research/`](research/): disposable research code (benchmarks and the
  GPU/daemon spike's evidence and screenshots; the Rust prototypes it measured
  are in the history). It is evidence, not a foundation, and is not part of
  the build.

## Licence

[MIT](LICENSE).
