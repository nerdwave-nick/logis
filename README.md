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
  fractional scale) until the compositor closes it or the process is ended. It takes
  exclusive keyboard focus and has no input handling yet, so end it from elsewhere
  (for example `timeout 5 logis daemon --debug-panel`).

The session daemon does not show the renderer's window yet, so it refuses every
session. Text, the list view, the dmenu flags, the app launcher and servitor mode
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

Requires Rust 1.91 or later, `pkg-config` and the libwayland-client development
files (`wayland` on Arch, `libwayland-dev` on Debian/Ubuntu).

```sh
cargo build --release
cargo test
```

`cargo test` renders offscreen and needs a GPU adapter (hardware, or Mesa lavapipe
from `vulkan-swrast` / `mesa-vulkan-drivers`); without one, the render tests print
`SKIPPED` unless their backend is listed in `LOGIS_TEST_REQUIRE_BACKENDS`
(`auto,vulkan,gl,software`), as CI does.

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
