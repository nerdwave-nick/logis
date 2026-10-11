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
  long (off by default).
- `logis` and `logis --dmenu` are clients. When no daemon answers they start one
  (`logis daemon --detach`, whose later diagnostics go to
  `$XDG_RUNTIME_DIR/logis.log`); if it cannot start, they exit 2 with its reason.
  Plain `logis` only makes sure the daemon runs. `logis --dmenu` streams stdin lines
  to the daemon and prints the chosen line's original bytes (exit 0), or nothing on
  dismissal (exit 1). The daemon has no window yet, so it refuses every session;
  the window, the list view and the dmenu flags are still to come, as are the app
  launcher and servitor mode.

Client and daemon speak length-prefixed JSON frames, one session per connection;
the protocol is described in [`src/protocol/mod.rs`](src/protocol/mod.rs). While a
session is visible, a new dmenu session queues and a new app or servitor session
replaces it.

## Building

Requires Rust 1.91 or later, `pkg-config` and the libwayland-client development
files (`wayland` on Arch, `libwayland-dev` on Debian/Ubuntu).

```sh
cargo build --release
cargo test
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
