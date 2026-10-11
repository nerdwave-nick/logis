# logis

A clean-room Wayland launcher and picker that looks like a cogitator of the Machine
God: a GPU-rendered, Adeptus Mechanicus-styled window with animated glow,
scanlines, grain and blur behind it.

logis is meant to be:

- a drop-in `dmenu` for scripts (`logis --dmenu`, fuzzel/dmenu-compatible flags and
  exit codes);
- an application launcher for desktop entries, with icons and frecency;
- a first-class front end for [servitor](https://github.com/nerdwave-nick/servitor):
  choose a rite, an aspect and its inscriptions, and see the verdict;
- instant to open, because a resident daemon keeps the GPU, fonts and app index
  warm while every invocation is a thin client.

## Status

**Early.** `logis --dmenu` works; the app launcher and servitor mode are still to
come. The binary does this much:

- `logis --version` prints the version.
- `logis --dmenu` (or `--dmenu0`) is a drop-in for `fuzzel --dmenu`: see
  [dmenu](#dmenu). Its window takes the [keyboard](#keyboard), the
  [pointer and paste](#pointer-and-paste).
- `logis daemon` is the resident daemon. It checks for a Wayland session whose
  compositor offers wlr-layer-shell (exit 2 with a message on stderr if there is
  none), listens on `$XDG_RUNTIME_DIR/logis.sock`, and wakes its window at start:
  the compositor connection (the only one it opens), the GPU and the fonts. When
  the compositor goes away it exits 2. `--idle-timeout SECONDS` lets it exit once
  no client has been connected for that long (off by default); it outranks the
  config's `idle-timeout`, and `--idle-timeout 0` keeps the daemon resident
  whatever the config says. It reads its [configuration](#configuration) at
  start (an invalid one is an error, exit 2) and again whenever the file changes.
- `logis` and `logis --dmenu` are clients. When no daemon answers they start one
  (`logis daemon --detach`, whose later diagnostics go to
  `$XDG_RUNTIME_DIR/logis.log`); if it cannot start, they exit 2 with its reason.
  Plain `logis` only makes sure the daemon runs.
- `logis --check-config [--config PATH]` validates the configuration without
  touching Wayland or the daemon: exit 0 if it is sound (or absent), 2 with the
  error's file, line and column on stderr.
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
in turn) and a new app or servitor session replaces it.

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
}
```

Unknown keys and values of the wrong kind are errors naming the key, its line and
its column. The daemon watches the file (and its directory, so editors that save by
replacing the file are seen too) and applies a sound edit at once. An invalid edit
changes nothing: the last sound configuration stays in force, and the error goes to
the daemon's log (and, once the window can show it, a themed notice).

`--config PATH` given to a client reaches the daemon only when that client starts
it; a daemon already running keeps the file it was started with. The window
follows `focus-loss` and `blur`; themes, the terminal and the app launcher do not
use their settings yet; the slices that build them will.

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

The durations, start and end poses and easing curves (CSS `cubic-bezier`) are the
parameters of `logis::motion::Motion`, for themes to set later; they are not
config keys yet. `LOGIS_MOTION=off` in the daemon's environment turns motion off,
and `LOGIS_MOTION=4` runs it four times slower (any factor up to 100).

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

logis asks libfontconfig, in-process, for the default family (`sans-serif`, after
the user's aliases) and, per character, for the first font in fontconfig's
fallback order that covers it; only those files are loaded. cosmic-text's full
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
  its own (warm open → open settled).
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

`LOGIS_TEST_TRACE=1` is the test-only observable in the window code: stderr lines
`logis-trace: <monotonic ns> <event>` for `keyboard-enter`/`keyboard-leave`,
`keymap`, `repeat-info <rate> <delay>`, `key <evdev code>
pressed|released|repeated`, `modifiers <depressed> <latched> <locked> <group>`,
`pointer-enter <x> <y>`, `pointer-motion <x> <y>`, `pointer-leave`, `cursor
default|unchanged`, `button <code> pressed|released`, `scroll wheel <120ths>|pixels
<px>|stop`, `paste <selection> <MIME type>|none|unavailable`, `pasted <selection>
<bytes>`, `commit` (a frame of the visible window), `commit closing` (a frame of
one fading out), `settled` (the first frame after which nothing moves), `faded` (a
closed window is gone), `blur <rectangles>` (a blur region was set) and
`blur-capable true|false` (the compositor's word); the debug panel adds what each key did: `query <JSON string>`, `select
<row>`, `accept <row>` and `dismiss`. The daemon's window reports its sessions:
`show <session>`, `items <total>`, `query <JSON string>`, `select <item>` (or
`select none`), `accept <item>`, `typed <JSON string>`, `dismiss` and `hide
<session>`.

Locally (inside a running Wayland session; a nested niri window opens briefly):

```sh
LOGIS_E2E=niri cargo test --test e2e --test dmenu --test pointer --test motion
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
