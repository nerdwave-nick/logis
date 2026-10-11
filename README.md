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

**Bootstrap. Nothing usable yet.** The repository holds the accepted decisions, the
research behind them, and a binary that only does this:

- `logis --version` prints the version.
- `logis` checks for a Wayland session whose compositor offers wlr-layer-shell. If
  there is none, it exits 2 with a message on stderr; otherwise it exits 0 and shows
  nothing. Any other argument exits 2.

- `logis daemon --debug-panel` shows a static panel drawn by the renderer core (a
  GPU-rendered overlay layer surface at the compositor's fractional scale) until the
  compositor closes it or the process is ended. It takes exclusive keyboard focus
  and has no input handling yet, so end it from elsewhere (for example
  `timeout 5 logis daemon --debug-panel`).

The real daemon, text, dmenu mode, app launcher and servitor mode are still to come.

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
