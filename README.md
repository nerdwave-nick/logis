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

**Early. Nothing usable yet.** The repository holds the accepted decisions, the
research behind them, and a binary that does this much:

- `logis --version` prints the version.
- `logis daemon` is the resident daemon. It checks for a Wayland session whose
  compositor offers wlr-layer-shell (exit 2 with a message on stderr if there is
  none), listens on `$XDG_RUNTIME_DIR/logis.sock`, and keeps the compositor
  connection open: when the compositor goes away it exits 2.
  `--idle-timeout SECONDS` lets it exit once no client has been connected for that
  long (off by default); it outranks the config's `idle-timeout`, and
  `--idle-timeout 0` keeps the daemon resident whatever the config says. It reads
  its [configuration](#configuration) at start (an invalid one is an error, exit 2)
  and again whenever the file changes.
- `logis` and `logis --dmenu` are clients. When no daemon answers they start one
  (`logis daemon --detach`, whose later diagnostics go to
  `$XDG_RUNTIME_DIR/logis.log`); if it cannot start, they exit 2 with its reason.
  Plain `logis` only makes sure the daemon runs. `logis --dmenu` streams stdin lines
  to the daemon and prints the chosen line's original bytes (exit 0), or nothing on
  dismissal (exit 1).
- `logis --check-config [--config PATH]` validates the configuration without
  touching Wayland or the daemon: exit 0 if it is sound (or absent), 2 with the
  error's file, line and column on stderr.
- `logis daemon --debug-panel` does not serve sessions: it shows a static panel drawn
  by the renderer core (a GPU-rendered overlay layer surface at the compositor's
  fractional scale, with text rows in several scripts and colour emoji). It takes
  exclusive keyboard focus: Up and Down move its selection, Escape closes it (as
  does the compositor, or ending the process).

The session daemon does not show the renderer's window yet, so it refuses every
session. The list view, the dmenu flags, the app launcher and servitor mode
are still to come.

Client and daemon speak length-prefixed JSON frames, one session per connection;
the protocol is described in [`src/protocol/mod.rs`](src/protocol/mod.rs). While a
session is visible, a new dmenu session queues and a new app or servitor session
replaces it.

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
}
```

Unknown keys and values of the wrong kind are errors naming the key, its line and
its column. The daemon watches the file (and its directory, so editors that save by
replacing the file are seen too) and applies a sound edit at once. An invalid edit
changes nothing: the last sound configuration stays in force, and the error goes to
the daemon's log (and, once the window can show it, a themed notice).

`--config PATH` given to a client reaches the daemon only when that client starts
it; a daemon already running keeps the file it was started with. Themes, the
window and the app launcher do not use these settings yet; the slices that build
them will.

## Building

Requires Rust 1.91 or later, `pkg-config`, and the libwayland-client and
libfontconfig development files (`wayland` and `fontconfig` on Arch,
`libwayland-dev` and `libfontconfig-dev` on Debian/Ubuntu).

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
system scan (`FontSystem::new()`) is forbidden by `clippy.toml`.

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
  trace (`wait_for_focus()`, `wait_for_commit()` …). `Harness::connect(&compositor)`
  refuses any socket whose server is not that compositor process (peer
  credentials), then offers `virtual_keyboard()`, `type_text`, `tap`, `hold`/
  `release` (real modifier keys plus masks), `toggle` (Caps/Num Lock) — all
  requiring the `Focused` proof from `wait_for_focus()` — and `capture()`
  (wlr-screencopy, upright). Without `LOGIS_E2E` these tests print `SKIPPED`.
- **Latency** ([`tests/latency.rs`](tests/latency.rs),
  [`latency.rs`](tests/support/latency.rs)): the research/bench `T0_NS` method:
  the harness reads `CLOCK_MONOTONIC` just before starting the client, the
  client stamps `commit` (and key) events with the same clock. Each series is
  reported against its spec §4.10 budget; `LOGIS_LATENCY_REPORT=<file>` appends
  JSON lines and `LOGIS_LATENCY_BUDGETS=enforce` fails a median over budget.

`LOGIS_TEST_TRACE=1` is the test-only observable in the window code: stderr lines
`logis-trace: <monotonic ns> <event>` for `keyboard-enter`/`keyboard-leave`,
`key <evdev code> pressed|released`, `modifiers <depressed> <latched> <locked>
<group>` and `commit`.

Locally (inside a running Wayland session; a nested niri window opens briefly):

```sh
LOGIS_E2E=niri cargo test --test e2e
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
