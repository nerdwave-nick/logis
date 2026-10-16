# A resident daemon and a thin client in one binary

Status: accepted (2026-10-11, owner decisions on ddba629b, /clarify Q2 and the Q7
addendum, and the spec follow-up on session policy and systemd units).
Evidence: the one-shot cold-start measurements below (code in
[`research/bench/`](../../research/bench/), method in
[`research/README.md`](../../research/README.md)),
[`docs/research/rendering.md`](../research/rendering.md) §3 and
[`docs/research/landscape.md`](../research/landscape.md).

A GPU path is expensive at cold start (EGL or Vulkan initialisation, shaders,
fonts), and every one-shot launcher measured, fuzzel included, took 22–38 ms to
its first committed frame. Opening from a keybinding must feel instant, and the
look needs the GPU (ADR 0002), so the expensive state has to stay warm. We
decided on a **resident daemon plus a thin client, both in the single `logis`
binary**:

- The **daemon** owns the Wayland connection, the GPU device (created at start, not
  lazily), font and glyph caches, the app index and the providers. It shows and
  hides layer surfaces per session.
- Every user-facing invocation (`--dmenu`, app mode, servitor mode) is a **client**.
  It connects to `$XDG_RUNTIME_DIR/logis.sock`; if none answers, it **starts the
  daemon itself** and waits. An optional systemd user socket unit may start it
  instead; `logis.socket` and `logis.service` ship in the repository and the user
  enables them.
- The daemon **stays resident** by default. An optional `idle-timeout` setting (off
  by default) lets it exit when idle; the next open restarts it.
- Client and daemon speak a **framed Unix stream** with explicit sessions,
  completion and cancellation. A client's disconnect cancels its session. Each
  item's original input bytes are preserved separately from its display text. The
  result goes only to the client that opened the session. (The message format is a
  ticket-level design.)
- **Concurrent sessions:** while a session is visible, a new dmenu session queues
  (each caller gets its answer in order) and a new interactive session (app
  launcher, servitor) replaces the visible one.
- On compositor disconnect the daemon exits non-zero; the next client start (or the
  socket unit) restarts it.

One-shot cold start, launch → first frame committed on a layer surface
(832×351 px, medians; Arch Linux, niri 26.04, Intel Arc, Mesa 26.2.3,
2026-10-11):

| Path | First frame committed | Notes |
|---|---|---|
| C, wl_shm CPU fill | **3.6 ms** | presented 24.6 ms (vblank-quantised) |
| C, raw Vulkan clear | 15.6 ms | instance 4.6 + device 6 ms |
| C, EGL + GLES2 clear | 34.2 ms | `eglInitialize` ≈ 23 ms; outliers to 108 ms |
| Go prototype (servitor research), CPU | 22–28 ms | pure Go, no cgo |
| fuzzel 1.15.0 | 22–38 ms | |
| GTK4 4.22 window | 79 ms | cairo renderer 61 ms |
| Qt Quick (Quickshell 0.3.1, layer-shell QML) | 152 ms | software backend 94 ms |

A resident GPU daemon on EGL + OpenGL ES 3 through libwayland, the route ADR 0002
took, measured a **warm open of 2.6–3.7 ms** median from request to committed
frame and held **55–61 MB RSS** after a visible window; its first open after exec
took 57–317 ms in two rounds at equal load (rendering.md §3.1, §3.4). Resident
designs are also how the fast-feeling launchers in the landscape work: walker,
anyrun, vicinae, hyprlauncher, Quickshell shells (landscape.md §4.1).

## Considered Options

- **One-shot process per invocation** — 22–28 ms with CPU rendering, and an EGL
  context alone costs 34 ms before anything is drawn; too slow for the GPU look.
- **A one-shot render as fallback when no daemon runs** — not adopted: the
  client starts the daemon itself and waits, so there is one renderer and one
  code path.
- **Socket activation only** — not adopted as the only start path: the units are
  shipped but the user enables them, so the client must be able to start the daemon
  without them.
- **Exit after an idle timeout by default** — not adopted: resident by default, the
  timeout is opt-in (Q7 addendum).
- **Fire-and-forget datagrams** — they only measure opening; real sessions need
  completion, cancellation and per-client results.
- **A QML front end inside the owner's running Quickshell** — avoids a cold start
  but couples logis to the shell's lifecycle and is not a standalone program.

## Consequences

- logis has two roles to keep apart in one binary, and a client ↔ daemon protocol
  to design and test (framing, sessions, cancellation, queue-versus-replace).
- Daemon lifecycle work is required: exit and restart on compositor disconnect,
  live reload of config and themes, inotify on desktop entry directories, and
  launching apps into the session environment (systemd user environment) with
  xdg-activation tokens, never the daemon's stale environment.
- Dismissing a picker never kills an action already running (such as a servitor
  invocation), because actions belong to providers, not to the window.
- The performance budgets (spec §4.10) assume this model: warm open ≤ 15 ms, first
  open after the client starts the daemon ≤ 150 ms, idle daemon RSS ≤ 64 MB and no
  wake-ups beyond event waits.
